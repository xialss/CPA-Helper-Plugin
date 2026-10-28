package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cpa-helper-plugin/internal/policy"
	"cpa-helper-plugin/internal/snapshot"
)

// TestClaudeTTLFeasibility observes the unmodified official CPA binary. Unlike
// production acceptance tests, unsupported candidates are recorded as findings.
func TestClaudeTTLFeasibility(t *testing.T) {
	binary, library := os.Getenv("CPA_BINARY"), os.Getenv("CPA_TTL_PROBE_BINARY")
	if binary == "" || library == "" {
		t.Skip("CPA_BINARY and CPA_TTL_PROBE_BINARY required for diagnostic-only TTL proof")
	}
	for _, rules := range []string{"none", "late-default", "late-override", "cli-none", "cli-late-default", "cli-late-override"} {
		t.Run(rules, func(t *testing.T) {
			fixture := startTTLFixture(t, binary, library, rules)
			cases := []struct {
				name string
				body string
			}{
				{"auto", `"messages":[{"role":"user","content":"synthetic fixture"}]`},
				{"bare", `"messages":[{"role":"user","content":[{"type":"text","text":"synthetic fixture","cache_control":{"type":"ephemeral"}}]}]`},
				{"explicit-5m", `"system":[{"type":"text","text":"synthetic system","cache_control":{"type":"ephemeral","ttl":"5m"}}],"messages":[{"role":"user","content":"synthetic fixture"}]`},
				{"explicit-1h", `"system":[{"type":"text","text":"synthetic system","cache_control":{"type":"ephemeral","ttl":"1h"}}],"messages":[{"role":"user","content":"synthetic fixture"}]`},
				{"explicit-mode-empty", `"prompt_cache_options":{"mode":"explicit"},"messages":[{"role":"user","content":"synthetic fixture"}]`},
				{"explicit-mode-bare", `"prompt_cache_options":{"mode":"explicit"},"messages":[{"role":"user","content":[{"type":"text","text":"synthetic fixture","cache_control":{"type":"ephemeral"}}]}]`},
				{"mixed-order", `"tools":[{"name":"fixture_tool","description":"synthetic tool","input_schema":{"type":"object","properties":{}},"cache_control":{"type":"ephemeral","ttl":"5m"}}],"system":[{"type":"text","text":"synthetic system","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"text","text":"synthetic fixture","cache_control":{"type":"ephemeral","ttl":"1h"}}]}]`},
			}
			for _, tc := range cases {
				for _, ttl := range []string{"5m", "1h"} {
					for _, action := range []string{"marker", "rewrite"} {
						body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,` + tc.body + `}`)
						status, response := fixture.call(t, "/v1/messages", ttl, action, body)
						if status != 200 {
							t.Fatalf("%s/%s/%s: status=%d response=%s", tc.name, ttl, action, status, response)
						}
						got := fixture.take(t)
						t.Logf("WIRE case=%s caller=%s action=%s ttl=%v marker=%q", tc.name, ttl, action, got.controls, got.marker)
						// Pin the decisive observations rather than reporting every
						// candidate as a supported product feature.
						if !strings.HasPrefix(rules, "cli-") && tc.name == "mixed-order" {
							expected := "5m,bare-5m,bare-5m"
							if rules != "none" && ttl == "5m" {
								expected = "bare-5m,bare-5m,bare-5m"
							}
							if rules == "late-default" && ttl == "1h" && action == "marker" {
								expected = "5m,1h,bare-5m"
							}
							if rules == "late-override" && ttl == "1h" {
								expected = "1h,1h,1h"
							}
							if actual := strings.Join(got.controls, ","); actual != expected {
								t.Errorf("mixed TTL order: got %q, expected observed %q", actual, expected)
							}
						}
						if !strings.HasPrefix(rules, "cli-") && tc.name != "mixed-order" {
							expected := "bare-5m"
							switch tc.name {
							case "explicit-mode-empty":
								expected = ""
							case "explicit-5m":
								expected = "5m"
							case "explicit-1h":
								expected = "1h"
							}
							if ttl == "1h" && expected == "bare-5m" && (rules != "none" || (action == "rewrite" && tc.name != "auto")) {
								expected = "1h"
							}
							if rules != "none" && ttl == "5m" && expected != "" {
								expected = "bare-5m"
							}
							if rules == "late-override" && ttl == "1h" && expected != "" {
								expected = "1h"
							}
							if actual := strings.Join(got.controls, ","); actual != expected {
								t.Errorf("wire changed for %s/%s/%s: got %q, expected observed %q", tc.name, ttl, action, actual, expected)
							}
						}
						if strings.HasPrefix(rules, "cli-") && tc.name == "auto" {
							expected := "1h,1h"
							if rules != "cli-none" && ttl == "5m" {
								expected = "bare-5m,bare-5m"
							}
							if actual := strings.Join(got.controls, ","); actual != expected {
								t.Errorf("CLI auto wire: got %q, expected observed %q", actual, expected)
							}
						}
					}
				}
			}
			fixture.verifyNonClaude(t)
			// A header supplied by the client must not choose another caller's TTL.
			if status, out := fixture.call(t, "/v1/messages", "5m", "spoof", []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"synthetic fixture"}]}`)); status != 200 {
				t.Fatalf("spoof request: %d %s", status, out)
			}
			spoof := fixture.take(t)
			t.Logf("SPOOF final=%+v", spoof)
			if strings.Contains(rules, "late-") {
				for _, ttl := range spoof.controls {
					if ttl != "bare-5m" {
						t.Error("client-supplied marker overrode authenticated 5m caller")
					}
				}
			}
			fixture.waitUsage(t, len(cases)*4+5)
			fixture.events(t, true)
			fixture.queue(t) // Discard the preceding synthetic setup requests.
			// Same body, concurrent callers: no body digest is unique enough here.
			for _, protocol := range []string{"claude", "openai"} {
				for _, stream := range []bool{false, true} {
					path := "/v1/messages"
					if protocol == "openai" {
						path = "/v1/chat/completions"
					}
					body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":16,"stream":%t,"messages":[{"role":"user","content":"identical synthetic fixture"}]}`, stream))
					var wg sync.WaitGroup
					for _, ttl := range []string{"5m", "1h"} {
						wg.Add(1)
						go func(ttl string) {
							defer wg.Done()
							status, out := fixture.call(t, path, ttl, "marker", body)
							if status != 200 {
								t.Errorf("%s stream=%t caller=%s: %d %s", protocol, stream, ttl, status, out)
							}
						}(ttl)
					}
					wg.Wait()
					fixture.take(t)
					fixture.take(t)
					events := fixture.waitUsage(t, 2)
					summary := summarizeTTLEvents(events)
					t.Logf("HOOKS protocol=%s stream=%t %s", protocol, stream, summary)
					verifyTTLHookIdentity(t, events)
					verifyTTLUsageEvidence(t, events, protocol, stream)
					fixture.verifyQueue(t, events)
					fixture.events(t, true)
				}
			}
		})
	}
}

