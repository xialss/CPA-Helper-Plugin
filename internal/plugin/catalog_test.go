package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-helper-plugin/internal/catalog"
	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func publishCatalog(t *testing.T, a *App, credentials []catalog.Credential) {
	t.Helper()
	s := a.currentCatalogStore()
	base := s.Health().Revision
	p := catalog.Snapshot{ContractVersion: "v1", PluginVersion: policy.Version, CPAVersion: CPAVersion, Revision: base + 1, GeneratedAt: time.Now().UTC(), Credentials: credentials}
	if _, err := s.Apply(p, base, fmt.Sprint(p.Revision)); err != nil {
		t.Fatal(err)
	}
}

func TestModelListUsesEffectiveCredentialRules(t *testing.T) {
	a := configured(t)
	publishCatalog(t, a, []catalog.Credential{
		{Ref: policy.CredentialRef("a"), Source: "ai-providers", Provider: "first", Enabled: true, Models: []string{"only-a", "shared"}},
		{Ref: policy.CredentialRef("b"), Source: "auth-files", Provider: "second", Enabled: true, Models: []string{"only-b", "shared"}},
		{Ref: policy.CredentialRef("disabled"), Source: "ai-providers", Provider: "first", Models: []string{"disabled-only"}},
	})
	for _, tc := range []struct {
		name string
		rule policy.Rule
		want []string
	}{
		{"only a", policy.Rule{CredentialIDs: []string{policy.CredentialRef("a")}}, []string{"only-a", "shared"}},
		{"deny a", policy.Rule{DeniedCredentialIDs: []string{policy.CredentialRef("a")}}, []string{"only-b", "shared"}},
		{"category deny", policy.Rule{DeniedCredentialProviders: []policy.Selector{{Source: "auth-files", Provider: "second"}}}, []string{"only-a", "shared"}},
		{"all denied", policy.Rule{DeniedCredentialIDs: []string{policy.CredentialRef("a"), policy.CredentialRef("b")}}, []string{}},
		{"model deny wins", policy.Rule{CredentialIDs: []string{policy.CredentialRef("a")}, DeniedModels: []string{"shared"}}, []string{"only-a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, _ := a.currentStore().Current()
			base := p.Revision
			p.Revision++
			p.Groups = []policy.Group{{ID: "group", Name: "Group", Rule: tc.rule}}
			p.Keys = []policy.Key{{ID: policy.CallerScope("key"), Enabled: true, GroupIDs: []string{"group"}}}
			if _, err := a.currentStore().Apply(p, base, tc.name); err != nil {
				t.Fatal(err)
			}
			resp := listResponse(t, a, pluginapi.ResponseInterceptRequest{SourceFormat: "openai", StatusCode: 200, RequestHeaders: http.Header{"Authorization": {"Bearer key"}}, Body: []byte(`{"data":[{"id":"only-a","extra":42},{"id":"only-b"},{"id":"shared"},{"id":"disabled-only"}]}`)})
			var result struct {
				Data []struct{ ID string } `json:"data"`
			}
			if err := json.Unmarshal(resp.Body, &result); err != nil || result.Data == nil {
				t.Fatalf("unexpected catalog: %s", resp.Body)
			}
			got := make([]string, 0, len(result.Data))
			for _, model := range result.Data {
				got = append(got, model.ID)
			}
			if !reflect.DeepEqual(got, tc.want) || len(a.runtime.Counts()) != 0 {
				t.Fatalf("catalog = %v, want %v; active %v", got, tc.want, a.runtime.Counts())
			}
		})
	}
	if a.currentCatalogStore().Health().Revision != 1 {
		t.Fatal("rule updates required directory republishing")
	}
}

