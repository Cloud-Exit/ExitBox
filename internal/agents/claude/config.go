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

package claude

import (
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
)

// GenerateConfig produces a Claude Code settings.json config map. Claude Code
// has no apiBaseUrl or apiKey setting; the documented route is the env block
// (https://code.claude.com/docs/en/settings). Claude Code appends /v1/messages
// itself, so a trailing /v1 from an OpenAI-style base URL is dropped.
func (c *Claude) GenerateConfig(cfg config.ServerConfig) (map[string]interface{}, error) {
	envBlock := map[string]interface{}{
		"ANTHROPIC_BASE_URL": anthropicBaseURL(cfg.BaseURL),
	}
	if cfg.APIKey != "" && cfg.VaultKeyName == "" {
		envBlock["ANTHROPIC_AUTH_TOKEN"] = cfg.APIKey
	}
	return map[string]interface{}{
		"env":   envBlock,
		"model": cfg.ModelID,
	}, nil
}

func anthropicBaseURL(u string) string {
	u = strings.TrimRight(u, "/")
	return strings.TrimSuffix(u, "/v1")
}

// LogSearchDirs returns directories to search for Claude Code log files.
func (c *Claude) LogSearchDirs(home, agentCfgDir string) []string {
	return []string{
		filepath.Join(home, ".claude"),
		filepath.Join(agentCfgDir, ".claude"),
	}
}

func (c *Claude) ConfigFilePath(agentDir string) string {
	return filepath.Join(agentDir, ".claude", "settings.json")
}

// ExtractConfigServerURLs reads env.ANTHROPIC_BASE_URL from a Claude config,
// plus the legacy top-level apiBaseUrl that older ExitBox versions wrote.
func (c *Claude) ExtractConfigServerURLs(data map[string]interface{}) []string {
	var urls []string
	if envBlock, ok := data["env"].(map[string]interface{}); ok {
		if baseURL, ok := envBlock["ANTHROPIC_BASE_URL"].(string); ok && baseURL != "" {
			urls = append(urls, baseURL)
		}
	}
	if baseURL, ok := data["apiBaseUrl"].(string); ok && baseURL != "" {
		urls = append(urls, baseURL)
	}
	return urls
}
