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

package config

import "os"

// EnsureDirs creates the exitbox directory structure if it doesn't exist.
func EnsureDirs() {
	dirs := []string{
		Home,
		Cache,
		Data,
		ProjectsDir(),
		AgentDir("claude"),
		AgentDir("codex"),
		AgentDir("opencode"),
	}
	for _, d := range dirs {
		_ = os.MkdirAll(d, 0755)
	}
}

// ConfigExists returns true if config.yaml exists.
func ConfigExists() bool {
	_, err := os.Stat(ConfigFile())
	return err == nil
}

// WriteDefaults writes default config and allowlist if they don't exist.
func WriteDefaults() error {
	if !ConfigExists() {
		if err := SaveConfig(DefaultConfig()); err != nil {
			return err
		}
	}
	if _, err := os.Stat(AllowlistFile()); os.IsNotExist(err) {
		if err := SaveAllowlist(DefaultAllowlist()); err != nil {
			return err
		}
	}
	return nil
}
