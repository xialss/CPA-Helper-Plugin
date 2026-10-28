package policy

import (
	"encoding/json"
	"testing"
)

func TestClaudeCacheTTLPolicy(t *testing.T) {
	for _, ttl := range []string{"", "5m", "1h", "5M", "30m", " 1h"} {
		s := Initial()
		s.Keys = []Key{{ID: CallerScope("fixture"), Enabled: true, GroupIDs: []string{}, ClaudeCacheTTL: ttl}}
		e, err := Compile(s)
		valid := ttl == "" || ttl == "5m" || ttl == "1h"
		if (err == nil) != valid {
			t.Fatalf("TTL=%q: %v", ttl, err)
		}
		if valid && e.Resolve(s.Keys[0].ID).ClaudeCacheTTL != ttl {
			t.Fatal("TTL lost during compile")
		}
	}
	for _, field := range []string{`null`, `7`, `true`} {
		var key Key
		if json.Unmarshal([]byte(`{"id":"x","enabled":true,"group_ids":[],"max_concurrency":0,"rule":{},"claude_cache_ttl":`+field+`}`), &key) == nil {
			t.Fatalf("accepted TTL %s", field)
		}
	}
}
