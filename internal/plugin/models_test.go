package plugin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestModelListFilterReconfiguration(t *testing.T) {
	a := configured(t)
	setPolicy(t, a, policy.Key{ID: policy.CallerScope("key"), Enabled: false, GroupIDs: []string{}})
	req := pluginapi.ResponseInterceptRequest{SourceFormat: "openai", StatusCode: 200, RequestHeaders: http.Header{"Authorization": {"Bearer key"}}, Body: []byte(`{"data":[{"id":"blocked"}]}`)}
	for _, enabled := range []bool{false, true} {
		setting := "false"
		if enabled {
			setting = "true"
		}
		configYAML := "state_dir: " + a.stateDir + "\nmodel_list_filter_enabled: " + setting
		invoke(t, a, pluginabi.MethodPluginReconfigure, lifecycle{SchemaVersion: 6, ConfigYAML: []byte(configYAML)})
		resp := listResponse(t, a, req)
		if !enabled && len(resp.Body) != 0 {
			t.Fatal("disabled filter changed response")
		}
		if enabled && string(resp.Body) != `{"data":[]}` {
			t.Fatalf("enabled filter did not apply policy: %s", resp.Body)
		}
		w := httptest.NewRecorder()
		a.serveManagement(w, httptest.NewRequest("GET", BasePath+"/capabilities", nil), "")
		var caps struct {
			CPA     string `json:"cpa_version"`
			Version string `json:"plugin_version"`
			Enabled bool   `json:"model_list_filter_enabled"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &caps); err != nil {
			t.Fatal(err)
		}
		if caps.CPA != "v7.3.8" || caps.Version != "0.1.2" || caps.Enabled != enabled {
			t.Fatalf("incorrect capabilities: %+v", caps)
		}
		generation := interception(t, invoke(t, a, pluginabi.MethodRequestInterceptBefore, pluginapi.RequestInterceptRequest{RequestID: "toggle-test", Model: "blocked", Metadata: map[string]any{"caller_scope": policy.CallerScope("key")}}))
		if generation.StatusCode != 401 {
			t.Fatal("toggle bypassed generation policy")
		}
	}
}

func TestModelListContract(t *testing.T) {
	for _, tc := range []struct{ name, format, body, want string }{
		{"openai", "openai", `{"object":"list","data":[{"id":"allowed","extra":42},{"id":"blocked"}]}`, `{"object":"list","data":[{"id":"allowed","extra":42}]}`},
		{"gemini", "gemini", `{"models":[{"name":"models/allowed"},{"name":"models/blocked"}]}`, `{"models":[{"name":"models/allowed"}]}`},
		{"codex", "openai", `{"models":[{"slug":"allowed"},{"slug":"blocked"}]}`, `{"models":[{"slug":"allowed"}]}`},
		{"claude", "claude", `{"data":[{"id":"claude-fable-5-dd-dekcolb"},{"id":"claude-fable-5-dd-dewolla"}],"first_id":"old","last_id":"old","has_more":false}`, `{"data":[{"id":"claude-fable-5-dd-dewolla"}],"first_id":"claude-fable-5-dd-dewolla","last_id":"claude-fable-5-dd-dewolla","has_more":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := configured(t)
			p, _ := a.currentStore().Current()
			base := p.Revision
			p.Revision++
			p.Groups = []policy.Group{{ID: "group", Name: "Group", Rule: policy.Rule{DeniedModels: []string{"blocked"}}}}
			p.Keys = []policy.Key{{ID: policy.CallerScope("test-key"), Enabled: true, GroupIDs: []string{"group"}, Rule: policy.Rule{Models: []string{"allowed", "blocked"}}}}
			if _, err := a.currentStore().Apply(p, base, "test"); err != nil {
				t.Fatal(err)
			}
			req := pluginapi.ResponseInterceptRequest{SourceFormat: tc.format, StatusCode: 200, RequestHeaders: http.Header{"Authorization": {"Bearer test-key"}}, Body: []byte(tc.body)}
			resp := listResponse(t, a, req)
			var got, want any
			if err := json.Unmarshal(resp.Body, &got); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
				t.Fatal(err)
			}
			g, _ := json.Marshal(got)
			w, _ := json.Marshal(want)
			if string(g) != string(w) {
				t.Fatalf("got %s want %s", g, w)
			}
			if len(a.runtime.Counts()) != 0 {
				t.Fatal("listing reserved concurrency")
			}
		})
	}
}

