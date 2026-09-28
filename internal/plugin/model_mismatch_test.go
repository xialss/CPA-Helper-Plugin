package plugin

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func mismatchApp(t *testing.T) *App {
	t.Helper()
	a := configured(t)
	a.modelMismatch.Enabled = true
	// Legacy protocol unit tests exercise the pre-strict per-chunk adapter;
	// strict Responses-only behavior has a dedicated test below.
	a.modelMismatch.ResponsesOnlyStream = false
	return a
}
func beginCheck(t *testing.T, a *App, id, body string, stream bool) {
	t.Helper()
	resp := interception(t, invoke(t, a, pluginabi.MethodRequestInterceptBefore, pluginapi.RequestInterceptRequest{
		RequestID: id, RequestedModel: "client-model", Model: "routed-model", Stream: stream, Body: []byte(body),
		Metadata: map[string]any{"caller_scope": policy.CallerScope("test-key"), "generate": true}}))
	if resp.Terminate {
		t.Fatal("verification rejected before execution", resp)
	}
}
func rawModel(t *testing.T, a *App, original, body string) {
	t.Helper()
	invoke(t, a, pluginabi.MethodResponseNormalizeBefore, pluginapi.ResponseTransformRequest{OriginalRequest: []byte(original), Body: []byte(body)})
}

func TestResponseModelRawIdentity(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"match", `{"model":"client-model"}`, ""},
		{"mismatch", `{"model":"watered-model","choices":[{"message":{"content":"secret-output"}}]}`, "upstream_model_mismatch"},
		{"claude", `{"type":"message_start","message":{"model":"watered-model"}}`, "upstream_model_mismatch"},
		{"gemini", `{"modelVersion":"watered-model"}`, "upstream_model_mismatch"},
		{"responses", `{"response":{"model":"watered-model"}}`, "upstream_model_mismatch"},
		{"unknown", `{"choices":[]}`, ""},
		{"malformed", "not-json", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := mismatchApp(t)
			original := `{"model":"client-model"}`
			beginCheck(t, a, "one", original, false)
			rawModel(t, a, original, tc.raw)
			// The translated body deliberately lies about model identity.
			out := listResponse(t, a, pluginapi.ResponseInterceptRequest{RequestID: "one", RequestedModel: "client-model", Model: "routed-model", StatusCode: 200, Body: []byte(`{"model":"client-model","text":"secret-output"}`)})
			if tc.want == "" {
				if len(out.Body) != 0 {
					t.Fatalf("modified matching/unknown result: %s", out.Body)
				}
			} else {
				if !strings.Contains(string(out.Body), tc.want) || strings.Contains(string(out.Body), "secret-output") || out.Headers.Get("X-CPA-Helper-Model-Mismatch") != "true" {
					t.Fatalf("bad replacement: %+v", out)
				}
			}
			invoke(t, a, pluginabi.MethodRequestComplete, pluginapi.RequestCompletion{RequestID: "one"})
			if len(a.modelMismatchStates) != 0 || len(a.mismatchRequests) != 0 || len(a.runtime.Counts()) != 0 {
				t.Fatal("state leak")
			}
		})
	}
}

func TestModelVerificationUnknownAndAmbiguous(t *testing.T) {
	for _, unknown := range []string{"pass", "reject"} {
		a := mismatchApp(t)
		a.modelMismatch.UnknownAction = unknown
		beginCheck(t, a, "one", "same", true)
		beginCheck(t, a, "two", "same", true)
		rawModel(t, a, "same", `{"model":"wrong"}`)
		for _, id := range []string{"one", "two"} {
			out := listResponse(t, a, pluginapi.ResponseInterceptRequest{RequestID: id, RequestedModel: "client-model", StatusCode: 200})
			if !strings.Contains(string(out.Body), "ambiguous_request_identity") || strings.Contains(string(out.Body), "upstream_model_mismatch") {
				t.Fatalf("ambiguous response was not handled as unverifiable: unknown=%s body=%s", unknown, out.Body)
			}
			a.completeModelMismatch(id)
		}
		if len(a.mismatchRequests) != 0 {
			t.Fatal("digest index leak")
		}
	}
}

func TestStreamMismatchLatchedAndConfigurationPinned(t *testing.T) {
	a := mismatchApp(t)
	beginCheck(t, a, "stream", "request", true)
	rawModel(t, a, "request", "data: {\"model\":\"wrong\"}\n\ndata: {\"model\":\"client-model\"}\n\n")
	a.mu.Lock()
	a.modelMismatch.Enabled = false
	a.mu.Unlock()
	for i := 0; i < 3; i++ {
		env := invoke(t, a, pluginabi.MethodResponseInterceptStreamChunk, pluginapi.StreamChunkInterceptRequest{RequestID: "stream", SourceFormat: "openai", ChunkIndex: i, Body: []byte(`{"model":"client-model","content":"leak"}`)})
		var out pluginapi.StreamChunkInterceptResponse
		if err := json.Unmarshal(env.Result, &out); err != nil {
			t.Fatal(err)
		}
		if i == 0 && (!json.Valid(out.Body) || !strings.Contains(string(out.Body), "upstream_model_mismatch")) {
			t.Fatalf("invalid Chat chunk: %s", out.Body)
		}
		if i > 0 && !out.DropChunk {
			t.Fatal("rejected stream resumed")
		}
	}
	a.completeModelMismatch("stream")
}

