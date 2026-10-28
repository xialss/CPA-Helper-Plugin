package catalog

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"cpa-helper-plugin/internal/policy"
)

func testCatalog() Snapshot {
	return Snapshot{ContractVersion: "v1", PluginVersion: policy.Version, CPAVersion: CPAVersion, Revision: 1, GeneratedAt: time.Now().UTC(), Credentials: []Credential{
		{Ref: policy.CredentialRef("a"), Source: "ai-providers", Provider: "first", Enabled: true, Models: []string{"only-a", "shared", "team/public-alias"}},
		{Ref: policy.CredentialRef("b"), Source: "auth-files", Provider: "second", Enabled: true, Models: []string{"only-b", "shared", "team/public-alias"}},
		{Ref: policy.CredentialRef("disabled"), Source: "ai-providers", Provider: "first", Models: []string{"disabled-only"}},
	}}
}

func TestCredentialModelVisibility(t *testing.T) {
	e, err := Compile(testCatalog())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		rule  policy.Rule
		model string
		want  bool
	}{
		{"exclusive allowed", policy.Rule{CredentialIDs: []string{policy.CredentialRef("a")}}, "only-a", true},
		{"exclusive forbidden", policy.Rule{CredentialIDs: []string{policy.CredentialRef("a")}}, "only-b", false},
		{"shared has allowed owner", policy.Rule{DeniedCredentialIDs: []string{policy.CredentialRef("a")}}, "shared", true},
		{"shared all owners denied", policy.Rule{DeniedCredentialIDs: []string{policy.CredentialRef("a"), policy.CredentialRef("b")}}, "shared", false},
		{"category allow", policy.Rule{CredentialProviders: []policy.Selector{{Source: "auth-files", Provider: "second"}}}, "only-b", true},
		{"category source matters", policy.Rule{CredentialProviders: []policy.Selector{{Source: "ai-providers", Provider: "second"}}}, "only-b", false},
		{"deny wins over category allow", policy.Rule{CredentialProviders: []policy.Selector{{Source: "ai-providers", Provider: "first"}}, DeniedCredentialIDs: []string{policy.CredentialRef("a")}}, "only-a", false},
		{"disabled owner", policy.Rule{}, "disabled-only", false},
		{"public prefix and alias", policy.Rule{CredentialIDs: []string{policy.CredentialRef("b")}}, "TEAM/PUBLIC-ALIAS", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.CanRoute(tc.rule, tc.model)
			if err != nil || got != tc.want {
				t.Fatalf("CanRoute = %v, %v; want %v", got, err, tc.want)
			}
		})
	}
	if _, err := e.CanRoute(policy.Rule{}, "new-host-model"); !errors.Is(err, ErrUnknownModel) {
		t.Fatal("unknown model was silently classified", err)
	}
}

func TestCatalogValidationIsComplete(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{"contract", func(s *Snapshot) { s.ContractVersion = "v2" }},
		{"host version", func(s *Snapshot) { s.CPAVersion = "v8.0.29" }},
		{"producer", func(s *Snapshot) { s.PluginVersion = "unknown" }},
		{"revision", func(s *Snapshot) { s.Revision = 0 }},
		{"timestamp", func(s *Snapshot) { s.GeneratedAt = time.Time{} }},
		{"missing credentials", func(s *Snapshot) { s.Credentials = nil }},
		{"duplicate reference", func(s *Snapshot) { s.Credentials[1].Ref = s.Credentials[0].Ref }},
		{"raw reference", func(s *Snapshot) { s.Credentials[1].Ref = "raw-secret" }},
		{"unknown source", func(s *Snapshot) { s.Credentials[1].Source = "unknown" }},
		{"unknown provider", func(s *Snapshot) { s.Credentials[1].Provider = "" }},
		{"missing models", func(s *Snapshot) { s.Credentials[1].Models = nil }},
		{"duplicate models", func(s *Snapshot) { s.Credentials[1].Models = []string{"model", "MODEL"} }},
		{"malformed model", func(s *Snapshot) { s.Credentials[1].Models = []string{"model\nsecret"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := testCatalog()
			tc.mutate(&p)
			if e, err := Compile(p); err == nil || e != nil {
				t.Fatal("partially accepted invalid catalog")
			}
		})
	}
	if _, err := Compile(Snapshot{ContractVersion: "v1", PluginVersion: policy.Version, CPAVersion: CPAVersion, Revision: 1, GeneratedAt: time.Now().UTC(), Credentials: []Credential{}}); err != nil {
		t.Fatal("empty complete directory rejected", err)
	}
}

