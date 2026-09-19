// Package policy implements transport-independent key routing decisions.
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Version identifies this plugin's policy producer. Release builds override it
// with the Git tag through Go's string-variable linker flag.
var Version = "0.1.1"

func compatiblePluginVersion(version string) bool {
	// v0.1.1 changed host configuration only; the v1 policy shape is unchanged.
	return version == Version || version == "0.1.0"
}

// Selector identifies a credential category without exposing credentials.
type Selector struct {
	Source   string `json:"source"`
	Provider string `json:"provider"`
}

// Rule unions allow and deny selections; denial always wins.
type Rule struct {
	Models                    []string   `json:"models,omitempty"`
	DeniedModels              []string   `json:"denied_models,omitempty"`
	CredentialIDs             []string   `json:"credential_ids,omitempty"`
	DeniedCredentialIDs       []string   `json:"denied_credential_ids,omitempty"`
	CredentialProviders       []Selector `json:"credential_providers,omitempty"`
	DeniedCredentialProviders []Selector `json:"denied_credential_providers,omitempty"`
}

// Group is a reusable routing rule, with a stable identifier.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
	Rule Rule   `json:"rule"`
}

// Key configures one CPA caller scope, never a raw API key.
type Key struct {
	ID             string   `json:"id"`
	Label          string   `json:"label,omitempty"`
	Enabled        bool     `json:"enabled"`
	GroupIDs       []string `json:"group_ids"`
	MaxConcurrency int      `json:"max_concurrency"`
	Rule           Rule     `json:"rule"`
}

// Snapshot is the complete, versioned policy transaction.
type Snapshot struct {
	ContractVersion string    `json:"contract_version"`
	PluginVersion   string    `json:"plugin_version"`
	Revision        uint64    `json:"policy_revision"`
	GeneratedAt     time.Time `json:"generated_at"`
	Groups          []Group   `json:"groups"`
	Keys            []Key     `json:"keys"`
}

// Engine is immutable once compiled and safe for concurrent reads.
type Engine struct{ keys map[string]Key }

// Initial explicitly permits CPA-authenticated keys without extra restrictions.
func Initial() Snapshot { return Snapshot{"v1", Version, 1, time.Now().UTC(), []Group{}, []Key{}} }

