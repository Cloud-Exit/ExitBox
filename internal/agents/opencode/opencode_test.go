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

	"github.com/cloud-exit/exitbox/internal/agent"
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

	// GetDockerfileInstall defaults to the 2.x install path.
	df, err := o.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "registry.npmjs.org/@opencode%2fcli-${OC_TARGET}") {
		t.Error("GetDockerfileInstall() should install the OpenCode 2.x npm platform package")
	}

	// GetFullDockerfile
	full, err := o.GetFullDockerfile("2.0.29")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
	if !strings.Contains(full, "OPENCODE_VERSION=2.0.29") {
		t.Error("GetFullDockerfile() should include OPENCODE_VERSION ARG")
	}
	if !strings.Contains(full, "ENV OPENCODE_DISABLE_AUTOUPDATE=1") {
		t.Error("GetFullDockerfile() should disable OpenCode self-updates")
	}
}

func TestOpenCodeDockerfile_V2(t *testing.T) {
	o := &OpenCode{}
	tests := []struct {
		name       string
		libc       string
		wantTarget string
		noTarget   string
	}{
		{name: "alpine musl", libc: "", wantTarget: `OC_TARGET="${OC_TARGET}-musl"`},
		{name: "glibc gpu", libc: "gnu", wantTarget: `OC_TARGET="${OC_TARGET}"`, noTarget: "-musl"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			df, err := o.fullDockerfile(agent.PrepareBuildInput{Version: "2.0.29", Libc: tc.libc})
			if err != nil {
				t.Fatalf("fullDockerfile() error: %v", err)
			}
			for _, want := range []string{
				tc.wantTarget,
				`OC_TARGET="${OC_TARGET}-baseline"`,
				"grep -qwi avx2 /proc/cpuinfo",
				".dist.integrity",
				"sha512sum",
				"^[a-f0-9]{128}$",
				"https://registry.npmjs.org/@opencode/cli-*",
				"Checksum mismatch",
				"package/bin/opencode /usr/local/bin/opencode",
				"/usr/local/bin/opencode --version",
			} {
				if !strings.Contains(df, want) {
					t.Errorf("v2 Dockerfile missing %q", want)
				}
			}
			if tc.noTarget != "" && strings.Contains(df, tc.noTarget) {
				t.Errorf("v2 Dockerfile should not contain %q", tc.noTarget)
			}
			if strings.Contains(df, "api.github.com") {
				t.Error("v2 Dockerfile should not use GitHub releases")
			}
		})
	}
}

func TestOpenCodeDockerfile_V1(t *testing.T) {
	o := &OpenCode{}
	df, err := o.fullDockerfile(agent.PrepareBuildInput{Version: "1.19.0"})
	if err != nil {
		t.Fatalf("fullDockerfile() error: %v", err)
	}
	if !strings.Contains(df, "api.github.com/repos/anomalyco/opencode") {
		t.Error("v1 Dockerfile should fetch release metadata from GitHub API")
	}
	if !strings.Contains(df, "ARG GITHUB_TOKEN") {
		t.Error("v1 Dockerfile should allow optional GITHUB_TOKEN build arg")
	}
	if strings.Contains(df, "Bearer ${GITHUB_TOKEN:-}") {
		t.Error("v1 Dockerfile should not send an empty Authorization header")
	}
	if !strings.Contains(df, "sha256sum") {
		t.Error("v1 Dockerfile should verify SHA-256 checksum")
	}
	if !strings.Contains(df, "Checksum mismatch") {
		t.Error("v1 Dockerfile should fail on checksum mismatch")
	}
	if !strings.Contains(df, "/usr/local/bin/opencode --version") {
		t.Error("v1 Dockerfile should verify opencode binary works")
	}
	if !strings.Contains(df, "sed '/^$/d' | wc -l") {
		t.Error("v1 Dockerfile should count zero fallback binary matches correctly")
	}
	if strings.Contains(df, "registry.npmjs.org") {
		t.Error("v1 Dockerfile should not use the 2.x npm packages")
	}
}

func TestOpenCodePrepareBuild_WritesPinnedVersion(t *testing.T) {
	for _, version := range []string{"1.19.0", "2.0.29"} {
		t.Run(version, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "Dockerfile")
			o := &OpenCode{}
			if err := o.PrepareBuild(agent.PrepareBuildInput{Version: version, DockerfilePath: path}); err != nil {
				t.Fatalf("PrepareBuild() error: %v", err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read Dockerfile: %v", err)
			}
			if !strings.Contains(string(data), "ARG OPENCODE_VERSION="+version) {
				t.Errorf("Dockerfile should pin OPENCODE_VERSION=%s", version)
			}
			wantV2 := strings.HasPrefix(version, "2.")
			if got := strings.Contains(string(data), "@opencode%2fcli-"); got != wantV2 {
				t.Errorf("v2 install path = %v, want %v", got, wantV2)
			}
		})
	}
}

func TestIsV2(t *testing.T) {
	tests := map[string]bool{
		"":        true,
		"2.0.29":  true,
		"v2.0.0":  true,
		"3.1.0":   true,
		"1.19.0":  false,
		"v1.0.0":  false,
		"0.15.31": false,
		"latest":  false,
	}
	for version, want := range tests {
		if got := IsV2(version); got != want {
			t.Errorf("IsV2(%q) = %v, want %v", version, got, want)
		}
	}
}

func TestParseRegistryVersion(t *testing.T) {
	if v, err := parseRegistryVersion([]byte(`{"name":"@opencode/cli","version":"2.0.29"}`)); err != nil || v != "2.0.29" {
		t.Errorf("parseRegistryVersion() = %q, %v; want 2.0.29", v, err)
	}
	if _, err := parseRegistryVersion([]byte(`{"error":"Not found"}`)); err == nil {
		t.Error("parseRegistryVersion() should fail on registry error")
	}
	if _, err := parseRegistryVersion([]byte(`{}`)); err == nil {
		t.Error("parseRegistryVersion() should fail on empty version")
	}
	if _, err := parseRegistryVersion([]byte(`not json`)); err == nil {
		t.Error("parseRegistryVersion() should fail on invalid JSON")
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
