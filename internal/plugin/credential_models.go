package plugin

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"cpa-helper-plugin/internal/policy"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// credentialModels deliberately omits the host's raw credential ID and account data.
type credentialModels struct {
	Ref       string   `json:"ref"`
	AuthIndex string   `json:"auth_index"`
	Provider  string   `json:"provider"`
	Models    []string `json:"models"`
}

func hostText(s string) bool {
	return s != "" && s == strings.TrimSpace(s) && !strings.ContainsAny(s, "\x00\r\n\t")
}

func (a *App) registeredCredentialModels(callback string) ([]credentialModels, error) {
	if a.host == nil {
		return nil, errors.New("host credential models unavailable")
	}
	raw, err := a.host(pluginabi.MethodHostAuthModels, map[string]string{"host_callback_id": callback})
	if err != nil {
		return nil, errors.New("host credential models failed")
	}
	var response pluginapi.HostAuthModelsResponse
	if err := json.Unmarshal(raw, &response); err != nil || response.Credentials == nil {
		return nil, errors.New("invalid host credential models")
	}
	out := make([]credentialModels, 0, len(response.Credentials))
	seen := make(map[string]bool, len(response.Credentials))
	for _, entry := range response.Credentials {
		if !hostText(entry.ID) || !hostText(entry.AuthIndex) || !hostText(entry.Provider) || seen[entry.ID] || entry.Models == nil {
			return nil, errors.New("incomplete or duplicate host credential models")
		}
		seen[entry.ID] = true
		models := make([]string, 0, len(entry.Models))
		modelSet := make(map[string]bool, len(entry.Models))
		for _, model := range entry.Models {
			if !hostText(model) {
				return nil, errors.New("invalid registered model identifier")
			}
			key := strings.ToLower(model)
			if !modelSet[key] {
				models = append(models, model)
				modelSet[key] = true
			}
		}
		sort.Strings(models)
		out = append(out, credentialModels{Ref: policy.CredentialRef(entry.ID), AuthIndex: entry.AuthIndex, Provider: strings.ToLower(entry.Provider), Models: models})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out, nil
}
