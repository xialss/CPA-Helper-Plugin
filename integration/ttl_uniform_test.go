package integration

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestClaudeUniformTTLFeasibility verifies the revised one-TTL-per-request scope.
// It intentionally uses the final override, rather than source-aware defaults.
func TestClaudeUniformTTLFeasibility(t *testing.T) {
	binary, library := os.Getenv("CPA_BINARY"), os.Getenv("CPA_TTL_PROBE_BINARY")
	if binary == "" || library == "" {
		t.Skip("CPA_BINARY and CPA_TTL_PROBE_BINARY required for diagnostic-only TTL proof")
	}
	for _, profile := range []string{"late-override", "cli-late-override"} {
		t.Run(profile, func(t *testing.T) {
			fixture := startTTLFixtureWithUsage(t, binary, library, profile, true)
			cases := []struct {
				name string
				body string
			}{
				{"auto", `"messages":[{"role":"user","content":"synthetic fixture"}]`},
				{"all-bare", uniformTTLBody("")},
				{"all-5m", uniformTTLBody("5m")},
				{"all-1h", uniformTTLBody("1h")},
				{"explicit-mode-empty", `"prompt_cache_options":{"mode":"explicit"},"messages":[{"role":"user","content":"synthetic fixture"}]`},
				{"explicit-mode-5m", `"prompt_cache_options":{"mode":"explicit"},` + uniformTTLBody("5m")},
				{"explicit-mode-1h", `"prompt_cache_options":{"mode":"explicit"},` + uniformTTLBody("1h")},
			}
			for _, tc := range cases {
				for _, ttl := range []string{"5m", "1h"} {
					for _, stream := range []bool{false, true} {
						body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":16,"stream":%t,%s}`, stream, tc.body))
						status, response := fixture.call(t, "/v1/messages", ttl, "marker", body)
						if status != 200 {
							t.Fatalf("%s caller=%s stream=%t: %d %s", tc.name, ttl, stream, status, response)
						}
						wire := fixture.take(t)
						verifyUniformWire(t, wire, ttl, tc.name == "explicit-mode-empty")
						events := fixture.waitUsage(t, 1)
						verifyUniformAccounting(t, fixture.queue(t), events, ttl, tc.name == "explicit-mode-empty")
						fixture.events(t, true)
					}
				}
			}
			for _, ttl := range []string{"5m", "1h"} {
				for _, stream := range []bool{false, true} {
					body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":16,"stream":%t,"messages":[{"role":"user","content":"synthetic fixture"}]}`, stream))
					status, response := fixture.call(t, "/v1/chat/completions", ttl, "marker", body)
					if status != 200 {
						t.Fatalf("Chat caller=%s stream=%t: %d %s", ttl, stream, status, response)
					}
					verifyUniformWire(t, fixture.take(t), ttl, false)
					events := fixture.waitUsage(t, 1)
					verifyUniformAccounting(t, fixture.queue(t), events, ttl, false)
					fixture.events(t, true)
				}
			}
			t.Log("UNIFORM 32 requests: every final cache control uses the caller TTL; explicit empty mode stays empty; official usage/queue retain only totals")
		})
	}
}

func uniformTTLBody(ttl string) string {
	control := `{"type":"ephemeral"}`
	if ttl != "" {
		control = fmt.Sprintf(`{"type":"ephemeral","ttl":%q}`, ttl)
	}
	return fmt.Sprintf(`"tools":[{"name":"fixture_tool","description":"synthetic tool","input_schema":{"type":"object","properties":{}},"cache_control":%s}],"system":[{"type":"text","text":"synthetic system","cache_control":%s}],"messages":[{"role":"user","content":[{"type":"text","text":"synthetic fixture","cache_control":%s}]}]`, control, control, control)
}

func verifyUniformWire(t *testing.T, wire ttlWire, ttl string, empty bool) {
	t.Helper()
	if empty {
		if len(wire.controls) != 0 {
			t.Fatalf("explicit empty mode gained cache breakpoints: %v", wire.controls)
		}
		return
	}
	if len(wire.controls) == 0 {
		t.Fatal("uniform request has no final cache controls")
	}
	want := ttl
	if ttl == "5m" {
		want = "bare-5m"
	}
	for _, actual := range wire.controls {
		if actual != want {
			t.Errorf("caller TTL=%s, final controls=%v", ttl, wire.controls)
		}
	}
}

func verifyUniformAccounting(t *testing.T, rows, events []map[string]any, ttl string, empty bool) {
	t.Helper()
	if len(rows) != 1 {
		t.Fatalf("expected one uniform usage queue record, got %d", len(rows))
	}
	want := float64(120)
	if empty {
		want = 0
	}
	tokens, ok := rows[0]["tokens"].(map[string]any)
	if !ok || tokens["cache_creation_tokens"] != want {
		t.Fatalf("unexpected official queue totals: %v", tokens)
	}
	for _, field := range []string{"cache_creation_ttl", "cache_creation_5m_tokens", "cache_creation_1h_tokens"} {
		if _, present := tokens[field]; present {
			t.Errorf("stock queue contract changed: %s is present", field)
		}
	}
	usageCount, rawEvidence := 0, 0
	for _, event := range events {
		if event["hook"] == "usage.handle" {
			usageCount++
			detail, ok := event["detail"].(map[string]any)
			if !ok || detail["CacheCreationTokens"] != want || event["wire_has_buckets"] != false {
				t.Errorf("unexpected uniform usage observation: %v", event)
			}
			if event["request_id"] != rows[0]["execution_id"] {
				t.Error("uniform usage ID does not match the queue execution ID")
			}
		}
		evidence, _ := event["evidence"].([]any)
		for _, entry := range evidence {
			counter, ok := entry.(map[string]any)
			if !ok {
				t.Fatal("invalid diagnostic evidence")
			}
			if _, present := counter["ephemeral_5m_input_tokens"]; !present {
				continue
			}
			rawEvidence++
			want5m, want1h := float64(0), float64(0)
			if ttl == "5m" {
				want5m = want
			} else {
				want1h = want
			}
			if counter["ephemeral_5m_input_tokens"] != want5m || counter["ephemeral_1h_input_tokens"] != want1h {
				t.Errorf("synthetic single-TTL upstream evidence changed: %v", counter)
			}
		}
	}
	if usageCount != 1 || rawEvidence == 0 {
		t.Errorf("expected one usage record and raw fixture evidence, got %d records, %d evidence items", usageCount, rawEvidence)
	}
}

// syntheticUniformUsage models a cooperative upstream for the revised proof.
// It generates synthetic evidence; production must never infer actual usage
// buckets by reusing this helper or by inspecting requested TTL settings.
func syntheticUniformUsage(controls []string) (string, error) {
	created, tokens5m, tokens1h := 120, 0, 0
	if len(controls) == 0 {
		created = 0
	} else {
		first := controls[0]
		if first != "bare-5m" && first != "5m" && first != "1h" {
			return "", fmt.Errorf("unexpected uniform fixture TTL %q", first)
		}
		for _, ttl := range controls {
			if strings.TrimPrefix(ttl, "bare-") != strings.TrimPrefix(first, "bare-") {
				return "", fmt.Errorf("uniform fixture received mixed TTL controls")
			}
		}
		if first == "1h" {
			tokens1h = created
		} else {
			tokens5m = created
		}
	}
	return fmt.Sprintf(`{"input_tokens":10,"output_tokens":2,"cache_creation_input_tokens":%d,"cache_read_input_tokens":3,"cache_creation":{"ephemeral_5m_input_tokens":%d,"ephemeral_1h_input_tokens":%d}}`, created, tokens5m, tokens1h), nil
}
