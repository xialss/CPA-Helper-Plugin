package policy

import (
	"fmt"
	"slices"
	"strings"
)

// ResponseModelConfig controls comparison against upstream-declared identity.
type ResponseModelConfig struct {
	Enabled              bool                `yaml:"enabled" json:"enabled"`
	StreamEnabled        bool                `yaml:"stream_enabled" json:"stream_enabled"`
	MaxBufferBytes       int                 `yaml:"max_buffer_bytes" json:"max_buffer_bytes"`
	MaxTotalBufferBytes  int                 `yaml:"max_total_buffer_bytes" json:"max_total_buffer_bytes"`
	Action               string              `yaml:"action" json:"action"`
	UnknownAction        string              `yaml:"unknown_action" json:"unknown_action"`
	CaseSensitive        bool                `yaml:"case_sensitive" json:"case_sensitive"`
	IgnoreThinkingSuffix bool                `yaml:"ignore_thinking_suffix" json:"ignore_thinking_suffix"`
	IgnoredModels        []string            `yaml:"ignored_models" json:"ignored_models"`
	Accepted             map[string][]string `yaml:"accepted_models" json:"accepted_models"`
}

// DefaultResponseModelConfig disables verification until explicitly enabled.
func DefaultResponseModelConfig() ResponseModelConfig {
	return ResponseModelConfig{StreamEnabled: true, MaxBufferBytes: 2 * 1024 * 1024, MaxTotalBufferBytes: 8 * 1024 * 1024, Action: "reject", UnknownAction: "pass", IgnoreThinkingSuffix: true, IgnoredModels: []string{}, Accepted: map[string][]string{}}
}

// Validate runs before configuration activation.
func (c ResponseModelConfig) Validate() error {
	if c.Action != "reject" && c.Action != "audit" {
		return fmt.Errorf("response_model_mismatch.action must be reject or audit")
	}
	if c.UnknownAction != "pass" && c.UnknownAction != "reject" {
		return fmt.Errorf("response_model_mismatch.unknown_action must be pass or reject")
	}
	if c.MaxBufferBytes < 1024*1024 || c.MaxBufferBytes > 256*1024*1024 {
		return fmt.Errorf("response_model_mismatch.max_buffer_bytes must be between 1048576 and 268435456")
	}
	if c.MaxTotalBufferBytes < c.MaxBufferBytes || c.MaxTotalBufferBytes > 1024*1024*1024 {
		return fmt.Errorf("response_model_mismatch.max_total_buffer_bytes must be at least max_buffer_bytes and no more than 1073741824")
	}
	seen := map[string]bool{}
	for from, values := range c.Accepted {
		key := c.Normalize(from)
		if key == "" || len(values) == 0 || seen[key] {
			return fmt.Errorf("response_model_mismatch.accepted_models contains an empty or duplicate normalized model")
		}
		seen[key] = true
		for _, v := range values {
			if c.Normalize(v) == "" {
				return fmt.Errorf("response_model_mismatch.accepted_models contains an empty target")
			}
		}
	}
	for _, v := range c.IgnoredModels {
		if c.Normalize(v) == "" {
			return fmt.Errorf("response_model_mismatch.ignored_models contains an empty model")
		}
	}
	return nil
}

// Clone detaches settings exposed to management callers.
func (c ResponseModelConfig) Clone() ResponseModelConfig {
	c.IgnoredModels = slices.Clone(c.IgnoredModels)
	m := make(map[string][]string, len(c.Accepted))
	for k, v := range c.Accepted {
		m[k] = slices.Clone(v)
	}
	c.Accepted = m
	return c
}

// Normalize removes configured presentation differences, never version suffixes.
func (c ResponseModelConfig) Normalize(model string) string {
	model = strings.TrimSpace(model)
	if c.IgnoreThinkingSuffix {
		if i := strings.LastIndex(model, "("); i >= 0 && strings.HasSuffix(model, ")") {
			model = strings.TrimSpace(model[:i])
		}
	}
	model = strings.TrimPrefix(model, "models/")
	if !c.CaseSensitive {
		model = strings.ToLower(model)
	}
	return model
}

// Ignores identifies explicit exemptions.
func (c ResponseModelConfig) Ignores(model string) bool {
	for _, v := range c.IgnoredModels {
		if c.Normalize(v) == c.Normalize(model) {
			return true
		}
	}
	return false
}

// Matches accepts equality or an explicit directional mapping.
func (c ResponseModelConfig) Matches(requested, actual string) bool {
	r, a := c.Normalize(requested), c.Normalize(actual)
	if r == "" || a == "" {
		return false
	}
	if r == a {
		return true
	}
	for k, values := range c.Accepted {
		if c.Normalize(k) == r {
			for _, v := range values {
				if c.Normalize(v) == a {
					return true
				}
			}
		}
	}
	return false
}
