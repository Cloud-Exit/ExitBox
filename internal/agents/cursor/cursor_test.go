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

package cursor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCursorAgent(t *testing.T) {
	c := &Cursor{}

	if c.Name() != "cursor" {
		t.Errorf("Name() = %q, want %q", c.Name(), "cursor")
	}
	if c.DisplayName() != "Cursor CLI" {
		t.Errorf("DisplayName() = %q, want %q", c.DisplayName(), "Cursor CLI")
	}

	paths := c.HostConfigPaths()
	if len(paths) != 1 {
		t.Fatalf("HostConfigPaths() returned %d paths, want 1", len(paths))
	}
	if !strings.HasSuffix(paths[0], ".cursor") {
		t.Errorf("HostConfigPaths()[0] = %q, want path ending in .cursor", paths[0])
	}

	mounts := c.ContainerMounts("/cfg")
	if len(mounts) != 1 {
		t.Fatalf("ContainerMounts() returned %d mounts, want 1", len(mounts))
	}
	if mounts[0].Target != "/home/user/.cursor" {
		t.Errorf("mounts[0].Target = %q, want /home/user/.cursor", mounts[0].Target)
	}

	if v := c.OllamaEnvVars("http://localhost:11434"); v != nil {
		t.Errorf("OllamaEnvVars() = %v, want nil (Cursor authenticates against its backend)", v)
	}

	df, err := c.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "cursor.com/install") {
		t.Error("GetDockerfileInstall() should install via the cursor.com install script")
	}
	if !strings.Contains(df, "/usr/local/bin/cursor-agent") {
		t.Error("GetDockerfileInstall() should expose cursor-agent on the global PATH")
	}
	if !strings.Contains(df, "/usr/local/bin/cursor") {
		t.Error("GetDockerfileInstall() should expose the binary as 'cursor' (the entrypoint launches by registry name)")
	}

	full, err := c.GetFullDockerfile("latest")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
	if !strings.Contains(full, "CURSOR_VERSION=latest") {
		t.Error("GetFullDockerfile() should include CURSOR_VERSION ARG")
	}
}

func TestCursorGetLatestVersion(t *testing.T) {
	c := &Cursor{}
	v, err := c.GetLatestVersion()
	if err != nil {
		t.Fatalf("GetLatestVersion() error: %v", err)
	}
	if v != "latest" {
		t.Errorf("GetLatestVersion() = %q, want latest", v)
	}
}

func TestCursorGetInstalledVersion_NilRuntime(t *testing.T) {
	c := &Cursor{}
	if _, err := c.GetInstalledVersion(nil, "some-image"); err == nil {
		t.Errorf("GetInstalledVersion(nil, ...) should return error")
	}
}

func TestCursorImportConfig(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "cli-config.json"), []byte(`{"version":1}`), 0644)

	c := &Cursor{}
	if err := c.ImportConfig(src, dst); err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".cursor", "cli-config.json")); err != nil {
		t.Errorf("expected .cursor/cli-config.json to exist: %v", err)
	}
}
