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

const qwencodeGitHubRepo = "QwenLM/qwen-code"

// QwenCode implements the Agent interface for QwenCode.
type QwenCode struct{}

var _ Agent = (*QwenCode)(nil)

func (o *QwenCode) Name() string        { return "qwencode" }
func (o *QwenCode) DisplayName() string { return "QwenCode" }

func (o *QwenCode) GetLatestVersion() (string, error) {
	out, err := exec.Command("curl", "-s",
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", qwencodeGitHubRepo)).Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch QwenCode latest version: %w", err)
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

func (o *QwenCode) GetInstalledVersion(rt container.Runtime, img string) (string, error) {
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
func (o *QwenCode) BinaryName() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		return "qwencode-linux-x64-musl.tar.gz"
	case "arm64":
		return "qwencode-linux-arm64-musl.tar.gz"
	default:
		return ""
	}
}

func (o *QwenCode) GetDockerfileInstall(buildCtx string) (string, error) {
	return fmt.Sprintf(`# Install QwenCode binary with SHA-256 verification
ARG QWENCODE_VERSION
ARG QWENCODE_CHECKSUM
COPY %s /tmp/qwencode.tar.gz
RUN echo "${QWENCODE_CHECKSUM}  /tmp/qwencode.tar.gz" | sha256sum -c - && \
    tar -xzf /tmp/qwencode.tar.gz -C /usr/local/bin && \
    chmod +x /usr/local/bin/qwencode && \
    rm -f /tmp/qwencode.tar.gz`, o.BinaryName()), nil
}

// GetFullDockerfile returns the complete Dockerfile for QwenCode.
// Builds on exitbox-base with pre-downloaded musl binary (same pattern as Claude/Codex).
func (o *QwenCode) GetFullDockerfile(version string) (string, error) {
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

func (o *QwenCode) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".qwencode"),
		filepath.Join(home, ".config", "qwencode"),
	}
}

func (o *QwenCode) ContainerMounts(cfgDir string) []Mount {
	return []Mount{
		{Source: filepath.Join(cfgDir, ".qwencode"), Target: "/home/user/.qwencode"},
		{Source: filepath.Join(cfgDir, ".config", "qwencode"), Target: "/home/user/.config/qwencode"},
		{Source: filepath.Join(cfgDir, ".local", "share", "qwencode"), Target: "/home/user/.local/share/qwencode"},
	}
}

func (o *QwenCode) DetectHostConfig() (string, error) {
	home := os.Getenv("HOME")
	for _, p := range []string{
		filepath.Join(home, ".qwencode"),
		filepath.Join(home, ".config", "qwencode"),
	} {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("no QwenCode config found")
}

func (o *QwenCode) ImportConfig(src, dst string) error {
	if strings.Contains(src, filepath.Join(".config", "qwencode")) {
		target := filepath.Join(dst, ".config", "qwencode")
		_ = os.MkdirAll(target, 0755)
		return copyDirContents(src, target)
	}
	target := filepath.Join(dst, ".qwencode")
	_ = os.MkdirAll(target, 0755)
	return copyDirContents(src, target)
}
