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
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	modelcatalog "cpa-helper-plugin/internal/catalog"
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
	responseModel := "upstream-model"
	finiteStream := false
	streamEnding := "done"
	streamStarted := make(chan struct{}, 1)
	var streamRelease <-chan struct{}
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
		actual, finite, ending, release := responseModel, finiteStream, streamEnding, streamRelease
		mu.Unlock()
		if body.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: {\"id\":\"fixture\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n", actual)
			w.(http.Flusher).Flush()
			if finite {
				fmt.Fprint(w, "data: {\"model\":\"route-model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"later-secret\"}}]}\n\n")
				if ending != "truncated" {
					fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"terminal-secret\"},\"finish_reason\":\"stop\"}]}\n\n")
				}
				if ending == "done" {
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
				return
			}
			select {
			case streamStarted <- struct{}{}:
			default:
			}
			select {
			case <-release:
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
			case <-r.Context().Done():
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"id":"fixture","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, actual)
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
	// CPA derives the plugin ID from the filename, including for overridden builds.
	if err = os.WriteFile(filepath.Join(plugins, plugin.ID+filepath.Ext(library)), raw, 0600); err != nil {
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
logging-to-file: true
plugins:
  enabled: true
  dir: %q
  configs:
    cpa-helper-plugin:
      enabled: true
      state_dir: %q
      response_model_mismatch:
        enabled: true
        unknown_action: reject
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
	for _, path := range []string{"/health", "/policy", "/directory", "/capabilities", "/model-catalog", "/credential-models"} {
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
	for _, asset := range []string{"app.js", "directory.js", "style.css", "lucide.js", "sha256.js", "logo.svg"} {
		status, body = call("GET", plugin.ResourcePath+"/"+asset, "", nil, nil)
		if status != 200 || len(body) == 0 {
			t.Fatalf("asset %s: %d", asset, status)
		}
	}
	type hostPlugin struct {
		ID               string `json:"id"`
		Registered       bool   `json:"registered"`
		EffectiveEnabled bool   `json:"effective_enabled"`
		ConfigFields     []struct {
			Name    string          `json:"name"`
			Type    string          `json:"type"`
			Default json.RawMessage `json:"default"`
		} `json:"config_fields"`
	}
	getPlugin := func() hostPlugin {
		t.Helper()
		code, data := call("GET", "/v8/management/plugins", "fixture-management", nil, nil)
		var list struct {
			Plugins []hostPlugin `json:"plugins"`
		}
		if code != 200 || json.Unmarshal(data, &list) != nil {
			t.Fatalf("host plugin list: %d %s", code, data)
		}
		for _, item := range list.Plugins {
			if item.ID == plugin.ID {
				return item
			}
		}
		t.Fatalf("host plugin list missing %s: %s", plugin.ID, data)
		return hostPlugin{}
	}
	info := getPlugin()
	if !info.Registered || !info.EffectiveEnabled || len(info.ConfigFields) != 3 {
		t.Fatalf("host plugin registration: %+v", info)
	}
	status, body = call("GET", plugin.BasePath+"/capabilities", "fixture-management", nil, nil)
	var defaults struct {
		Filter bool   `json:"model_list_filter_enabled"`
		ABI    uint32 `json:"abi_version"`
		Schema uint32 `json:"schema_version"`
		CPA    string `json:"cpa_version"`
		Plugin string `json:"plugin_version"`
	}
	if status != 200 || json.Unmarshal(body, &defaults) != nil {
		t.Fatalf("default capabilities: %d %s", status, body)
	}
	if defaults.ABI != 1 || defaults.Schema != 7 || defaults.CPA != "v8.0.45" || defaults.Plugin != policy.Version {
		t.Fatalf("target capabilities do not match the pinned contract: %s", body)
	}
	if bytes.Contains(body, []byte("response_model_mismatch")) {
		t.Fatalf("removed response verification is advertised: %s", body)
	}
	status, body = call("POST", plugin.BasePath+"/response-model/validate", "fixture-management", map[string]any{"enabled": true}, nil)
	if status != 404 {
		t.Fatalf("removed verification route: %d %s", status, body)
	}
	// The host's serialized defaults must agree with the plugin's omitted settings.
	wantDefaults, err := json.Marshal(map[string]any{
		"state_dir":                 map[string]any{"type": "string", "default": filepath.Join("plugins", "cpa-helper-state")},
		"model_list_filter_enabled": map[string]any{"type": "boolean", "default": defaults.Filter},
		"claude_cache_ttl":          map[string]any{"type": "object", "default": map[string]any{"enabled": false, "host_config_path": "", "delegate_builtin": ""}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var expectedDefaults map[string]any
	if err = json.Unmarshal(wantDefaults, &expectedDefaults); err != nil {
		t.Fatal(err)
	}
	actualDefaults := map[string]any{}
	for _, field := range info.ConfigFields {
		var value any
		if err = json.Unmarshal(field.Default, &value); err != nil {
			t.Fatalf("host config default %s: %v", field.Name, err)
		}
		actualDefaults[field.Name] = map[string]any{"type": field.Type, "default": value}
	}
	if !reflect.DeepEqual(actualDefaults, expectedDefaults) {
		t.Fatalf("host config defaults: got %+v, want %+v", actualDefaults, expectedDefaults)
	}
	status, body = call("GET", plugin.BasePath+"/policy", "fixture-management", nil, nil)
	var p policy.Snapshot
	if err = json.Unmarshal(body, &p); err != nil {
		t.Fatal(err)
	}
	status, body = call("GET", "/v8/management/credentials", "fixture-management", nil, nil)
	var directory struct {
		Files []struct {
			ID    string `json:"id"`
			Index string `json:"auth_index"`
		} `json:"files"`
	}
	if status != 200 || json.Unmarshal(body, &directory) != nil {
		t.Fatalf("v8 credential directory: %d %s", status, body)
	}
	status, body = call("GET", "/v8/management/config/api-keys/openai-compatibility", "fixture-management", nil, nil)
	var groups []struct {
		Keys []struct {
			Key   string `json:"api-key"`
			Index string `json:"auth_index"`
		} `json:"keys"`
	}
	if status != 200 || json.Unmarshal(body, &groups) != nil {
		t.Fatalf("v8 configured credentials: %d %s", status, body)
	}
	refs := map[string]string{}
	authIDs := map[string]string{}
	for _, group := range groups {
		for _, key := range group.Keys {
			if key.Index == "" {
				t.Fatal("configured credential has no auth_index")
			}
			// v8.0.12 does not expose config credentials through /credentials.
			// Pin the scheduler-ID bridge with actual credential subset routing.
			sum := sha256.Sum256([]byte("openai-compatibility:fixture\x00" + key.Key + "\x00" + upstream.URL + "/v1\x00"))
			authIDs[key.Key] = "openai-compatibility:fixture:" + hex.EncodeToString(sum[:])[:12]
			refs[key.Key] = policy.CredentialRef(authIDs[key.Key])
		}
	}
	if refs["upstream-a"] == "" || refs["upstream-b"] == "" {
		t.Fatal("fixture credential indexes missing")
	}
	p.Revision = 2
	p.Keys = []policy.Key{{ID: policy.CallerScope("fixture-downstream"), Enabled: true, GroupIDs: []string{}, MaxConcurrency: 1, Rule: policy.Rule{Models: []string{"route-model"}, CredentialIDs: []string{refs["upstream-a"], refs["upstream-b"]}}}}
	put := func(p policy.Snapshot) {
		t.Helper()
		status, body := call("PUT", plugin.BasePath+"/policy", "fixture-management", p, map[string]string{"If-Match": fmt.Sprint(p.Revision - 1), "Idempotency-Key": fmt.Sprintf("policy-%d", p.Revision)})
		if status != 200 {
			t.Fatalf("policy: %d %s", status, body)
		}
	}
	put(p)
	status, body = call("GET", "/v1/models", "fixture-downstream", nil, nil)
	if status != 200 || !bytes.Contains(body, []byte("model_catalog_unavailable")) || bytes.Contains(body, []byte(`"data"`)) {
		t.Fatalf("missing credential catalog did not fail explicitly: %d %s", status, body)
	}
	modelSnapshot := modelcatalog.Snapshot{ContractVersion: "v1", PluginVersion: policy.Version, CPAVersion: plugin.CPAVersion, Revision: 1, GeneratedAt: time.Now().UTC(), Credentials: []modelcatalog.Credential{}}
	status, body = call("GET", plugin.BasePath+"/credential-models", "fixture-management", nil, nil)
	var hostModels struct {
		Credentials []struct {
			Ref      string   `json:"ref"`
			Index    string   `json:"auth_index"`
			Provider string   `json:"provider"`
			Models   []string `json:"models"`
		} `json:"credentials"`
	}
	if status != 200 || json.Unmarshal(body, &hostModels) != nil || len(hostModels.Credentials) != 3 || bytes.Contains(body, []byte("openai-compatibility:fixture:")) {
		t.Fatalf("host.auth.models contract: %d %s", status, body)
	}
	for _, key := range []string{"upstream-a", "upstream-b", "upstream-forbidden"} {
		credential := modelcatalog.Credential{Ref: refs[key], Source: "ai-providers", Provider: "openai-compatible-fixture", Enabled: true, Models: []string{}}
		for _, entry := range hostModels.Credentials {
			if entry.Ref == credential.Ref {
				credential.Models = append(credential.Models, entry.Models...)
			}
		}
		if len(credential.Models) != 2 || !slices.Contains(credential.Models, "route-model") || slices.Contains(credential.Models, "upstream-model") {
			t.Fatalf("catalog lost public alias: %s", body)
		}
		modelSnapshot.Credentials = append(modelSnapshot.Credentials, credential)
	}
	rawCatalog, err := json.Marshal(modelSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, from, to string }{
		{"envelope-case", `"credentials":`, `"Credentials":`},
		{"duplicate-enabled", `"enabled":true`, `"enabled":false,"enabled":true`},
	} {
		invalid := json.RawMessage(strings.Replace(string(rawCatalog), tc.from, tc.to, 1))
		status, body = call("PUT", plugin.BasePath+"/model-catalog", "fixture-management", invalid, map[string]string{"If-Match": "0", "Idempotency-Key": "catalog-invalid-" + tc.name})
		if status != 400 || !bytes.Contains(body, []byte(`"invalid_model_catalog"`)) {
			t.Fatalf("ambiguous model catalog %s: %d %s", tc.name, status, body)
		}
	}
	status, body = call("PUT", plugin.BasePath+"/model-catalog", "fixture-management", modelSnapshot, map[string]string{"If-Match": "0", "Idempotency-Key": "catalog-1"})
	if status != 200 {
		t.Fatalf("publish registered model catalog: %d %s", status, body)
	}
	status, body = call("GET", "/v1/models", "fixture-downstream", nil, nil)
	var catalog struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if status != 200 || json.Unmarshal(body, &catalog) != nil || len(catalog.Data) != 1 || catalog.Data[0].ID != "route-model" {
		t.Fatalf("filtered model catalog: %d %s", status, body)
	}
	for _, tc := range []struct {
		model, key string
		status     int
	}{
		{"route-model", "fixture-downstream", 200},
		{"blocked-model", "fixture-downstream", 404},
		{"blocked-model", "fixture-unconfigured", 200},
	} {
		status, body = call("GET", "/v1/models/"+tc.model, tc.key, nil, nil)
		if status != tc.status {
			t.Fatalf("model detail %s: %d %s", tc.model, status, body)
		}
	}
	for _, header := range []string{"X-Api-Key", "X-Goog-Api-Key"} {
		status, body = call("GET", "/v1/models", "", nil, map[string]string{header: "fixture-downstream"})
		if status != 200 || json.Unmarshal(body, &catalog) != nil || len(catalog.Data) != 1 || catalog.Data[0].ID != "route-model" {
			t.Fatalf("%s model catalog: %d %s", header, status, body)
		}
	}
	status, body = call("GET", "/v1/models", "fixture-unconfigured", nil, nil)
	if status != 200 || json.Unmarshal(body, &catalog) != nil || len(catalog.Data) != 2 {
		t.Fatalf("unconfigured model catalog: %d %s", status, body)
	}
	for _, tc := range []struct {
		path, key string
		headers   map[string]string
	}{
		{"/v1/models?key=fixture-downstream", "", nil},
		{"/v1/models?auth_token=fixture-downstream", "", nil},
		{"/v1/models", "fixture-downstream", map[string]string{"X-Api-Key": "fixture-unconfigured"}},
	} {
		status, body = call("GET", tc.path, tc.key, nil, tc.headers)
		if status != 200 || json.Unmarshal(body, &catalog) != nil || len(catalog.Data) != 2 || !bytes.Contains(body, []byte("blocked-model")) {
			t.Fatalf("ambiguous model identity: %d %s", status, body)
		}
	}
	for _, key := range []string{"", "invalid-fixture-key"} {
		status, body = call("GET", "/v1/models", key, nil, nil)
		if status != 401 || bytes.Contains(body, []byte("blocked-model")) {
			t.Fatalf("unauthenticated catalog: %d %s", status, body)
		}
	}
	for _, tc := range []struct {
		path, key string
		headers   map[string]string
	}{
		{"/v1/chat/completions?key=fixture-downstream", "", nil},
		{"/v1/chat/completions", "fixture-downstream", map[string]string{"X-Api-Key": "fixture-unconfigured"}},
	} {
		status, body = call("POST", tc.path, tc.key, map[string]any{"model": "blocked-model", "messages": []map[string]string{{"role": "user", "content": "fixture"}}}, tc.headers)
		if status != 403 || !bytes.Contains(body, []byte("model_forbidden")) {
			t.Fatalf("listing fallback weakened admission: %d %s", status, body)
		}
	}
	chat := func(model, key string) (int, []byte) {
		return call("POST", "/v1/chat/completions", key, map[string]any{"model": model, "messages": []map[string]string{{"role": "user", "content": "fixture"}}}, nil)
	}
	// Publish a disabled policy temporarily, then restore it at a new revision.
	p.Revision++
	p.Keys[0].Enabled = false
	put(p)
	status, body = chat("route-model", "fixture-downstream")
	if status != 401 || !bytes.Contains(body, []byte("api_key_disabled")) || !bytes.Contains(body, []byte("fixtur...ream")) || !bytes.Contains(body, []byte("已被禁用")) || bytes.Contains(body, []byte("fixture-downstream")) {
		t.Fatalf("disabled key response: %d %s", status, body)
	}
	p.Revision++
	p.Keys[0].Enabled = true
	put(p)
	for _, enabled := range []bool{false, true} {
		status, body = call("PATCH", "/v8/management/config/plugins/configs/"+plugin.ID, "fixture-management", map[string]bool{"model_list_filter_enabled": enabled}, nil)
		if status != 200 {
			t.Fatalf("toggle config: %d %s", status, body)
		}
		// CPA persists configuration first; its file watcher applies it asynchronously.
		deadline := time.Now().Add(10 * time.Second)
		for {
			status, body = call("GET", plugin.BasePath+"/capabilities", "fixture-management", nil, nil)
			var caps struct {
				Enabled bool   `json:"model_list_filter_enabled"`
				Version string `json:"plugin_version"`
				CPA     string `json:"cpa_version"`
			}
			if status != 200 || json.Unmarshal(body, &caps) != nil {
				t.Fatalf("toggle capabilities: %d %s", status, body)
			}
			if caps.Enabled == enabled && caps.Version == policy.Version && caps.CPA == plugin.CPAVersion {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("CPA did not apply toggle: %s", body)
			}
			time.Sleep(100 * time.Millisecond)
		}
		status, body = call("GET", "/v1/models", "fixture-downstream", nil, nil)
		want := 2
		if enabled {
			want = 1
		}
		if status != 200 || json.Unmarshal(body, &catalog) != nil || len(catalog.Data) != want {
			t.Fatalf("toggle catalog: %d %s", status, body)
		}
		status, body = chat("blocked-model", "fixture-downstream")
		if status != 403 {
			t.Fatalf("toggle weakened admission: %d %s", status, body)
		}
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
	// Responses text must arrive while the upstream is still open, including
	// Codex-originated requests. The removed verifier used to buffer until EOF.
	for _, originator := range []string{"", "codex_cli_rs"} {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, "POST", base+"/v1/responses", strings.NewReader(`{"model":"route-model","stream":true,"input":"incremental passthrough"}`))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer fixture-downstream")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Originator", originator)
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			t.Fatal("Responses waited for upstream completion", err)
		}
		reader := bufio.NewReader(resp.Body)
		var received strings.Builder
		for !strings.Contains(received.String(), "hello") {
			line, err := reader.ReadString('\n')
			received.WriteString(line)
			if err != nil {
				cancel()
				resp.Body.Close()
				t.Fatalf("Responses text was buffered: %v; %s", err, received.String())
			}
		}
		cancel()
		resp.Body.Close()
		select {
		case <-streamStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("Responses upstream not started")
		}
		waitIdle(t, call)
	}
	// Legacy enabled/reject configuration must not restore response verification.
	mu.Lock()
	responseModel = "watered-model"
	finiteStream = true
	mu.Unlock()
	for _, stream := range []bool{false, true} {
		status, body = call("POST", "/v1/chat/completions", "fixture-downstream", map[string]any{"model": "route-model", "stream": stream, "messages": []map[string]string{{"role": "user", "content": "passthrough"}}}, nil)
		want := "ok"
		if stream {
			want = "terminal-secret"
		}
		if status != 200 || bytes.Contains(body, []byte(`"error":{`)) || !bytes.Contains(body, []byte(want)) || !bytes.Contains(body, []byte("watered-model")) {
			t.Fatalf("response passthrough stream=%v: %d %s", stream, status, body)
		}
		waitIdle(t, call)
	}
	mu.Lock()
	streamEnding = "truncated"
	mu.Unlock()
	_, body = call("POST", "/v1/chat/completions", "fixture-downstream", map[string]any{"model": "route-model", "stream": true, "messages": []map[string]string{{"role": "user", "content": "truncated"}}}, nil)
	if !bytes.Contains(body, []byte("hello")) || !bytes.Contains(body, []byte("upstream stream closed")) || bytes.Contains(body, []byte("finish_reason\":\"stop")) {
		t.Fatalf("real truncation hidden: %s", body)
	}
	waitIdle(t, call)
	mu.Lock()
	streamEnding = "done"
	responseModel = "watered-model"
	mu.Unlock()
	status, body = call("POST", "/v1/responses", "fixture-downstream", map[string]any{"model": "route-model", "input": "passthrough responses"}, nil)
	if status != 200 || bytes.Contains(body, []byte(`"error":{`)) || !bytes.Contains(body, []byte("ok")) {
		t.Fatalf("translated response passthrough: %d %s", status, body)
	}
	waitIdle(t, call)
	for _, tc := range []struct {
		path string
		body map[string]any
	}{
		{"/v1/responses", map[string]any{"model": "route-model", "stream": true, "input": "passthrough responses"}},
		{"/v1/messages", map[string]any{"model": "route-model", "max_tokens": 10, "stream": true, "messages": []map[string]string{{"role": "user", "content": "passthrough claude"}}}},
		{"/v1beta/models/route-model:streamGenerateContent?alt=sse", map[string]any{"contents": []map[string]any{{"parts": []map[string]string{{"text": "passthrough gemini"}}}}}},
	} {
		status, body = call("POST", tc.path, "fixture-downstream", tc.body, nil)
		if status != 200 || bytes.Contains(body, []byte(`"error":{`)) || !bytes.Contains(body, []byte("hello")) || !bytes.Contains(body, []byte("later-secret")) {
			t.Fatalf("converted stream passthrough %s: %d %s", tc.path, status, body)
		}
		waitIdle(t, call)
	}
	for _, ending := range []string{"eof", "truncated"} {
		mu.Lock()
		streamEnding = ending
		mu.Unlock()
		status, body = call("POST", "/v1/responses", "fixture-downstream", map[string]any{"model": "route-model", "stream": true, "input": "EOF contract"}, nil)
		// CPA may commit stream headers before discovering a truncated upstream.
		if status != 200 && !(ending == "truncated" && status == 502) {
			t.Fatalf("Responses %s: %d %s", ending, status, body)
		}
		if ending == "eof" {
			if !bytes.Contains(body, []byte("response.completed")) || !bytes.Contains(body, []byte("hello")) || bytes.Contains(body, []byte(`"error":{`)) {
				t.Fatalf("clean EOF not completed: %s", body)
			}
		} else if bytes.Contains(body, []byte("response.completed")) || !bytes.Contains(body, []byte("error")) {
			t.Fatalf("truncated Responses reported success: %s", body)
		}
		waitIdle(t, call)
	}
	mu.Lock()
	streamEnding = "done"
	responseModel = ""
	mu.Unlock()
	status, body = chat("route-model", "fixture-downstream")
	if status != 200 || bytes.Contains(body, []byte(`"error":{`)) || !bytes.Contains(body, []byte("ok")) {
		t.Fatalf("missing response model blocked: %d %s", status, body)
	}
	waitIdle(t, call)
	mu.Lock()
	responseModel = "upstream-model"
	finiteStream = false
	mu.Unlock()
	p.Revision++
	p.Keys[0].Rule.CredentialIDs = []string{policy.CredentialRef("nonexistent")}
	put(p)
	status, body = call("GET", "/v1/models", "fixture-downstream", nil, nil)
	if status != 200 || !bytes.Contains(body, []byte(`"data":[]`)) || bytes.Contains(body, []byte(`"error"`)) {
		t.Fatalf("all credential routes denied still advertised models: %d %s", status, body)
	}
	status, body = call("GET", "/v1/models/route-model", "fixture-downstream", nil, nil)
	if status != 404 {
		t.Fatalf("credential-hidden model detail: %d %s", status, body)
	}
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
	status, body = call("GET", "/v1/models", "fixture-downstream", nil, nil)
	if status != 200 || !bytes.Contains(body, []byte(`"data":[]`)) {
		t.Fatalf("restart lost credential-aware catalog restriction: %d %s", status, body)
	}
	status, body = chat("route-model", "fixture-downstream")
	if status != 503 {
		t.Fatalf("restart lost policy: %d %s", status, body)
	}
	status, body = call("POST", plugin.BasePath+"/policy/rollback", "fixture-management", map[string]uint64{"policy_revision": p.Revision - 1}, map[string]string{"If-Match": fmt.Sprint(p.Revision), "Idempotency-Key": "rollback"})
	if status != 200 {
		t.Fatalf("rollback: %d %s", status, body)
	}
	status, body = chat("route-model", "fixture-downstream")
	if status != 200 {
		t.Fatalf("rollback routing: %d %s", status, body)
	}
	waitIdle(t, call)
	status, body = call("GET", plugin.BasePath+"/policy", "fixture-management", nil, nil)
	var persistedPolicy policy.Snapshot
	if status != 200 || json.Unmarshal(body, &persistedPolicy) != nil {
		t.Fatalf("policy before unload: %d %s", status, body)
	}
	for _, path := range []string{"/v8/management/config/plugins/configs/" + plugin.ID, "/v8/management/config/plugins"} {
		finish := make(chan struct{})
		mu.Lock()
		streamRelease = finish
		mu.Unlock()
		activeRequest, err := http.NewRequest("POST", base+"/v1/chat/completions", bytes.NewReader(streamBody))
		if err != nil {
			t.Fatal(err)
		}
		activeRequest.Header.Set("Authorization", "Bearer fixture-downstream")
		activeRequest.Header.Set("Content-Type", "application/json")
		activeResponse, err := client.Do(activeRequest)
		if err != nil {
			t.Fatal(err)
		}
		defer activeResponse.Body.Close()
		if activeResponse.StatusCode != 200 {
			data, _ := io.ReadAll(activeResponse.Body)
			t.Fatalf("stream before unload at %s: %d %s", path, activeResponse.StatusCode, data)
		}
		select {
		case <-streamStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("stream before unload not started")
		}
		status, body = call("GET", plugin.BasePath+"/health", "fixture-management", nil, nil)
		var active struct {
			Counts map[string]int `json:"active"`
		}
		if status != 200 || json.Unmarshal(body, &active) != nil || active.Counts[policy.CallerScope("fixture-downstream")] != 1 {
			t.Fatalf("unload fixture has no active reservation: %d %s", status, body)
		}
		for _, enabled := range []bool{false, true} {
			status, body = call("PATCH", path, "fixture-management", map[string]bool{"enabled": enabled}, nil)
			if status != 200 {
				t.Fatalf("plugin enable=%t at %s: %d %s", enabled, path, status, body)
			}
			deadline := time.Now().Add(10 * time.Second)
			for {
				info = getPlugin()
				status, body = call("GET", plugin.BasePath+"/health", "fixture-management", nil, nil)
				if info.Registered == enabled && info.EffectiveEnabled == enabled &&
					(enabled && status == 200 && bytes.Contains(body, []byte(`"status":"ok"`)) || !enabled && status == 404) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("plugin enable=%t at %s not applied: %+v; health %d %s", enabled, path, info, status, body)
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !enabled {
				status, body = call("GET", plugin.ResourcePath+"/ui", "", nil, nil)
				if status != 404 {
					t.Fatalf("unloaded plugin resource: %d %s", status, body)
				}
				// CPA no longer sends completions to a detached plugin. Finish the
				// admitted stream while detached, before the same DLL is reused.
				close(finish)
				data, readErr := io.ReadAll(activeResponse.Body)
				closeErr := activeResponse.Body.Close()
				if readErr != nil || closeErr != nil || !bytes.Contains(data, []byte("[DONE]")) {
					t.Fatalf("stream while unloaded at %s: %v / %v; %s", path, readErr, closeErr, data)
				}
				continue
			}
			status, body = call("GET", plugin.BasePath+"/policy", "fixture-management", nil, nil)
			var restored policy.Snapshot
			if status != 200 || json.Unmarshal(body, &restored) != nil || !reflect.DeepEqual(restored, persistedPolicy) {
				t.Fatalf("re-enabled plugin lost policy: %d %s", status, body)
			}
			status, body = chat("blocked-model", "fixture-downstream")
			if status != 403 {
				t.Fatalf("re-enabled plugin lost model denial: %d %s", status, body)
			}
			mu.Lock()
			allowedBefore := seen["Bearer upstream-a"] + seen["Bearer upstream-b"]
			forbiddenBefore := seen["Bearer upstream-forbidden"]
			mu.Unlock()
			status, body = chat("route-model", "fixture-downstream")
			mu.Lock()
			allowedDelta := seen["Bearer upstream-a"] + seen["Bearer upstream-b"] - allowedBefore
			forbiddenDelta := seen["Bearer upstream-forbidden"] - forbiddenBefore
			mu.Unlock()
			if status != 200 || allowedDelta != 1 || forbiddenDelta != 0 {
				t.Fatalf("re-enabled plugin routing: %d %s; allowed=%d forbidden=%d", status, body, allowedDelta, forbiddenDelta)
			}
			waitIdle(t, call)
		}
	}
	stop()
	// Reload both verified older catalogs without rewriting their
	// provenance, then explicitly publish and reload the next current revision.
	for _, producer := range [][2]string{{"0.1.8", "v8.0.32"}, {"0.1.9", "v8.0.45"}} {
		legacy := modelcatalog.Clone(modelSnapshot)
		legacy.PluginVersion, legacy.CPAVersion, legacy.Revision = producer[0], producer[1], 7
		legacy.GeneratedAt = time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
		legacyJSON, err := json.Marshal(legacy)
		if err != nil {
			t.Fatal(err)
		}
		legacyDigest := sha256.Sum256(append([]byte("catalog:6:"), legacyJSON...))
		legacyState := map[string]any{"format": 1, "current": legacy, "accepted_at": legacy.GeneratedAt.Add(time.Minute), "receipts": map[string]any{"legacy": map[string]any{"digest": hex.EncodeToString(legacyDigest[:]), "revision": 7}}}
		legacyDisk, err := json.Marshal(legacyState)
		if err != nil {
			t.Fatal(err)
		}
		catalogPath := filepath.Join(dir, "state", "model-catalog.json")
		if err := os.WriteFile(catalogPath, legacyDisk, 0600); err != nil {
			t.Fatal(err)
		}
		start()
		status, body = call("GET", plugin.BasePath+"/model-catalog", "fixture-management", nil, nil)
		if status != 200 || !bytes.Contains(body, []byte(`"refresh_required":true`)) || !bytes.Contains(body, []byte(`"cpa_version":"`+producer[1]+`"`)) || !bytes.Contains(body, []byte(`"plugin_version":"`+producer[0]+`"`)) {
			t.Fatalf("legacy catalog unavailable after real CPA upgrade: %d %s", status, body)
		}
		if saved, err := os.ReadFile(catalogPath); err != nil || !bytes.Equal(saved, legacyDisk) {
			t.Fatal("CPA startup rewrote the legacy catalog", err)
		}
		status, body = call("GET", "/v1/models", "fixture-downstream", nil, nil)
		if status != 200 || !bytes.Contains(body, []byte(`"id":"route-model"`)) || bytes.Contains(body, []byte(`"id":"blocked-model"`)) {
			t.Fatalf("legacy catalog lost policy filtering: %d %s", status, body)
		}
		upgrade := modelcatalog.Clone(legacy)
		upgrade.PluginVersion, upgrade.CPAVersion, upgrade.Revision = policy.Version, plugin.CPAVersion, 8
		upgrade.GeneratedAt = time.Now().UTC()
		status, body = call("PUT", plugin.BasePath+"/model-catalog", "fixture-management", upgrade, map[string]string{"If-Match": "7", "Idempotency-Key": "upgrade"})
		if status != 200 || !bytes.Contains(body, []byte(`"catalog_revision":8`)) {
			t.Fatalf("legacy upgrade publication: %d %s", status, body)
		}
		stop()
		start()
		status, body = call("GET", plugin.BasePath+"/model-catalog", "fixture-management", nil, nil)
		if status != 200 || !bytes.Contains(body, []byte(`"refresh_required":false`)) || !bytes.Contains(body, []byte(`"catalog_revision":8`)) {
			t.Fatalf("restart lost upgraded catalog: %d %s", status, body)
		}
		status, body = call("GET", plugin.BasePath+"/policy", "fixture-management", nil, nil)
		var afterUpgrade policy.Snapshot
		if status != 200 || json.Unmarshal(body, &afterUpgrade) != nil || !reflect.DeepEqual(afterUpgrade, persistedPolicy) {
			t.Fatalf("catalog upgrade changed policy: %d %s", status, body)
		}
		status, body = call("PUT", plugin.BasePath+"/model-catalog", "fixture-management", legacy, map[string]string{"If-Match": "6", "Idempotency-Key": "legacy"})
		if status != 200 || !bytes.Contains(body, []byte(`"catalog_revision":7`)) {
			t.Fatalf("restart lost legacy receipt: %d %s", status, body)
		}
		stop()
	}
	stored, err := os.ReadFile(filepath.Join(dir, "state", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-downstream", "fixture-management", "upstream-a", "upstream-b"} {
		if strings.Contains(string(stored), secret) {
			t.Fatal("state leaked credential")
		}
	}
	stored, err = os.ReadFile(filepath.Join(dir, "state", "model-catalog.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"fixture-downstream", "fixture-management", "upstream-a", "upstream-b", "openai-compatibility:fixture:"} {
		if bytes.Contains(stored, []byte(secret)) {
			t.Fatal("model catalog leaked credential identity")
		}
	}
	t.Log("Configured CPA binary: dynamic load, management auth/resources and config defaults, model-list filtering and authenticated visibility fallback, response verification removal with legacy config and Chat/Responses/Claude/Gemini passthrough, alias policy, subset weights, streaming cancellation, truncation, concurrency, lifecycle cleanup, fail-closed routing, restart, rollback and policy recovery after per-plugin/global disable/re-enable passed")
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
