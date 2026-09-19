package plugin

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

func configured(t *testing.T) *App {
	t.Helper()
	a := New(nil)
	raw, _ := json.Marshal(lifecycle{SchemaVersion: 4, ConfigYAML: []byte("state_dir: " + filepath.ToSlash(filepath.Join(t.TempDir(), "state")))})
	env := invoke(t, a, "plugin.register", json.RawMessage(raw))
	if !env.OK {
		t.Fatal(string(env.Result))
	}
	var registration registration
	if err := json.Unmarshal(env.Result, &registration); err != nil {
		t.Fatal(err)
	}
	if registration.Metadata.GitHubRepository == "" || registration.Metadata.Author == "" || !registration.Capabilities["scheduler"] || registration.SchemaVersion != 4 {
		t.Fatal("invalid CPA registration")
	}
	return a
}

func TestPluginRegisterRejectsUnknownConfigurationField(t *testing.T) {
	a := New(nil)
	validDir := filepath.ToSlash(filepath.Join(t.TempDir(), "valid"))
	wrongDir := filepath.ToSlash(filepath.Join(t.TempDir(), "wrong"))
	raw, err := json.Marshal(lifecycle{
		SchemaVersion: 4,
		ConfigYAML:    []byte("state_dir: " + validDir + "\nstate_dri: " + wrongDir),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = a.Handle(pluginabi.MethodPluginRegister, raw); err == nil {
		t.Fatal("accepted unknown plugin configuration field")
	}
	if a.currentStore() != nil {
		t.Fatal("opened a policy store after rejecting configuration")
	}
}

func TestPluginLifecycleAcceptsHostManagedStoreMetadata(t *testing.T) {
	a := New(nil)
	stateDir := filepath.ToSlash(filepath.Join(t.TempDir(), "state"))
	raw, err := json.Marshal(lifecycle{
		SchemaVersion: 4,
		ConfigYAML: []byte("enabled: true\nstate_dir: " + stateDir + "\nstore:\n" +
			"  id: cpa-helper-plugin\n" +
			"  release-tag: v0.1.0\n" +
			"  install:\n" +
			"    type: github-release\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure} {
		if env := invoke(t, a, method, json.RawMessage(raw)); !env.OK {
			t.Fatal(method, env.Error)
		}
	}
	if a.currentStore() == nil {
		t.Fatal("policy store was not opened")
	}
}

func invoke(t *testing.T, a *App, method string, req any) pluginabi.Envelope {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	out, err := a.Handle(method, raw)
	if err != nil {
		t.Fatal(err)
	}
	var env pluginabi.Envelope
	if err = json.Unmarshal(out, &env); err != nil {
		t.Fatal(err)
	}
	return env
}
func setPolicy(t *testing.T, a *App, k policy.Key) {
	t.Helper()
	p, _ := a.currentStore().Current()
	base := p.Revision
	p.Revision++
	p.Keys = []policy.Key{k}
	if _, err := a.currentStore().Apply(p, base, "set"); err != nil {
		t.Fatal(err)
	}
}
func interception(t *testing.T, env pluginabi.Envelope) pluginapi.RequestInterceptResponse {
	t.Helper()
	var resp pluginapi.RequestInterceptResponse
	if !env.OK {
		t.Fatal(env.Error)
	}
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCPAInterceptionAndCompletionContract(t *testing.T) {
	a := configured(t)
	scope := policy.CallerScope("private-client-key")
	setPolicy(t, a, policy.Key{ID: scope, Enabled: true, GroupIDs: []string{}, MaxConcurrency: 1, Rule: policy.Rule{Models: []string{"public-alias"}}})
	request := pluginapi.RequestInterceptRequest{RequestID: "r1", Model: "upstream", RequestedModel: "public-alias(high)", Metadata: map[string]any{"caller_scope": scope, "generate": true}}
	if resp := interception(t, invoke(t, a, "request.intercept_before", request)); resp.Terminate {
		t.Fatal(resp)
	}
	if resp := interception(t, invoke(t, a, "request.intercept_before", request)); resp.Terminate {
		t.Fatal("retry double counted")
	}
	request.RequestID = "r2"
	if resp := interception(t, invoke(t, a, "request.intercept_before", request)); resp.StatusCode != 429 {
		t.Fatal(resp)
	}
	for _, outcome := range []pluginapi.RequestCompletionOutcome{pluginapi.RequestCompletionSucceeded, pluginapi.RequestCompletionFailed, pluginapi.RequestCompletionCanceled, pluginapi.RequestCompletionRejected} {
		invoke(t, a, "request.complete", pluginapi.RequestCompletion{RequestID: "r1", Outcome: outcome})
	}
	if len(a.runtime.Counts()) != 0 {
		t.Fatal("completion leak")
	}
	if resp := interception(t, invoke(t, a, "request.intercept_before", request)); resp.Terminate {
		t.Fatal("slot not released")
	}
	invoke(t, a, "request.complete", pluginapi.RequestCompletion{RequestID: "r2"})
	request.RequestID = "r3"
	request.RequestedModel = "forbidden"
	if resp := interception(t, invoke(t, a, "request.intercept_before", request)); resp.StatusCode != 403 || bytes.Contains(resp.ResponseBody, []byte("private-client-key")) {
		t.Fatal(resp)
	}
	request.RequestID = "r4"
	request.RequestedModel = "public-alias"
	request.Metadata["generate"] = false
	if resp := interception(t, invoke(t, a, "request.intercept_before", request)); resp.Terminate {
		t.Fatal(resp)
	}
	if len(a.runtime.Counts()) != 0 {
		t.Fatal("count-tokens occupied slot")
	}
}

func TestCPAReconfigureAfterQuiesceRestoresAdmission(t *testing.T) {
	a := configured(t)
	scope := policy.CallerScope("rollback-key")
	setPolicy(t, a, policy.Key{ID: scope, Enabled: true, GroupIDs: []string{}, MaxConcurrency: 1})
	request := pluginapi.RequestInterceptRequest{RequestID: "r1", Metadata: map[string]any{"caller_scope": scope, "generate": true}}
	if resp := interception(t, invoke(t, a, pluginabi.MethodRequestInterceptBefore, request)); resp.Terminate {
		t.Fatal("initial request was rejected", resp)
	}
	if env := invoke(t, a, pluginabi.MethodPluginQuiesce, struct{}{}); !env.OK {
		t.Fatal(env.Error)
	}
	request.RequestID = "quiesced"
	if resp := interception(t, invoke(t, a, pluginabi.MethodRequestInterceptBefore, request)); resp.StatusCode != 503 {
		t.Fatal("quiesced instance admitted a request", resp)
	}
	configYAML, err := yaml.Marshal(config{StateDir: a.stateDir, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if env := invoke(t, a, pluginabi.MethodPluginReconfigure, lifecycle{SchemaVersion: 4, ConfigYAML: configYAML}); !env.OK {
		t.Fatal(env.Error)
	}
	request.RequestID = "still-limited"
	if resp := interception(t, invoke(t, a, pluginabi.MethodRequestInterceptBefore, request)); resp.StatusCode != 429 {
		t.Fatal("reconfigure reset in-flight accounting", resp)
	}
	invoke(t, a, pluginabi.MethodRequestComplete, pluginapi.RequestCompletion{RequestID: "r1"})
	request.RequestID = "after-completion"
	if resp := interception(t, invoke(t, a, pluginabi.MethodRequestInterceptBefore, request)); resp.Terminate {
		t.Fatal("reconfigure did not restore admission", resp)
	}
	invoke(t, a, pluginabi.MethodRequestComplete, pluginapi.RequestCompletion{RequestID: "after-completion"})
}

func TestCPASchedulerContract(t *testing.T) {
	a := configured(t)
	scope := policy.CallerScope("k")
	setPolicy(t, a, policy.Key{ID: scope, Enabled: true, GroupIDs: []string{}, Rule: policy.Rule{CredentialIDs: []string{policy.CredentialRef("allowed")}}})
	candidates := []pluginapi.SchedulerAuthCandidate{{ID: "forbidden", Provider: "codex", Priority: 10}, {ID: "allowed", Provider: "codex", Priority: 10}}
	req := pluginapi.SchedulerPickRequest{Model: "m", Candidates: candidates, Options: pluginapi.SchedulerOptions{Metadata: map[string]any{"caller_scope": scope}}}
	env := invoke(t, a, "scheduler.pick", req)
	var result pluginapi.SchedulerPickResponse
	if err := json.Unmarshal(env.Result, &result); err != nil {
		t.Fatal(err)
	}
	if !env.OK || !result.Handled || result.AuthID != "allowed" {
		t.Fatal(env, result)
	}
	req.Candidates = candidates[:1]
	env = invoke(t, a, "scheduler.pick", req)
	if env.OK || env.Error.HTTPStatus != 503 {
		t.Fatal("escaped supplied priority tier", env)
	}
	req.Candidates = candidates[1:]
	env = invoke(t, a, "scheduler.pick", req)
	_ = json.Unmarshal(env.Result, &result)
	if !env.OK || result.Handled {
		t.Fatal("did not delegate whole candidate set", env)
	}
	req.Options.Metadata["caller_scope"] = policy.CallerScope("unknown")
	req.Candidates = candidates
	env = invoke(t, a, "scheduler.pick", req)
	_ = json.Unmarshal(env.Result, &result)
	if result.Handled {
		t.Fatal("unknown key restricted")
	}
}

func TestManagementContract(t *testing.T) {
	a := configured(t)
	call := func(method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, BasePath+path, bytes.NewReader(raw))
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		a.serveManagement(w, req, "")
		return w
	}
	w := call("GET", "/policy", nil, nil)
	if w.Code != 200 || w.Header().Get("ETag") != "\"1\"" {
		t.Fatal(w)
	}
	var p policy.Snapshot
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	p.Revision = 2
	if w = call("PUT", "/policy", p, nil); w.Code != 428 {
		t.Fatal(w.Code)
	}
	headers := map[string]string{"If-Match": "\"1\"", "Idempotency-Key": "test"}
	for i := 0; i < 2; i++ {
		if w = call("PUT", "/policy", p, headers); w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	headers["Idempotency-Key"] = "stale"
	if w = call("PUT", "/policy", p, headers); w.Code != 409 {
		t.Fatal(w.Code)
	}
	p.Revision = 3
	p.Keys = []policy.Key{{ID: "raw-secret-key", GroupIDs: []string{}}}
	headers["If-Match"] = "2"
	if w = call("PUT", "/policy", p, headers); w.Code != 400 || strings.Contains(w.Body.String(), "raw-secret-key") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", "/health", nil, nil); w.Code != 200 || !strings.Contains(w.Body.String(), "last_error") {
		t.Fatal(w.Body.String())
	}
	headers["Idempotency-Key"] = "rollback"
	if w = call("POST", "/policy/rollback", map[string]int{"policy_revision": 1}, headers); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("GET", "/directory", nil, nil); w.Code != 502 {
		t.Fatal("hidden host error", w.Code)
	}
}

func TestStaticResourcesCannotReadPolicy(t *testing.T) {
	a := configured(t)
	for _, tc := range []struct {
		path   string
		status int
	}{{ResourcePath + "/ui", 200}, {ResourcePath + "/app.js", 200}, {ResourcePath + "/sha256.js", 200}, {ResourcePath + "/policy", 404}, {ResourcePath + "/../v1/policy", 404}} {
		t.Run(tc.path, func(t *testing.T) {
			env := invoke(t, a, "management.handle", pluginapi.ManagementRequest{Method: "GET", Path: tc.path})
			var response pluginapi.ManagementResponse
			_ = json.Unmarshal(env.Result, &response)
			if response.StatusCode != tc.status {
				t.Fatal(response.StatusCode)
			}
			if tc.status == 200 && response.Headers.Get("Content-Security-Policy") == "" {
				t.Fatal("no CSP")
			}
		})
	}
}

func TestDirectoryRedactsHostSecrets(t *testing.T) {
	a := configured(t)
	a.host = func(method string, payload any) (json.RawMessage, error) {
		if method != "host.auth.list" {
			t.Fatal(method)
		}
		return json.RawMessage(`{"files":[{"id":"secret-id","name":"secret-api-key.json","provider":"codex","source":"file","account":"secret-account","email":"secret@example.com"}]}`), nil
	}
	items, err := a.directory("test")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(items)
	if bytes.Contains(raw, []byte("secret")) {
		t.Fatal(string(raw))
	}
	if items[0].Ref != policy.CredentialRef("secret-id") || items[0].Source != "auth-files" {
		t.Fatal(items)
	}
}

func TestSourceAndModelFixtures(t *testing.T) {
	for _, tc := range []struct {
		attrs map[string]string
		want  string
	}{{map[string]string{"source": "config:codex[abc]"}, "ai-providers"}, {map[string]string{"source_backend": "file"}, "auth-files"}, {map[string]string{"runtime_only": "true"}, "ai-providers"}, {map[string]string{"source": "objectstore"}, "auth-files"}, {nil, ""}} {
		if got := sourceFromCandidate(pluginapi.SchedulerAuthCandidate{Attributes: tc.attrs}); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	for _, tc := range []struct{ model, requested, want string }{{"upstream", "alias(high)", "alias"}, {"m(high)", "auto", "m"}, {"m(1024)", "", "m"}, {"upstream", "prefix/alias", "prefix/alias"}} {
		if got := requestedModel(tc.model, tc.requested); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}