// CallerScope matches CPA v7.2.143's public caller identity contract.
func CallerScope(key string) string {
	sum := sha256.Sum256([]byte("cli-proxy-api:caller-scope:v1\x00" + strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

// CredentialRef is the reference plugin's stable, non-secret credential fingerprint.
func CredentialRef(id string) string {
	sum := sha256.Sum256([]byte("cpa-key-billing:credential:v1\x00" + strings.TrimSpace(id)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ValidScope validates CPA's hexadecimal caller scope.
func ValidScope(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && len(b) == 32 && id == strings.ToLower(id)
}

// Compile validates and pre-merges all key rules in a snapshot.
func Compile(s Snapshot) (*Engine, error) {
	if s.ContractVersion != "v1" || !compatiblePluginVersion(s.PluginVersion) || s.Revision == 0 || s.Revision > 9007199254740991 || s.GeneratedAt.IsZero() || s.Keys == nil || s.Groups == nil {
		return nil, fmt.Errorf("invalid snapshot envelope")
	}
	groups := make(map[string]Rule, len(s.Groups))
	for _, g := range s.Groups {
		if !validText(g.ID) || !validText(g.Name) {
			return nil, fmt.Errorf("invalid group identity")
		}
		if _, ok := groups[g.ID]; ok {
			return nil, fmt.Errorf("duplicate group")
		}
		if err := validateRule(g.Rule); err != nil {
			return nil, err
		}
		groups[g.ID] = merge(Rule{}, g.Rule)
	}
	e := &Engine{keys: make(map[string]Key, len(s.Keys))}
	for _, k := range s.Keys {
		if !ValidScope(k.ID) || k.MaxConcurrency < 0 || uint64(k.MaxConcurrency) > 9007199254740991 || k.GroupIDs == nil {
			return nil, fmt.Errorf("invalid key configuration")
		}
		if _, ok := e.keys[k.ID]; ok {
			return nil, fmt.Errorf("duplicate key")
		}
		if err := validateRule(k.Rule); err != nil {
			return nil, err
		}
		k.Rule = merge(Rule{}, k.Rule)
		k.GroupIDs = slices.Clone(k.GroupIDs)
		seen := map[string]bool{}
		for _, id := range k.GroupIDs {
			r, ok := groups[id]
			if !ok || seen[id] {
				return nil, fmt.Errorf("missing or duplicate group binding")
			}
			seen[id] = true
			k.Rule = merge(k.Rule, r)
		}
		e.keys[k.ID] = k
	}
	return e, nil
}

// Resolve returns an unrestricted key only when no explicit policy is bound.
func (e *Engine) Resolve(scope string) Key {
	if k, ok := e.keys[scope]; ok {
		return k
	}
	return Key{ID: scope, Enabled: true}
}

func validText(s string) bool {
	return s != "" && s == strings.TrimSpace(s) && !strings.ContainsAny(s, "\x00\r\n\t")
}
func contains(values []string, value string) bool {
	return slices.ContainsFunc(values, func(v string) bool { return strings.EqualFold(v, value) })
}

func validateRule(r Rule) error {
	for _, pair := range [][2][]string{{r.Models, r.DeniedModels}, {r.CredentialIDs, r.DeniedCredentialIDs}} {
		for _, list := range pair {
			seen := map[string]bool{}
			for _, v := range list {
				n := strings.ToLower(v)
				if !validText(v) || seen[n] {
					return fmt.Errorf("invalid or duplicate rule selection")
				}
				seen[n] = true
			}
		}
		for _, v := range pair[0] {
			if contains(pair[1], v) {
				return fmt.Errorf("selection cannot be both allowed and denied within a rule")
			}
		}
	}
	for _, list := range [][]string{r.CredentialIDs, r.DeniedCredentialIDs} {
		for _, id := range list {
			if !strings.HasPrefix(id, "sha256:") || !ValidScope(strings.TrimPrefix(id, "sha256:")) {
				return fmt.Errorf("invalid credential reference")
			}
		}
	}
	for _, list := range [][]Selector{r.CredentialProviders, r.DeniedCredentialProviders} {
		seen := map[Selector]bool{}
		for _, s := range list {
			if (s.Source != "auth-files" && s.Source != "ai-providers") || !validText(s.Provider) || s.Provider != strings.ToLower(s.Provider) || seen[s] {
				return fmt.Errorf("invalid credential category")
			}
			seen[s] = true
		}
	}
	for _, s := range r.CredentialProviders {
		if slices.Contains(r.DeniedCredentialProviders, s) {
			return fmt.Errorf("category cannot be both allowed and denied within a rule")
		}
	}
	return nil
}

func join[T comparable](a, b []T) []T {
	out := slices.Clone(a)
	for _, v := range b {
		if !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	return out
}
func merge(a, b Rule) Rule {
	return Rule{join(a.Models, b.Models), join(a.DeniedModels, b.DeniedModels), join(a.CredentialIDs, b.CredentialIDs), join(a.DeniedCredentialIDs, b.DeniedCredentialIDs), join(a.CredentialProviders, b.CredentialProviders), join(a.DeniedCredentialProviders, b.DeniedCredentialProviders)}
}

// AllowsModel compares public model identifiers case-insensitively.
func (r Rule) AllowsModel(model string) bool {
	return !contains(r.DeniedModels, model) && (len(r.Models) == 0 || contains(r.Models, model))
}

// AllowsCredential applies category and individual credential restrictions.
func (r Rule) AllowsCredential(ref, source, provider string) bool {
	s := Selector{strings.ToLower(source), strings.ToLower(provider)}
	if contains(r.DeniedCredentialIDs, ref) {
		return false
	}
	for _, d := range r.DeniedCredentialProviders {
		if (source == "" || s.Source == d.Source) && (provider == "" || s.Provider == d.Provider) {
			return false
		}
	}
	return len(r.CredentialIDs) == 0 && len(r.CredentialProviders) == 0 || contains(r.CredentialIDs, ref) || slices.Contains(r.CredentialProviders, s)
}