func TestCredentialCatalogFormats(t *testing.T) {
	for _, tc := range []struct{ format, body, want string }{
		{"openai", `{"data":[{"id":"team/public-alias"},{"id":"blocked"}]}`, `{"data":[{"id":"team/public-alias"}]}`},
		{"gemini", `{"models":[{"name":"models/allowed"},{"name":"models/blocked"}]}`, `{"models":[{"name":"models/allowed"}]}`},
		{"openai", `{"models":[{"slug":"allowed"},{"slug":"blocked"}]}`, `{"models":[{"slug":"allowed"}]}`},
		{"claude", `{"data":[{"id":"claude-fable-5-dd-dekcolb"},{"id":"claude-fable-5-dd-dewolla"}],"first_id":"old","last_id":"old"}`, `{"data":[{"id":"claude-fable-5-dd-dewolla"}],"first_id":"claude-fable-5-dd-dewolla","last_id":"claude-fable-5-dd-dewolla"}`},
	} {
		a := configured(t)
		setPolicy(t, a, policy.Key{ID: policy.CallerScope("key"), Enabled: true, GroupIDs: []string{}, Rule: policy.Rule{CredentialIDs: []string{policy.CredentialRef("a")}}})
		publishCatalog(t, a, []catalog.Credential{
			{Ref: policy.CredentialRef("a"), Source: "ai-providers", Provider: "first", Enabled: true, Models: []string{"allowed", "team/public-alias"}},
			{Ref: policy.CredentialRef("b"), Source: "auth-files", Provider: "second", Enabled: true, Models: []string{"blocked"}},
		})
		resp := listResponse(t, a, pluginapi.ResponseInterceptRequest{SourceFormat: tc.format, StatusCode: 200, RequestHeaders: http.Header{"X-Api-Key": {"key"}}, Body: []byte(tc.body)})
		var got, want any
		if err := json.Unmarshal(resp.Body, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(tc.want), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: %s", tc.format, resp.Body)
		}
	}
}

func TestMissingIncompleteAndOfflineCatalog(t *testing.T) {
	a := configured(t)
	key := policy.Key{ID: policy.CallerScope("key"), Enabled: true, GroupIDs: []string{}, Rule: policy.Rule{CredentialIDs: []string{policy.CredentialRef("a")}}}
	set := func() {
		t.Helper()
		p, _ := a.currentStore().Current()
		base := p.Revision
		p.Revision++
		p.Keys = []policy.Key{key}
		if _, err := a.currentStore().Apply(p, base, fmt.Sprint(p.Revision)); err != nil {
			t.Fatal(err)
		}
	}
	set()
	req := pluginapi.ResponseInterceptRequest{SourceFormat: "openai", StatusCode: 200, RequestHeaders: http.Header{"Authorization": {"Bearer key"}}, Body: []byte(`{"data":[{"id":"m"}]}`)}
	resp := listResponse(t, a, req)
	if resp.Headers.Get("X-CPA-Helper-Error") != "model_catalog_unavailable" || bytes.Contains(resp.Body, []byte(`"data"`)) {
		t.Fatal("missing catalog leaked models", string(resp.Body))
	}
	key.Rule.DeniedModels = []string{"m"}
	set()
	if resp = listResponse(t, a, req); string(resp.Body) != `{"data":[]}` {
		t.Fatal("explicit model denial needed a catalog", string(resp.Body))
	}
	key.Rule.DeniedModels = nil
	set()
	publishCatalog(t, a, []catalog.Credential{{Ref: policy.CredentialRef("a"), Source: "ai-providers", Provider: "fixture", Enabled: true, Models: []string{"other"}}})
	if resp = listResponse(t, a, req); resp.Headers.Get("X-CPA-Helper-Error") != "model_catalog_out_of_sync" {
		t.Fatal("incomplete directory silently classified a new model", string(resp.Body))
	}
	publishCatalog(t, a, []catalog.Credential{{Ref: policy.CredentialRef("a"), Source: "ai-providers", Provider: "fixture", Enabled: true, Models: []string{"m"}}})
	// Restart without a host callback or a connected management panel.
	restarted := New(nil)
	invoke(t, restarted, pluginabi.MethodPluginRegister, lifecycle{SchemaVersion: 6, ConfigYAML: []byte("state_dir: " + a.stateDir)})
	restarted.host = func(string, any) (json.RawMessage, error) { t.Fatal("list queried the host"); return nil, nil }
	if resp = listResponse(t, restarted, req); string(resp.Body) != `{"data":[{"id":"m"}]}` {
		t.Fatal("offline restart lost valid associations", string(resp.Body))
	}
	restarted.currentCatalogStore().RecordRejection("sync failed")
	if resp = listResponse(t, restarted, req); string(resp.Body) != `{"data":[{"id":"m"}]}` {
		t.Fatal("rejected sync removed valid associations", string(resp.Body))
	}
}

