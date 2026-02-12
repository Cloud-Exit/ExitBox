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

// Config is the top-level exitbox configuration (config.yaml).
type Config struct {
	Version        int              `yaml:"version"`
	Roles          []string         `yaml:"roles,omitempty"`
	Workspaces     WorkspaceCatalog `yaml:"workspaces,omitempty"`
	ToolCategories []string         `yaml:"tool_categories,omitempty"`
	Agents         AgentConfig      `yaml:"agents"`
	Tools          ToolsConfig      `yaml:"tools"`
	Settings       SettingsConfig   `yaml:"settings"`
}

// WorkspaceCatalog stores named workspaces and the active workspace name.
type WorkspaceCatalog struct {
	Active string      `yaml:"active,omitempty"`
	Items  []Workspace `yaml:"items,omitempty"`
}

// VaultConfig holds encrypted vault settings for a workspace.
type VaultConfig struct {
	Enabled bool `yaml:"enabled"`
}

// Workspace is a named workspace (e.g. personal/work) with development stacks.
type Workspace struct {
	Name        string      `yaml:"name"`
	Development []string    `yaml:"development,omitempty"`
	Packages    []string    `yaml:"packages,omitempty"`
	Directory   string      `yaml:"directory,omitempty"`
	Vault       VaultConfig `yaml:"vault,omitempty"`
}

// AgentConfig holds enable/disable state for each agent.
type AgentConfig struct {
	Claude   AgentEntry `yaml:"claude"`
	Codex    AgentEntry `yaml:"codex"`
	OpenCode AgentEntry `yaml:"opencode"`
}

// AgentEntry is the per-agent configuration.
type AgentEntry struct {
	Enabled bool `yaml:"enabled"`
}

// ToolsConfig holds user-specified extra packages.
type ToolsConfig struct {
	User     []string       `yaml:"user,omitempty"`
	Binaries []BinaryConfig `yaml:"binaries,omitempty"`
}

// BinaryConfig represents a tool installed via direct download.
type BinaryConfig struct {
	Name       string `yaml:"name"`
	URLPattern string `yaml:"url"` // URL with {arch} placeholder (amd64/arm64)
}

// SettingsConfig holds global settings.
type SettingsConfig struct {
	AutoUpdate       bool              `yaml:"auto_update"`
	StatusBar        bool              `yaml:"status_bar"`
	DefaultWorkspace string            `yaml:"default_workspace,omitempty"`
	DefaultFlags     DefaultFlags      `yaml:"default_flags"`
	Keybindings      KeybindingsConfig `yaml:"keybindings,omitempty"`
}

// KeybindingsConfig holds configurable tmux keybinding overrides.
type KeybindingsConfig struct {
	WorkspaceMenu string `yaml:"workspace_menu,omitempty"`
	SessionMenu   string `yaml:"session_menu,omitempty"`
}

// DefaultFlags holds the default CLI flag values.
type DefaultFlags struct {
	NoFirewall bool   `yaml:"no_firewall"`
	ReadOnly   bool   `yaml:"read_only"`
	NoEnv      bool   `yaml:"no_env"`
	AutoResume bool   `yaml:"auto_resume"`
	Memory     string `yaml:"memory,omitempty"`
	CPUs       string `yaml:"cpus,omitempty"`
}

// Allowlist is the domain allowlist (allowlist.yaml).
type Allowlist struct {
	Version        int      `yaml:"version"`
	AIProviders    []string `yaml:"ai_providers"`
	Development    []string `yaml:"development"`
	CloudServices  []string `yaml:"cloud_services"`
	CommonServices []string `yaml:"common_services"`
	Custom         []string `yaml:"custom,omitempty"`
}

// AllDomains returns all domains flattened and deduplicated.
func (a *Allowlist) AllDomains() []string {
	seen := make(map[string]struct{})
	var result []string
	for _, list := range [][]string{
		a.AIProviders,
		a.Development,
		a.CloudServices,
		a.CommonServices,
		a.Custom,
	} {
		for _, d := range list {
			if _, ok := seen[d]; !ok {
				seen[d] = struct{}{}
				result = append(result, d)
			}
		}
	}
	return result
}

// IsAgentEnabled returns whether the named agent is enabled.
func (c *Config) IsAgentEnabled(name string) bool {
	switch name {
	case "claude":
		return c.Agents.Claude.Enabled
	case "codex":
		return c.Agents.Codex.Enabled
	case "opencode":
		return c.Agents.OpenCode.Enabled
	}
	return false
}

// SetAgentEnabled sets the enable state for a named agent.
func (c *Config) SetAgentEnabled(name string, enabled bool) {
	switch name {
	case "claude":
		c.Agents.Claude.Enabled = enabled
	case "codex":
		c.Agents.Codex.Enabled = enabled
	case "opencode":
		c.Agents.OpenCode.Enabled = enabled
	}
}
