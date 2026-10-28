package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"cpa-helper-plugin/internal/plugin"
	"cpa-helper-plugin/internal/policy"
	"cpa-helper-plugin/internal/snapshot"
)

func TestClaudeUniformTTLProduction(t *testing.T) {
	binary, library := os.Getenv("CPA_BINARY"), os.Getenv("CPA_PLUGIN_BINARY")
	if binary == "" || library == "" {
		t.Skip("CPA_BINARY and CPA_PLUGIN_BINARY required for production DLL TTL fixture")
	}
	for _, profile := range []string{"late-override", "cli-late-override"} {
		t.Run(profile, func(t *testing.T) {
			f := startTTLFixtureMode(t, binary, library, profile, true, true)
			anonymous, err := f.client.Post(f.base+plugin.BasePath+"/claude-ttl/executions", "application/json", bytes.NewBufferString(`{"execution_ids":["e229118e-a1be-4fa4-93ea-7b003d5b9761"]}`))
			if err != nil {
				t.Fatal(err)
			}
			_ = anonymous.Body.Close()
			if anonymous.StatusCode != 401 {
				t.Fatalf("TTL metadata route anonymous status=%d", anonymous.StatusCode)
			}
			for _, body := range []string{`"messages":[{"role":"user","content":"fixture"}]`, uniformTTLBody(""), uniformTTLBody("5m"), uniformTTLBody("1h"), `"prompt_cache_options":{"mode":"explicit"},"messages":[{"role":"user","content":"fixture"}]`, `"prompt_cache_options":{"mode":"explicit"},` + uniformTTLBody("5m"), `"prompt_cache_options":{"mode":"explicit"},` + uniformTTLBody("1h")} {
				for _, ttl := range []string{"5m", "1h"} {
					for _, stream := range []bool{false, true} {
						for _, path := range []string{"/v1/messages", "/v1/chat/completions"} {
							// Chat input has no Claude cache-control surface; exercise native explicit cases separately.
							if path != "/v1/messages" && body != `"messages":[{"role":"user","content":"fixture"}]` {
								continue
							}
							status, response := f.call(t, path, ttl, "spoof", []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":16,"stream":%t,%s}`, stream, body)))
							if status != 200 {
								t.Fatalf("%s ttl=%s stream=%t: %d %s", path, ttl, stream, status, response)
							}
							wire := f.take(t)
							if len(wire.rootControl) != 0 {
								t.Fatalf("root cache_control was created for a root-free input: %s", wire.rootControl)
							}
							empty := bytes.Contains([]byte(body), []byte(`"mode":"explicit"`)) && !bytes.Contains([]byte(body), []byte(`"cache_control"`))
							verifyUniformWire(t, wire, ttl, empty)
							rows := f.productionQueue(t, 1)
							want := float64(120)
							if empty {
								want = 0
							}
							if tokens, ok := rows[0]["tokens"].(map[string]any); !ok || tokens["cache_creation_tokens"] != want {
								t.Fatalf("authoritative queue totals changed: %v", rows)
							}
							f.verifyProductionMetadata(t, rows, map[string]string{policy.CallerScope("fixture-ttl-" + ttl): ttl})
						}
					}
				}
			}
			var wg sync.WaitGroup
			for i := 0; i < 12; i++ {
				for _, ttl := range []string{"5m", "1h"} {
					wg.Add(1)
					go func(ttl string) {
						defer wg.Done()
						status, response := f.call(t, "/v1/messages", ttl, "spoof", []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"identical"}]}`))
						if status != 200 {
							t.Errorf("concurrent: %d %s", status, response)
						}
					}(ttl)
				}
			}
			wg.Wait()
			for i := 0; i < 24; i++ {
				f.take(t)
			}
			f.verifyProductionMetadata(t, f.productionQueue(t, 24), map[string]string{policy.CallerScope("fixture-ttl-5m"): "5m", policy.CallerScope("fixture-ttl-1h"): "1h"})
			f.verifyNonClaude(t)
			rows := f.productionQueue(t, 4)
			ids := []string{}
			for _, row := range rows {
				ids = append(ids, row["execution_id"].(string))
			}
			var result struct {
				Records []snapshot.TTLMetadata `json:"records"`
				Missing []string               `json:"missing_execution_ids"`
			}
			f.productionAPI(t, "POST", "/claude-ttl/executions", map[string]any{"execution_ids": ids}, &result)
			if len(result.Records) != 0 || len(result.Missing) != 4 {
				t.Fatalf("non-Claude metadata: %+v", result)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cancelReq, err := http.NewRequestWithContext(ctx, "POST", f.base+"/v1/messages", bytes.NewBufferString(`{"model":"claude-sonnet-4-5","max_tokens":16,"stream":true,"messages":[{"role":"user","content":"cancel-ttl"}]}`))
			if err != nil {
				t.Fatal(err)
			}
			cancelReq.Header.Set("Authorization", "Bearer fixture-ttl-1h")
			cancelReq.Header.Set("Content-Type", "application/json")
			cancelResponse, err := f.client.Do(cancelReq)
			if err != nil {
				t.Fatal(err)
			}
			if cancelResponse.StatusCode != 200 {
				_ = cancelResponse.Body.Close()
				t.Fatalf("cancel stream status=%d", cancelResponse.StatusCode)
			}
			var one [1]byte
			if _, err = cancelResponse.Body.Read(one[:]); err != nil {
				t.Fatal(err)
			}
			verifyUniformWire(t, f.take(t), "1h", false)
			cancel()
			_ = cancelResponse.Body.Close()
			f.verifyProductionMetadata(t, f.productionQueue(t, 1), map[string]string{policy.CallerScope("fixture-ttl-1h"): "1h"})
			f.waitTTLCompletion(t)
			// Freeze across a failed attempt, policy hot update and fallback to the
			// other credential. The new request after it must pick up revision 3.
			done := make(chan int, 1)
			go func() {
				status, response := f.call(t, "/v1/messages", "1h", "spoof", []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"retry-hold-ttl"}]}`))
				if status != 200 {
					t.Errorf("retry: %d %s", status, response)
				}
				done <- status
			}()
			verifyUniformWire(t, f.take(t), "1h", false)
			var next policy.Snapshot
			f.productionAPI(t, "GET", "/policy", nil, &next)
			next.Revision++
			next.GeneratedAt = time.Now().UTC()
			for i := range next.Keys {
				if next.Keys[i].ID == policy.CallerScope("fixture-ttl-1h") {
					next.Keys[i].ClaudeCacheTTL = "5m"
				}
			}
			f.productionAPI(t, "PUT", "/policy", next, nil)
			f.holdOnce.Do(func() { close(f.holdRelease) })
			select {
			case <-done:
			case <-time.After(12 * time.Second):
				t.Fatal("retry did not finish")
			}
			verifyUniformWire(t, f.take(t), "1h", false)
			f.verifyProductionMetadata(t, f.productionQueue(t, 2), map[string]string{policy.CallerScope("fixture-ttl-1h"): "1h"})
			status, response := f.call(t, "/v1/messages", "1h", "spoof", []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"new request"}]}`))
			if status != 200 {
				t.Fatalf("new revision: %d %s", status, response)
			}
			verifyUniformWire(t, f.take(t), "5m", false)
			f.verifyProductionMetadata(t, f.productionQueue(t, 1), map[string]string{policy.CallerScope("fixture-ttl-1h"): "5m"}, 3)
			status, response = f.call(t, "/v1/messages/count_tokens", "1h", "spoof", []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"count only"}]}`))
			if status != 200 {
				t.Fatalf("count_tokens: %d %s", status, response)
			}
			select {
			case <-f.wire:
				t.Fatal("count_tokens reached synthetic generation upstream")
			default:
			}
			f.waitTTLCompletion(t)
			// Any file drift disables enforcement, including changes unrelated to TTL.
			raw, err := os.ReadFile(f.configPath)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(f.configPath, append(raw, []byte("\n# drift fixture\n")...), 0600); err != nil {
				t.Fatal(err)
			}
			status, response = f.call(t, "/v1/messages", "1h", "spoof", []byte(`{"model":"claude-sonnet-4-5","max_tokens":16,"messages":[{"role":"user","content":"drift"}]}`))
			if status != 503 || !bytes.Contains(response, []byte("claude_ttl_unavailable")) {
				t.Fatalf("drift was not rejected: %d %s", status, response)
			}
			select {
			case <-f.wire:
				t.Fatal("drift request reached upstream")
			default:
			}
		})
	}
}