type ttlWire struct {
	controls    []string
	marker      string
	rootControl json.RawMessage
}

type ttlFixture struct {
	base        string
	client      *http.Client
	wire        chan ttlWire
	openAI      chan []byte
	configPath  string
	holdRelease chan struct{}
	holdOnce    sync.Once
	retry       atomic.Int32
}

func startTTLFixture(t *testing.T, binary, library, rules string) *ttlFixture {
	t.Helper()
	return startTTLFixtureWithUsage(t, binary, library, rules, false)
}

func startTTLFixtureWithUsage(t *testing.T, binary, library, rules string, uniformUsage bool) *ttlFixture {
	return startTTLFixtureMode(t, binary, library, rules, uniformUsage, false)
}

func startTTLFixtureMode(t *testing.T, binary, library, rules string, uniformUsage, production bool) *ttlFixture {
	t.Helper()
	cli := strings.HasPrefix(rules, "cli-")
	rules = strings.TrimPrefix(rules, "cli-")
	f := &ttlFixture{client: &http.Client{Timeout: 10 * time.Second}, wire: make(chan ttlWire, 128), openAI: make(chan []byte, 4), holdRelease: make(chan struct{})}
	openAI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "invalid fixture body", 400)
			return
		}
		f.openAI <- body
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"fixture-openai","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`)
	}))
	t.Cleanup(openAI.Close)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid fixture body", 400)
			return
		}
		var controls []string
		var visit func(any)
		visit = func(node any) {
			switch v := node.(type) {
			case map[string]any:
				if cc, ok := v["cache_control"].(map[string]any); ok && cc["type"] == "ephemeral" {
					ttl, _ := cc["ttl"].(string)
					if ttl == "" {
						ttl = "bare-5m"
					}
					controls = append(controls, ttl)
				}
				for _, k := range []string{"tools", "system", "messages", "content"} {
					visit(v[k])
				}
			case []any:
				for _, child := range v {
					visit(child)
				}
			}
		}
		visit(body)
		var rootControl json.RawMessage
		if control, exists := body["cache_control"]; exists {
			var err error
			rootControl, err = json.Marshal(control)
			if err != nil {
				http.Error(w, "invalid root fixture control", http.StatusBadRequest)
				return
			}
		}
		f.wire <- ttlWire{controls: controls, marker: r.Header.Get("X-CPA-TTL-Probe"), rootControl: rootControl}
		if production && strings.Contains(fmt.Sprint(body["messages"]), "retry-hold-ttl") && f.retry.CompareAndSwap(0, 1) {
			<-f.holdRelease
			http.Error(w, "synthetic retry", http.StatusServiceUnavailable)
			return
		}
		usage := `{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":120,"cache_read_input_tokens":3,"cache_creation":{"ephemeral_5m_input_tokens":80,"ephemeral_1h_input_tokens":40}}`
		if uniformUsage {
			var err error
			usage, err = syntheticUniformUsage(controls)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		}
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_fixture\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-5\",\"content\":[],\"usage\":%s}}\n\n", usage)
			if production && strings.Contains(fmt.Sprint(body["messages"]), "cancel-ttl") {
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				return
			}
			fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
			fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
			fmt.Fprint(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
			fmt.Fprintf(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":%s}\n\n", usage)
			fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"msg_fixture","type":"message","role":"assistant","model":"claude-sonnet-4-5","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","stop_sequence":null,"usage":%s}`, usage)
	}))
	t.Cleanup(upstream.Close)
	t.Cleanup(func() { f.holdOnce.Do(func() { close(f.holdRelease) }) })
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	if err := os.Mkdir(plugins, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(library)
	if err != nil {
		t.Fatal(err)
	}
	pluginID := "ttl-probe"
	if production {
		pluginID = "cpa-helper-plugin"
	}
	if err = os.WriteFile(filepath.Join(plugins, pluginID+filepath.Ext(library)), raw, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	f.base = fmt.Sprintf("http://127.0.0.1:%d", port)
	config := fmt.Sprintf(`host: "127.0.0.1"
