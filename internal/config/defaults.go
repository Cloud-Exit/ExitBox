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

// DefaultKeybindings returns the default keybinding configuration.
func DefaultKeybindings() KeybindingsConfig {
	return KeybindingsConfig{
		WorkspaceMenu: "C-M-p",
		SessionMenu:   "C-M-s",
	}
}

// EnvValue serializes keybindings to a compact string for passing via
// environment variable (e.g. "workspace_menu=C-M-p,session_menu=C-M-s").
// Returns empty string when both bindings are at their defaults.
func (kb KeybindingsConfig) EnvValue() string {
	def := DefaultKeybindings()
	wm := kb.WorkspaceMenu
	if wm == "" {
		wm = def.WorkspaceMenu
	}
	sm := kb.SessionMenu
	if sm == "" {
		sm = def.SessionMenu
	}
	if wm == def.WorkspaceMenu && sm == def.SessionMenu {
		return ""
	}
	return "workspace_menu=" + wm + ",session_menu=" + sm
}

// DefaultConfig returns a minimal default configuration.
func DefaultConfig() *Config {
	return &Config{
		Version: 1,
		Workspaces: WorkspaceCatalog{
			Active: "default",
			Items: []Workspace{
				{Name: "default"},
			},
		},
		Agents: AgentConfig{
			Claude:   AgentEntry{Enabled: false},
			Codex:    AgentEntry{Enabled: false},
			OpenCode: AgentEntry{Enabled: false},
		},
		Settings: SettingsConfig{
			AutoUpdate:       false,
			StatusBar:        true,
			DefaultWorkspace: "default",
			DefaultFlags: DefaultFlags{
				AutoResume: false,
			},
			Keybindings: DefaultKeybindings(),
		},
	}
}

// DefaultAllowlist returns the default domain allowlist.
func DefaultAllowlist() *Allowlist {
	return &Allowlist{
		Version: 1,
		AIProviders: []string{
			"anthropic.com",
			"claude.ai",
			"claude.com",
			"openai.com",
			"chatgpt.com",
			"oaiusercontent.com",
			"googleapis.com",
			"google.com",
			"azure.com",
			"microsoftonline.com",
			"amazonaws.com",
			"mistral.ai",
			"cohere.ai",
			"groq.com",
			"opencode.ai",
			"models.dev",
			"together.xyz",
			"together.ai",
			"replicate.com",
			"huggingface.co",
			"localhost",
			"127.0.0.1",
		},
		Development: []string{
			"github.com",
			"githubusercontent.com",
			"gitlab.com",
			"bitbucket.org",
			"npmjs.org",
			"npmjs.com",
			"pypi.org",
			"pythonhosted.org",
			"crates.io",
			"golang.org",
			"rubygems.org",
			"packagist.org",
			"getcomposer.org",
			"pub.dev",
		},
		CloudServices: []string{
			"googleapis.com",
			"amazonaws.com",
			"azure.com",
		},
		CommonServices: []string{
			"sentry.io",
			"statsig.com",
		},
	}
}
