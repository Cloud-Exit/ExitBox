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

package copilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCopilotAgent(t *testing.T) {
	c := &Copilot{}

	if c.Name() != "copilot" {
		t.Errorf("Name() = %q, want %q", c.Name(), "copilot")
	}
	if c.DisplayName() != "GitHub Copilot CLI" {
		t.Errorf("DisplayName() = %q, want %q", c.DisplayName(), "GitHub Copilot CLI")
	}

	paths := c.HostConfigPaths()
	if len(paths) != 1 {
		t.Fatalf("HostConfigPaths() returned %d paths, want 1", len(paths))
	}
	if !strings.HasSuffix(paths[0], ".copilot") {
		t.Errorf("HostConfigPaths()[0] = %q, want path ending in .copilot", paths[0])
	}

	mounts := c.ContainerMounts("/cfg")
	if len(mounts) != 1 {
		t.Fatalf("ContainerMounts() returned %d mounts, want 1", len(mounts))
	}
	if mounts[0].Target != "/home/user/.copilot" {
		t.Errorf("mounts[0].Target = %q, want /home/user/.copilot", mounts[0].Target)
	}

	if v := c.OllamaEnvVars("http://localhost:11434"); v != nil {
		t.Errorf("OllamaEnvVars() = %v, want nil (Copilot is GitHub-authenticated)", v)
	}

	df, err := c.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "npm install") {
		t.Error("GetDockerfileInstall() should install via npm")
	}
	if !strings.Contains(df, "@github/copilot") {
		t.Error("GetDockerfileInstall() should reference @github/copilot")
	}

	full, err := c.GetFullDockerfile("1.0.62")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
	if !strings.Contains(full, "COPILOT_VERSION=1.0.62") {
		t.Error("GetFullDockerfile() should include COPILOT_VERSION ARG")
	}
}

func TestCopilotGetInstalledVersion_NilRuntime(t *testing.T) {
	c := &Copilot{}
	if _, err := c.GetInstalledVersion(nil, "some-image"); err == nil {
		t.Errorf("GetInstalledVersion(nil, ...) should return error")
	}
}

func TestCopilotImportConfig(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{}`), 0644)

	c := &Copilot{}
	if err := c.ImportConfig(src, dst); err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".copilot", "settings.json")); err != nil {
		t.Errorf("expected .copilot/settings.json to exist: %v", err)
	}
}
