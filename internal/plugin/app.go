// Package plugin adapts the official CPA ABI to independent policy services.
package plugin

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"cpa-helper-plugin/internal/policy"
	run "cpa-helper-plugin/internal/runtime"
	"cpa-helper-plugin/internal/snapshot"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

// ID is both the CPA plugin filename and configuration identifier.
const ID = "cpa-helper-plugin"

// BasePath is authenticated by CPA's management middleware.
const BasePath = "/v0/management/plugins/" + ID + "/v1"

// ResourcePath contains public static resources only.
const ResourcePath = "/v0/resource/plugins/" + ID

// HostCall invokes the official host callback table and returns its decoded result.
type HostCall func(method string, payload any) (json.RawMessage, error)

// App is a single loaded plugin instance.
type App struct {
	mu                     sync.RWMutex
	modelListFilterEnabled bool
	store                  *snapshot.Store
	stateDir               string
	runtime                *run.Runtime
	host                   HostCall
	inventoryMu            sync.Mutex
	credentials            map[string]credential
}

// New constructs a plugin before CPA registration supplies configuration.
func New(host HostCall) *App {
	return &App{modelListFilterEnabled: true, runtime: run.New(), host: host, credentials: map[string]credential{}}
}

type lifecycle struct {
	ConfigYAML    []byte `json:"config_yaml"`
	SchemaVersion uint32 `json:"schema_version"`
}
type config struct {
	ModelListFilterEnabled bool      `yaml:"model_list_filter_enabled"`
	StateDir               string    `yaml:"state_dir"`
	Enabled                bool      `yaml:"enabled"`
	Priority               int       `yaml:"priority"`
	Store                  yaml.Node `yaml:"store"`
}
type registration struct {
	SchemaVersion uint32             `json:"schema_version"`
	Metadata      pluginapi.Metadata `json:"metadata"`
	Capabilities  map[string]bool    `json:"capabilities"`
}

// Handle dispatches CPA's JSON RPC methods without accepting a parallel request proxy.
func (a *App) Handle(method string, raw []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		var req lifecycle
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, errors.New("invalid registration payload")
		}
		if req.SchemaVersion < 4 {
			return nil, errors.New("CPA schema 4 or newer is required")
		}
		cfg := config{StateDir: filepath.Join("plugins", "cpa-helper-state"), Enabled: true, ModelListFilterEnabled: true}
		if len(bytes.TrimSpace(req.ConfigYAML)) > 0 {
			decoder := yaml.NewDecoder(bytes.NewReader(req.ConfigYAML))
			decoder.KnownFields(true)
			if err := decoder.Decode(&cfg); err != nil {
				return nil, errors.New("invalid plugin configuration")
			}
		}
		if strings.TrimSpace(cfg.StateDir) == "" {
			return nil, errors.New("state_dir must not be empty")
		}
		dir, err := filepath.Abs(cfg.StateDir)
		if err != nil {
			return nil, err
		}
		a.mu.Lock()
		if a.store != nil && a.stateDir != dir {
			a.mu.Unlock()
			return nil, errors.New("state_dir change requires a CPA restart")
		}
		if a.store == nil {
			a.store = snapshot.Open(dir)
			a.stateDir = dir
		}
		a.modelListFilterEnabled = cfg.ModelListFilterEnabled
		a.runtime.Resume()
		a.mu.Unlock()
		return ok(registration{4, pluginapi.Metadata{Name: ID, Version: policy.Version, Author: "CPA-Helper", GitHubRepository: "https://github.com/xialss/CPA-Helper-Plugin", Logo: ResourcePath + "/logo.svg", ConfigFields: []pluginapi.ConfigField{{Name: "state_dir", Type: pluginapi.ConfigFieldTypeString, Description: "Dedicated persistent policy directory; parent must exist."}, {Name: "model_list_filter_enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "模型列表过滤：默认启用。关闭后返回 CPA 原始目录，不影响生成权限与路由规则。"}}}, map[string]bool{"request_interceptor": true, "response_interceptor": true, "request_lifecycle_plugin": true, "scheduler": true, "management_api": true}})
	case pluginabi.MethodResponseInterceptAfter:
		if !a.modelListFiltering() {
			return ok(pluginapi.ResponseInterceptResponse{})
		}
		var req pluginapi.ResponseInterceptRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return modelListError("invalid_model_catalog", "模型列表响应载荷格式无效")
		}
		return a.filterModelList(req)
	case pluginabi.MethodRequestInterceptBefore:
		var req pluginapi.RequestInterceptRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, errors.New("invalid interception payload")
		}
		return a.intercept(req)
	case pluginabi.MethodRequestInterceptAfter:
		return ok(pluginapi.RequestInterceptResponse{})
	case pluginabi.MethodRequestComplete:
		var req pluginapi.RequestCompletion
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, errors.New("invalid completion payload")
		}
		a.runtime.Complete(req.RequestID)
		return ok(struct{}{})
	case pluginabi.MethodSchedulerPick:
		var req pluginapi.SchedulerPickRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, errors.New("invalid scheduler payload")
		}
		return a.pick(req)
	case pluginabi.MethodManagementRegister:
		return ok(managementRegistration())
	case pluginabi.MethodManagementHandle:
		var req managementRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, errors.New("invalid management payload")
		}
		return a.management(req)
	case pluginabi.MethodPluginQuiesce, pluginabi.MethodPluginShutdown:
		a.runtime.Quiesce()
		return ok(struct{}{})
	default:
		return ErrorEnvelope("unknown_method", "unsupported plugin method", http.StatusNotFound)
	}
}

