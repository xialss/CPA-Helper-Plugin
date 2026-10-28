// This diagnostic-only plugin must never be installed in a real deployment.
package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef struct { uint32_t abi_version; void* host_ctx; void* call; void* free_buffer; } cliproxy_host_api;
typedef int (*plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*plugin_free_fn)(void*, size_t);
typedef void (*plugin_shutdown_fn)(void);
typedef struct { uint32_t abi_version; plugin_call_fn call; plugin_free_fn free_buffer; plugin_shutdown_fn shutdown; } cliproxy_plugin_api;
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
*/
import "C"

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"unsafe"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const endpoint = "/v0/management/plugins/ttl-probe/events"
const marker = "X-CPA-TTL-Probe"

var records struct {
	sync.Mutex
	Events []map[string]any
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, out *C.cliproxy_plugin_api) C.int {
	if host == nil || out == nil || host.abi_version != 1 {
		return 1
	}
	out.abi_version = 1
	out.call = C.plugin_call_fn(C.cliproxyPluginCall)
	out.free_buffer = C.plugin_free_fn(C.cliproxyPluginFree)
	out.shutdown = C.plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, n C.size_t, out *C.cliproxy_buffer) C.int {
	if out == nil || method == nil || (n > 0 && request == nil) || uint64(n) > uint64(^uint32(0)>>1) {
		return 1
	}
	out.ptr, out.len = nil, 0
	raw := C.GoBytes(unsafe.Pointer(request), C.int(n))
	result, err := handle(C.GoString(method), raw)
	env := pluginabi.Envelope{OK: err == nil}
	if err != nil {
		env.Error = &pluginabi.Error{Code: "probe_error", Message: err.Error(), HTTPStatus: 500}
	} else {
		env.Result, err = json.Marshal(result)
		if err != nil {
			return 1
		}
	}
	response, err := json.Marshal(env)
	if err != nil {
		return 1
	}
	out.ptr, out.len = C.CBytes(response), C.size_t(len(response))
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, _ C.size_t) { C.free(ptr) }

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}

func record(method, id, scope, from, to string, body []byte, extra map[string]any) {
	e := map[string]any{"hook": method, "request_id": id, "caller_scope": scope, "from": from, "to": to}
	var evidence []map[string]any
	for _, line := range bytes.Split(body, []byte("\n")) {
		line = bytes.TrimSpace(bytes.TrimPrefix(line, []byte("data:")))
		var node any
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.UseNumber()
		if decoder.Decode(&node) == nil {
			collectUsage(node, &evidence)
		}
	}
	e["evidence"] = evidence
	for k, v := range extra {
		e[k] = v
	}
	records.Lock()
	records.Events = append(records.Events, e)
	records.Unlock()
}

func collectUsage(node any, out *[]map[string]any) {
	switch v := node.(type) {
	case map[string]any:
		if usage, ok := v["usage"].(map[string]any); ok {
			safe := map[string]any{}
			for _, key := range []string{"input_tokens", "output_tokens", "cache_creation_input_tokens", "cache_read_input_tokens"} {
				if n, ok := usage[key].(json.Number); ok {
					safe[key] = n
				}
			}
			if buckets, ok := usage["cache_creation"].(map[string]any); ok {
				for _, key := range []string{"ephemeral_5m_input_tokens", "ephemeral_1h_input_tokens"} {
					if n, ok := buckets[key].(json.Number); ok {
						safe[key] = n
					}
				}
			}
			*out = append(*out, safe)
		}
		for _, child := range v {
			collectUsage(child, out)
		}
	case []any:
		for _, child := range v {
			collectUsage(child, out)
		}
	}
}

