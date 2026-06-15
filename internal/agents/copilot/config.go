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

package copilot

import (
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/config"
)

// GenerateConfig produces a Copilot CLI settings.json config map. Copilot CLI is
// authenticated against GitHub and does not accept a custom API base URL, so only
// the model selection is written; an empty model leaves Copilot's default ("auto").
func (c *Copilot) GenerateConfig(cfg config.ServerConfig) (map[string]interface{}, error) {
	model := cfg.ModelID
	if model == "" {
		model = "auto"
	}
	return map[string]interface{}{
		"model": model,
	}, nil
}

// LogSearchDirs returns directories to search for Copilot CLI log files.
func (c *Copilot) LogSearchDirs(home, agentCfgDir string) []string {
	return []string{
		filepath.Join(home, ".copilot", "logs"),
		filepath.Join(home, ".copilot"),
		filepath.Join(agentCfgDir, ".copilot", "logs"),
		filepath.Join(agentCfgDir, ".copilot"),
	}
}

func (c *Copilot) ConfigFilePath(agentDir string) string {
	return filepath.Join(agentDir, ".copilot", "settings.json")
}

// ExtractConfigServerURLs returns no URLs: Copilot CLI talks to a fixed GitHub
// backend (api.githubcopilot.com) and exposes no configurable base URL.
func (c *Copilot) ExtractConfigServerURLs(data map[string]interface{}) []string {
	return nil
}