func TestStrictResponsesOnlyRejectsOtherStreamingProtocols(t *testing.T) {
	a := mismatchApp(t)
	a.modelMismatch.ResponsesOnlyStream = true
	for i, format := range []string{"openai", ""} {
		env := invoke(t, a, pluginabi.MethodRequestInterceptBefore, pluginapi.RequestInterceptRequest{
			RequestID: []string{"chat-stream", "unknown-stream"}[i], SourceFormat: format, Model: "client-model", RequestedModel: "client-model", Stream: true,
			Body: []byte("request"), Metadata: map[string]any{"caller_scope": policy.CallerScope("test-key"), "generate": true}})
		var response pluginapi.RequestInterceptResponse
		if err := json.Unmarshal(env.Result, &response); err != nil {
			t.Fatal(err)
		}
		if !response.Terminate || response.StatusCode != 400 || !strings.Contains(string(response.ResponseBody), "response_model_stream_unsupported") {
			t.Fatalf("strict stream format %q was not rejected: %+v", format, response)
		}
	}
	a.modelMismatch.StreamEnabled = false
	resp := interception(t, invoke(t, a, pluginabi.MethodRequestInterceptBefore, pluginapi.RequestInterceptRequest{
		RequestID: "stream-disabled", SourceFormat: "openai", Model: "client-model", RequestedModel: "client-model", Stream: true,
		Body: []byte("request"), Metadata: map[string]any{"caller_scope": policy.CallerScope("test-key"), "generate": true}}))
	if resp.Terminate {
		t.Fatalf("stream-disabled should bypass strict protocol gate: %+v", resp)
	}
	a.runtime.Complete("stream-disabled")
	a.modelMismatch.StreamEnabled = true
	a.modelMismatch.IgnoredModels = []string{"client-model"}
	resp = interception(t, invoke(t, a, pluginabi.MethodRequestInterceptBefore, pluginapi.RequestInterceptRequest{
		RequestID: "ignored-stream", SourceFormat: "openai", Model: "client-model", RequestedModel: "client-model", Stream: true,
		Body: []byte("request"), Metadata: map[string]any{"caller_scope": policy.CallerScope("test-key"), "generate": true}}))
	if resp.Terminate {
		t.Fatalf("ignored model should bypass strict protocol gate: %+v", resp)
	}
	a.runtime.Complete("ignored-stream")
}

func TestMissingResponseStateFailsClosed(t *testing.T) {
	a := mismatchApp(t)
	out := listResponse(t, a, pluginapi.ResponseInterceptRequest{RequestID: "missing-state", SourceFormat: "openai", Model: "client-model", RequestedModel: "client-model", StatusCode: 200, Body: []byte(`{"model":"client-model","secret":"leak"}`)})
	if !strings.Contains(string(out.Body), "response_model_unverifiable") || strings.Contains(string(out.Body), "secret") {
		t.Fatalf("missing response state was not rejected safely: %s", out.Body)
	}
	a.modelMismatch.Action = "audit"
	out = listResponse(t, a, pluginapi.ResponseInterceptRequest{RequestID: "missing-state-audit", SourceFormat: "openai", Model: "client-model", RequestedModel: "client-model", StatusCode: 200, Body: []byte(`{"model":"client-model","secret":"keep"}`)})
	if len(out.Body) != 0 {
		t.Fatalf("audit mode modified missing-state response: %s", out.Body)
	}
}

