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

import "testing"

func TestDefaultKeybindings(t *testing.T) {
	kb := DefaultKeybindings()
	if kb.WorkspaceMenu != "C-M-p" {
		t.Errorf("DefaultKeybindings().WorkspaceMenu = %q, want %q", kb.WorkspaceMenu, "C-M-p")
	}
	if kb.SessionMenu != "C-M-s" {
		t.Errorf("DefaultKeybindings().SessionMenu = %q, want %q", kb.SessionMenu, "C-M-s")
	}
}

func TestKeybindingsEnvValue_Defaults(t *testing.T) {
	kb := DefaultKeybindings()
	got := kb.EnvValue()
	if got != "" {
		t.Errorf("EnvValue() with defaults = %q, want empty string", got)
	}
}

func TestKeybindingsEnvValue_CustomWorkspaceMenu(t *testing.T) {
	kb := KeybindingsConfig{WorkspaceMenu: "C-b", SessionMenu: "C-M-s"}
	got := kb.EnvValue()
	want := "workspace_menu=C-b,session_menu=C-M-s"
	if got != want {
		t.Errorf("EnvValue() = %q, want %q", got, want)
	}
}

func TestKeybindingsEnvValue_CustomSessionMenu(t *testing.T) {
	kb := KeybindingsConfig{WorkspaceMenu: "C-M-p", SessionMenu: "F2"}
	got := kb.EnvValue()
	want := "workspace_menu=C-M-p,session_menu=F2"
	if got != want {
		t.Errorf("EnvValue() = %q, want %q", got, want)
	}
}

func TestKeybindingsEnvValue_BothCustom(t *testing.T) {
	kb := KeybindingsConfig{WorkspaceMenu: "F1", SessionMenu: "F2"}
	got := kb.EnvValue()
	want := "workspace_menu=F1,session_menu=F2"
	if got != want {
		t.Errorf("EnvValue() = %q, want %q", got, want)
	}
}

func TestKeybindingsEnvValue_EmptyFieldsUseDefaults(t *testing.T) {
	kb := KeybindingsConfig{}
	got := kb.EnvValue()
	if got != "" {
		t.Errorf("EnvValue() with empty fields = %q, want empty (defaults)", got)
	}
}

func TestDefaultConfig_HasKeybindingDefaults(t *testing.T) {
	cfg := DefaultConfig()
	kb := cfg.Settings.Keybindings
	if kb.WorkspaceMenu != "C-M-p" {
		t.Errorf("DefaultConfig().Settings.Keybindings.WorkspaceMenu = %q, want %q", kb.WorkspaceMenu, "C-M-p")
	}
	if kb.SessionMenu != "C-M-s" {
		t.Errorf("DefaultConfig().Settings.Keybindings.SessionMenu = %q, want %q", kb.SessionMenu, "C-M-s")
	}
}