func TestCatalogIndexDoesNotRetainMutableInput(t *testing.T) {
	p := testCatalog()
	e, err := Compile(p)
	if err != nil {
		t.Fatal(err)
	}
	p.Credentials[0].Enabled = false
	p.Credentials[0].Models[0] = "changed"
	allowed, err := e.CanRoute(policy.Rule{CredentialIDs: []string{policy.CredentialRef("a")}}, "only-a")
	if err != nil || !allowed {
		t.Fatal("caller mutation changed the published index", err)
	}
}

func TestStoredCatalogCompatibilityIsReadOnlyAndNarrow(t *testing.T) {
	for _, producer := range [][2]string{{"0.1.8", "v8.0.32"}, {"0.1.9", "v8.0.45"}} {
		t.Run(producer[0], func(t *testing.T) {
			p := testCatalog()
			p.PluginVersion, p.CPAVersion = producer[0], producer[1]
			if _, err := CompileStored(p); err != nil {
				t.Fatal(err)
			}
			if _, err := Compile(p); err == nil {
				t.Fatal("legacy snapshot accepted as a new write")
			}
			for _, mutate := range []func(*Snapshot){
				func(s *Snapshot) { s.CPAVersion = "v8.0.29" },
				func(s *Snapshot) { s.PluginVersion = "0.1.7" },
				func(s *Snapshot) { s.ContractVersion = "v2" },
				func(s *Snapshot) { s.Credentials[0].Models = nil },
			} {
				invalid := Clone(p)
				mutate(&invalid)
				if _, err := CompileStored(invalid); err == nil {
					t.Fatal("compatibility read skipped validation")
				}
			}
		})
	}
}

func TestCatalogCredentialRequiresExplicitEnabledJSON(t *testing.T) {
	fields := map[string]any{"ref": policy.CredentialRef("a"), "source": "ai-providers", "provider": "fixture", "models": []string{"alias"}}
	for _, enabled := range []any{nil, false, true} {
		if enabled == nil {
			delete(fields, "enabled")
		} else {
			fields["enabled"] = enabled
		}
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		var c Credential
		err = json.Unmarshal(raw, &c)
		if enabled == nil && err == nil || enabled != nil && (err != nil || c.Enabled != enabled) {
			t.Fatalf("enabled %v accepted incorrectly: %v", enabled, err)
		}
	}
	fields["api_key"] = "private-secret"
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	var c Credential
	if err := json.Unmarshal(raw, &c); err == nil {
		t.Fatal("nested unknown field accepted")
	}
}

func TestCatalogJSONRejectsAmbiguousFields(t *testing.T) {
	p := testCatalog()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	replace := func(from, to string) []byte {
		return []byte(strings.Replace(string(raw), from, to, 1))
	}
	for _, tc := range []struct {
		name string
		raw  []byte
	}{
		{"envelope case", replace(`"credentials":`, `"Credentials":`)},
		{"envelope duplicate", replace(`"credentials":`, `"credentials":[],"credentials":`)},
		{"credential case", replace(`"enabled":true`, `"Enabled":true`)},
		{"credential duplicate", replace(`"enabled":true`, `"enabled":false,"enabled":true`)},
		{"missing provider", replace(`"provider":"first",`, ``)},
		{"null models", replace(`"models":["only-a","shared","team/public-alias"]`, `"models":null`)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Clone(p)
			if err := json.Unmarshal(tc.raw, &got); err == nil {
				t.Fatal("ambiguous or incomplete catalog accepted")
			}
			if !reflect.DeepEqual(got, p) {
				t.Fatal("rejected JSON changed the caller's snapshot")
			}
		})
	}
}
