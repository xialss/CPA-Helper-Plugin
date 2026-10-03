package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestVerificationDecisionLogs(t *testing.T) {
	for _, tc := range []struct {
		name, actual, want string
		change             func(*modelMismatchConfig)
	}{
		{"blocked", "wrong", "blocked", func(c *modelMismatchConfig) {}},
		{"audit", "wrong", "audit_mismatch", func(c *modelMismatchConfig) { c.Action = "audit" }},
		{"unknown pass", "", "unknown_pass", func(c *modelMismatchConfig) {}},
		{"unknown reject", "", "blocked", func(c *modelMismatchConfig) { c.UnknownAction = "reject" }},
		{"match", "client-model", "matched", func(c *modelMismatchConfig) {}},
		{"mapping", "wrong", "matched", func(c *modelMismatchConfig) { c.Accepted = map[string][]string{"client-model": {"wrong"}} }},
		{"disabled", "wrong", "skipped", func(c *modelMismatchConfig) { c.Enabled = false }},
		{"stream disabled", "wrong", "skipped", func(c *modelMismatchConfig) { c.StreamEnabled = false }},
		{"ignored", "wrong", "skipped", func(c *modelMismatchConfig) { c.IgnoredModels = []string{"client-model"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := mismatchApp(t)
			tc.change(&a.modelMismatch)
			var logs []hostLogRequest
			a.host = func(method string, payload any) (json.RawMessage, error) {
				if method != pluginabi.MethodHostLog {
					t.Fatalf("unexpected callback: %s", method)
				}
				logs = append(logs, payload.(hostLogRequest))
				return json.RawMessage(`{}`), nil
			}
			beginCheck(t, a, "log-request", "private-request-body", true)
			raw, _ := json.Marshal(map[string]any{"model": tc.actual, "choices": []string{"private-response-body"}})
			rawModel(t, a, "private-request-body", string(raw))
			for i := 0; i < 3; i++ {
				invoke(t, a, pluginabi.MethodResponseInterceptStreamChunk, pluginapi.StreamChunkInterceptRequest{RequestID: "log-request", SourceFormat: "openai", ChunkIndex: i})
			}
			invoke(t, a, pluginabi.MethodRequestComplete, pluginapi.RequestCompletion{RequestID: "log-request"})
			if len(logs) != 1 {
				t.Fatalf("expected one decision, got %+v", logs)
			}
			entry := logs[0]
			for _, want := range []string{"[响应核验]", "result=" + tc.want, `requested_model="client-model"`, "stream=true"} {
				if !strings.Contains(entry.Message, want) {
					t.Fatalf("missing %s: %s", want, entry.Message)
				}
			}
			if entry.Fields["plugin_id"] != ID || entry.Fields["request_id"] != "log-request" {
				t.Fatal("missing log identity")
			}
			if strings.Contains(entry.Message, "private-") {
				t.Fatal("logged request or response body")
			}
		})
	}
}

func TestVerificationLogsDoNotClaimUninspectedOutputPassed(t *testing.T) {
	a := mismatchApp(t)
	var message string
	a.host = func(_ string, payload any) (json.RawMessage, error) {
		message = payload.(hostLogRequest).Message
		return nil, nil
	}
	beginCheck(t, a, "r", "request", false)
	rawModel(t, a, "request", `{"model":"client-model"}`)
	a.completeModelMismatch("r")
	if !strings.Contains(message, "result=not_checked") {
		t.Fatal(message)
	}
}

func TestVerificationLogFailurePreservesInterception(t *testing.T) {
	a := mismatchApp(t)
	a.host = func(_ string, _ any) (json.RawMessage, error) { return nil, errors.New("unavailable") }
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	beginCheck(t, a, "r", "request", false)
	rawModel(t, a, "request", `{"model":"wrong\nforged-line"}`)
	resp := listResponse(t, a, pluginapi.ResponseInterceptRequest{RequestID: "r", RequestedModel: "client-model", StatusCode: 200})
	if !strings.Contains(string(resp.Body), "upstream_model_mismatch") || !strings.Contains(output.String(), "CPA host.log failed") || !strings.Contains(output.String(), "result=blocked") {
		t.Fatal("logging failure hid rejection or diagnostic", output.String())
	}
	if strings.Contains(output.String(), "\nforged-line") {
		t.Fatal("log injection")
	}
}