// Shutdown prevents admission after the host begins unloading the instance.
func (a *App) Shutdown() { a.runtime.Quiesce() }
func (a *App) modelListFiltering() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.modelListFilterEnabled
}
func (a *App) currentStore() *snapshot.Store   { a.mu.RLock(); defer a.mu.RUnlock(); return a.store }
func text(m map[string]any, key string) string { s, _ := m[key].(string); return strings.TrimSpace(s) }
func internalRequest(m map[string]any) bool    { return text(m, "source") == "plugin_host_model_callback" }
func requestedModel(model, requested string) string {
	strip := func(s string) string {
		s = strings.TrimSpace(s)
		if i := strings.LastIndex(s, "("); i >= 0 && strings.HasSuffix(s, ")") {
			return strings.TrimSpace(s[:i])
		}
		return s
	}
	if value := strip(requested); value != "" && !strings.EqualFold(value, "auto") {
		return value
	}
	return strip(model)
}

func (a *App) intercept(req pluginapi.RequestInterceptRequest) ([]byte, error) {
	if internalRequest(req.Metadata) {
		return ok(pluginapi.RequestInterceptResponse{})
	}
	scope := text(req.Metadata, "caller_scope")
	if err := a.runtime.Begin(req.RequestID, scope); err != nil {
		return denied(503, "request_identity_unavailable", "请求身份不可用或请求已结束，请稍后重试", req.SourceFormat)
	}
	defer a.runtime.End(req.RequestID)
	s := a.currentStore()
	if s == nil {
		return denied(503, "policy_unavailable", "当前策略不可用，请联系管理员", req.SourceFormat)
	}
	e, rev, err := s.Engine()
	if err != nil {
		return denied(503, "policy_unavailable", "当前策略不可用，请联系管理员", req.SourceFormat)
	}
	k := e.Resolve(scope)
	model := requestedModel(req.Model, req.RequestedModel)
	if !k.Enabled {
		a.audit(scope, rev, "api_key_disabled")
		return denied(401, "api_key_disabled", disabledKeyMessage(scope, req.Headers), req.SourceFormat)
	}
	if reason := e.ModelDenial(scope, model); reason != "" {
		a.audit(scope, rev, "denied_model")
		return denied(403, "model_forbidden", reason, req.SourceFormat)
	}
	if generate, ok := req.Metadata["generate"].(bool); !ok || generate {
		if !a.runtime.Admit(req.RequestID, k.MaxConcurrency) {
			a.audit(scope, rev, "concurrency_rejected")
			return denied(429, "concurrency_limit", "当前 API Key 已达设定并发上限", req.SourceFormat)
		}
	}
	return ok(pluginapi.RequestInterceptResponse{})
}

func (a *App) pick(req pluginapi.SchedulerPickRequest) ([]byte, error) {
	if internalRequest(req.Options.Metadata) {
		return ok(pluginapi.SchedulerPickResponse{})
	}
	a.observe(req.Candidates)
	scope := text(req.Options.Metadata, "caller_scope")
	if !policy.ValidScope(scope) {
		return ErrorEnvelope("request_identity_unavailable", "请求身份不可用，请稍后重试", 503)
	}
	s := a.currentStore()
	if s == nil {
		return ErrorEnvelope("policy_unavailable", "当前策略不可用，请联系管理员", 503)
	}
	e, rev, err := s.Engine()
	if err != nil {
		return ErrorEnvelope("policy_unavailable", "当前策略不可用，请联系管理员", 503)
	}
	k := e.Resolve(scope)
	model := requestedModel(req.Model, text(req.Options.Metadata, "requested_model"))
	if !k.Enabled {
		return ErrorEnvelope("api_key_disabled", disabledKeyMessage(scope, http.Header(req.Options.Headers)), 401)
	}
	if reason := e.ModelDenial(scope, model); reason != "" {
		return ErrorEnvelope("model_forbidden", reason, 403)
	}
	allowed := make([]run.Candidate, 0, len(req.Candidates))
	for _, c := range req.Candidates {
		if k.Rule.AllowsCredential(policy.CredentialRef(c.ID), sourceFromCandidate(c), c.Provider) {
			weight := int64(1)
			if v := strings.TrimSpace(c.Attributes["weight"]); v != "" {
				parsed, err := strconv.ParseInt(v, 10, 64)
				if err != nil || parsed <= 0 {
					weight = 0
				} else {
					weight = parsed
				}
			}
			allowed = append(allowed, run.Candidate{ID: c.ID, Weight: weight})
		}
	}
	if len(allowed) == len(req.Candidates) && len(allowed) > 0 {
		return ok(pluginapi.SchedulerPickResponse{})
	}
	id := a.runtime.Pick(rev, scope, strings.ToLower(model), allowed)
	if id == "" {
		a.audit(scope, rev, "no_routed_credential")
		return ErrorEnvelope("no_routed_credential", "当前路由规则下没有可用的上游凭据", 503)
	}
	a.audit(scope, rev, "credential_selected")
	return ok(pluginapi.SchedulerPickResponse{AuthID: id, Handled: true})
}