port: %d
auth-dir: %q
api-keys: ["fixture-ttl-5m", "fixture-ttl-1h"]
remote-management:
  secret-key: "fixture-management"
  disable-control-panel: true
  disable-auto-update-panel: true
request-retry: 0
max-retry-interval: 0
usage-statistics-enabled: true
plugins:
  enabled: true
  dir: %q
  configs:
    ttl-probe:
      enabled: true
claude-api-key:
  - api-key: synthetic-upstream
    base-url: %q
    models:
      - name: claude-sonnet-4-5
        alias: claude-sonnet-4-5
    cloak:
      mode: never
`, port, filepath.ToSlash(filepath.Join(dir, "auth")), filepath.ToSlash(plugins), upstream.URL)
	if cli {
		config = strings.Replace(config, "mode: never", "mode: always\n    fingerprint-profile: claude-code-cli", 1)
	}
	config += fmt.Sprintf(`openai-compatibility:
  - name: fixture-openai
    base-url: %q
    api-key-entries:
      - api-key: synthetic-openai
    models:
      - name: fixture-openai
        alias: fixture-openai
`, openAI.URL+"/v1")
	if rules != "none" {
		kind := "default"
		if rules == "late-override" {
			kind = "override"
		}
		config += "payload:\n  " + kind + ":\n    - models:\n        - name: '*'\n          protocol: claude\n          headers:\n            X-CPA-TTL-Probe: 1h\n      params:\n"
		paths := []string{`tools.#(cache_control.type=="ephemeral")#.cache_control.ttl`, `system.#(cache_control.type=="ephemeral")#.cache_control.ttl`, `messages.#(content)#.content.#(cache_control.type=="ephemeral")#.cache_control.ttl`}
		for _, path := range paths {
			config += fmt.Sprintf("        '%s': 1h\n", path)
		}
		if production {
			config += "    - models:\n        - name: '*'\n          protocol: claude\n          headers:\n            X-CPA-TTL-Probe: 1h\n          match:\n            - cache_control.type: ephemeral\n      params:\n        cache_control.ttl: 1h\n"
		}
		config += "  filter:\n    - models:\n        - name: '*'\n          protocol: claude\n          headers:\n            X-CPA-TTL-Probe: 5m\n      params:\n"
		for _, path := range paths {
			config += fmt.Sprintf("        - '%s'\n", path)
		}
		if production {
			config += "    - models:\n        - name: '*'\n          protocol: claude\n          headers:\n            X-CPA-TTL-Probe: 5m\n          match:\n            - cache_control.type: ephemeral\n      params:\n        - cache_control.ttl\n"
		}
	}
	configPath := filepath.Join(dir, "config.yaml")
	f.configPath = configPath
	endpoint := "/v0/management/plugins/ttl-probe/events"
	if production {
		stateDir := filepath.Join(dir, "state")
		config = strings.ReplaceAll(config, "ttl-probe:", "cpa-helper-plugin:")
		config = strings.Replace(config, "request-retry: 0", "request-retry: 1", 1)
		config = strings.Replace(config, "openai-compatibility:", fmt.Sprintf("  - api-key: synthetic-upstream-2\n    base-url: %q\n    models:\n      - name: claude-sonnet-4-5\n        alias: claude-sonnet-4-5\n    cloak:\n      mode: never\nopenai-compatibility:", upstream.URL), 1)
		if cli {
			config = strings.ReplaceAll(config, "mode: never", "mode: always\n    fingerprint-profile: claude-code-cli")
		}
		config = strings.ReplaceAll(config, "X-CPA-TTL-Probe", "X-CPA-Helper-Claude-TTL")
		config = strings.Replace(config, "      enabled: true", fmt.Sprintf("      enabled: true\n      state_dir: %q\n      claude_cache_ttl:\n        enabled: true\n        host_config_path: %q\n        delegate_builtin: round-robin", filepath.ToSlash(stateDir), filepath.ToSlash(configPath)), 1)
		config += "routing:\n  strategy: round-robin\n"
		store := snapshot.Open(stateDir)
		p, err := store.Current()
		if err != nil {
			t.Fatal(err)
		}
		base := p.Revision
		p.Revision++
		for _, ttl := range []string{"5m", "1h"} {
			p.Keys = append(p.Keys, policy.Key{ID: policy.CallerScope("fixture-ttl-" + ttl), Enabled: true, GroupIDs: []string{}, ClaudeCacheTTL: ttl})
		}
		if _, err := store.Apply(p, base, "fixture-ttl"); err != nil {
			t.Fatal(err)
		}
		endpoint = "/v0/management/plugins/cpa-helper-plugin/v1/health"
	}
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(dir, "cpa.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "--config", configPath, "--local-model")
	cmd.Dir, cmd.Stdout, cmd.Stderr = dir, logFile, logFile
	cmd.Env = []string{}
	for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP", "HOME"} {
		if value := os.Getenv(name); value != "" {
			cmd.Env = append(cmd.Env, name+"="+value)
		}
	}
	if err = cmd.Start(); err != nil {
		_ = logFile.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		select {
		case <-done:
		default:
			_ = cmd.Process.Kill()
			<-done
		}
		_ = logFile.Close()
		if t.Failed() {
			log, _ := os.ReadFile(logPath)
			t.Logf("isolated CPA log: %s", log)
		}
	})
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequest("GET", f.base+endpoint, nil)
		req.Header.Set("Authorization", "Bearer fixture-management")
		resp, requestErr := f.client.Do(req)
		if requestErr == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 200 {
				anonymous, anonymousErr := f.client.Get(f.base + endpoint)
				if anonymousErr != nil {
					t.Fatal(anonymousErr)
				}
				_ = anonymous.Body.Close()
				if anonymous.StatusCode != 401 {
					t.Fatalf("diagnostic management route accessible anonymously: %d", anonymous.StatusCode)
				}
				return f
			}
		}
		select {
		case exitErr := <-done:
			done <- exitErr
			t.Fatalf("CPA exited before probe registration: %v", exitErr)
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("diagnostic plugin did not register before deadline")
	return nil
}

