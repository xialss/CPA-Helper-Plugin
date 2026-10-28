package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

func TestRegisteredCredentialModelsUseActualIDsAndRedact(t *testing.T) {
	a := configured(t)
	a.host = func(method string, payload any) (json.RawMessage, error) {
		if method != pluginabi.MethodHostAuthModels || payload.(map[string]string)["host_callback_id"] != "management-context" {
			t.Fatal("lost official callback context", method, payload)
		}
		return json.RawMessage(`{"credentials":[{"id":"secret-config-id","auth_index":"shared","provider":"Codex","models":["team/alias","shared"]},{"id":"secret-file.json","auth_index":"shared","provider":"claude","models":[]}]}`), nil
	}
	items, err := a.registeredCredentialModels("management-context")
	if err != nil || len(items) != 2 {
		t.Fatal(items, err)
	}
	raw, _ := json.Marshal(items)
	if bytes.Contains(raw, []byte("secret")) || !bytes.Contains(raw, []byte(policy.CredentialRef("secret-config-id"))) {
		t.Fatal("raw identity leaked or real ID lost", string(raw))
	}
	if len(a.runtime.Counts()) != 0 {
		t.Fatal("directory read reserved concurrency")
	}
}

func TestRegisteredCredentialModelsFailWholeManagementRead(t *testing.T) {
	for _, raw := range []string{`{`, `{}`, `{"credentials":null}`, `{"credentials":[{"id":"a","auth_index":"i","provider":"codex","models":null}]}`, `{"credentials":[{"id":"a","auth_index":"i","provider":"codex","models":["bad\nmodel"]}]}`, `{"credentials":[{"id":"a","auth_index":"i","provider":"codex","models":[]},{"id":"a","auth_index":"other","provider":"codex","models":[]}]}`} {
		a := configured(t)
		a.host = func(string, any) (json.RawMessage, error) { return json.RawMessage(raw), nil }
		w := httptest.NewRecorder()
		a.serveManagement(w, httptest.NewRequest("GET", BasePath+"/credential-models", nil), "test")
		if w.Code != 502 || bytes.Contains(w.Body.Bytes(), []byte(`"credentials"`)) {
			t.Fatal("partial/invalid read accepted", w.Code, w.Body.String())
		}
	}
	a := configured(t)
	a.host = func(string, any) (json.RawMessage, error) { return nil, errors.New("secret-host-detail") }
	if _, err := a.registeredCredentialModels("test"); err == nil || bytes.Contains([]byte(err.Error()), []byte("secret")) {
		t.Fatal("host error hidden or leaked", err)
	}
	a.host = func(string, any) (json.RawMessage, error) { return json.RawMessage(`{"credentials":[]}`), nil }
	if items, err := a.registeredCredentialModels("test"); err != nil || items == nil || len(items) != 0 {
		t.Fatal("valid empty inventory rejected", items, err)
	}
}
