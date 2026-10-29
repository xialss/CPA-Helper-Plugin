package plugin

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

func TestRegistrationNegotiatesHostMaximumSchema(t *testing.T) {
	for _, schema := range []uint32{0, 1, 6, 7, 8, 9, 100, 65535, ^uint32(0)} {
		for _, method := range []string{pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure} {
			t.Run(fmt.Sprintf("%s/host-%d", method, schema), func(t *testing.T) {
				a := New(nil)
				if method == pluginabi.MethodPluginReconfigure {
					a = configured(t)
				}
				previousStore := a.currentStore()
				dir := a.stateDir
				if dir == "" {
					dir = filepath.Join(t.TempDir(), "state")
				}
				// Unknown additive fields from a future host must not reject this
				// contract. This field is synthetic, not a current CPA API.
				raw, err := json.Marshal(map[string]any{"schema_version": schema, "config_yaml": []byte("state_dir: " + filepath.ToSlash(dir) + "\n"), "future_host_field": map[string]any{"enabled": true}})
				if err != nil {
					t.Fatal(err)
				}
				out, err := a.Handle(method, raw)
				if schema < 7 {
					if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("host advertised schema %d", schema)) {
						t.Fatalf("incompatible host must report its schema: %v", err)
					}
					if a.currentStore() != previousStore {
						t.Fatal("rejected negotiation changed the policy store")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				var env pluginabi.Envelope
				if err := json.Unmarshal(out, &env); err != nil || !env.OK {
					t.Fatalf("registration failed: %s, %v", out, err)
				}
				var reg registration
				if err := json.Unmarshal(env.Result, &reg); err != nil {
					t.Fatal(err)
				}
				if reg.SchemaVersion != 7 {
					t.Fatalf("plugin must declare its implemented schema, got %d", reg.SchemaVersion)
				}
				w := httptest.NewRecorder()
				a.serveManagement(w, httptest.NewRequest("GET", BasePath+"/capabilities", nil), "")
				var capabilities struct {
					SchemaVersion uint32 `json:"schema_version"`
				}
				if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &capabilities) != nil || capabilities.SchemaVersion != reg.SchemaVersion {
					t.Fatalf("management reported a different contract: %d %s", w.Code, w.Body.String())
				}
				for _, capability := range []string{"request_interceptor_omit_body_before", "request_interceptor_omit_body_after"} {
					if !reg.Capabilities[capability] {
						t.Fatalf("negotiation lost %s", capability)
					}
				}
				for _, capability := range []string{"request_interceptor_patch_body_before", "request_interceptor_patch_body_after", "request_body_patcher_before", "request_body_patcher_after", "response_stream_interceptor", "response_before_translator"} {
					if reg.Capabilities[capability] {
						t.Fatalf("negotiation enabled unused capability %s", capability)
					}
				}
			})
		}
	}
}
