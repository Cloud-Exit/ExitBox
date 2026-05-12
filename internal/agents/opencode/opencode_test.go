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

package opencode

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenCodeAgent(t *testing.T) {
	o := &OpenCode{}

	if o.Name() != "opencode" {
		t.Errorf("Name() = %q, want %q", o.Name(), "opencode")
	}
	if o.DisplayName() != "OpenCode" {
		t.Errorf("DisplayName() = %q, want %q", o.DisplayName(), "OpenCode")
	}

	// BinaryName
	bn := o.BinaryName()
	switch runtime.GOARCH {
	case "amd64":
		if bn != "opencode-linux-x64-musl.tar.gz" {
			t.Errorf("BinaryName() = %q on amd64", bn)
		}
	case "arm64":
		if bn != "opencode-linux-arm64-musl.tar.gz" {
			t.Errorf("BinaryName() = %q on arm64", bn)
		}
	}

	// HostConfigPaths
	paths := o.HostConfigPaths()
	if len(paths) != 2 {
		t.Fatalf("HostConfigPaths() returned %d paths, want 2", len(paths))
	}

	// ContainerMounts
	mounts := o.ContainerMounts("/cfg")
	if len(mounts) != 3 {
		t.Fatalf("ContainerMounts() returned %d mounts, want 3", len(mounts))
	}
	if mounts[0].Target != "/home/user/.opencode" {
		t.Errorf("mounts[0].Target = %q, want /home/user/.opencode", mounts[0].Target)
	}

	// GetDockerfileInstall
	df, err := o.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "api.github.com/repos/anomalyco/opencode") {
		t.Error("GetDockerfileInstall() should fetch release metadata from GitHub API")
	}
	if !strings.Contains(df, "sha256sum") {
		t.Error("GetDockerfileInstall() should verify SHA-256 checksum")
	}
	if !strings.Contains(df, "Checksum mismatch") {
		t.Error("GetDockerfileInstall() should fail on checksum mismatch")
	}
	if !strings.Contains(df, "/usr/local/bin/opencode") {
		t.Error("GetDockerfileInstall() should install opencode binary to /usr/local/bin")
	}
	if !strings.Contains(df, "/usr/local/bin/opencode --version") {
		t.Error("GetDockerfileInstall() should verify opencode binary works")
	}

	// GetFullDockerfile
	full, err := o.GetFullDockerfile("0.2.0")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
	if !strings.Contains(full, "OPENCODE_VERSION=0.2.0") {
		t.Error("GetFullDockerfile() should include OPENCODE_VERSION ARG")
	}
}

func TestOpenCodeGetInstalledVersion_NilRuntime(t *testing.T) {
	o := &OpenCode{}
	if _, err := o.GetInstalledVersion(nil, "some-image"); err == nil {
		t.Errorf("GetInstalledVersion(nil, ...) should return error")
	}
}

func TestOpenCodeImportConfig_DefaultDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "config.json"), []byte(`{}`), 0644)

	o := &OpenCode{}
	if err := o.ImportConfig(src, dst); err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".opencode", "config.json")); err != nil {
		t.Errorf("expected .opencode/config.json to exist: %v", err)
	}
}

func TestOpenCodeImportConfig_ConfigDir(t *testing.T) {
	src := filepath.Join(t.TempDir(), ".config", "opencode")
	dst := t.TempDir()
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{}`), 0644)

	o := &OpenCode{}
	if err := o.ImportConfig(src, dst); err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".config", "opencode", "settings.json")); err != nil {
		t.Errorf("expected .config/opencode/settings.json to exist: %v", err)
	}
}

func TestOpenCodeImportFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "opencode.json")
	content := []byte(`{"provider": "anthropic"}`)
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	o := &OpenCode{}
	if err := o.ImportFile(srcFile, dstDir); err != nil {
		t.Fatalf("ImportFile failed: %v", err)
	}

	target := filepath.Join(dstDir, ".config", "opencode", "opencode.json")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected file at %s: %v", target, err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", data, content)
	}
}
