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

package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPiAgent(t *testing.T) {
	p := &Pi{}

	if p.Name() != "pi" {
		t.Errorf("Name() = %q, want %q", p.Name(), "pi")
	}
	if p.DisplayName() != "Pi Coding Agent" {
		t.Errorf("DisplayName() = %q, want %q", p.DisplayName(), "Pi Coding Agent")
	}

	paths := p.HostConfigPaths()
	if len(paths) != 1 {
		t.Fatalf("HostConfigPaths() returned %d paths, want 1", len(paths))
	}
	if !strings.HasSuffix(paths[0], ".pi") {
		t.Errorf("HostConfigPaths()[0] = %q, want path ending in .pi", paths[0])
	}

	mounts := p.ContainerMounts("/cfg")
	if len(mounts) != 1 {
		t.Fatalf("ContainerMounts() returned %d mounts, want 1", len(mounts))
	}
	if mounts[0].Target != "/home/user/.pi" {
		t.Errorf("mounts[0].Target = %q, want /home/user/.pi", mounts[0].Target)
	}

	df, err := p.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "npm install") {
		t.Error("GetDockerfileInstall() should install via npm")
	}
	if !strings.Contains(df, "@earendil-works/pi-coding-agent") {
		t.Error("GetDockerfileInstall() should reference @earendil-works/pi-coding-agent")
	}
	if !strings.Contains(df, "--ignore-scripts") {
		t.Error("GetDockerfileInstall() should use --ignore-scripts (upstream guidance)")
	}

	full, err := p.GetFullDockerfile("0.79.10")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
	if !strings.Contains(full, "PI_VERSION=0.79.10") {
		t.Error("GetFullDockerfile() should include PI_VERSION ARG")
	}
}

func TestPiGetInstalledVersion_NilRuntime(t *testing.T) {
	p := &Pi{}
	if _, err := p.GetInstalledVersion(nil, "some-image"); err == nil {
		t.Errorf("GetInstalledVersion(nil, ...) should return error")
	}
}

func TestPiImportConfig(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{}`), 0644)

	p := &Pi{}
	if err := p.ImportConfig(src, dst); err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".pi", "settings.json")); err != nil {
		t.Errorf("expected .pi/settings.json to exist: %v", err)
	}
}
