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

package cursor

import (
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/config"
)

// GenerateConfig produces a Cursor CLI cli-config.json config map. The Cursor CLI
// authenticates against Cursor's backend and selects models there, so it exposes no
// custom API base URL; only the file's schema version is written. Cursor merges this
// with the credentials/preferences it writes during interactive login.
func (c *Cursor) GenerateConfig(cfg config.ServerConfig) (map[string]interface{}, error) {
	return map[string]interface{}{
		"version": 1,
	}, nil
}

// LogSearchDirs returns directories to search for Cursor CLI log files.
func (c *Cursor) LogSearchDirs(home, agentCfgDir string) []string {
	return []string{
		filepath.Join(home, ".cursor", "logs"),
		filepath.Join(home, ".cursor"),
		filepath.Join(agentCfgDir, ".cursor", "logs"),
		filepath.Join(agentCfgDir, ".cursor"),
	}
}

func (c *Cursor) ConfigFilePath(agentDir string) string {
	return filepath.Join(agentDir, ".cursor", "cli-config.json")
}

// ExtractConfigServerURLs returns no URLs: the Cursor CLI talks to a fixed Cursor
// backend and exposes no configurable base URL in cli-config.json.
func (c *Cursor) ExtractConfigServerURLs(data map[string]interface{}) []string {
	return nil
}