func (f *ttlFixture) call(t *testing.T, path, ttl, action string, body []byte) (int, []byte) {
	t.Helper()
	req, err := http.NewRequest("POST", f.base+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer fixture-ttl-"+ttl)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TTL-Probe-Action", action)
	if action == "spoof" {
		req.Header.Set("X-CPA-TTL-Probe", "1h")
		req.Header.Set("X-CPA-Helper-Claude-TTL", "1h")
		req.Header.Set("X-CPA-Helper-TTL-Request", "forged")
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out
}

func (f *ttlFixture) verifyNonClaude(t *testing.T) {
	t.Helper()
	body := []byte(`{"model":"fixture-openai","messages":[{"role":"user","content":"synthetic fixture"}],"max_tokens":16}`)
	for _, ttl := range []string{"5m", "1h"} {
		var baseline []byte
		for _, action := range []string{"marker", "rewrite"} {
			status, response := f.call(t, "/v1/chat/completions", ttl, action, body)
			if status != 200 {
				t.Fatalf("non-Claude %s: %d %s", action, status, response)
			}
			select {
			case wire := <-f.openAI:
				if action == "marker" {
					baseline = wire
				} else if !bytes.Equal(baseline, wire) {
					t.Error("non-Claude wire body differs between marker-only and rewriting probe")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no non-Claude upstream observation")
			}
		}
	}
	t.Log("NON-CLAUDE both callers: upstream request bodies byte-identical with rewriting enabled")
}

func (f *ttlFixture) take(t *testing.T) ttlWire {
	t.Helper()
	select {
	case got := <-f.wire:
		return got
	case <-time.After(5 * time.Second):
		t.Fatal("no upstream wire observation")
		return ttlWire{}
	}
}

func (f *ttlFixture) events(t *testing.T, clear bool) []map[string]any {
	t.Helper()
	method := "GET"
	if clear {
		method = "DELETE"
	}
	req, _ := http.NewRequest(method, f.base+"/v0/management/plugins/ttl-probe/events", nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Events []map[string]any `json:"events"`
	}
	if resp.StatusCode != 200 {
		t.Fatalf("events status: %d", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	serialized, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-ttl-5m", "fixture-ttl-1h", "synthetic-upstream", "synthetic fixture", "identical synthetic fixture"} {
		if bytes.Contains(serialized, []byte(secret)) {
			t.Fatal("diagnostic management response exposed a key or request body")
		}
	}
	return out.Events
}

func (f *ttlFixture) waitUsage(t *testing.T, count int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events := f.events(t, false)
		n := 0
		for _, e := range events {
			if e["hook"] == "usage.handle" {
				n++
			}
		}
		if n >= count {
			return events
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("expected %d usage observations", count)
	return nil
}

func (f *ttlFixture) queue(t *testing.T) []map[string]any {
	t.Helper()
	req, _ := http.NewRequest("GET", f.base+"/v0/management/usage-queue?count=128", nil)
	req.Header.Set("Authorization", "Bearer fixture-management")
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var rows []map[string]any
	if resp.StatusCode != 200 {
		t.Fatalf("queue status: %d", resp.StatusCode)
	}
	if err = json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func (f *ttlFixture) verifyQueue(t *testing.T, events []map[string]any) {
	t.Helper()
	rows := f.queue(t)
	if len(rows) != 2 {
		t.Fatalf("expected two isolated queue records, got %d", len(rows))
	}
	usageIDs := map[string]bool{}
	usageTraces := map[string]bool{}
	for _, event := range events {
		if event["hook"] == "usage.handle" {
			usageIDs[fmt.Sprint(event["request_id"])] = true
			usageTraces[fmt.Sprint(event["trace_id"])] = true
		}
	}
	for _, row := range rows {
		tokens, ok := row["tokens"].(map[string]any)
		if !ok || tokens["cache_creation_tokens"] != float64(120) {
			t.Fatalf("unexpected aggregate tokens: %v", tokens)
		}
		for _, field := range []string{"cache_creation_ttl", "cache_creation_5m_tokens", "cache_creation_1h_tokens"} {
			if _, present := tokens[field]; present {
				t.Errorf("stock CPA unexpectedly emits %s", field)
			}
		}
		if !usageIDs[fmt.Sprint(row["execution_id"])] || !usageTraces[fmt.Sprint(row["trace_id"])] {
			t.Errorf("queue execution_id/trace_id does not match usage.handle")
		}
	}
	t.Log("QUEUE 2 records: cache_creation_tokens=120, no TTL/buckets; execution_id/trace_id match usage.handle")
}

func summarizeTTLEvents(events []map[string]any) string {
	counts, buckets, ids := map[string]int{}, map[string]int{}, map[string]int{}
	var usage []any
	requestIDs, usageIDs := map[string]bool{}, map[string]bool{}
	requestTraces, usageTraces := map[string]bool{}, map[string]bool{}
	for _, e := range events {
		hook, _ := e["hook"].(string)
		counts[hook]++
		if e["request_id"] != "" {
			ids[hook]++
		}
		raw, _ := json.Marshal(e["evidence"])
		if bytes.Contains(raw, []byte("ephemeral_5m_input_tokens")) {
			buckets[hook]++
		}
		if hook == "usage.handle" {
			usage = append(usage, e["detail"])
			usageIDs[fmt.Sprint(e["request_id"])] = true
			usageTraces[fmt.Sprint(e["trace_id"])] = true
		}
		if hook == "request.intercept_after" {
			requestIDs[fmt.Sprint(e["request_id"])] = true
			requestTraces[fmt.Sprint(e["trace_id"])] = true
		}
	}
	idMatches, traceMatches := 0, 0
	for id := range usageIDs {
		if requestIDs[id] {
			idMatches++
		}
	}
	for id := range usageTraces {
		if id != "" && requestTraces[id] {
			traceMatches++
		}
	}
	raw, _ := json.Marshal(map[string]any{"counts": counts, "with_ids": ids, "with_buckets": buckets, "usage": usage, "request_usage_id_matches": idMatches, "request_usage_trace_matches": traceMatches})
	return string(raw)
}

func verifyTTLHookIdentity(t *testing.T, events []map[string]any) {
	t.Helper()
	ids := map[string]string{}
	scopes := map[string]bool{}
	for _, e := range events {
		if e["hook"] == "request.intercept_after" {
			id, _ := e["request_id"].(string)
			scope, _ := e["caller_scope"].(string)
			if id == "" || (scope != policy.CallerScope("fixture-ttl-5m") && scope != policy.CallerScope("fixture-ttl-1h")) {
				t.Errorf("missing authenticated execution identity: %v", e)
			}
			ids[id] = scope
			scopes[scope] = true
		}
	}
	if len(ids) != 2 || len(scopes) != 2 {
		t.Errorf("expected two distinct executions and callers, got %d IDs and %d scopes", len(ids), len(scopes))
	}
	for _, e := range events {
		hook, _ := e["hook"].(string)
		if strings.HasPrefix(hook, "response.intercept") {
			id, _ := e["request_id"].(string)
			if _, ok := ids[id]; !ok {
				t.Errorf("unmatched execution identity: %v", e)
			}
			if scope, _ := e["caller_scope"].(string); scope != ids[id] {
				t.Errorf("response caller scope changed for execution %s", id)
			}
		}
	}
}

func verifyTTLUsageEvidence(t *testing.T, events []map[string]any, protocol string, stream bool) {
	t.Helper()
	buckets := map[string]int{}
	usageCount := 0
	for _, event := range events {
		hook, _ := event["hook"].(string)
		if hook == "response.normalize_before" && event["wire_has_identity"] != false {
			t.Error("before-translator identity contract changed; reassess the correlation finding")
		}
		if hook == "usage.handle" {
			usageCount++
			detail, ok := event["detail"].(map[string]any)
			if !ok || detail["CacheCreationTokens"] != float64(120) || event["wire_has_buckets"] != false {
				t.Errorf("unexpected usage observation: %v", event)
			}
		}
		evidence, _ := event["evidence"].([]any)
		for _, item := range evidence {
			counter, ok := item.(map[string]any)
			if !ok {
				t.Fatal("invalid diagnostic usage evidence")
			}
			if _, present := counter["ephemeral_5m_input_tokens"]; present {
				if counter["ephemeral_5m_input_tokens"] != float64(80) || counter["ephemeral_1h_input_tokens"] != float64(40) || counter["cache_creation_input_tokens"] != float64(120) {
					t.Error("upstream TTL buckets changed during observation")
				}
				buckets[hook]++
			}
		}
	}
	if usageCount != 2 {
		t.Errorf("expected two usage observations, got %d", usageCount)
	}
	responseHook, expected := "response.intercept_after", 2
	if stream {
		responseHook, expected = "response.intercept_stream_chunk", 4
	}
	if protocol == "claude" {
		if buckets[responseHook] != expected {
			t.Errorf("native response lost TTL evidence: %v", buckets)
		}
	} else if buckets[responseHook] != 0 || buckets["response.normalize_before"] < expected {
		t.Errorf("translated response evidence contract changed: %v", buckets)
	}
}
