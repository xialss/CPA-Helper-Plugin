package plugin

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// Called under mismatchMu. A model can first appear (or change) at completion.
// Holding all successful events also prevents early tool execution by Codex.
func (a *App) interceptBufferedResponses(req pluginapi.StreamChunkInterceptRequest, s *modelMismatchState) ([]byte, error) {
	clearBuffer := func() {
		a.responseBufferBytes -= s.ResponseBuffer.Len()
		if a.responseBufferBytes < 0 {
			a.responseBufferBytes = 0
		}
		s.ResponseBuffer = bytes.Buffer{}
	}
	if s.Alerted || s.ResponseReleased {
		return ok(pluginapi.StreamChunkInterceptResponse{DropChunk: true})
	}
	reject := func(e *modelCheckError) ([]byte, error) {
		clearBuffer()
		s.ResponseHolding = false
		s.Alerted = true
		return ok(pluginapi.StreamChunkInterceptResponse{Body: responsesVerificationFailure(e)})
	}
	if s.Mismatch && !s.Ambiguous {
		return reject(a.modelDecision(req.RequestID, s))
	}
	atLineStart := s.ResponseScan == s.ResponseBuffer.Len()
	s.ResponseBuffer.Write(req.Body)
	a.responseBufferBytes += len(req.Body)
	if s.Config.MaxBufferBytes > 0 && (s.ResponseBuffer.Len() > s.Config.MaxBufferBytes || (s.Config.MaxTotalBufferBytes > 0 && a.responseBufferBytes > s.Config.MaxTotalBufferBytes)) {
		clearBuffer()
		s.ResponseHolding = false
		s.Terminal = true
		s.UpstreamFailed = true
		s.Alerted = true
		s.ResponseReleased = true
		a.logModelDecision(req.RequestID, s, "buffer_discarded", "response_buffer_limit", "response_buffer_limit_exceeded")
		return ok(pluginapi.StreamChunkInterceptResponse{Body: responsesBufferLimitFailure(req.RequestID)})
	}
	// CPA's native Codex translator emits complete data lines without SSE
	// delimiters. Preserve those chunk boundaries when accumulating them.
	trimmed := bytes.TrimSpace(req.Body)
	dataLine := trimmed
	if bytes.HasPrefix(trimmed, []byte("event:")) {
		if i := bytes.LastIndex(trimmed, []byte("\ndata:")); i >= 0 {
			dataLine = trimmed[i+1:]
		}
	}
	if atLineStart && bytes.HasPrefix(dataLine, []byte("data:")) && json.Valid(bytes.TrimSpace(dataLine[5:])) {
		if !bytes.HasSuffix(req.Body, []byte("\n\n")) && !bytes.HasSuffix(req.Body, []byte("\r\n\r\n")) {
			s.ResponseBuffer.WriteString("\n\n")
		}
	} else if atLineStart && !bytes.ContainsRune(req.Body, '\n') {
		for _, prefix := range []string{"event:", "id:", "retry:", ":"} {
			if bytes.HasPrefix(req.Body, []byte(prefix)) {
				s.ResponseBuffer.WriteByte('\n')
				break
			}
		}
	}
	terminal, failed, frame := responseTerminalFrame(s)
	if !terminal {
		s.ResponseHolding = true
		a.logModelDecision(req.RequestID, s, "buffering", "", "awaiting_terminal_verification")
		return ok(pluginapi.StreamChunkInterceptResponse{DropChunk: true})
	}
	s.ResponseHolding = false
	if failed {
		// Preserve the upstream failure, but never replay preceding tool calls.
		out := bytes.Clone(frame)
		clearBuffer()
		s.ResponseReleased = true
		s.Terminal = true
		s.UpstreamFailed = true
		a.logModelDecision(req.RequestID, s, "upstream_error", "", "buffered_content_discarded")
		return ok(pluginapi.StreamChunkInterceptResponse{Body: out})
	}
	if e := a.modelDecision(req.RequestID, s); e != nil {
		return reject(e)
	}
	// RPC serialization copies the bytes before this response leaves the lock.
	out := s.ResponseBuffer.Bytes()[:s.ResponseFrameStart]
	s.ResponseReleased = true
	s.Terminal = true
	result, err := ok(pluginapi.StreamChunkInterceptResponse{Body: out})
	clearBuffer()
	return result, err
}

// Incremental SSE framing handles delimiters split across chunks without
// reparsing the full accumulated response on each callback. Only a complete,
// valid terminal event permits release; EOF without one never releases content.
func responseTerminalFrame(s *modelMismatchState) (terminal, failed bool, frame []byte) {
	data := s.ResponseBuffer.Bytes()
	for s.ResponseScan < len(data) {
		lineEnd := bytes.IndexByte(data[s.ResponseScan:], '\n')
		if lineEnd < 0 {
			break
		}
		lineEnd += s.ResponseScan
		line := bytes.TrimSuffix(data[s.ResponseScan:lineEnd], []byte("\r"))
		s.ResponseScan = lineEnd + 1
		if len(line) != 0 {
			continue
		}
		frame = data[s.ResponseFrameStart:s.ResponseScan]
		s.ResponseFrameStart = s.ResponseScan
		var payload []byte
		for _, field := range bytes.Split(frame, []byte("\n")) {
			field = bytes.TrimSuffix(field, []byte("\r"))
			if bytes.HasPrefix(field, []byte("data:")) {
				if len(payload) > 0 {
					payload = append(payload, '\n')
				}
				payload = append(payload, bytes.TrimPrefix(field[5:], []byte(" "))...)
			}
		}
		var event struct {
			Type     string          `json:"type"`
			Error    json.RawMessage `json:"error"`
			Response struct {
				Error json.RawMessage `json:"error"`
			} `json:"response"`
		}
		if json.Unmarshal(payload, &event) != nil {
			continue
		}
		hasError := func(raw json.RawMessage) bool { return len(raw) > 0 && strings.TrimSpace(string(raw)) != "null" }
		if hasError(event.Error) || hasError(event.Response.Error) {
			return true, true, frame
		}
		switch event.Type {
		case "response.completed", "response.done":
			return true, false, frame
		case "response.failed", "response.error", "error", "response.incomplete":
			return true, true, frame
		}
	}
	return false, false, nil
}
