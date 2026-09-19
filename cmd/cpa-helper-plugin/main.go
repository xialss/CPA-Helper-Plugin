package main

/*
#include <stdint.h>
#include <stdlib.h>
typedef struct { void* ptr; size_t len; } cliproxy_buffer;
typedef int (*host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*host_free_fn)(void*, size_t);
typedef struct { uint32_t abi_version; void* host_ctx; host_call_fn call; host_free_fn free_buffer; } cliproxy_host_api;
typedef int (*plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*plugin_free_fn)(void*, size_t);
typedef void (*plugin_shutdown_fn)(void);
typedef struct { uint32_t abi_version; plugin_call_fn call; plugin_free_fn free_buffer; plugin_shutdown_fn shutdown; } cliproxy_plugin_api;
extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);
static cliproxy_host_api host_api;
static void set_host(cliproxy_host_api* host) { host_api = *host; }
static int call_host(char* method, uint8_t* request, size_t len, cliproxy_buffer* out) {
  if (!host_api.call) return 1;
  return host_api.call(host_api.host_ctx, method, request, len, out);
}
static void free_host(void* ptr, size_t len) { if (ptr && host_api.free_buffer) host_api.free_buffer(ptr, len); }
*/
import "C"

import (
	"encoding/json"
	"errors"
	"unsafe"

	"cpa-helper-plugin/internal/plugin"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
)

var app = plugin.New(callHost)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, out *C.cliproxy_plugin_api) C.int {
	if host == nil || out == nil || host.abi_version != 1 {
		return 1
	}
	C.set_host(host)
	out.abi_version = 1
	out.call = C.plugin_call_fn(C.cliproxyPluginCall)
	out.free_buffer = C.plugin_free_fn(C.cliproxyPluginFree)
	out.shutdown = C.plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, n C.size_t, out *C.cliproxy_buffer) (result C.int) {
	if out == nil {
		return 1
	}
	out.ptr = nil
	out.len = 0
	defer func() {
		if recover() != nil {
			raw, _ := plugin.ErrorEnvelope("plugin_panic", "Plugin execution failed", 500)
			writeResponse(out, raw)
			result = 1
		}
	}()
	if method == nil || (n > 0 && request == nil) || uint64(n) > uint64(^uint32(0)>>1) {
		return 1
	}
	var raw []byte
	if n > 0 {
		raw = C.GoBytes(unsafe.Pointer(request), C.int(n))
	}
	response, err := app.Handle(C.GoString(method), raw)
	if err != nil {
		response, _ = plugin.ErrorEnvelope("plugin_error", err.Error(), 500)
	}
	writeResponse(out, response)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, n C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() { app.Shutdown() }

func writeResponse(out *C.cliproxy_buffer, raw []byte) {
	if len(raw) > 0 {
		out.ptr = C.CBytes(raw)
		out.len = C.size_t(len(raw))
	}
}

func callHost(method string, payload any) (json.RawMessage, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	name := C.CString(method)
	defer C.free(unsafe.Pointer(name))
	data := C.CBytes(raw)
	defer C.free(data)
	var out C.cliproxy_buffer
	status := C.call_host(name, (*C.uint8_t)(data), C.size_t(len(raw)), &out)
	defer C.free_host(out.ptr, out.len)
	if status != 0 || out.ptr == nil || uint64(out.len) > uint64(^uint32(0)>>1) {
		return nil, errors.New("host callback failed")
	}
	var env pluginabi.Envelope
	if err = json.Unmarshal(C.GoBytes(out.ptr, C.int(out.len)), &env); err != nil {
		return nil, errors.New("invalid host callback response")
	}
	if !env.OK {
		return nil, errors.New("host callback rejected")
	}
	return env.Result, nil
}
