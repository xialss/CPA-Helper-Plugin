package plugin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

// listScope infers identity from the headers available in CPA v7.3.8.
// The host does not supply query credentials or the authenticated principal.
func listScope(headers http.Header) string {
	scope := ""
	for name, values := range headers {
		if !strings.EqualFold(name, "Authorization") && !strings.EqualFold(name, "X-Api-Key") && !strings.EqualFold(name, "X-Goog-Api-Key") {
			continue
		}
		for _, value := range values {
			if strings.EqualFold(name, "Authorization") {
				parts := strings.SplitN(value, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					value = parts[1]
				}
			}
			if strings.TrimSpace(value) == "" {
				continue
			}
			next := policy.CallerScope(value)
			if scope != "" && scope != next {
				return ""
			}
			scope = next
		}
	}
	return scope
}

func modelListError(code, message string) ([]byte, error) {
	slog.Warn("model list rejected", "reason", code)
	body, err := json.Marshal(map[string]any{"error": map[string]string{"type": "model_list_error", "code": code, "message": message}})
	if err != nil {
		return nil, err
	}
	return ok(pluginapi.ResponseInterceptResponse{Body: body, Headers: http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"no-store"}, "X-CPA-Helper-Error": {code}}})
}

func (a *App) filterModelList(req pluginapi.ResponseInterceptRequest) ([]byte, error) {
	// This is the captured v7.3.8 list-response shape, not a generation response.
	if req.Model != "" || req.RequestedModel != "" || req.Stream || len(req.OriginalRequest) != 0 || len(req.RequestBody) != 0 || req.StatusCode != http.StatusOK || internalRequest(req.Metadata) {
		return ok(pluginapi.ResponseInterceptResponse{})
	}
	scope := listScope(req.RequestHeaders)
	s := a.currentStore()
	if s == nil {
		return modelListError("policy_unavailable", "当前策略不可用，请联系管理员")
	}
	e, _, err := s.Engine()
	if err != nil {
		return modelListError("policy_unavailable", "当前策略不可用，请联系管理员")
	}
	k := e.Resolve(scope)
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(req.Body, &catalog); err != nil || catalog == nil {
		return modelListError("invalid_model_catalog", "模型目录格式无效")
	}
	field, idField := "data", "id"
	if req.SourceFormat == "gemini" {
		field, idField = "models", "name"
	} else if req.SourceFormat == "openai" && catalog["models"] != nil {
		field, idField = "models", "slug"
	} else if req.SourceFormat != "openai" && req.SourceFormat != "claude" {
		return modelListError("invalid_model_catalog", "不支持当前模型目录格式")
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(catalog[field], &entries); err != nil || entries == nil {
		return modelListError("invalid_model_catalog", "模型目录条目格式无效")
	}
	kept := make([]json.RawMessage, 0, len(entries))
	first, last := "", ""
	for _, entry := range entries {
		var obj map[string]json.RawMessage
		var id string
		if json.Unmarshal(entry, &obj) != nil || json.Unmarshal(obj[idField], &id) != nil || strings.TrimSpace(id) == "" {
			return modelListError("invalid_model_catalog", "模型标识缺失或无效")
		}
		model := id
		if req.SourceFormat == "gemini" {
			model = strings.TrimPrefix(model, "models/")
		}
		if req.SourceFormat == "claude" && strings.HasPrefix(model, "claude-fable-5-dd-") {
			runes := []rune(strings.TrimPrefix(model, "claude-fable-5-dd-"))
			for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
				runes[i], runes[j] = runes[j], runes[i]
			}
			model = string(runes)
		}
		if k.Enabled && k.Rule.AllowsModel(requestedModel(model, "")) {
			kept = append(kept, entry)
			if first == "" {
				first = id
			}
			last = id
		}
	}
	if scope == "" {
		// CPA authenticates before listing. Missing/ambiguous header identity only
		// relaxes catalog visibility; execution admission still uses caller_scope.
		slog.Info("model list unfiltered", "reason", "header_identity_unavailable")
		return ok(pluginapi.ResponseInterceptResponse{Body: req.Body, Headers: http.Header{
			"Cache-Control": {"no-store"}, "X-Cpa-Helper-Model-List": {"unfiltered-identity-unavailable"},
		}})
	}
	catalog[field], err = json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	if req.SourceFormat == "claude" {
		catalog["first_id"], _ = json.Marshal(first)
		catalog["last_id"], _ = json.Marshal(last)
	}
	body, err := json.Marshal(catalog)
	if err != nil {
		return nil, err
	}
	return ok(pluginapi.ResponseInterceptResponse{Body: body, Headers: http.Header{"Cache-Control": {"no-store"}}})
}
