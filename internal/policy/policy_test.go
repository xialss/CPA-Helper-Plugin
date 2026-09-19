package policy

import (
	"encoding/json"
	"testing"
)

func TestJSONContract(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `{"enabled":false,"id":"x","group_ids":[],"max_concurrency":0}`, `{"enabled":false,"id":"x","group_ids":[],"max_concurrency":0,"rule":null}`, `{"enabled":false,"enabled":true,"id":"x","group_ids":[],"max_concurrency":0,"rule":{}}`, `{"enabled":false,"id":"x","group_ids":[],"max_concurrency":0,"rule":{"models":null}}`} {
		var k Key
		if err := json.Unmarshal([]byte(raw), &k); err == nil {
			t.Fatalf("accepted incomplete key: %s", raw)
		}
	}
	var k Key
	if err := json.Unmarshal([]byte(`{"enabled":false,"id":"x","group_ids":[],"max_concurrency":0,"rule":{}}`), &k); err != nil {
		t.Fatal(err)
	}
}

func TestJSONFieldNamesAreExact(t *testing.T) {
	for _, raw := range []string{
		`{"denied_models":["secret"],"DENIED_MODELS":[]}`,
		`{"MODELS":[]}`,
		`{"credential_ids":[],"Credential_IDs":[]}`,
	} {
		var rule Rule
		if err := json.Unmarshal([]byte(raw), &rule); err == nil {
			t.Fatalf("accepted non-exact policy field: %s", raw)
		}
	}
	var rule Rule
	if err := json.Unmarshal([]byte(`{"denied_models":["secret"]}`), &rule); err != nil {
		t.Fatal(err)
	}
}

func TestOptionalGroupNote(t *testing.T) {
	for _, tc := range []struct {
		raw, note string
		valid     bool
	}{
		{`{"id":"g","name":"Name","rule":{}}`, "", true},
		{`{"id":"g","name":"Name","note":"","rule":{}}`, "", true},
		{`{"id":"g","name":"Name","note":"Independent note","rule":{}}`, "Independent note", true},
		{`{"id":"g","name":"Name","note":null,"rule":{}}`, "", false},
		{`{"id":"g","name":"Name","note":12,"rule":{}}`, "", false},
	} {
		var group Group
		err := json.Unmarshal([]byte(tc.raw), &group)
		if (err == nil) != tc.valid {
			t.Fatalf("valid=%v, error=%v", tc.valid, err)
		}
		if !tc.valid {
			continue
		}
		encoded, err := json.Marshal(group)
		if err != nil {
			t.Fatal(err)
		}
		var restored Group
		if err := json.Unmarshal(encoded, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.Note != tc.note || restored.Name != "Name" {
			t.Fatal("note round trip changed name or note")
		}
	}
}

func TestRuleComposition(t *testing.T) {
	id := CallerScope("test-key")
	s := Initial()
	s.Groups = []Group{{ID: "g1", Name: "First", Rule: Rule{Models: []string{"a", "b"}, CredentialProviders: []Selector{{"auth-files", "codex"}}}}, {ID: "g2", Name: "Second", Rule: Rule{Models: []string{"c"}, DeniedModels: []string{"b"}}}}
	s.Keys = []Key{{ID: id, Enabled: true, GroupIDs: []string{"g1", "g2"}, Rule: Rule{DeniedCredentialIDs: []string{CredentialRef("denied")}}}}
	e, err := Compile(s)
	if err != nil {
		t.Fatal(err)
	}
	k := e.Resolve(id)
	for _, tc := range []struct {
		name      string
		got, want bool
	}{{"union", k.Rule.AllowsModel("c"), true}, {"case", k.Rule.AllowsModel("A"), true}, {"deny wins", k.Rule.AllowsModel("b"), false}, {"not listed", k.Rule.AllowsModel("d"), false}, {"new category member", k.Rule.AllowsCredential(CredentialRef("new"), "auth-files", "codex"), true}, {"individual deny", k.Rule.AllowsCredential(CredentialRef("denied"), "auth-files", "codex"), false}, {"different source", k.Rule.AllowsCredential(CredentialRef("new"), "ai-providers", "codex"), false}, {"unknown key", e.Resolve(CallerScope("other")).Rule.AllowsModel("anything"), true}} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("got %v want %v", tc.got, tc.want)
			}
		})
	}
}

func TestValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Snapshot)
	}{
		{"version", func(s *Snapshot) { s.ContractVersion = "v2" }},
		{"zero revision", func(s *Snapshot) { s.Revision = 0 }},
		{"unsafe revision", func(s *Snapshot) { s.Revision = 9007199254740992 }},
		{"missing collections", func(s *Snapshot) { s.Keys = nil }},
		{"raw key", func(s *Snapshot) { s.Keys = []Key{{ID: "sk-secret", GroupIDs: []string{}}} }},
		{"duplicate key", func(s *Snapshot) { k := Key{ID: CallerScope("a"), GroupIDs: []string{}}; s.Keys = []Key{k, k} }},
		{"missing binding", func(s *Snapshot) { s.Keys = []Key{{ID: CallerScope("a"), GroupIDs: []string{"missing"}}} }},
		{"negative concurrency", func(s *Snapshot) { s.Keys = []Key{{ID: CallerScope("a"), GroupIDs: []string{}, MaxConcurrency: -1}} }},
		{"duplicate group", func(s *Snapshot) { g := Group{ID: "g", Name: "G"}; s.Groups = []Group{g, g} }},
		{"ambiguous selection", func(s *Snapshot) {
			s.Groups = []Group{{ID: "g", Name: "G", Rule: Rule{Models: []string{"a"}, DeniedModels: []string{"A"}}}}
		}},
		{"raw credential", func(s *Snapshot) {
			s.Groups = []Group{{ID: "g", Name: "G", Rule: Rule{CredentialIDs: []string{"upstream-secret"}}}}
		}},
		{"unknown category", func(s *Snapshot) {
			s.Groups = []Group{{ID: "g", Name: "G", Rule: Rule{CredentialProviders: []Selector{{"unknown", "codex"}}}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := Initial()
			tc.change(&s)
			if _, err := Compile(s); err == nil {
				t.Fatal("accepted invalid policy")
			}
		})
	}
}

func TestEmptyAllowlistAndUnknownCategory(t *testing.T) {
	r := Rule{DeniedModels: []string{"bad"}, DeniedCredentialProviders: []Selector{{"auth-files", "codex"}}}
	if !r.AllowsModel("good") || r.AllowsModel("bad") {
		t.Fatal("empty allowlist semantics")
	}
	if r.AllowsCredential(CredentialRef("x"), "", "codex") {
		t.Fatal("unknown source bypassed deny")
	}
	if !r.AllowsCredential(CredentialRef("x"), "ai-providers", "codex") {
		t.Fatal("known different source denied")
	}
}
