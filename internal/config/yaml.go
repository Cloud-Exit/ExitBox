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

import (
	"os"

	"gopkg.in/yaml.v3"
)

// LoadConfig reads and parses config.yaml.
func LoadConfig() (*Config, error) {
	return LoadConfigFrom(ConfigFile())
}

// LoadConfigFrom reads config from a specific path.
func LoadConfigFrom(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := *DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	migrateTools(&cfg)
	return &cfg, nil
}

// packageReplacements maps deprecated Alpine packages to their replacements.
// Empty string means remove with no replacement.
var packageReplacements = map[string]string{
	"terraform": "opentofu",
	"ansible":   "",
	"docker":    "docker-cli",
	"node":      "nodejs",
}

// migrateTools replaces deprecated packages in the user tools list.
func migrateTools(cfg *Config) {
	var migrated []string
	seen := make(map[string]bool)
	for _, pkg := range cfg.Tools.User {
		if replacement, ok := packageReplacements[pkg]; ok {
			if replacement != "" && !seen[replacement] {
				seen[replacement] = true
				migrated = append(migrated, replacement)
			}
			continue
		}
		if !seen[pkg] {
			seen[pkg] = true
			migrated = append(migrated, pkg)
		}
	}
	cfg.Tools.User = migrated
}

// SaveConfig writes config to config.yaml.
func SaveConfig(cfg *Config) error {
	return SaveConfigTo(cfg, ConfigFile())
}

// SaveConfigTo writes config to a specific path.
func SaveConfigTo(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadAllowlist reads and parses allowlist.yaml.
func LoadAllowlist() (*Allowlist, error) {
	return LoadAllowlistFrom(AllowlistFile())
}

// LoadAllowlistFrom reads allowlist from a specific path.
func LoadAllowlistFrom(path string) (*Allowlist, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var al Allowlist
	if err := yaml.Unmarshal(data, &al); err != nil {
		return nil, err
	}
	return &al, nil
}

// SaveAllowlist writes allowlist to allowlist.yaml.
func SaveAllowlist(al *Allowlist) error {
	return SaveAllowlistTo(al, AllowlistFile())
}

// SaveAllowlistTo writes allowlist to a specific path.
func SaveAllowlistTo(al *Allowlist, path string) error {
	data, err := yaml.Marshal(al)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadProjectProfiles reads profiles.yaml from the given path.
func LoadProjectProfiles(path string) (*ProjectProfiles, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pp ProjectProfiles
	if err := yaml.Unmarshal(data, &pp); err != nil {
		return nil, err
	}
	return &pp, nil
}

// SaveProjectProfiles writes profiles.yaml to the given path.
func SaveProjectProfiles(pp *ProjectProfiles, path string) error {
	data, err := yaml.Marshal(pp)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadOrDefault loads config or returns defaults if file doesn't exist.
func LoadOrDefault() *Config {
	cfg, err := LoadConfig()
	if err != nil {
		return DefaultConfig()
	}
	return cfg
}

// LoadAllowlistOrDefault loads allowlist or returns defaults if file doesn't exist.
func LoadAllowlistOrDefault() *Allowlist {
	al, err := LoadAllowlist()
	if err != nil {
		return DefaultAllowlist()
	}
	return al
}
