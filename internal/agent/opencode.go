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

package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cloud-exit/exitbox/internal/container"
)

const opencodeGitHubRepo = "anomalyco/opencode"

// OpenCode implements the Agent interface for OpenCode.
type OpenCode struct{}

func (o *OpenCode) Name() string        { return "opencode" }
func (o *OpenCode) DisplayName() string { return "OpenCode" }

func (o *OpenCode) GetLatestVersion() (string, error) {
	out, err := exec.Command("curl", "-s",
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", opencodeGitHubRepo)).Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch OpenCode latest version: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(out, &release); err != nil {
		return "", err
	}
	// Strip leading 'v' if present
	v := strings.TrimPrefix(release.TagName, "v")
	if v == "" {
		return "", fmt.Errorf("empty tag_name")
	}
	return v, nil
}

func (o *OpenCode) GetInstalledVersion(rt container.Runtime, img string) (string, error) {
	if rt == nil || !rt.ImageExists(img) {
		return "", fmt.Errorf("image %s not found", img)
	}
	out, err := rt.ImageInspect(img, `{{index .Config.Labels "exitbox.agent.version"}}`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// BinaryName returns the platform-specific binary tarball name (musl build for Alpine).
func (o *OpenCode) BinaryName() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		return "opencode-linux-x64-musl.tar.gz"
	case "arm64":
		return "opencode-linux-arm64-musl.tar.gz"
	default:
		return ""
	}
}

func (o *OpenCode) GetDockerfileInstall(buildCtx string) (string, error) {
	return fmt.Sprintf(`# Install OpenCode binary with SHA-256 verification
ARG OPENCODE_VERSION
ARG OPENCODE_CHECKSUM
COPY %s /tmp/opencode.tar.gz
RUN echo "${OPENCODE_CHECKSUM}  /tmp/opencode.tar.gz" | sha256sum -c - && \
    tar -xzf /tmp/opencode.tar.gz -C /usr/local/bin && \
    chmod +x /usr/local/bin/opencode && \
    rm -f /tmp/opencode.tar.gz`, o.BinaryName()), nil
}

// GetFullDockerfile returns the complete Dockerfile for OpenCode.
// Builds on exitbox-base with pre-downloaded musl binary (same pattern as Claude/Codex).
func (o *OpenCode) GetFullDockerfile(version string) (string, error) {
	install, err := o.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version != "" {
		df += fmt.Sprintf("ARG OPENCODE_VERSION=%s\n", version)
	}
	df += install
	return df, nil
}

func (o *OpenCode) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".opencode"),
		filepath.Join(home, ".config", "opencode"),
	}
}

func (o *OpenCode) ContainerMounts(cfgDir string) []Mount {
	return []Mount{
		{Source: filepath.Join(cfgDir, ".opencode"), Target: "/home/user/.opencode"},
		{Source: filepath.Join(cfgDir, ".config", "opencode"), Target: "/home/user/.config/opencode"},
		{Source: filepath.Join(cfgDir, ".local", "share", "opencode"), Target: "/home/user/.local/share/opencode"},
	}
}

func (o *OpenCode) DetectHostConfig() (string, error) {
	home := os.Getenv("HOME")
	for _, p := range []string{
		filepath.Join(home, ".opencode"),
		filepath.Join(home, ".config", "opencode"),
	} {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("no OpenCode config found")
}

func (o *OpenCode) ImportConfig(src, dst string) error {
	if strings.Contains(src, filepath.Join(".config", "opencode")) {
		target := filepath.Join(dst, ".config", "opencode")
		_ = os.MkdirAll(target, 0755)
		return copyDirContents(src, target)
	}
	target := filepath.Join(dst, ".opencode")
	_ = os.MkdirAll(target, 0755)
	return copyDirContents(src, target)
}