func (a *App) audit(scope string, rev uint64, reason string) {
	slog.Info("policy decision", "caller_scope", scope, "policy_revision", rev, "reason", reason)
}

func denied(status int, code, message, format string) ([]byte, error) {
	typ := "server_error"
	if status == 401 {
		typ = "authentication_error"
	}
	if status == 403 {
		typ = "permission_error"
	}
	if status == 429 {
		typ = "rate_limit_error"
	}
	obj := map[string]any{"error": map[string]string{"type": typ, "code": code, "message": message}}
	if format == "claude" {
		obj["type"] = "error"
	}
	body, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	return ok(pluginapi.RequestInterceptResponse{Terminate: true, StatusCode: status, ResponseHeaders: http.Header{"Content-Type": {"application/json"}}, ResponseBody: body})
}
func ok(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(pluginabi.Envelope{OK: true, Result: raw})
}

// ErrorEnvelope preserves HTTP status across the CPA v7.3.8 C ABI boundary.
func ErrorEnvelope(code, message string, status int) ([]byte, error) {
	return json.Marshal(pluginabi.Envelope{OK: false, Error: &pluginabi.Error{Code: code, Message: message, HTTPStatus: status}})
}

type credential struct {
	Ref      string `json:"ref"`
	Source   string `json:"source"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Status   string `json:"status"`
}

func sourceFromCandidate(c pluginapi.SchedulerAuthCandidate) string {
	b := strings.ToLower(strings.TrimSpace(c.Attributes["source_backend"]))
	s := strings.ToLower(strings.TrimSpace(c.Attributes["source"]))
	if b == "config" || b == "memory" || strings.EqualFold(c.Attributes["runtime_only"], "true") || strings.HasPrefix(s, "config:") || s == "memory" || s == "runtime" || s == "runtime_only" {
		return "ai-providers"
	}
	if b == "file" || b == "git" || b == "objectstore" || b == "postgres" || c.Attributes["path"] != "" || s == "file" || s == "filesystem" || s == "git" || s == "objectstore" || s == "postgres" {
		return "auth-files"
	}
	return ""
}
func (a *App) observe(candidates []pluginapi.SchedulerAuthCandidate) {
	a.inventoryMu.Lock()
	defer a.inventoryMu.Unlock()
	for _, c := range candidates {
		source := sourceFromCandidate(c)
		if c.ID == "" || source == "" {
			continue
		}
		ref := policy.CredentialRef(c.ID)
		a.credentials[ref] = credential{ref, source, strings.ToLower(c.Provider), c.Provider + " " + ref[7:19], c.Status}
	}
}

func (a *App) directory(callback string) ([]credential, error) {
	if a.host == nil {
		return nil, errors.New("host credential inventory unavailable")
	}
	raw, err := a.host(pluginabi.MethodHostAuthList, map[string]string{"host_callback_id": callback})
	if err != nil {
		return nil, errors.New("host credential inventory failed")
	}
	var response struct {
		Files []pluginapi.HostAuthFileEntry `json:"files"`
	}
	if err = json.Unmarshal(raw, &response); err != nil {
		return nil, errors.New("invalid host credential inventory")
	}
	a.inventoryMu.Lock()
	defer a.inventoryMu.Unlock()
	next := map[string]credential{}
	for ref, c := range a.credentials {
		if c.Source == "ai-providers" {
			next[ref] = c
		}
	}
	for _, f := range response.Files {
		if f.ID == "" {
			continue
		}
		source := "auth-files"
		if f.RuntimeOnly || f.Source == "memory" || f.Source == "config" || strings.HasPrefix(f.Source, "config:") {
			source = "ai-providers"
		}
		provider := f.Provider
		if provider == "" {
			provider = f.Type
		}
		ref := policy.CredentialRef(f.ID)
		status := f.Status
		if f.Disabled {
			status = "disabled"
		}
		next[ref] = credential{ref, source, strings.ToLower(provider), fmt.Sprintf("%s %s", provider, ref[7:19]), status}
	}
	a.credentials = next
	out := make([]credential, 0, len(next))
	for _, c := range next {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}
