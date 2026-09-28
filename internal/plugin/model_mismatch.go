package plugin

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type modelMismatchConfig = policy.ResponseModelConfig

func defaultModelMismatchConfig() modelMismatchConfig { return policy.DefaultResponseModelConfig() }

type modelMismatchState struct {
	Config                                                   modelMismatchConfig
	Digest                                                   [32]byte
	Requested, Actual, LoggedCode, SkipReason                string
	SourceFormat                                             string
	Stream                                                   bool
	Observed, Ambiguous, Mismatch, Blocked, Alerted, Checked bool
	ResponseBuffer                                           bytes.Buffer
	ResponseScan                                             int
	ResponseFrameStart                                       int
	ResponseReleased, ResponseHolding                        bool
	Terminal, UpstreamFailed                                 bool
}

func (a *App) modelMismatchSettings() modelMismatchConfig {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.modelMismatch.Clone()
}

// The raw-response ABI lacks RequestID. Digests associate observations only
// with a unique live original body. No request content is retained or logged.
func (a *App) beginModelMismatch(req pluginapi.RequestInterceptRequest) {
	c := a.modelMismatchSettings()
	skip := ""
	switch {
	case !c.Enabled:
		skip = "disabled"
	case internalRequest(req.Metadata):
		skip = "internal_request"
	case req.Stream && !c.StreamEnabled:
		skip = "stream_disabled"
	case c.Ignores(req.RequestedModel):
		skip = "ignored_model"
	}
	if generate, ok := req.Metadata["generate"].(bool); ok && !generate {
		skip = "non_generation"
	}
	if skip != "" {
		c.Enabled = false
	}
	s := &modelMismatchState{Config: c, Digest: sha256.Sum256(req.Body), Requested: req.RequestedModel, Stream: req.Stream, SourceFormat: req.SourceFormat, SkipReason: skip}
	a.mismatchMu.Lock()
	defer a.mismatchMu.Unlock()
	// Completion marks the runtime first, then removes verification state.
	// Holding mismatchMu across this check prevents late state resurrection.
	if internalRequest(req.Metadata) {
		return
	}
	if !a.runtime.IsLive(req.RequestID) {
		return
	}
	if a.modelMismatchStates[req.RequestID] != nil {
		return
	}
	ids := a.mismatchRequests[s.Digest]
	if ids == nil {
		ids = map[string]bool{}
		a.mismatchRequests[s.Digest] = ids
	}
	for id := range ids {
		a.modelMismatchStates[id].Ambiguous = true
		s.Ambiguous = true
	}
	ids[req.RequestID] = true
	a.modelMismatchStates[req.RequestID] = s
}

// declaredModels reads protocol metadata, never assistant text or tool output.
func declaredModels(raw []byte) []string {
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	var out []string
	for _, field := range []string{"model", "modelVersion"} {
		var model string
		if json.Unmarshal(obj[field], &model) == nil && strings.TrimSpace(model) != "" {
			out = append(out, model)
		}
	}
	for _, field := range []string{"message", "response", "interaction"} {
		value := obj[field]
		if len(value) == 0 {
			continue
		}
		var nested map[string]json.RawMessage
		if json.Unmarshal(value, &nested) == nil {
			for _, identity := range []string{"model", "modelVersion"} {
				var model string
				if json.Unmarshal(nested[identity], &model) == nil && strings.TrimSpace(model) != "" {
					out = append(out, model)
				}
			}
		}
	}
	return out
}

func payloadModels(body []byte) []string {
	trimmed := bytes.TrimSpace(body)
	if json.Valid(trimmed) {
		return declaredModels(trimmed)
	}
	var out []string
	// CPA translation supplies complete JSON frames or complete SSE data lines.
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if bytes.HasPrefix(line, []byte("data:")) {
			out = append(out, declaredModels(bytes.TrimSpace(line[5:]))...)
		}
	}
	return out
}

func (a *App) observeResponseModel(req pluginapi.ResponseTransformRequest) {
	digest := sha256.Sum256(req.OriginalRequest)
	a.mismatchMu.Lock()
	defer a.mismatchMu.Unlock()
	ids := a.mismatchRequests[digest]
	if len(ids) != 1 {
		return
	}
	for id := range ids {
		s := a.modelMismatchStates[id]
		if !s.Config.Enabled || s.Ambiguous || s.Mismatch {
			return
		}
		models := payloadModels(req.Body)
		s.Observed = true
		for _, model := range models {
			s.Actual = model
			if s.Requested != "" && !s.Config.Matches(s.Requested, model) {
				s.Mismatch = true
				return
			}
		}
	}
}

