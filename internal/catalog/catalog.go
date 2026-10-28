// Package catalog evaluates model visibility using synchronized credential associations.
package catalog

import (
	"errors"
	"slices"
	"strings"
	"time"

	"cpa-helper-plugin/internal/policy"
)

// CPAVersion pins the management API that supplies registered public model IDs.
const CPAVersion = "v8.0.45"

// ErrUnknownModel indicates that the saved catalog does not cover a host model.
var ErrUnknownModel = errors.New("model absent from synchronized catalog")

// Credential contains only non-secret routing attributes and registered public IDs.
type Credential struct {
	Ref      string   `json:"ref"`
	Source   string   `json:"source"`
	Provider string   `json:"provider"`
	Enabled  bool     `json:"enabled"`
	Models   []string `json:"models"`
}

// Snapshot is a complete, independently revisioned model directory transaction.
type Snapshot struct {
	ContractVersion string       `json:"contract_version"`
	PluginVersion   string       `json:"plugin_version"`
	CPAVersion      string       `json:"cpa_version"`
	Revision        uint64       `json:"catalog_revision"`
	GeneratedAt     time.Time    `json:"generated_at"`
	Credentials     []Credential `json:"credentials"`
}

// Engine is an immutable index of registered models and their credential owners.
type Engine struct {
	models map[string][]Credential
}

// Clone detaches a snapshot from caller-owned slices.
func Clone(s Snapshot) Snapshot {
	s.Credentials = slices.Clone(s.Credentials)
	for i := range s.Credentials {
		s.Credentials[i].Models = slices.Clone(s.Credentials[i].Models)
	}
	return s
}

func validText(s string) bool {
	return s != "" && s == strings.TrimSpace(s) && !strings.ContainsAny(s, "\x00\r\n\t")
}

// Compile validates every association before publishing any of the directory.
func Compile(s Snapshot) (*Engine, error) {
	if s.PluginVersion != policy.Version || s.CPAVersion != CPAVersion {
		return nil, errors.New("unsupported model catalog producer or CPA version")
	}
	return compile(s)
}

// CompileStored accepts the last verified producer only when reloading durable
// state. It preserves the old provenance and does not relax new-write validation.
func CompileStored(s Snapshot) (*Engine, error) {
	if s.PluginVersion == "0.1.8" && s.CPAVersion == "v8.0.32" {
		return compile(s)
	}
	return Compile(s)
}

func compile(s Snapshot) (*Engine, error) {
	if s.ContractVersion != "v1" || s.Revision == 0 || s.Revision > 9007199254740991 || s.GeneratedAt.IsZero() || s.Credentials == nil {
		return nil, errors.New("invalid model catalog envelope")
	}
	e := &Engine{models: make(map[string][]Credential)}
	refs := make(map[string]bool, len(s.Credentials))
	for _, c := range s.Credentials {
		if !strings.HasPrefix(c.Ref, "sha256:") || !policy.ValidScope(strings.TrimPrefix(c.Ref, "sha256:")) || refs[c.Ref] || (c.Source != "auth-files" && c.Source != "ai-providers") || !validText(c.Provider) || c.Provider != strings.ToLower(c.Provider) || c.Models == nil {
			return nil, errors.New("invalid or duplicate model catalog credential")
		}
		refs[c.Ref] = true
		models := make(map[string]bool, len(c.Models))
		for _, model := range c.Models {
			id := strings.ToLower(model)
			if !validText(model) || models[id] {
				return nil, errors.New("invalid or duplicate registered model")
			}
			models[id] = true
			// Keep only the routing attributes; callers cannot mutate this index.
			e.models[id] = append(e.models[id], Credential{Ref: c.Ref, Source: c.Source, Provider: c.Provider, Enabled: c.Enabled})
		}
	}
	return e, nil
}

// CanRoute requires at least one enabled credential allowed by the effective rule.
// It does not schedule, check transient upstream health, or reserve a request.
func (e *Engine) CanRoute(rule policy.Rule, model string) (bool, error) {
	credentials, known := e.models[strings.ToLower(model)]
	if !known {
		return false, ErrUnknownModel
	}
	for _, c := range credentials {
		if c.Enabled && rule.AllowsCredential(c.Ref, c.Source, c.Provider) {
			return true, nil
		}
	}
	return false, nil
}