func TestModelConfigAtomicAndValidateRoute(t *testing.T) {
	a := configured(t)
	for _, cfg := range []string{"action: invalid", "unknown_action: invalid", "unexpected: true"} {
		raw, _ := json.Marshal(lifecycle{SchemaVersion: pluginabi.SchemaVersion, ConfigYAML: []byte("state_dir: " + a.stateDir + "\nmodel_list_filter_enabled: false\nresponse_model_mismatch:\n  " + cfg)})
		if _, err := a.Handle(pluginabi.MethodPluginReconfigure, raw); err == nil {
			t.Fatal("invalid settings accepted")
		}
		if !a.modelListFiltering() {
			t.Fatal("partial configuration activation")
		}
	}
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"enabled":true,"accepted_models":{"client-model":["actual"]}}`, 200},
		{`{"action":"bad"}`, 400}, {`{"surprise":true}`, 400}, {`{} {}`, 400}, {`null`, 400}, {`[]`, 400},
	} {
		w := httptest.NewRecorder()
		a.serveManagement(w, httptest.NewRequest("POST", BasePath+"/response-model/validate", strings.NewReader(tc.body)), "")
		if w.Code != tc.status {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
	}
}

func TestResponseObservationConcurrentCleanup(t *testing.T) {
	a := mismatchApp(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		id := string(rune('a' + i))
		beginCheck(t, a, id, id, true)
		wg.Go(func() {
			for j := 0; j < 20; j++ {
				a.observeResponseModel(pluginapi.ResponseTransformRequest{OriginalRequest: []byte(id), Body: []byte(`{"model":"wrong"}`)})
			}
			a.completeModelMismatch(id)
		})
	}
	wg.Wait()
	if len(a.modelMismatchStates) != 0 || len(a.mismatchRequests) != 0 {
		t.Fatal("state retained")
	}
}

func TestVerificationConfigurationBehavior(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*modelMismatchConfig)
		stream bool
		want   bool
	}{
		{"disabled", func(c *modelMismatchConfig) { c.Enabled = false }, false, false},
		{"audit", func(c *modelMismatchConfig) { c.Action = "audit" }, false, false},
		{"stream disabled", func(c *modelMismatchConfig) { c.StreamEnabled = false }, true, false},
		{"ignored", func(c *modelMismatchConfig) { c.IgnoredModels = []string{"client-model"} }, false, false},
		{"mapped", func(c *modelMismatchConfig) { c.Accepted = map[string][]string{"client-model": {"wrong"}} }, false, false},
		{"reject", func(c *modelMismatchConfig) {}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := mismatchApp(t)
			tc.change(&a.modelMismatch)
			beginCheck(t, a, "r", "request", tc.stream)
			rawModel(t, a, "request", `{"model":"wrong"}`)
			a.mismatchMu.Lock()
			e := a.modelDecision("r", a.modelMismatchStates["r"])
			a.mismatchMu.Unlock()
			if (e != nil) != tc.want {
				t.Fatalf("decision: %+v", e)
			}
			a.completeModelMismatch("r")
		})
	}
}

func TestDisabledDuplicateCannotContaminateEnabledRequest(t *testing.T) {
	a := mismatchApp(t)
	beginCheck(t, a, "enabled", "same", false)
	a.modelMismatch.Enabled = false
	beginCheck(t, a, "disabled", "same", false)
	rawModel(t, a, "same", `{"model":"wrong"}`)
	if !a.modelMismatchStates["enabled"].Ambiguous || a.modelMismatchStates["enabled"].Mismatch {
		t.Fatal("misattributed disabled request")
	}
	a.completeModelMismatch("disabled")
	rawModel(t, a, "same", `{"model":"wrong"}`)
	if a.modelMismatchStates["enabled"].Mismatch {
		t.Fatal("ambiguity was cleared prematurely")
	}
	a.completeModelMismatch("enabled")
}

func TestExactHostSchemaRequired(t *testing.T) {
	for _, version := range []uint32{0, 4, 5, 7} {
		a := New(nil)
		raw, _ := json.Marshal(lifecycle{SchemaVersion: version})
		if _, err := a.Handle(pluginabi.MethodPluginRegister, raw); err == nil {
			t.Fatalf("accepted schema %d", version)
		}
	}
}

func TestCompletedRequestCannotStartVerification(t *testing.T) {
	a := mismatchApp(t)
	if err := a.runtime.Begin("cancelled", policy.CallerScope("test-key")); err != nil {
		t.Fatal(err)
	}
	defer a.runtime.End("cancelled")
	invoke(t, a, pluginabi.MethodRequestComplete, pluginapi.RequestCompletion{RequestID: "cancelled"})
	a.beginModelMismatch(pluginapi.RequestInterceptRequest{RequestID: "cancelled", RequestedModel: "client-model", Body: []byte("request")})
	if a.modelMismatchActive() != 0 || len(a.mismatchRequests) != 0 {
		t.Fatal("completed request resurrected verification state")
	}
}

func TestResponsesVerificationIsTerminal(t *testing.T) {
	for _, code := range []string{"upstream_model_mismatch", "response_model_unverifiable"} {
		body := responsesVerificationFailure(&modelCheckError{Code: code, Message: "核验拒绝", RequestID: "r", Requested: "wanted"})
		if !strings.HasPrefix(string(body), "event: response.failed\ndata: ") {
			t.Fatalf("not a terminal event: %s", body)
		}
		var event struct {
			Type     string `json:"type"`
			Response struct {
				Status string `json:"status"`
				Error  struct {
					Code             string `json:"code"`
					VerificationCode string `json:"verification_code"`
					Status           int    `json:"status"`
					Retryable        bool   `json:"retryable"`
				} `json:"error"`
			} `json:"response"`
		}
		payload := strings.TrimSpace(strings.TrimPrefix(string(body), "event: response.failed\ndata: "))
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type != "response.failed" || event.Response.Status != "failed" || event.Response.Error.Code != "invalid_prompt" || event.Response.Error.VerificationCode != code || event.Response.Error.Status != 403 || event.Response.Error.Retryable {
			t.Fatalf("incorrect terminal classification: %s", body)
		}
	}
}
