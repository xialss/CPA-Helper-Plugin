package plugin

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestDisabledKeyMessage(t *testing.T) {
	for _, tc := range []struct{ key, header, want string }{
		{"sk-abc-private-wxyz", "sk-abc-private-wxyz", "当前 API Key: 'sk-abc...wxyz' 已被禁用"},
		{"short", "short", "当前 API Key: '***' 已被禁用"},
		{"sk-abc-private-wxyz", "wrong-key", "无法显示脱敏值"},
		{"sk-abc-private-wxyz", "", "无法显示脱敏值"},
	} {
		a := configured(t)
		scope := policy.CallerScope(tc.key)
		setPolicy(t, a, policy.Key{ID: scope, Enabled: false, GroupIDs: []string{}})
		headers := http.Header{"Authorization": {"Bearer " + tc.header}}
		resp := interception(t, invoke(t, a, "request.intercept_before", pluginapi.RequestInterceptRequest{RequestID: "disabled", Model: "m", Headers: headers, Metadata: map[string]any{"caller_scope": scope}}))
		if resp.StatusCode != 401 || !strings.Contains(string(resp.ResponseBody), tc.want) || bytes.Contains(resp.ResponseBody, []byte(tc.key)) {
			t.Fatalf("unexpected disabled response: %s", resp.ResponseBody)
		}
		env := invoke(t, a, "scheduler.pick", pluginapi.SchedulerPickRequest{Model: "m", Options: pluginapi.SchedulerOptions{Headers: headers, Metadata: map[string]any{"caller_scope": scope}}})
		if env.OK || env.Error.HTTPStatus != 401 || !strings.Contains(env.Error.Message, tc.want) {
			t.Fatal(env)
		}
	}
}