type modelCheckError struct {
	Type      string `json:"type"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Requested string `json:"requested_model"`
	Actual    string `json:"actual_model,omitempty"`
	RequestID string `json:"request_id"`
	Reason    string `json:"reason,omitempty"`
}

// Called under mismatchMu. Rejection stays latched across chunks and reloads.
func (a *App) modelDecision(id string, s *modelMismatchState) *modelCheckError {
	if s == nil || !s.Config.Enabled {
		return nil
	}
	s.Checked = true
	reason := ""
	switch {
	case s.Ambiguous:
		reason = "ambiguous_request_identity"
	case s.Requested == "":
		reason = "requested_model_missing"
	case !s.Observed:
		reason = "raw_response_unavailable"
	case s.Actual == "":
		reason = "response_model_missing"
	}
	if !s.Blocked && !s.Mismatch && reason == "" {
		return nil
	}
	e := &modelCheckError{Type: "model_verification_error", Code: "response_model_unverifiable", Message: "无法核验上游响应模型", Requested: s.Requested, RequestID: id, Reason: reason}
	knownMismatch := s.Mismatch && !s.Ambiguous
	if knownMismatch {
		e.Type = "model_mismatch"
		e.Code = "upstream_model_mismatch"
		e.Actual = s.Actual
		e.Reason = ""
		e.Message = fmt.Sprintf("上游响应模型不一致（请求：%q；上游声明：%q），结果已拦截", s.Requested, s.Actual)
	}
	if !s.Blocked && (s.Config.Action == "audit" || (!knownMismatch && !s.Ambiguous && s.Config.UnknownAction == "pass")) {
		result := "unknown_pass"
		if s.Config.Action == "audit" {
			result = "audit_unverifiable"
		}
		if knownMismatch {
			result = "audit_mismatch"
		}
		a.logModelDecision(id, s, result, e.Code, e.Reason)
		return nil
	}
	s.Blocked = true
	a.logModelDecision(id, s, "blocked", e.Code, e.Reason)
	return e
}

func verificationBody(e *modelCheckError, format string) []byte {
	obj := map[string]any{"error": e}
	if format == "claude" {
		obj["type"] = "error"
	}
	body, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	} // String-only error schema.
	return body
}

func verificationHeaders(e *modelCheckError) http.Header {
	h := http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"no-store"}, "X-Cpa-Helper-Error": {e.Code}}
	if e.Code == "upstream_model_mismatch" {
		h.Set("X-CPA-Helper-Model-Mismatch", "true")
	}
	return h
}

func (a *App) interceptResponseModel(req pluginapi.ResponseInterceptRequest) ([]byte, error) {
	if req.Stream || req.StatusCode < 200 || req.StatusCode >= 300 || internalRequest(req.Metadata) {
		return ok(pluginapi.ResponseInterceptResponse{})
	}
	a.mismatchMu.Lock()
	defer a.mismatchMu.Unlock()
	s := a.modelMismatchStates[req.RequestID]
	if s == nil {
		c := a.modelMismatchSettings()
		if !c.Enabled {
			return ok(pluginapi.ResponseInterceptResponse{})
		}
		e := &modelCheckError{Type: "model_verification_error", Code: "response_model_unverifiable", Message: "无法核验上游响应模型", Requested: requestedModel(req.Model, req.RequestedModel), RequestID: req.RequestID, Reason: "state_missing"}
		a.verificationLog("error", req.RequestID, fmt.Sprintf("[响应核验] 请求状态缺失 result=state_missing request_id=%q requested_model=%q stream=false code=%q reason=%q action=%s unknown_action=%s", req.RequestID, e.Requested, e.Code, e.Reason, c.Action, c.UnknownAction))
		if c.Action == "audit" {
			return ok(pluginapi.ResponseInterceptResponse{})
		}
		return ok(pluginapi.ResponseInterceptResponse{Headers: verificationHeaders(e), Body: verificationBody(e, req.SourceFormat), ClearHeaders: []string{"Content-Length", "Content-Encoding"}})
	}
	e := a.modelDecision(req.RequestID, s)
	if e == nil {
		return ok(pluginapi.ResponseInterceptResponse{})
	}
	return ok(pluginapi.ResponseInterceptResponse{Headers: verificationHeaders(e), Body: verificationBody(e, req.SourceFormat), ClearHeaders: []string{"Content-Length", "Content-Encoding"}})
}

func (a *App) interceptStreamModel(req pluginapi.StreamChunkInterceptRequest) ([]byte, error) {
	if req.ChunkIndex < 0 || internalRequest(req.Metadata) {
		return ok(pluginapi.StreamChunkInterceptResponse{})
	}
	a.mismatchMu.Lock()
	defer a.mismatchMu.Unlock()
	s := a.modelMismatchStates[req.RequestID]
	if s == nil {
		if c := a.modelMismatchSettings(); c.Enabled {
			if req.ChunkIndex <= 0 {
				a.verificationLog("error", req.RequestID, "[响应核验] 请求状态缺失，已丢弃流块 result=state_missing")
			}
			return ok(pluginapi.StreamChunkInterceptResponse{DropChunk: true})
		}
		return ok(pluginapi.StreamChunkInterceptResponse{})
	}
	if s.Alerted && s.Config.ResponsesOnlyStream && s.Config.Action == "reject" && req.SourceFormat != "openai-response" {
		return ok(pluginapi.StreamChunkInterceptResponse{DropChunk: true})
	}
	format := req.SourceFormat
	if format == "" {
		format = s.SourceFormat
	}
	if s.Config.Enabled && s.Config.Action == "reject" && s.Config.ResponsesOnlyStream && format != "" && format != "openai-response" {
		s.Alerted = true
		e := &modelCheckError{Type: "model_verification_error", Code: "response_model_stream_unsupported", Message: "严格响应核验模式仅支持 Responses 流式协议", Requested: s.Requested, RequestID: req.RequestID, Reason: "non_responses_stream"}
		s.Blocked = true
		s.Terminal = true
		s.UpstreamFailed = true
		a.logModelDecision(req.RequestID, s, "blocked", e.Code, e.Reason)
		body := verificationBody(e, format)
		if format != "openai" {
			body = []byte("event: error\ndata: " + string(body) + "\n\n")
		}
		return ok(pluginapi.StreamChunkInterceptResponse{Body: body})
	}
	if format == "openai-response" && s.Config.Enabled && s.Config.Action == "reject" {
		return a.interceptBufferedResponses(req, s)
	}
	e := a.modelDecision(req.RequestID, s)
	if e == nil {
		return ok(pluginapi.StreamChunkInterceptResponse{})
	}
	if s.Alerted {
		return ok(pluginapi.StreamChunkInterceptResponse{DropChunk: true})
	}
	s.Alerted = true
	if req.SourceFormat == "openai-response" {
		return ok(pluginapi.StreamChunkInterceptResponse{Body: responsesVerificationFailure(e)})
	}
	body := verificationBody(e, format)
	// OpenAI Chat's HTTP handler adds SSE framing to JSON chunks. Other handlers
	// consume already-framed SSE. Verify these shapes in the real CPA fixture.
	if format != "openai" {
		prefix := "data: "
		if format == "claude" || format == "openai-response" {
			prefix = "event: error\ndata: "
		}
		body = []byte(prefix + string(body) + "\n\n")
	}
	return ok(pluginapi.StreamChunkInterceptResponse{Body: body})
}

// Codex retries unknown response.failed codes, regardless of retryable=false.
// invalid_prompt maps to its terminal InvalidRequest classification. Preserve
// our domain reason separately; this is a response-policy failure, not a claim
// that the user's prompt itself is malformed.
func responsesVerificationFailure(e *modelCheckError) []byte {
	detail := map[string]any{
		"type": "invalid_request_error", "code": "invalid_prompt",
		"verification_code": e.Code, "message": "[" + e.Code + "] " + e.Message + "；请检查上游或核验配置后再试。",
		"status": http.StatusForbidden, "retryable": false,
		"requested_model": e.Requested, "actual_model": e.Actual,
		"request_id": e.RequestID, "reason": e.Reason,
	}
	body, err := json.Marshal(map[string]any{
		"type": "response.failed", "response": map[string]any{"status": "failed", "error": detail},
	})
	if err != nil {
		panic(err)
	}
	return append(append([]byte("event: response.failed\ndata: "), body...), []byte("\n\n")...)
}

func (a *App) completeModelMismatch(id string) {
	a.mismatchMu.Lock()
	defer a.mismatchMu.Unlock()
	if s := a.modelMismatchStates[id]; s != nil {
		a.responseBufferBytes -= s.ResponseBuffer.Len()
		if a.responseBufferBytes < 0 {
			a.responseBufferBytes = 0
		}
		if !s.Config.Enabled {
			a.logModelDecision(id, s, "skipped", "", s.SkipReason)
		} else if s.Terminal || s.UpstreamFailed {
			// A terminal response already emitted its decision log.
		} else if s.ResponseHolding {
			a.logModelDecision(id, s, "buffer_discarded", "", "stream_ended_without_verified_terminal")
		} else if !s.Checked {
			// A raw model alone does not prove that an output interceptor ran.
			a.logModelDecision(id, s, "not_checked", "", "no_successful_output")
		} else if !s.Blocked && !s.Mismatch && !s.Ambiguous && s.Actual != "" && s.Requested != "" {
			a.logModelDecision(id, s, "matched", "", "")
		}
		delete(a.mismatchRequests[s.Digest], id)
		if len(a.mismatchRequests[s.Digest]) == 0 {
			delete(a.mismatchRequests, s.Digest)
		}
		delete(a.modelMismatchStates, id)
	}
}

func responsesBufferLimitFailure(id string) []byte {
	e := &modelCheckError{Type: "model_verification_error", Code: "response_model_buffer_limit", Message: "响应核验缓存超过上限，已终止响应", RequestID: id, Reason: "response_buffer_limit_exceeded"}
	return responsesVerificationFailure(e)
}

func (a *App) clearModelMismatchStates() {
	a.mismatchMu.Lock()
	defer a.mismatchMu.Unlock()
	a.modelMismatchStates = map[string]*modelMismatchState{}
	a.mismatchRequests = map[[32]byte]map[string]bool{}
	a.responseBufferBytes = 0
}

func (a *App) modelMismatchActive() int {
	a.mismatchMu.Lock()
	defer a.mismatchMu.Unlock()
	return len(a.modelMismatchStates)
}
