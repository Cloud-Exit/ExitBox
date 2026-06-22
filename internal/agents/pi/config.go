// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package pi

import (
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/config"
)

// GenerateConfig produces a Pi models.json declaring a custom OpenAI-compatible
// provider + model, so `pi` can target a local/self-hosted server. Pi selects a
// model via /model (or Ctrl+L); this makes the configured one available.
func (p *Pi) GenerateConfig(cfg config.ServerConfig) (map[string]interface{}, error) {
	providerKey := cfg.ProviderID
	if providerKey == "" {
		providerKey = "local"
	}
	modelID := cfg.ModelID
	if modelID == "" {
		modelID = "default"
	}

	// Pi requires an apiKey field. When the key lives in the vault it is injected
	// as an env var at runtime, so reference it via Pi's "$ENV" interpolation
	// rather than writing the secret to disk. Local servers ignore the value.
	apiKey := cfg.APIKey
	if cfg.VaultKeyName != "" {
		apiKey = "$" + cfg.VaultKeyName
	} else if apiKey == "" {
		apiKey = "local"
	}

	model := map[string]interface{}{"id": modelID}
	if cfg.ModelName != "" {
		model["name"] = cfg.ModelName
	}

	return map[string]interface{}{
		"providers": map[string]interface{}{
			providerKey: map[string]interface{}{
				"baseUrl": cfg.BaseURL,
				"api":     "openai-completions",
				"apiKey":  apiKey,
				"models":  []map[string]interface{}{model},
			},
		},
	}, nil
}

// LogSearchDirs returns directories to search for Pi log/session files.
func (p *Pi) LogSearchDirs(home, agentCfgDir string) []string {
	return []string{
		filepath.Join(home, ".pi", "agent", "sessions"),
		filepath.Join(home, ".pi"),
		filepath.Join(agentCfgDir, ".pi", "agent", "sessions"),
		filepath.Join(agentCfgDir, ".pi"),
	}
}

func (p *Pi) ConfigFilePath(agentDir string) string {
	return filepath.Join(agentDir, ".pi", "agent", "models.json")
}

// ExtractConfigServerURLs walks providers.*.baseUrl in Pi's models.json.
func (p *Pi) ExtractConfigServerURLs(data map[string]interface{}) []string {
	providers, ok := data["providers"].(map[string]interface{})
	if !ok {
		return nil
	}
	var urls []string
	for _, pv := range providers {
		entry, ok := pv.(map[string]interface{})
		if !ok {
			continue
		}
		if baseURL, ok := entry["baseUrl"].(string); ok && baseURL != "" {
			urls = append(urls, baseURL)
		}
	}
	return urls
}