func TestClaudeUniformTTLProductionTopLevel(t *testing.T) {
	binary, library := os.Getenv("CPA_BINARY"), os.Getenv("CPA_PLUGIN_BINARY")
	if binary == "" || library == "" {
		t.Skip("CPA_BINARY and CPA_PLUGIN_BINARY required for production DLL TTL fixture")
	}
	for _, profile := range []string{"late-override", "cli-late-override"} {
		t.Run(profile, func(t *testing.T) {
			f := startTTLFixtureMode(t, binary, library, profile, true, true)
			for _, tc := range []struct {
				name     string
				control  string
				explicit bool
			}{
				{"root-bare", `{"type":"ephemeral"}`, false},
				{"root-5m", `{"type":"ephemeral","ttl":"5m"}`, false},
				{"root-1h", `{"type":"ephemeral","ttl":"1h"}`, false},
				{"empty-root", `{}`, false},
				{"explicit-root-bare", `{"type":"ephemeral"}`, true},
				{"explicit-root-5m", `{"type":"ephemeral","ttl":"5m"}`, true},
				{"explicit-root-1h", `{"type":"ephemeral","ttl":"1h"}`, true},
				{"explicit-empty-root", `{}`, true},
				{"explicit-no-root", "", true},
				{"no-root", "", false},
			} {
				for _, ttl := range []string{"5m", "1h"} {
					for _, stream := range []bool{false, true} {
						t.Run(fmt.Sprintf("%s/key-%s/stream-%t", tc.name, ttl, stream), func(t *testing.T) {
							optional := ""
							if tc.control != "" {
								optional += `"cache_control":` + tc.control + ","
							}
							if tc.explicit {
								optional += `"prompt_cache_options":{"mode":"explicit"},`
							}
							body := []byte(fmt.Sprintf(`{"model":"claude-sonnet-4-5","max_tokens":16,"stream":%t,%s"messages":[{"role":"user","content":"top-level automatic caching"}]}`, stream, optional))
							status, response := f.call(t, "/v1/messages", ttl, "spoof", body)
							wire := f.take(t)
							t.Logf("TOP-LEVEL case=%s caller=%s stream=%t final_controls=%v status=%d", tc.name, ttl, stream, wire.controls, status)
							if tc.control == "" {
								if len(wire.rootControl) != 0 {
									t.Fatalf("root cache_control was created: %s", wire.rootControl)
								}
							} else {
								var root map[string]any
								if err := json.Unmarshal(wire.rootControl, &root); err != nil || root == nil {
									t.Fatalf("root cache_control was removed or malformed: %s", wire.rootControl)
								}
								if tc.control == "{}" {
									if len(root) != 0 {
										t.Fatalf("empty root cache_control became a breakpoint: %s", wire.rootControl)
									}
								} else if root["type"] != "ephemeral" || (ttl == "5m" && root["ttl"] != nil) || (ttl == "1h" && root["ttl"] != "1h") {
									t.Fatalf("root cache_control did not use the frozen Key TTL: %s", wire.rootControl)
								}
							}
							// The wire collector visits the complete body, so this assertion
							// covers automatic root controls as well as block breakpoints.
							empty := tc.explicit && (tc.control == "" || tc.control == "{}")
							verifyUniformWire(t, wire, ttl, empty)
							if status != 200 {
								t.Fatalf("top-level automatic cache TTL escaped normalization: %d %s", status, response)
							}
							rows := f.productionQueue(t, 1)
							want := float64(120)
							if empty {
								want = 0
							}
							if tokens, ok := rows[0]["tokens"].(map[string]any); !ok || tokens["cache_creation_tokens"] != want {
								t.Fatalf("top-level authoritative queue totals changed: %v", rows)
							}
							f.verifyProductionMetadata(t, rows, map[string]string{policy.CallerScope("fixture-ttl-" + ttl): ttl})
						})
					}
				}
			}
		})
	}
}

