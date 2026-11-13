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
	ExternalTools  []string         `yaml:"external_tools,omitempty"`
	Agents         AgentConfig      `yaml:"agents"`
	Tools          ToolsConfig      `yaml:"tools"`
	Settings       SettingsConfig   `yaml:"settings"`
	Web            WebConfig        `yaml:"web,omitempty"`
}

// DefaultWebListen is where exitbox web listens when nothing is configured.
const DefaultWebListen = "127.0.0.1:4848"

// WebConfig holds the exitbox web settings. An empty Username means the UI
// has no login.
type WebConfig struct {
	Listen       string `yaml:"listen,omitempty"`
	Username     string `yaml:"username,omitempty"`
	PasswordHash string `yaml:"password_hash,omitempty"` // bcrypt
	// RecentDays is how long an ended session shows as recently ended; 0 means the default.
	RecentDays int `yaml:"recent_days,omitempty"`
}

// DefaultWebRecentDays is how long an ended session shows as recently ended when nothing is configured.
const DefaultWebRecentDays = 3

// RecentDaysOrDefault returns the configured recently ended days or the default.
func (w WebConfig) RecentDaysOrDefault() int {
	if w.RecentDays <= 0 {
		return DefaultWebRecentDays
	}
	return w.RecentDays
}

// AuthEnabled reports whether the web UI asks for a login.
func (w WebConfig) AuthEnabled() bool {
	return w.Username != "" && w.PasswordHash != ""
}

// ListenOrDefault returns the configured listen address or the default.
func (w WebConfig) ListenOrDefault() string {
	if w.Listen == "" {
		return DefaultWebListen
	}
	return w.Listen
}

// WorkspaceCatalog stores named workspaces and the active workspace name.
type WorkspaceCatalog struct {
	Active string      `yaml:"active,omitempty"`
	Items  []Workspace `yaml:"items,omitempty"`
}

// VaultConfig holds encrypted vault settings for a workspace.
type VaultConfig struct {
	Enabled  bool `yaml:"enabled"`
	ReadOnly bool `yaml:"read_only,omitempty"`
}

// Workspace is a named workspace (e.g. personal/work) with development stacks.
type Workspace struct {
	Name        string      `yaml:"name"`
	Development []string    `yaml:"development,omitempty"`
	Packages    []string    `yaml:"packages,omitempty"`
	Directory   string      `yaml:"directory,omitempty"`
	Vault       VaultConfig `yaml:"vault,omitempty"`
	// GPU enables experimental GPU passthrough for containers started with
	// this workspace. It requires a working host GPU runtime (NVIDIA
	// Container Toolkit / CDI) and is off by default.
	GPU bool `yaml:"gpu,omitempty"`
	// Plugins holds per-agent plugin configuration for this workspace,
	// keyed by agent name and then by plugin name.
	Plugins PluginMap `yaml:"plugins,omitempty"`
}

// AgentConfig holds enable/disable state for each agent.
type AgentConfig struct {
	Claude   AgentEntry `yaml:"claude"`
	Codex    AgentEntry `yaml:"codex"`
	Copilot  AgentEntry `yaml:"copilot"`
	Cursor   AgentEntry `yaml:"cursor"`
	Kimi     AgentEntry `yaml:"kimi"`
	OpenCode AgentEntry `yaml:"opencode"`
	Pi       AgentEntry `yaml:"pi"`
	Qwen     AgentEntry `yaml:"qwen"`
}

// AgentEntry is the per-agent configuration.
type AgentEntry struct {
	Enabled bool   `yaml:"enabled"`
	Version string `yaml:"version,omitempty"`
}

// ToolsConfig holds user-specified extra packages and pinned tool versions.
type ToolsConfig struct {
	User     []string       `yaml:"user,omitempty"`
	Binaries []BinaryConfig `yaml:"binaries,omitempty"`
	// Versions pins a tool or language to a specific version. The special
	// value "latest" tracks the newest upstream version at image build time,
	// and an empty/missing entry uses the built-in default.
	Versions map[string]string `yaml:"versions,omitempty"`
}

// BinaryConfig represents a tool installed via direct download.
type BinaryConfig struct {
	Name       string `yaml:"name"`
	URLPattern string `yaml:"url"` // URL with {arch} placeholder (amd64/arm64)
}

// GetToolVersion returns the configured version for a managed tool or
// language. An empty string means the built-in default (e.g. the Alpine
// package version for apk-installed tools).
func (c *Config) GetToolVersion(name string) string {
	if c == nil || c.Tools.Versions == nil {
		return ""
	}
	return c.Tools.Versions[name]
}

