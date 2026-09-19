// Package integration verifies the shipped C ABI against an isolated real CPA.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"cpa-helper-plugin/internal/plugin"
	"cpa-helper-plugin/internal/policy"
)

func TestCPACompatibility(t *testing.T) {
	binary := os.Getenv("CPA_BINARY")
	if binary == "" {
		t.Skip("CPA_BINARY not set: real CPA dynamic-loading test not run")
	}
	library := os.Getenv("CPA_PLUGIN_BINARY")
	if library == "" {
		ext := "so"
		if runtime.GOOS == "windows" {
			ext = "dll"
		}
		library = filepath.Join("..", "dist", plugin.ID+"."+ext)
	}
	var mu sync.Mutex
	seen := map[string]int{}
	streamStarted := make(chan struct{}, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid", 400)
			return
		}
		mu.Lock()
		seen[r.Header.Get("Authorization")]++
		mu.Unlock()
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"model\":\"upstream-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n")
			w.(http.Flusher).Flush()
			select {
			case streamStarted <- struct{}{}:
			default:
			}
			<-r.Context().Done()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"fixture","object":"chat.completion","model":"upstream-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()
	dir := t.TempDir()
	plugins := filepath.Join(dir, "plugins")
	if err := os.Mkdir(plugins, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(library)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(plugins, filepath.Base(library)), raw, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	configPath := filepath.Join(dir, "config.yaml")
	config := fmt.Sprintf(`host: "127.0.0.1"
port: %d
auth-dir: %q
api-keys: ["fixture-downstream", "fixture-unconfigured"]
remote-management:
  secret-key: "fixture-management"
  allow-remote: false
  disable-control-panel: true
  disable-auto-update-panel: true
request-retry: 0
max-retry-interval: 0
plugins:
  enabled: true
  dir: %q
  configs:
    cpa-helper-plugin:
      enabled: true
      state_dir: %q
      store:
        id: cpa-helper-plugin
openai-compatibility:
  - name: fixture
    base-url: %q
    api-key-entries:
      - api-key: upstream-a
        weight: 3
      - api-key: upstream-b
        weight: 1
      - api-key: upstream-forbidden
        weight: 1
    models:
      - name: upstream-model
        alias: route-model
      - name: blocked-model
        alias: blocked-model
`, port, filepath.ToSlash(filepath.Join(dir, "auth")), filepath.ToSlash(plugins), filepath.ToSlash(filepath.Join(dir, "state")), upstream.URL+"/v1")
	if err = os.WriteFile(configPath, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	call := func(method, path, key string, body any, headers map[string]string) (int, []byte) {
		t.Helper()
		var data []byte
		if body != nil {
			data, _ = json.Marshal(body)
		}
		req, err := http.NewRequest(method, base+path, bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if key != "" {
			req.Header.Set("Authorization", "Bearer "+key)
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := client.Do(req)
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
	var process *exec.Cmd
	var done chan error
	var logFile *os.File
	stop := func() {
		if process == nil {
			return
		}
		select {
		case <-done:
		default:
			_ = process.Process.Kill()
			<-done
		}
		_ = logFile.Close()
		process = nil
	}
	t.Cleanup(stop)
	start := func() {
		t.Helper()
		logFile, err = os.Create(filepath.Join(dir, "cpa.log"))
		if err != nil {
			t.Fatal(err)
		}
		process = exec.Command(binary, "--config", configPath, "--local-model")
		process.Dir = dir
		process.Stdout = logFile
		process.Stderr = logFile
		// Explicit environment avoids inheriting storage credentials or deployment overrides.
		process.Env = []string{}
		for _, name := range []string{"PATH", "SystemRoot", "WINDIR", "TEMP", "TMP", "HOME"} {
			if value := os.Getenv(name); value != "" {
				process.Env = append(process.Env, name+"="+value)
			}
		}
		if err = process.Start(); err != nil {
			t.Fatal(err)
		}
		done = make(chan error, 1)
		go func(cmd *exec.Cmd, ch chan error) { ch <- cmd.Wait() }(process, done)
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			req, _ := http.NewRequest("GET", base+plugin.BasePath+"/health", nil)
			req.Header.Set("Authorization", "Bearer fixture-management")
			resp, requestErr := client.Do(req)
			if requestErr == nil {
				body, _ := io.ReadAll(resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == 200 && bytes.Contains(body, []byte(`"status":"ok"`)) {
					return
				}
			}
			time.Sleep(150 * time.Millisecond)
		}
		log, _ := os.ReadFile(filepath.Join(dir, "cpa.log"))
		t.Fatalf("CPA did not load plugin: %s", log)
	}
	start()
	for _, path := range []string{"/health", "/policy", "/directory", "/capabilities"} {
		status, body := call("GET", plugin.BasePath+path, "fixture-management", nil, nil)
		if status != 200 {
			t.Fatalf("%s: %d %s", path, status, body)
		}
	}
	status, _ := call("GET", plugin.BasePath+"/policy", "", nil, nil)
	if status != 401 {
		t.Fatalf("unauthenticated policy status %d", status)
	}
	status, body := call("GET", plugin.ResourcePath+"/ui", "", nil, nil)
	if status != 200 || !bytes.Contains(body, []byte("API Key")) {
		t.Fatalf("UI: %d %s", status, body)
	}
	for _, asset := range []string{"app.js", "style.css", "lucide.js", "sha256.js", "logo.svg"} {
		status, body = call("GET", plugin.ResourcePath+"/"+asset, "", nil, nil)
		if status != 200 || len(body) == 0 {
			t.Fatalf("asset %s: %d", asset, status)
		}
	}
	status, body = call("GET", plugin.BasePath+"/policy", "fixture-management", nil, nil)
	var p policy.Snapshot
	if err = json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	fingerprint := func(key string) string {
		sum := sha256.Sum256([]byte("openai-compatibility:fixture\x00" + key + "\x00" + upstream.URL + "/v1\x00"))
		id := "openai-compatibility:fixture:" + hex.EncodeToString(sum[:])[:12]
		return policy.CredentialRef(id)
	}
	p.Revision = 2
	p.Keys = []policy.Key{{ID: policy.CallerScope("fixture-downstream"), Enabled: true, GroupIDs: []string{}, MaxConcurrency: 1, Rule: policy.Rule{Models: []string{"route-model"}, CredentialIDs: []string{fingerprint("upstream-a"), fingerprint("upstream-b")}}}}
	put := func(p policy.Snapshot) {
		t.Helper()
		status, body := call("PUT", plugin.BasePath+"/policy", "fixture-management", p, map[string]string{"If-Match": fmt.Sprint(p.Revision - 1), "Idempotency-Key": fmt.Sprintf("policy-%d", p.Revision)})
		if status != 200 {
			t.Fatalf("policy: %d %s", status, body)
		}
	}
	put(p)
	chat := func(model, key string) (int, []byte) {
		return call("POST", "/v1/chat/completions", key, map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "fixture"}}}, nil)
	}
	status, body = chat("blocked-model", "fixture-downstream")
	if status != 403 {
		t.Fatalf("model denial: %d %s", status, body)
	}
	for i := 0; i < 8; i++ {
		status, body = chat("route-model", "fixture-downstream")
		if status != 200 {
			t.Fatalf("routed chat: %d %s", status, body)
		}
		waitIdle(t, call)
	}
	mu.Lock()
	aCount, bCount, forbidden := seen["Bearer upstream-a"], seen["Bearer upstream-b"], seen["Bearer upstream-forbidden"]
	mu.Unlock()
	if aCount != 6 || bCount != 2 || forbidden != 0 {
		t.Fatalf("weighted subset: %d/%d/%d", aCount, bCount, forbidden)
	}
	streamBody := []byte(`{"model":"route-model","stream":true,"messages":[{"role":"user","content":"fixture"}]}`)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", base+"/v1/chat/completions", bytes.NewReader(streamBody))
	req.Header.Set("Authorization", "Bearer fixture-downstream")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("stream: %d %s", resp.StatusCode, b)
	}
	if _, err = bufio.NewReader(resp.Body).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	select {
	case <-streamStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("stream not started")
	}
	status, body = chat("route-model", "fixture-downstream")
	if status != 429 {
		t.Fatalf("concurrency: %d %s", status, body)
	}
	cancel()
	_ = resp.Body.Close()
	waitIdle(t, call)
	p.Revision = 3
	p.Keys[0].Rule.CredentialIDs = []string{policy.CredentialRef("nonexistent")}
	put(p)
	status, body = chat("route-model", "fixture-downstream")
	if status != 503 {
		t.Fatalf("no routed credential: %d %s", status, body)
	}
	waitIdle(t, call)
	status, body = chat("blocked-model", "fixture-unconfigured")
	if status != 200 {
		t.Fatalf("unknown key: %d %s", status, body)
	}
	waitIdle(t, call)
	stop()
	start()
	status, body = chat("route-model", "fixture-downstream")
	if status != 503 {
		t.Fatalf("restart lost policy: %d %s", status, body)
	}
	status, body = call("POST", plugin.BasePath+"/policy/rollback", "fixture-management", map[string]int{"policy_revision": 2}, map[string]string{"If-Match": "3", "Idempotency-Key": "rollback"})
	if status != 200 {
		t.Fatalf("rollback: %d %s", status, body)
	}
	status, body = chat("route-model", "fixture-downstream")
	if status != 200 {
		t.Fatalf("rollback routing: %d %s", status, body)
	}
	stop()
	stored, err := os.ReadFile(filepath.Join(dir, "state", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-downstream", "fixture-management", "upstream-a", "upstream-b"} {
		if strings.Contains(string(stored), secret) {
			t.Fatal("state leaked credential")
		}
	}
	t.Log("Configured CPA binary: dynamic load, management auth/resources, alias policy, subset weights, streaming cancellation, concurrency, fail-closed routing, restart and rollback passed")
}

func waitIdle(t *testing.T, call func(string, string, string, any, map[string]string) (int, []byte)) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, body := call("GET", plugin.BasePath+"/health", "fixture-management", nil, nil)
		var h struct {
			Active map[string]int `json:"active"`
		}
		if status == 200 && json.Unmarshal(body, &h) == nil && len(h.Active) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("completion did not release concurrency")
}