func listResponse(t *testing.T, a *App, req pluginapi.ResponseInterceptRequest) pluginapi.ResponseInterceptResponse {
	t.Helper()
	env := invoke(t, a, pluginabi.MethodResponseInterceptAfter, req)
	var resp pluginapi.ResponseInterceptResponse
	if !env.OK {
		t.Fatal(env.Error)
	}
	if err := json.Unmarshal(env.Result, &resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestModelListIdentityAndFailures(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		headers               http.Header
		body                  string
		disabled, unavailable bool
		want                  string
	}{
		{"unconfigured", http.Header{"Authorization": {"Bearer other"}}, `{"data":[{"id":"m"}]}`, false, false, `"id":"m"`},
		{"anthropic", http.Header{"X-Api-Key": {"key"}}, `{"data":[]}`, false, false, `"data":[]`},
		{"google", http.Header{"X-Goog-Api-Key": {"key"}}, `{"data":[]}`, false, false, `"data":[]`},
		{"missing", nil, `{"data":[{"id":"m"}]}`, false, false, `"id":"m"`},
		{"mixed", http.Header{"Authorization": {"Bearer key"}, "X-Api-Key": {"other"}}, `{"data":[{"id":"m"}]}`, false, false, `"id":"m"`},
		{"duplicate", http.Header{"Authorization": {"Bearer key", "Bearer other"}}, `{"data":[{"id":"m"}]}`, false, false, `"id":"m"`},
		{"disabled", http.Header{"Authorization": {"Bearer key"}}, `{"data":[{"id":"m"}]}`, true, false, `"data":[]`},
		{"policy missing", http.Header{"Authorization": {"Bearer key"}}, `{"data":[]}`, false, true, "policy_unavailable"},
		{"policy missing without identity", nil, `{"data":[]}`, false, true, "policy_unavailable"},
		{"malformed without identity", nil, `{"data":[{}]}`, false, false, "invalid_model_catalog"},
		{"malformed", http.Header{"Authorization": {"Bearer key"}}, `{"data":[{}]}`, false, false, "invalid_model_catalog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := configured(t)
			if tc.disabled {
				setPolicy(t, a, policy.Key{ID: policy.CallerScope("key"), GroupIDs: []string{}, Enabled: false})
			}
			if tc.unavailable {
				a = New(nil)
			}
			resp := listResponse(t, a, pluginapi.ResponseInterceptRequest{SourceFormat: "openai", StatusCode: 200, RequestHeaders: tc.headers, Body: []byte(tc.body)})
			if !strings.Contains(string(resp.Body), tc.want) {
				t.Fatalf("unexpected response: %s", resp.Body)
			}
			if strings.Contains(string(resp.Body), "secret") {
				t.Fatal("catalog leaked on failure")
			}
			if tc.name == "missing" || tc.name == "mixed" || tc.name == "duplicate" {
				if string(resp.Body) != tc.body || resp.Headers.Get("X-CPA-Helper-Model-List") != "unfiltered-identity-unavailable" || resp.Headers.Get("X-CPA-Helper-Error") != "" {
					t.Fatal("identity fallback must preserve the catalog and report its reason")
				}
			}
		})
	}
	resp := listResponse(t, configured(t), pluginapi.ResponseInterceptRequest{Model: "m", StatusCode: 200, Body: []byte(`{"choices":[]}`)})
	if len(resp.Body) != 0 {
		t.Fatal("generation response modified")
	}
}