// SetToolVersion pins a managed tool to version. An empty version or the
// special value "default" removes the pin and restores the built-in default.
func (c *Config) SetToolVersion(name, version string) {
	if c == nil {
		return
	}
	if c.Tools.Versions == nil {
		c.Tools.Versions = make(map[string]string)
	}
	if version == "" || version == "default" {
		delete(c.Tools.Versions, name)
		return
	}
	c.Tools.Versions[name] = version
}

// SettingsConfig holds global settings.
type SettingsConfig struct {
	AutoUpdate       bool              `yaml:"auto_update"`
	AutoUpdateTools  bool              `yaml:"auto_update_tools"`
	AlpineVersion    string            `yaml:"alpine_version,omitempty"` // base image Alpine release, e.g. "3.21"
	StatusBar        bool              `yaml:"status_bar"`
	RTK              bool              `yaml:"rtk"`
	DefaultWorkspace string            `yaml:"default_workspace,omitempty"`
	DefaultFlags     DefaultFlags      `yaml:"default_flags"`
	Keybindings      KeybindingsConfig `yaml:"keybindings,omitempty"`
}

// DefaultAlpineVersion is the Alpine release used when no base image
// version is configured.
const DefaultAlpineVersion = "3.21"

// AlpineVersionOrDefault returns the configured base image Alpine release,
// falling back to DefaultAlpineVersion when unset.
func (c *Config) AlpineVersionOrDefault() string {
	if c == nil || c.Settings.AlpineVersion == "" {
		return DefaultAlpineVersion
	}
	return c.Settings.AlpineVersion
}

// KeybindingsConfig holds configurable tmux keybinding overrides.
type KeybindingsConfig struct {
	WorkspaceMenu string `yaml:"workspace_menu,omitempty"`
	SessionMenu   string `yaml:"session_menu,omitempty"`
}

// DefaultFlags holds the default CLI flag values.
type DefaultFlags struct {
	NoFirewall     bool   `yaml:"no_firewall"`
	ReadOnly       bool   `yaml:"read_only"`
	NoEnv          bool   `yaml:"no_env"`
	AutoResume     bool   `yaml:"auto_resume"`
	FullGitSupport bool   `yaml:"full_git_support"`
	Memory         string `yaml:"memory,omitempty"`
	CPUs           string `yaml:"cpus,omitempty"`
}

// Allowlist is the domain allowlist (allowlist.yaml).
type Allowlist struct {
	Version        int      `yaml:"version"`
	AIProviders    []string `yaml:"ai_providers"`
	Development    []string `yaml:"development"`
	CloudServices  []string `yaml:"cloud_services"`
	CommonServices []string `yaml:"common_services"`
	HostPorts      []int    `yaml:"host_ports,omitempty"`
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
	case "copilot":
		return c.Agents.Copilot.Enabled
	case "cursor":
		return c.Agents.Cursor.Enabled
	case "kimi":
		return c.Agents.Kimi.Enabled
	case "opencode":
		return c.Agents.OpenCode.Enabled
	case "pi":
		return c.Agents.Pi.Enabled
	case "qwen":
		return c.Agents.Qwen.Enabled
	}
	return false
}

// GetAgentVersion returns the pinned version for a named agent, or "" if not set.
func (c *Config) GetAgentVersion(name string) string {
	switch name {
	case "claude":
		return c.Agents.Claude.Version
	case "codex":
		return c.Agents.Codex.Version
	case "copilot":
		return c.Agents.Copilot.Version
	case "cursor":
		return c.Agents.Cursor.Version
	case "kimi":
		return c.Agents.Kimi.Version
	case "opencode":
		return c.Agents.OpenCode.Version
	case "pi":
		return c.Agents.Pi.Version
	case "qwen":
		return c.Agents.Qwen.Version
	}
	return ""
}

// SetAgentEnabled sets the enable state for a named agent.
func (c *Config) SetAgentEnabled(name string, enabled bool) {
	switch name {
	case "claude":
		c.Agents.Claude.Enabled = enabled
	case "codex":
		c.Agents.Codex.Enabled = enabled
	case "copilot":
		c.Agents.Copilot.Enabled = enabled
	case "cursor":
		c.Agents.Cursor.Enabled = enabled
	case "kimi":
		c.Agents.Kimi.Enabled = enabled
	case "opencode":
		c.Agents.OpenCode.Enabled = enabled
	case "pi":
		c.Agents.Pi.Enabled = enabled
	case "qwen":
		c.Agents.Qwen.Enabled = enabled
	}
}
