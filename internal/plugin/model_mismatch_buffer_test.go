package plugin

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func bufferedChunk(t *testing.T, a *App, id, body string) pluginapi.StreamChunkInterceptResponse {
	t.Helper()
	raw, err := a.interceptStreamModel(pluginapi.StreamChunkInterceptRequest{RequestID: id, SourceFormat: "openai-response", Body: []byte(body)})
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Result pluginapi.StreamChunkInterceptResponse `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Result
}

func TestResponsesDoNotReleaseEarlyTools(t *testing.T) {
	for _, initialModel := range []string{"", "client-model"} {
		a := mismatchApp(t)
		beginCheck(t, a, "r", "request", true)
		rawModel(t, a, "request", `{"model":"`+initialModel+`"}`)
		for _, frame := range []string{
			"data: {\"type\":\"response.output_text.delta\",\"delta\":\"private-text\"}\n\n",
			"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"function_call\",\"name\":\"apply_patch\",\"arguments\":\"private-tool\"}}\n\n",
		} {
			out := bufferedChunk(t, a, "r", frame)
			if !out.DropChunk || len(out.Body) != 0 {
				t.Fatalf("early output escaped: %+v", out)
			}
		}
		rawModel(t, a, "request", `{"type":"response.completed","response":{"model":"wrong"}}`)
		out := bufferedChunk(t, a, "r", "data: {\"type\":\"response.completed\"}\n\n")
		if !strings.Contains(string(out.Body), "upstream_model_mismatch") || strings.Contains(string(out.Body), "private-") {
			t.Fatalf("bad rejection: %s", out.Body)
		}
		if a.modelMismatchStates["r"].ResponseBuffer.Len() != 0 {
			t.Fatal("rejection retained content")
		}
		if !bufferedChunk(t, a, "r", "late").DropChunk {
			t.Fatal("output resumed")
		}
	}
}

func TestResponsesReleaseOnlyVerifiedTerminal(t *testing.T) {
	for _, unknown := range []string{"pass", "reject"} {
		for _, actual := range []string{"client-model", ""} {
			a := mismatchApp(t)
			a.modelMismatch.UnknownAction = unknown
			beginCheck(t, a, "r", "request", true)
			prefix := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"saved\"}\r\n\r\n"
			if !bufferedChunk(t, a, "r", prefix).DropChunk {
				t.Fatal("early release")
			}
			rawModel(t, a, "request", `{"response":{"model":"`+actual+`"}}`)
			terminal := "event: response.completed\r\ndata: {\"type\":\"response.completed\"}\r\n\r\n"
			for i := 0; i < len(terminal)-1; i++ {
				if !bufferedChunk(t, a, "r", terminal[i:i+1]).DropChunk {
					t.Fatal("partial terminal released")
				}
			}
			out := bufferedChunk(t, a, "r", terminal[len(terminal)-1:])
			if actual == "" && unknown == "reject" {
				if !strings.Contains(string(out.Body), "response_model_unverifiable") || strings.Contains(string(out.Body), "saved") {
					t.Fatal(string(out.Body))
				}
			} else if string(out.Body) != prefix+terminal {
				t.Fatalf("reordered or lost content: %s", out.Body)
			}
			if !bufferedChunk(t, a, "r", "late").DropChunk {
				t.Fatal("post-terminal output escaped")
			}
		}
	}
}

func TestResponsesBufferPreservesFailureAndDiscardsOnCancel(t *testing.T) {
	a := mismatchApp(t)
	beginCheck(t, a, "r", "request", true)
	bufferedChunk(t, a, "r", "data: {\"type\":\"response.output_item.done\",\"item\":{\"name\":\"private-tool\"}}\n\n")
	failure := "event: response.failed\ndata: {\"type\":\"response.failed\",\"response\":{\"error\":{\"code\":\"upstream_failure\"}}}\n\n"
	out := bufferedChunk(t, a, "r", failure)
	if string(out.Body) != failure {
		t.Fatalf("failure changed or content leaked: %s", out.Body)
	}
	a.completeModelMismatch("r")
	beginCheck(t, a, "cancel", "cancel-request", true)
	bufferedChunk(t, a, "cancel", "data: {\"type\":\"response.output_text.delta\"}\n\n")
	a.completeModelMismatch("cancel")
	if a.modelMismatchActive() != 0 {
		t.Fatal("cancel retained buffer")
	}
}

func TestNativeCodexDataLinesWithoutDelimiters(t *testing.T) {
	a := mismatchApp(t)
	beginCheck(t, a, "r", "request", true)
	rawModel(t, a, "request", `{"response":{"model":"client-model"}}`)
	bufferedChunk(t, a, "r", "event: response.output_text.delta")
	if !bufferedChunk(t, a, "r", `data: {"type":"response.output_text.delta","delta":"saved"}`).DropChunk {
		t.Fatal("early release")
	}
	out := bufferedChunk(t, a, "r", `data: {"type":"response.completed"}`)
	if !strings.Contains(string(out.Body), "response.output_text.delta") || !strings.Contains(string(out.Body), "response.completed") {
		t.Fatalf("lost native frame boundaries: %s", out.Body)
	}
}

func TestChatTranslatedEventsWithoutDelimiters(t *testing.T) {
	a := mismatchApp(t)
	beginCheck(t, a, "r", "request", true)
	rawModel(t, a, "request", `{"model":"client-model"}`)
	prefix := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"saved\"}"
	if !bufferedChunk(t, a, "r", prefix).DropChunk {
		t.Fatal("early release")
	}
	terminal := "event: response.completed\ndata: {\"type\":\"response.completed\"}"
	out := bufferedChunk(t, a, "r", terminal)
	if !strings.Contains(string(out.Body), prefix) || !strings.Contains(string(out.Body), terminal) {
		t.Fatalf("lost translated event boundaries: %s", out.Body)
	}
}

func TestResponsesBufferLimitFailsClosed(t *testing.T) {
	a := mismatchApp(t)
	a.modelMismatch.MaxBufferBytes = 1024 * 1024
	beginCheck(t, a, "r", "request", true)
	out := bufferedChunk(t, a, "r", strings.Repeat("x", 1024*1024+1))
	if !strings.Contains(string(out.Body), "response_model_buffer_limit") || strings.Contains(string(out.Body), "xxx") {
		t.Fatalf("buffer limit did not fail closed: %s", out.Body)
	}
}

func TestResponsesTotalBufferLimitAndCompletionAccounting(t *testing.T) {
	a := mismatchApp(t)
	a.modelMismatch.MaxBufferBytes = 2 * 1024 * 1024
	a.modelMismatch.MaxTotalBufferBytes = 2 * 1024 * 1024
	beginCheck(t, a, "one", "one-request", true)
	beginCheck(t, a, "two", "two-request", true)
	first := bufferedChunk(t, a, "one", strings.Repeat("x", 1024*1024+1))
	if !first.DropChunk || a.responseBufferBytes == 0 {
		t.Fatal("first response was not retained")
	}
	second := bufferedChunk(t, a, "two", strings.Repeat("y", 1024*1024))
	if !strings.Contains(string(second.Body), "response_model_buffer_limit") {
		t.Fatalf("total limit did not fail closed: %s", second.Body)
	}
	a.completeModelMismatch("one")
	if a.responseBufferBytes != 0 {
		t.Fatalf("completion retained %d buffered bytes", a.responseBufferBytes)
	}
}
