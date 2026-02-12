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

import "testing"

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
		"dfdsfs",    // random gibberish
		"C-",        // modifier with no key
		"M-",        // modifier with no key
		"C-M-",      // multiple modifiers with no key
		"F0",        // invalid function key
		"F21",       // out of range
		"ctrl+a",    // wrong notation (should be C-a)
		"Alt-b",     // wrong notation
		"Foo",       // not a valid special key
		"abc",       // multi-char non-special
		"",          // empty (handled before validation but test anyway)
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
