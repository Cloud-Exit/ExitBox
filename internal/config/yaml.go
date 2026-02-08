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
	migrateProfilesToWorkspaces(data, &cfg)
	return &cfg, nil
}

// legacyConfig mirrors the old YAML keys so we can read configs that still
// use "profiles" / "default_profile" on disk.
type legacyConfig struct {
	Profiles struct {
		Active string `yaml:"active"`
		Items  []struct {
			Name        string   `yaml:"name"`
			Development []string `yaml:"development"`
		} `yaml:"items"`
	} `yaml:"profiles"`
	Settings struct {
		DefaultProfile string `yaml:"default_profile"`
	} `yaml:"settings"`
}

// migrateProfilesToWorkspaces reads legacy "profiles" / "default_profile" keys
// from raw YAML and copies them into the Workspaces / DefaultWorkspace fields
// when the new keys are absent (i.e. still at defaults from an old config).
func migrateProfilesToWorkspaces(data []byte, cfg *Config) {
	var legacy legacyConfig
	if err := yaml.Unmarshal(data, &legacy); err != nil {
		return
	}

	// Only migrate if the old key had data and the new key is at defaults.
	if len(legacy.Profiles.Items) > 0 && isDefaultWorkspaces(cfg) {
		cfg.Workspaces.Active = legacy.Profiles.Active
		cfg.Workspaces.Items = nil
		for _, item := range legacy.Profiles.Items {
			cfg.Workspaces.Items = append(cfg.Workspaces.Items, Workspace{
				Name:        item.Name,
				Development: item.Development,
			})
		}
	}

	if legacy.Settings.DefaultProfile != "" && cfg.Settings.DefaultWorkspace == "default" {
		cfg.Settings.DefaultWorkspace = legacy.Settings.DefaultProfile
	}
}

// isDefaultWorkspaces returns true if the workspaces catalog looks like
// the untouched default (single "default" workspace with no dev stack).
func isDefaultWorkspaces(cfg *Config) bool {
	if len(cfg.Workspaces.Items) != 1 {
		return false
	}
	return cfg.Workspaces.Items[0].Name == "default" && len(cfg.Workspaces.Items[0].Development) == 0
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
