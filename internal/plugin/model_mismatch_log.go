package plugin

import (
	"fmt"
	"log/slog"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
)

// hostLogRequest mirrors the official host.log wire contract.
type hostLogRequest struct {
	Level   string         `json:"level"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields"`
}

// CPA's text formatter omits custom fields. Keep diagnostic details quoted in
// the message so both the log viewer and file output retain them on one line.
func (a *App) verificationLog(level, id, message string) {
	if a.host != nil {
		_, err := a.host(pluginabi.MethodHostLog, hostLogRequest{
			Level: level, Message: message,
			Fields: map[string]any{"plugin_id": ID, "request_id": id},
		})
		if err == nil {
			return
		}
		// Logging failure must remain visible without bypassing interception.
		slog.Error("CPA host.log failed; response verification diagnostic follows on stderr", "error", err)
	}
	if level == "warn" {
		slog.Warn(message)
	} else {
		slog.Info(message)
	}
}

// Called under mismatchMu. Report decisions once, not once per stream chunk.
func (a *App) logModelDecision(id string, s *modelMismatchState, result, code, reason string) {
	key := result + ":" + code + ":" + reason
	if s.LoggedCode == key {
		return
	}
	s.LoggedCode = key
	label, level := "", "info"
	switch result {
	case "blocked":
		label, level = "已拦截：替换为错误响应", "warn"
	case "audit_mismatch":
		label, level = "模型不一致，仅审计未拦截", "warn"
	case "audit_unverifiable":
		label, level = "无法核验，仅审计未拦截", "warn"
	case "unknown_pass":
		label, level = "无法核验，已放行", "warn"
	case "matched":
		label = "核验通过，已放行（含允许映射）"
	case "skipped":
		label = "已跳过核验"
	case "not_checked":
		label = "未核验：未收到可检查的成功输出"
	case "buffering":
		label = "暂存响应，尚未向客户端放行文本或工具调用"
	case "buffer_discarded":
		label = "响应未完成核验，已丢弃暂存内容"
	case "upstream_error":
		label = "上游失败，已丢弃暂存内容，仅返回上游错误"
	}
	actual := s.Actual
	if s.Ambiguous {
		actual = ""
	}
	a.verificationLog(level, id, fmt.Sprintf("[响应核验] %s result=%s request_id=%q requested_model=%q actual_model=%q stream=%t code=%q reason=%q action=%s unknown_action=%s",
		label, result, id, s.Requested, actual, s.Stream, code, reason, s.Config.Action, s.Config.UnknownAction))
}

func (a *App) logModelConfiguration(c modelMismatchConfig) {
	a.verificationLog("info", "", fmt.Sprintf("[响应核验] 配置已生效 enabled=%t stream_enabled=%t action=%s unknown_action=%s ignored_models=%d accepted_models=%d",
		c.Enabled, c.StreamEnabled, c.Action, c.UnknownAction, len(c.IgnoredModels), len(c.Accepted)))
}

func (a *App) logUnsupportedStream(id, model, format string) {
	a.verificationLog("warn", id, fmt.Sprintf("[响应核验] 严格模式已在执行前拒绝非 Responses 流 result=blocked request_id=%q requested_model=%q actual_model=%q stream=true code=%q reason=%q source_format=%q action=reject unknown_action=pass",
		id, model, "", "response_model_stream_unsupported", "non_responses_stream", format))
}
