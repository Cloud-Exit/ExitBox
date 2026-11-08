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

package opencode

import (
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/config"
)

// GenerateConfig produces an OpenCode config map in the 1.x schema, which
// OpenCode 2.x migrates on load, so it works on either pinned major.
func (o *OpenCode) GenerateConfig(cfg config.ServerConfig) (map[string]interface{}, error) {
	result := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]interface{}{
			cfg.ProviderID: map[string]interface{}{
				"npm":  "@ai-sdk/openai-compatible",
				"name": cfg.ProviderName,
				"options": map[string]interface{}{
					"baseURL": cfg.BaseURL,
				},
				"models": map[string]interface{}{
					cfg.ModelID: map[string]interface{}{
						"name": cfg.ModelName,
					},
				},
			},
		},
		"model": cfg.ProviderID + "/" + cfg.ModelID,
	}
	if cfg.Compaction {
		result["compaction"] = map[string]interface{}{
			"auto":  true,
			"prune": true,
		}
	}
	return result, nil
}

// LogSearchDirs returns directories to search for OpenCode log files.
func (o *OpenCode) LogSearchDirs(home, agentCfgDir string) []string {
	return []string{
		filepath.Join(home, ".local", "share", "opencode", "log"),
		filepath.Join(home, ".local", "share", "opencode", "logs"),
		filepath.Join(home, ".opencode"),
		filepath.Join(agentCfgDir, ".opencode"),
		filepath.Join(agentCfgDir, ".config", "opencode"),
	}
}

func (o *OpenCode) ConfigFilePath(agentDir string) string {
	return filepath.Join(agentDir, ".config", "opencode", "opencode.json")
}

// ExtractConfigServerURLs walks provider.*.options.baseURL (1.x) and
// providers.*.settings.baseURL (2.x) in an OpenCode config.
func (o *OpenCode) ExtractConfigServerURLs(data map[string]interface{}) []string {
	var urls []string
	urls = append(urls, providerBaseURLs(data, "provider", "options")...)
	urls = append(urls, providerBaseURLs(data, "providers", "settings")...)
	return urls
}

func providerBaseURLs(data map[string]interface{}, providersKey, optionsKey string) []string {
	providers, ok := data[providersKey].(map[string]interface{})
	if !ok {
		return nil
	}
	var urls []string
	for _, pv := range providers {
		provider, ok := pv.(map[string]interface{})
		if !ok {
			continue
		}
		opts, ok := provider[optionsKey].(map[string]interface{})
		if !ok {
			continue
		}
		if baseURL, ok := opts["baseURL"].(string); ok && baseURL != "" {
			urls = append(urls, baseURL)
		}
	}
	return urls
}
