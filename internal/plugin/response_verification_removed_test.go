package plugin

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

func TestRemovedResponseVerificationLegacyConfiguration(t *testing.T) {
	for _, legacy := range []string{
		"",
		"response_model_mismatch:\n  enabled: true\n  action: reject\n  unknown_action: reject\n  max_buffer_bytes: 268435456\n",
		"response_model_mismatch:\n  removed_option: ignored\n  max_buffer_bytes: -1\n",
	} {
		t.Run(legacy, func(t *testing.T) {
			a := New(nil)
			req := lifecycle{SchemaVersion: pluginabi.SchemaVersion, ConfigYAML: []byte("state_dir: " + filepath.ToSlash(filepath.Join(t.TempDir(), "state")) + "\n" + legacy)}
			for _, method := range []string{pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure} {
				env := invoke(t, a, method, req)
				if !env.OK {
					t.Fatal(method, env.Error)
				}
				var reg registration
				if err := json.Unmarshal(env.Result, &reg); err != nil {
					t.Fatal(err)
				}
				for _, hook := range []string{"response_stream_interceptor", "response_before_translator"} {
					if reg.Capabilities[hook] {
						t.Fatalf("legacy config restored %s", hook)
					}
				}
				if !reg.Capabilities["request_interceptor_omit_body_before"] || !reg.Capabilities["request_interceptor_omit_body_after"] {
					t.Fatal("unused request bodies are still requested")
				}
				for _, field := range reg.Metadata.ConfigFields {
					if field.Name == "response_model_mismatch" {
						t.Fatal("removed setting is advertised")
					}
				}
			}
		})
	}
}

func TestRemovedResponseVerificationSurfaces(t *testing.T) {
	a := configured(t)
	for _, method := range []string{pluginabi.MethodResponseNormalizeBefore, pluginabi.MethodResponseInterceptStreamChunk} {
		env := invoke(t, a, method, struct{}{})
		if env.OK || env.Error == nil || env.Error.HTTPStatus != 404 {
			t.Fatal("removed response method still handled", method, env)
		}
	}
	for _, path := range []string{"/capabilities", "/health"} {
		w := httptest.NewRecorder()
		a.serveManagement(w, httptest.NewRequest("GET", BasePath+path, nil), "")
		if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("response_model")) {
			t.Fatalf("removed verification state in %s: %d %s", path, w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	a.serveManagement(w, httptest.NewRequest("POST", BasePath+"/response-model/validate", bytes.NewBufferString(`{"enabled":true}`)), "")
	if w.Code != 404 {
		t.Fatal("removed validation route is still handled", w.Code)
	}
	raw, err := json.Marshal(managementRegistration())
	if err != nil || bytes.Contains(raw, []byte("/response-model/")) {
		t.Fatalf("removed validation route is still registered: %s, %v", raw, err)
	}
}
