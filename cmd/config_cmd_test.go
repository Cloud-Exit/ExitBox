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

package cmd

import (
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/env"
)

// TestDefaultEnvProfile verifies that `config edit` (via defaultEnvProfile) targets
// the workspace's default env profile, matching what `exitbox run` loads.
func TestDefaultEnvProfile(t *testing.T) {
	oldData := config.Data
	config.Data = t.TempDir()
	t.Cleanup(func() { config.Data = oldData })

	ws := "ws-config-edit-test"

	// No default set → empty (falls back to base config).
	if got := defaultEnvProfile(ws); got != "" {
		t.Errorf("defaultEnvProfile with no default = %q, want \"\"", got)
	}

	// Save a profile and mark it default → it is returned.
	if err := env.Save(ws, &env.Profile{Name: "openrouter", Vars: map[string]string{"OPENAI_API_KEY": "x"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := env.SetDefault(ws, "openrouter"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if got := defaultEnvProfile(ws); got != "openrouter" {
		t.Errorf("defaultEnvProfile = %q, want openrouter", got)
	}
}
