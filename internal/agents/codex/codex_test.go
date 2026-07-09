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

package codex

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodexAgent(t *testing.T) {
	c := &Codex{}

	if c.Name() != "codex" {
		t.Errorf("Name() = %q, want %q", c.Name(), "codex")
	}
	if c.DisplayName() != "OpenAI Codex" {
		t.Errorf("DisplayName() = %q, want %q", c.DisplayName(), "OpenAI Codex")
	}

	// BinaryName
	bn := c.BinaryName()
	switch runtime.GOARCH {
	case "amd64":
		if bn != "codex-x86_64-unknown-linux-musl.tar.gz" {
			t.Errorf("BinaryName() = %q on amd64", bn)
		}
	case "arm64":
		if bn != "codex-aarch64-unknown-linux-musl.tar.gz" {
			t.Errorf("BinaryName() = %q on arm64", bn)
		}
	}

	// CodeModeHostBinaryName
	hbn := c.CodeModeHostBinaryName()
	switch runtime.GOARCH {
	case "amd64":
		if hbn != "codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz" {
			t.Errorf("CodeModeHostBinaryName() = %q on amd64", hbn)
		}
	case "arm64":
		if hbn != "codex-code-mode-host-aarch64-unknown-linux-musl.tar.gz" {
			t.Errorf("CodeModeHostBinaryName() = %q on arm64", hbn)
		}
	}

	// HostConfigPaths
	paths := c.HostConfigPaths()
	if len(paths) != 2 {
		t.Fatalf("HostConfigPaths() returned %d paths, want 2", len(paths))
	}

	// ContainerMounts
	mounts := c.ContainerMounts("/cfg")
	if len(mounts) != 2 {
		t.Fatalf("ContainerMounts() returned %d mounts, want 2", len(mounts))
	}
	if mounts[0].Target != "/home/user/.codex" {
		t.Errorf("mounts[0].Target = %q, want /home/user/.codex", mounts[0].Target)
	}

	// GetDockerfileInstall
	df, err := c.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "sha256sum") {
		t.Error("GetDockerfileInstall() should contain sha256sum verification")
	}
	if !strings.Contains(df, "apk add --no-cache bubblewrap") {
		t.Error("GetDockerfileInstall() should install bubblewrap")
	}
	if !strings.Contains(df, "$HOME/.local/bin/codex-code-mode-host") {
		t.Error("GetDockerfileInstall() should install codex-code-mode-host alongside codex")
	}
	if !strings.Contains(df, "CODEX_CODE_MODE_HOST_CHECKSUM") {
		t.Error("GetDockerfileInstall() should verify the codex-code-mode-host checksum")
	}

	// GetFullDockerfile
	full, err := c.GetFullDockerfile("v0.1.0")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
	if !strings.Contains(full, "CODEX_VERSION=v0.1.0") {
		t.Error("GetFullDockerfile() should include CODEX_VERSION ARG")
	}
}

func TestCodexGetInstalledVersion_NilRuntime(t *testing.T) {
	c := &Codex{}
	if _, err := c.GetInstalledVersion(nil, "some-image"); err == nil {
		t.Errorf("GetInstalledVersion(nil, ...) should return error")
	}
}

func TestCodexImportFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	srcFile := filepath.Join(srcDir, "config.toml")
	content := []byte("[model]\nprovider = \"custom\"\n")
	if err := os.WriteFile(srcFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	c := &Codex{}
	if err := c.ImportFile(srcFile, dstDir); err != nil {
		t.Fatalf("ImportFile failed: %v", err)
	}

	target := filepath.Join(dstDir, ".codex", "config.toml")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("expected file at %s: %v", target, err)
	}
	if string(data) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", data, content)
	}
}

func TestCodexImportFile_NonexistentSource(t *testing.T) {
	dstDir := t.TempDir()

	c := &Codex{}
	err := c.ImportFile("/nonexistent/file.toml", dstDir)
	if err == nil {
		t.Fatal("expected error for nonexistent source file")
	}
}

func TestCodexImportConfig_DefaultDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "config.json"), []byte(`{}`), 0644)

	c := &Codex{}
	if err := c.ImportConfig(src, dst); err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".codex", "config.json")); err != nil {
		t.Errorf("expected .codex/config.json to exist: %v", err)
	}
}

func TestCodexImportConfig_ConfigDir(t *testing.T) {
	src := filepath.Join(t.TempDir(), ".config", "codex")
	dst := t.TempDir()
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "config.json"), []byte(`{}`), 0644)

	c := &Codex{}
	if err := c.ImportConfig(src, dst); err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".config", "codex", "config.json")); err != nil {
		t.Errorf("expected .config/codex/config.json to exist: %v", err)
	}
}
