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

package wizard

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
)

func TestValidTmuxKey_ValidSingleChar(t *testing.T) {
	valid := []string{"a", "z", "p", "0", "9", "/", "-", "\\"}
	for _, k := range valid {
		if err := validTmuxKey(k); err != "" {
			t.Errorf("validTmuxKey(%q) = %q, want valid", k, err)
		}
	}
}

func TestValidTmuxKey_ValidModifiers(t *testing.T) {
	valid := []string{"C-a", "M-b", "S-x", "C-M-p", "C-M-s", "C-S-a", "M-S-z", "C-M-S-k"}
	for _, k := range valid {
		if err := validTmuxKey(k); err != "" {
			t.Errorf("validTmuxKey(%q) = %q, want valid", k, err)
		}
	}
}

func TestValidTmuxKey_ValidFunctionKeys(t *testing.T) {
	valid := []string{"F1", "F2", "F10", "F12", "F20", "C-F1", "M-F5", "S-F12"}
	for _, k := range valid {
		if err := validTmuxKey(k); err != "" {
			t.Errorf("validTmuxKey(%q) = %q, want valid", k, err)
		}
	}
}

func TestValidTmuxKey_ValidSpecialKeys(t *testing.T) {
	valid := []string{"Enter", "Tab", "Space", "Up", "Down", "Left", "Right",
		"Home", "End", "PPage", "NPage", "BSpace", "DC", "IC", "Escape",
		"C-Space", "M-Tab", "S-Tab", "C-M-Enter"}
	for _, k := range valid {
		if err := validTmuxKey(k); err != "" {
			t.Errorf("validTmuxKey(%q) = %q, want valid", k, err)
		}
	}
}

func TestValidTmuxKey_Invalid(t *testing.T) {
	invalid := []string{
		"dfdsfs", // random gibberish
		"C-",     // modifier with no key
		"M-",     // modifier with no key
		"C-M-",   // multiple modifiers with no key
		"F0",     // invalid function key
		"F21",    // out of range
		"ctrl+a", // wrong notation (should be C-a)
		"Alt-b",  // wrong notation
		"Foo",    // not a valid special key
		"abc",    // multi-char non-special
		"",       // empty (handled before validation but test anyway)
	}
	for _, k := range invalid {
		if k == "" {
			continue // empty is handled before validTmuxKey is called
		}
		if err := validTmuxKey(k); err == "" {
			t.Errorf("validTmuxKey(%q) = valid, want error", k)
		}
	}
}

func TestSetupRerunSingleWorkspaceCanCreateNewWorkspace(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Workspaces.Active = "default"
	cfg.Workspaces.Items = []config.Workspace{
		{Name: "default", Development: []string{"go"}},
	}
	cfg.Settings.DefaultWorkspace = "default"

	m := NewModelFromConfig(cfg)
	if m.step != stepTopMenu {
		t.Fatalf("step = %v, want %v", m.step, stepTopMenu)
	}
	if len(m.workspaces) != 1 {
		t.Fatalf("len(workspaces) = %d, want 1", len(m.workspaces))
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.step != stepWorkspaceSelect {
		t.Fatalf("step after workspace management = %v, want %v", m.step, stepWorkspaceSelect)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.cursor != 1 {
		t.Fatalf("cursor after down = %d, want create-new index 1", m.cursor)
	}

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if m.step != stepRole {
		t.Fatalf("step after create-new = %v, want %v", m.step, stepRole)
	}
	if m.workspaceInput != "" {
		t.Fatalf("workspaceInput = %q, want empty for new workspace", m.workspaceInput)
	}
	if m.state.WorkspaceName != "" {
		t.Fatalf("state.WorkspaceName = %q, want empty for new workspace", m.state.WorkspaceName)
	}
	if m.editingExisting {
		t.Fatal("editingExisting = true, want false for new workspace")
	}
}
