package plugin

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"cpa-helper-plugin/internal/policy"
	"cpa-helper-plugin/internal/snapshot"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

//go:embed web/*
var assets embed.FS

type managementRequest struct {
	pluginapi.ManagementRequest
	HostCallbackID string `json:"host_callback_id"`
}

func managementRegistration() any {
	routes := []pluginapi.ManagementRoute{}
	for _, r := range [][2]string{{"GET", "/health"}, {"GET", "/capabilities"}, {"GET", "/policy"}, {"PUT", "/policy"}, {"POST", "/policy/rollback"}, {"GET", "/directory"}} {
		routes = append(routes, pluginapi.ManagementRoute{Method: r[0], Path: BasePath + r[1]})
	}
	resources := []pluginapi.ResourceRoute{{Path: "/ui", Menu: "CPA Helper", Description: "API key routing and concurrency"}, {Path: "/app.js"}, {Path: "/style.css"}, {Path: "/logo.svg"}, {Path: "/lucide.js"}, {Path: "/sha256.js"}}
	return struct {
		Routes    []pluginapi.ManagementRoute `json:"routes"`
		Resources []pluginapi.ResourceRoute   `json:"resources"`
	}{routes, resources}
}

func (a *App) management(req managementRequest) ([]byte, error) {
	if strings.HasPrefix(req.Path, ResourcePath+"/") {
		name := strings.TrimPrefix(req.Path, ResourcePath+"/")
		mime := ""
		switch name {
		case "ui":
			name = "index.html"
			mime = "text/html; charset=utf-8"
		case "app.js", "lucide.js", "sha256.js":
			mime = "application/javascript"
		case "style.css":
			mime = "text/css"
		case "logo.svg":
			mime = "image/svg+xml"
		default:
			return ok(pluginapi.ManagementResponse{StatusCode: 404})
		}
		if req.Method != "GET" {
			return ok(pluginapi.ManagementResponse{StatusCode: 405})
		}
		raw, err := assets.ReadFile("web/" + name)
		if err != nil {
			return nil, err
		}
		return ok(pluginapi.ManagementResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {mime}, "X-Content-Type-Options": {"nosniff"}, "Cache-Control": {"no-store"}, "Content-Security-Policy": {"default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'self'"}}, Body: raw})
	}
	// Only the host's authenticated management route table can reach this branch.
	r, err := http.NewRequest(req.Method, req.Path, bytes.NewReader(req.Body))
	if err != nil {
		return nil, errors.New("invalid management request URL")
	}
	r.Header = req.Headers.Clone()
	w := &managementWriter{header: make(http.Header), status: http.StatusOK}
	a.serveManagement(w, r, req.HostCallbackID)
	return ok(pluginapi.ManagementResponse{StatusCode: w.status, Headers: w.Header(), Body: w.body.Bytes()})
}

type managementWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *managementWriter) Header() http.Header           { return w.header }
func (w *managementWriter) WriteHeader(status int)        { w.status = status }
func (w *managementWriter) Write(raw []byte) (int, error) { return w.body.Write(raw) }

func jsonResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "response encoding failed", 500)
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
func problem(w http.ResponseWriter, status int, code, message string) {
	jsonResponse(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func (a *App) serveManagement(w http.ResponseWriter, r *http.Request, callback string) {
	path := strings.TrimPrefix(r.URL.Path, BasePath)
	if !strings.HasPrefix(r.URL.Path, BasePath+"/") {
		problem(w, 404, "not_found", "Unknown resource")
		return
	}
	if r.Method == "GET" && path == "/capabilities" {
		jsonResponse(w, 200, map[string]any{"plugin_id": ID, "plugin_version": policy.Version, "contract_version": "v1", "abi_version": 1, "schema_version": 4, "cpa_version": "v7.3.8", "model_list_filter_enabled": a.modelListFiltering(), "modules": []string{"model_rules", "model_list_filter", "credential_routes", "concurrency"}, "policy_fields": []string{"groups[].id", "groups[].name", "groups[].note", "groups[].rule", "keys[].id", "keys[].label", "keys[].enabled", "keys[].group_ids", "keys[].max_concurrency", "keys[].rule"}, "unknown_key": "allow", "concurrency_scope": "instance", "billing": false})
		return
	}
	s := a.currentStore()
	if s == nil {
		problem(w, 503, "policy_unavailable", "Plugin not configured")
		return
	}
	switch r.Method + " " + path {
	case "GET /health":
		h := s.Health()
		status := 200
		if h.Status != "ok" {
			status = 503
		}
		jsonResponse(w, status, struct {
			snapshot.Health
			Active map[string]int `json:"active"`
		}{h, a.runtime.Counts()})
	case "GET /policy":
		p, err := s.Current()
		if err != nil {
			problem(w, 503, "policy_unavailable", err.Error())
			return
		}
		w.Header().Set("ETag", strconv.Quote(strconv.FormatUint(p.Revision, 10)))
		jsonResponse(w, 200, p)
	case "GET /directory":
		items, err := a.directory(callback)
		if err != nil {
			problem(w, 502, "host_unavailable", err.Error())
			return
		}
		jsonResponse(w, 200, map[string]any{"credentials": items})
	case "PUT /policy", "POST /policy/rollback":
		base, err := strconv.ParseUint(strings.Trim(r.Header.Get("If-Match"), "\""), 10, 64)
		if err != nil || base == 0 {
			s.RecordRejection("If-Match must contain the current revision")
			problem(w, 428, "precondition_required", "If-Match must contain the current revision")
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			s.RecordRejection("Idempotency-Key is required")
			problem(w, 400, "invalid", "Idempotency-Key is required")
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			s.RecordRejection("Cannot read request body")
			problem(w, 400, "invalid", "Cannot read request body")
			return
		}
		var rev uint64
		if path == "/policy" {
			var p policy.Snapshot
			if err = snapshot.Decode(raw, &p); err == nil {
				rev, err = s.Apply(p, base, key)
			}
		} else {
			var body struct {
				Revision uint64 `json:"policy_revision"`
			}
			if err = snapshot.Decode(raw, &body); err == nil {
				rev, err = s.Rollback(base, body.Revision, key)
			}
		}
		if err != nil {
			s.RecordRejection(err.Error())
			status := 400
			code := "invalid_policy"
			if errors.Is(err, snapshot.ErrConflict) {
				status = 409
				code = "revision_conflict"
			}
			if s.Health().Status != "ok" {
				status = 503
				code = "policy_unavailable"
			}
			if errors.Is(err, snapshot.ErrPersistenceUncertain) {
				status = 500
				code = "persistence_uncertain"
			}
			if err.Error() == "cannot persist policy transaction" {
				status = 500
				code = "persistence_failed"
			}
			problem(w, status, code, err.Error())
			return
		}
		jsonResponse(w, 200, map[string]uint64{"policy_revision": rev})
	default:
		problem(w, 404, "not_found", "Unknown resource or method")
	}
}
