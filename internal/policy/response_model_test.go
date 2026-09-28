package policy

import "testing"

func TestResponseModelComparison(t *testing.T) {
	c := DefaultResponseModelConfig()
	if !c.ResponsesOnlyStream || c.MaxBufferBytes != 16*1024*1024 || c.MaxTotalBufferBytes != 64*1024*1024 {
		t.Fatalf("unexpected response buffering defaults: %+v", c)
	}
	c.Accepted = map[string][]string{"alias": {"upstream"}}
	for _, tc := range []struct {
		requested, actual string
		want              bool
	}{
		{"model", "model", true}, {" MODEL(high) ", "model", true}, {"models/model", "model", true},
		{"model", "model-2026", false}, {"alias", "upstream", true}, {"upstream", "alias", false},
		{"", "model", false}, {"model", "", false},
	} {
		if got := c.Matches(tc.requested, tc.actual); got != tc.want {
			t.Fatalf("%q vs %q: %v", tc.requested, tc.actual, got)
		}
	}
	c.CaseSensitive = true
	if c.Matches("MODEL", "model") {
		t.Fatal("case ignored")
	}
	c.Accepted = map[string][]string{"alias": {}}
	if c.Validate() == nil {
		t.Fatal("empty mapping accepted")
	}
	c = DefaultResponseModelConfig()
	c.MaxTotalBufferBytes = c.MaxBufferBytes - 1
	if c.Validate() == nil {
		t.Fatal("invalid total buffer limit accepted")
	}
}
