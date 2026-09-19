package policy

import (
	"strings"
	"testing"
)

func TestModelDenialSources(t *testing.T) {
	s := Initial()
	s.Groups = []Group{{ID: "g", Name: "受限组", Rule: Rule{Models: []string{"allowed"}, DeniedModels: []string{"blocked"}}}}
	s.Keys = []Key{{ID: CallerScope("key"), Enabled: true, GroupIDs: []string{"g"}, Rule: Rule{Models: []string{"key-allowed"}, DeniedModels: []string{"own-blocked", "blocked"}}}}
	e, err := Compile(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ model, want string }{
		{"allowed", ""}, {"key-allowed", ""}, {"BLOCKED", "受限组"}, {"own-blocked", "独立规则"}, {"outside", "合并后的模型允许列表"},
	} {
		got := e.ModelDenial(s.Keys[0].ID, tc.model)
		if (tc.want == "" && got != "") || (tc.want != "" && !strings.Contains(got, tc.want)) {
			t.Fatalf("%s: %s", tc.model, got)
		}
	}
	if got := e.ModelDenial(s.Keys[0].ID, "blocked"); !strings.Contains(got, "独立规则") || !strings.Contains(got, "受限组") {
		t.Fatal(got)
	}
	// Compiled explanations must not observe later changes to the input snapshot.
	s.Groups[0].Rule.DeniedModels[0] = "changed"
	if !strings.Contains(e.ModelDenial(s.Keys[0].ID, "blocked"), "受限组") {
		t.Fatal("mutable explanation")
	}
}
