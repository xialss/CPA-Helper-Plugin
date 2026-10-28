package plugin

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const ttlHeader = "X-CPA-Helper-Claude-TTL"
const ttlRequestHeader = "X-CPA-Helper-TTL-Request"

// The final rules deliberately match only markers set by this plugin. Empty
// explicit caching stays empty because these paths never create breakpoints.
var ttlPaths = []string{
	`tools.#(cache_control.type=="ephemeral")#.cache_control.ttl`,
	`system.#(cache_control.type=="ephemeral")#.cache_control.ttl`,
	`messages.#(content)#.content.#(cache_control.type=="ephemeral")#.cache_control.ttl`,
}

type claudeTTLConfig struct {
	Enabled         bool   `yaml:"enabled" json:"enabled"`
	HostConfigPath  string `yaml:"host_config_path" json:"host_config_path"`
	DelegateBuiltin string `yaml:"delegate_builtin" json:"delegate_builtin"`
}

type ttlConfigGuard struct {
	path     string
	digest   [32]byte
	delegate string
	invalid  atomic.Bool
}

func requiredTTLPayload() map[string]any {
	models := func(ttl string) []any {
		return []any{map[string]any{"name": "*", "protocol": "claude", "headers": map[string]any{ttlHeader: ttl}}}
	}
	params := map[string]any{}
	paths := make([]any, 0, len(ttlPaths))
	for _, path := range ttlPaths {
		params[path] = "1h"
		paths = append(paths, path)
	}
	rootModels := func(ttl string) []any {
		entries := models(ttl)
		entries[0].(map[string]any)["match"] = []any{map[string]any{"cache_control.type": "ephemeral"}}
		return entries
	}
	return map[string]any{
		"override": []any{
			map[string]any{"models": models("1h"), "params": params},
			// The match evaluates the prepared payload. An unconditional root
			// assignment would create a control where the client supplied none.
			map[string]any{"models": rootModels("1h"), "params": map[string]any{"cache_control.ttl": "1h"}},
		},
		"filter": []any{
			map[string]any{"models": models("5m"), "params": paths},
			map[string]any{"models": rootModels("5m"), "params": []any{"cache_control.ttl"}},
		},
	}
}

func validateTTLConfig(cfg claudeTTLConfig) (*ttlConfigGuard, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if !filepath.IsAbs(cfg.HostConfigPath) {
		return nil, errors.New("claude_cache_ttl.host_config_path must be the absolute CPA configuration path")
	}
	if cfg.DelegateBuiltin != pluginapi.SchedulerBuiltinRoundRobin && cfg.DelegateBuiltin != pluginapi.SchedulerBuiltinFillFirst {
		return nil, errors.New("claude_cache_ttl.delegate_builtin must match the explicit CPA routing.strategy (round-robin or fill-first)")
	}
	raw, err := os.ReadFile(cfg.HostConfigPath)
	if err != nil {
		return nil, errors.New("cannot read Claude TTL host configuration")
	}
	var host struct {
		Payload map[string]any `yaml:"payload"`
		Routing struct {
			Strategy string `yaml:"strategy"`
		} `yaml:"routing"`
		Plugins struct {
			Configs map[string]struct {
				TTL claudeTTLConfig `yaml:"claude_cache_ttl"`
			} `yaml:"configs"`
		} `yaml:"plugins"`
	}
	if yaml.Unmarshal(raw, &host) != nil || host.Plugins.Configs[ID].TTL != cfg || host.Routing.Strategy != cfg.DelegateBuiltin {
		return nil, errors.New("Claude TTL configuration must match the loaded plugin settings and CPA routing.strategy")
	}
	if !reflect.DeepEqual(host.Payload, requiredTTLPayload()) {
		return nil, errors.New("Claude TTL requires the exact supported payload rules; merge or conflicting rules are not supported")
	}
	return &ttlConfigGuard{path: cfg.HostConfigPath, digest: sha256.Sum256(raw), delegate: cfg.DelegateBuiltin}, nil
}

func (g *ttlConfigGuard) check() error {
	if g == nil {
		return errors.New("Claude TTL enforcement is not configured")
	}
	if g.invalid.Load() {
		return errors.New("CPA configuration drift was detected; reload the plugin before retrying")
	}
	raw, err := os.ReadFile(g.path)
	if err != nil || sha256.Sum256(raw) != g.digest {
		g.invalid.Store(true)
		return errors.New("CPA configuration changed or became unreadable; reload the plugin with validated TTL rules before retrying")
	}
	return nil
}