func TestModelCatalogManagementContract(t *testing.T) {
	a := configured(t)
	call := func(method string, body any, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, BasePath+"/model-catalog", bytes.NewReader(raw))
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		w := httptest.NewRecorder()
		a.serveManagement(w, r, "")
		return w
	}
	w := call("GET", nil, nil)
	if w.Code != 200 || w.Header().Get("ETag") != `"0"` || !strings.Contains(w.Body.String(), `"snapshot":null`) {
		t.Fatal("bootstrap unavailable", w.Code, w.Body.String())
	}
	p := catalog.Snapshot{ContractVersion: "v1", PluginVersion: policy.Version, CPAVersion: CPAVersion, Revision: 1, GeneratedAt: time.Now().UTC(), Credentials: []catalog.Credential{}}
	if w = call("PUT", p, nil); w.Code != 428 {
		t.Fatal(w.Code)
	}
	headers := map[string]string{"If-Match": "0", "Idempotency-Key": "sync"}
	for i := 0; i < 2; i++ {
		if w = call("PUT", p, headers); w.Code != 200 || !strings.Contains(w.Body.String(), `"catalog_revision":1`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	headers["Idempotency-Key"] = "stale"
	if w = call("PUT", p, headers); w.Code != 409 {
		t.Fatal(w.Code)
	}
	headers["If-Match"] = "1"
	if w = call("PUT", map[string]any{"api_key": "must-not-leak"}, headers); w.Code != 400 || strings.Contains(w.Body.String(), "must-not-leak") {
		t.Fatal("unknown secret field accepted or exposed", w.Code, w.Body.String())
	}
	view, h := a.currentCatalogStore().View()
	if view.Revision != 1 || h.Revision != 1 || h.Status != "ok" || h.LastError == "" || a.currentStore().Health().Revision != 1 {
		t.Fatal("rejection changed either active transaction", h)
	}
}

func TestModelCatalogManagementRejectsAmbiguousJSON(t *testing.T) {
	for _, tc := range []struct{ name, from, to string }{
		{"envelope case", `"credentials":`, `"Credentials":`},
		{"envelope duplicate", `"credentials":`, `"credentials":[],"credentials":`},
		{"credential case", `"enabled":false`, `"Enabled":false`},
		{"credential duplicate", `"enabled":false`, `"enabled":false,"enabled":true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := configured(t)
			publishCatalog(t, a, []catalog.Credential{{Ref: policy.CredentialRef("a"), Source: "ai-providers", Provider: "fixture", Enabled: false, Models: []string{"alias"}}})
			s := a.currentCatalogStore()
			p, err := s.Current()
			if err != nil {
				t.Fatal(err)
			}
			p.Revision++
			raw, err := json.Marshal(p)
			if err != nil {
				t.Fatal(err)
			}
			body := strings.Replace(string(raw), tc.from, tc.to, 1)
			r := httptest.NewRequest("PUT", BasePath+"/model-catalog", strings.NewReader(body))
			r.Header.Set("If-Match", "1")
			r.Header.Set("Idempotency-Key", tc.name)
			w := httptest.NewRecorder()
			a.serveManagement(w, r, "")
			if w.Code != 400 || !strings.Contains(w.Body.String(), `"invalid_model_catalog"`) {
				t.Fatalf("invalid catalog returned %d: %s", w.Code, w.Body.String())
			}
			e, err := s.Engine()
			if err != nil {
				t.Fatal(err)
			}
			if allowed, err := e.CanRoute(policy.Rule{}, "alias"); err != nil || allowed || s.Health().Revision != 1 {
				t.Fatal("rejected synchronization changed active visibility", allowed, err, s.Health())
			}
		})
	}
}
