package plugin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestRemovedClaudeTTLLegacyConfiguration(t *testing.T) {
	for _, legacy := range []string{
		"",
		"claude_cache_ttl:\n  enabled: true\n  host_config_path: missing.yaml\n  delegate_builtin: obsolete\n",
		"claude_cache_ttl:\n  removed_option: ignored\n",
	} {
		t.Run(legacy, func(t *testing.T) {
			a := configured(t)
			// A damaged historical metadata path must have no effect on startup.
			path := filepath.Join(a.stateDir, "ttl-metadata")
			const sentinel = "inert historical metadata"
			if err := os.WriteFile(path, []byte(sentinel), 0600); err != nil {
				t.Fatal(err)
			}
			req := lifecycle{SchemaVersion: 8, ConfigYAML: []byte("state_dir: " + filepath.ToSlash(a.stateDir) + "\n" + legacy)}
			for _, method := range []string{pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure} {
				env := invoke(t, a, method, req)
				var reg registration
				if !env.OK || json.Unmarshal(env.Result, &reg) != nil {
					t.Fatal("legacy configuration blocked startup", env)
				}
				if reg.Capabilities["usage_plugin"] || reg.SchemaVersion != 7 || !reg.Capabilities["request_interceptor_omit_body_before"] || !reg.Capabilities["request_interceptor_omit_body_after"] {
					t.Fatal("removed usage hook or changed schema/body contract", reg)
				}
				for _, field := range reg.Metadata.ConfigFields {
					if field.Name == "claude_cache_ttl" {
						t.Fatal("removed configuration is advertised")
					}
				}
			}
			if raw, err := os.ReadFile(path); err != nil || string(raw) != sentinel {
				t.Fatal("historical TTL metadata was touched", err)
			}
		})
	}
}

func TestRemovedClaudeTTLSurfaces(t *testing.T) {
	a := configured(t)
	env := invoke(t, a, pluginabi.MethodUsageHandle, pluginapi.UsageRecord{})
	if env.OK || env.Error == nil || env.Error.HTTPStatus != 404 {
		t.Fatal("usage callback is still handled", env)
	}
	for _, path := range []string{"/capabilities", "/health", "/policy"} {
		w := httptest.NewRecorder()
		a.serveManagement(w, httptest.NewRequest("GET", BasePath+path, nil), "")
		if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("claude_cache_ttl")) {
			t.Fatalf("removed TTL state in %s: %d %s", path, w.Code, w.Body)
		}
	}
	for _, route := range [][2]string{{"GET", "/claude-ttl/requirements"}, {"POST", "/claude-ttl/executions"}} {
		w := httptest.NewRecorder()
		a.serveManagement(w, httptest.NewRequest(route[0], BasePath+route[1], bytes.NewBufferString(`{"execution_ids":[]}`)), "")
		if w.Code != 404 {
			t.Fatal("removed TTL route is still handled", route, w.Code)
		}
	}
	raw, err := json.Marshal(managementRegistration())
	if err != nil || bytes.Contains(raw, []byte("claude-ttl")) {
		t.Fatalf("removed TTL route is registered: %s, %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(a.stateDir, "ttl-metadata")); !os.IsNotExist(err) {
		t.Fatal("metadata storage was created", err)
	}
}

func TestRemovedClaudeTTLLeavesRequestsAndSchedulerUntouched(t *testing.T) {
	a := configured(t)
	scope := policy.CallerScope("fixture-key")
	setPolicy(t, a, policy.Key{ID: scope, Enabled: true, GroupIDs: []string{}, MaxConcurrency: 1})
	req := pluginapi.RequestInterceptRequest{
		RequestID: "request", SourceFormat: "claude", ToFormat: "claude", Model: "model",
		Headers:  http.Header{"X-CPA-Helper-Claude-Ttl": {"1h"}, "X-CPA-Helper-Ttl-Request": {"old-marker"}},
		Body:     []byte(`{"cache_control":{"type":"ephemeral","ttl":"5m"}}`),
		Metadata: map[string]any{"caller_scope": scope, "generate": true},
	}
	for _, method := range []string{pluginabi.MethodRequestInterceptBefore, pluginabi.MethodRequestInterceptAfter} {
		response := interception(t, invoke(t, a, method, req))
		if !reflect.DeepEqual(response, pluginapi.RequestInterceptResponse{}) {
			t.Fatalf("request mutated or blocked by removed TTL: %+v", response)
		}
	}
	selection := pluginapi.SchedulerPickRequest{
		Model: "model", Candidates: []pluginapi.SchedulerAuthCandidate{{ID: "fixture-upstream", Provider: "claude"}},
		Options: pluginapi.SchedulerOptions{Metadata: req.Metadata, Headers: req.Headers},
	}
	var picked pluginapi.SchedulerPickResponse
	env := invoke(t, a, pluginabi.MethodSchedulerPick, selection)
	if !env.OK || json.Unmarshal(env.Result, &picked) != nil || !reflect.DeepEqual(picked, pluginapi.SchedulerPickResponse{}) {
		t.Fatal("unrestricted scheduling must remain with CPA", env, picked)
	}
	invoke(t, a, pluginabi.MethodRequestComplete, pluginapi.RequestCompletion{RequestID: req.RequestID})
	if len(a.runtime.Counts()) != 0 {
		t.Fatal("completion did not release concurrency")
	}
	// No frozen request or scheduler identity is required for the unused after hook.
	req.RequestID = "after-only"
	if response := interception(t, invoke(t, a, pluginabi.MethodRequestInterceptAfter, req)); response.Terminate {
		t.Fatal("after hook depends on deleted TTL state", response)
	}
}