func handle(method string, raw []byte) (any, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return map[string]any{"schema_version": pluginabi.SchemaVersion,
			"metadata": pluginapi.Metadata{Name: "ttl-probe", Version: "0.0.0-probe", Author: "CPA-Helper",
				GitHubRepository: "https://github.com/xialss/CPA-Helper-Plugin"},
			"capabilities": map[string]bool{"request_interceptor": true, "response_interceptor": true,
				"response_stream_interceptor": true, "response_before_translator": true,
				"request_lifecycle_plugin": true, "usage_plugin": true, "management_api": true}}, nil
	case pluginabi.MethodManagementRegister:
		return map[string]any{"routes": []map[string]string{{"Method": "GET", "Path": endpoint}, {"Method": "DELETE", "Path": endpoint}}}, nil
	case pluginabi.MethodManagementHandle:
		var req pluginapi.ManagementRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		records.Lock()
		defer records.Unlock()
		if req.Method == "DELETE" {
			records.Events = nil
		}
		body, err := json.Marshal(map[string]any{"events": records.Events})
		return pluginapi.ManagementResponse{StatusCode: 200, Headers: http.Header{"Content-Type": {"application/json"}}, Body: body}, err
	case pluginabi.MethodRequestInterceptBefore, pluginabi.MethodRequestInterceptAfter:
		var req pluginapi.RequestInterceptRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		record(method, req.RequestID, req.CallerScope, req.SourceFormat, req.ToFormat, nil, map[string]any{"trace_id": req.TraceID})
		if method == pluginabi.MethodRequestInterceptBefore || req.ToFormat != "claude" {
			return pluginapi.RequestInterceptResponse{}, nil
		}
		ttl := ""
		// Synthetic fixture identities only. This is not a policy implementation.
		for _, candidate := range []string{"5m", "1h"} {
			if req.CallerScope == policy.CallerScope("fixture-ttl-"+candidate) {
				ttl = candidate
			}
		}
		if ttl == "" {
			return nil, fmt.Errorf("unexpected synthetic caller scope")
		}
		response := pluginapi.RequestInterceptResponse{Headers: http.Header{marker: {ttl}}}
		if req.Headers.Get("X-TTL-Probe-Action") == "rewrite" && req.SourceFormat == "claude" {
			var node any
			if err := json.Unmarshal(req.Body, &node); err != nil {
				return nil, err
			}
			// Only change existing bare ephemeral controls; never add breakpoints.
			var rewrite func(any)
			rewrite = func(node any) {
				switch v := node.(type) {
				case map[string]any:
					if control, ok := v["cache_control"].(map[string]any); ok && control["type"] == "ephemeral" && control["ttl"] == nil && ttl == "1h" {
						control["ttl"] = ttl
					}
					for _, child := range v {
						rewrite(child)
					}
				case []any:
					for _, child := range v {
						rewrite(child)
					}
				}
			}
			rewrite(node)
			response.Body, _ = json.Marshal(node)
		}
		return response, nil
	case pluginabi.MethodResponseNormalizeBefore:
		var req pluginapi.ResponseTransformRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			return nil, err
		}
		hasIdentity := false
		for _, key := range []string{"RequestID", "TraceID", "CallerScope", "request_id", "trace_id", "caller_scope"} {
			if _, present := fields[key]; present {
				hasIdentity = true
			}
		}
		record(method, "", "", req.FromFormat, req.ToFormat, req.Body, map[string]any{"wire_has_identity": hasIdentity})
		return pluginapi.PayloadResponse{}, nil
	case pluginabi.MethodResponseInterceptAfter:
		var req pluginapi.ResponseInterceptRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		scope, _ := req.Metadata[pluginapi.CallerScopeMetadataKey].(string)
		record(method, req.RequestID, scope, req.SourceFormat, "", req.Body, nil)
		return pluginapi.ResponseInterceptResponse{}, nil
	case pluginabi.MethodResponseInterceptStreamChunk:
		var req pluginapi.StreamChunkInterceptRequest
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		scope, _ := req.Metadata[pluginapi.CallerScopeMetadataKey].(string)
		record(method, req.RequestID, scope, req.SourceFormat, "", req.Body, nil)
		return pluginapi.StreamChunkInterceptResponse{}, nil
	case pluginabi.MethodUsageHandle:
		var req pluginapi.UsageRecord
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		record(method, req.RequestID, "", "", "", nil, map[string]any{"detail": req.Detail, "failed": req.Failed,
			"trace_id": req.TraceID, "selection_id": req.SelectionID,
			"wire_has_buckets": strings.Contains(string(raw), "ephemeral_5m_input_tokens") || strings.Contains(string(raw), "CacheCreation5m")})
		return struct{}{}, nil
	case pluginabi.MethodRequestComplete:
		var req pluginapi.RequestCompletion
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		record(method, req.RequestID, "", req.SourceFormat, "", nil, map[string]any{"outcome": req.Outcome})
		return struct{}{}, nil
	case pluginabi.MethodPluginQuiesce, pluginabi.MethodPluginShutdown:
		return struct{}{}, nil
	default:
		return nil, fmt.Errorf("unsupported diagnostic method: %s", method)
	}
}