func (f *ttlFixture) waitTTLCompletion(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var health struct {
			TTL struct {
				Active int `json:"active_requests"`
			} `json:"claude_cache_ttl"`
		}
		f.productionAPI(t, "GET", "/health", nil, &health)
		if health.TTL.Active == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("TTL lifecycle did not release completed/canceled requests")
}

func (f *ttlFixture) productionAPI(t *testing.T, method, path string, body any, out any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(method, f.base+plugin.BasePath+path, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer fixture-management")
	req.Header.Set("Content-Type", "application/json")
	if method == "PUT" && path == "/policy" {
		p := body.(policy.Snapshot)
		req.Header.Set("If-Match", fmt.Sprint(p.Revision-1))
		req.Header.Set("Idempotency-Key", fmt.Sprintf("fixture-update-%d", p.Revision))
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	response, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("%s: %d %s", path, resp.StatusCode, response)
	}
	if out != nil {
		if err := json.Unmarshal(response, out); err != nil {
			t.Fatal(err)
		}
	}
}

func (f *ttlFixture) productionQueue(t *testing.T, count int) []map[string]any {
	t.Helper()
	rows := []map[string]any{}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		rows = append(rows, f.queue(t)...)
		if len(rows) >= count {
			if len(rows) != count {
				t.Fatalf("queue count=%d want=%d", len(rows), count)
			}
			return rows
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("queue count=%d want=%d", len(rows), count)
	return nil
}

func (f *ttlFixture) verifyProductionMetadata(t *testing.T, rows []map[string]any, expected map[string]string, revisions ...uint64) {
	t.Helper()
	revision := uint64(2)
	if len(revisions) > 0 {
		revision = revisions[0]
	}
	ids := []string{}
	for _, row := range rows {
		id, _ := row["execution_id"].(string)
		if !snapshot.ValidExecutionID(id) {
			t.Fatalf("invalid stock execution_id: %v", row)
		}
		ids = append(ids, id)
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		var result struct {
			Version string                 `json:"metadata_version"`
			Records []snapshot.TTLMetadata `json:"records"`
			Missing []string               `json:"missing_execution_ids"`
		}
		f.productionAPI(t, "POST", "/claude-ttl/executions", map[string]any{"execution_ids": ids}, &result)
		if len(result.Missing) == 0 {
			if result.Version != "v1" || len(result.Records) != len(ids) {
				t.Fatalf("invalid metadata batch: %+v", result)
			}
			byID := map[string]snapshot.TTLMetadata{}
			for _, m := range result.Records {
				if m.TTL != expected[m.CallerScope] || m.Source != "enforced_request" || m.PolicyRevision != revision {
					t.Fatalf("metadata mismatch: %+v", m)
				}
				byID[m.ExecutionID] = m
			}
			for _, row := range rows {
				m, exists := byID[row["execution_id"].(string)]
				if !exists {
					t.Fatal("missing exact execution join")
				}
				if rawKey, ok := row["api_key"].(string); !ok || rawKey == "" || policy.CallerScope(rawKey) != m.CallerScope {
					t.Fatal("metadata crossed callers")
				}
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("TTL metadata did not arrive for stock execution IDs")
}
