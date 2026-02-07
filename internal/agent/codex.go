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

const codexGitHubRepo = "openai/codex"

// Codex implements the Agent interface for OpenAI Codex.
type Codex struct{}

func (c *Codex) Name() string        { return "codex" }
func (c *Codex) DisplayName() string { return "OpenAI Codex" }

// BinaryName returns the platform-specific binary tarball name.
func (c *Codex) BinaryName() string {
	arch := runtime.GOARCH
	switch arch {
	case "amd64":
		return "codex-x86_64-unknown-linux-musl.tar.gz"
	case "arm64":
		return "codex-aarch64-unknown-linux-musl.tar.gz"
	default:
		return ""
	}
}

func (c *Codex) GetLatestVersion() (string, error) {
	out, err := exec.Command("curl", "-s",
		fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", codexGitHubRepo)).Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch Codex latest version: %w", err)
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(out, &release); err != nil {
		return "", err
	}
	if release.TagName == "" {
		return "", fmt.Errorf("empty tag_name")
	}
	return release.TagName, nil
}

func (c *Codex) GetInstalledVersion(rt container.Runtime, img string) (string, error) {
	if rt == nil || !rt.ImageExists(img) {
		return "", fmt.Errorf("image %s not found", img)
	}
	out, err := rt.ImageInspect(img, `{{index .Config.Labels "exitbox.agent.version"}}`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Codex) GetDockerfileInstall(buildCtx string) (string, error) {
	binaryName := c.BinaryName()
	if binaryName == "" {
		return "", fmt.Errorf("unsupported architecture for Codex")
	}
	binaryInside := strings.TrimSuffix(binaryName, ".tar.gz")

	return fmt.Sprintf(`# Install Codex binary with SHA-256 verification
ARG CODEX_VERSION
ARG CODEX_CHECKSUM
COPY %s /tmp/codex.tar.gz
RUN echo "${CODEX_CHECKSUM}  /tmp/codex.tar.gz" | sha256sum -c - && \
    mkdir -p $HOME/.local/bin && \
    tar -xzf /tmp/codex.tar.gz -C /tmp && \
    mv /tmp/%s $HOME/.local/bin/codex && \
    chmod +x $HOME/.local/bin/codex && \
    rm -f /tmp/codex.tar.gz && \
    $HOME/.local/bin/codex --version`, binaryName, binaryInside), nil
}

func (c *Codex) GetFullDockerfile(version string) (string, error) {
	install, err := c.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version != "" {
		df += fmt.Sprintf("ARG CODEX_VERSION=%s\n", version)
	}
	df += install
	return df, nil
}

func (c *Codex) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".config", "codex"),
	}
}

func (c *Codex) ContainerMounts(cfgDir string) []Mount {
	return []Mount{
		{Source: filepath.Join(cfgDir, ".codex"), Target: "/home/user/.codex"},
		{Source: filepath.Join(cfgDir, ".config", "codex"), Target: "/home/user/.config/codex"},
	}
}

func (c *Codex) DetectHostConfig() (string, error) {
	home := os.Getenv("HOME")
	for _, p := range []string{
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".config", "codex"),
	} {
		if info, err := os.Stat(p); err == nil && info.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("no Codex config found")
}

func (c *Codex) ImportConfig(src, dst string) error {
	if strings.Contains(src, ".config/codex") {
		target := filepath.Join(dst, ".config", "codex")
		_ = os.MkdirAll(target, 0755)
		return copyDirContents(src, target)
	}
	target := filepath.Join(dst, ".codex")
	_ = os.MkdirAll(target, 0755)
	return copyDirContents(src, target)
}

func copyDirContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			_ = os.MkdirAll(dstPath, 0755)
			_ = copyDirContents(srcPath, dstPath)
		} else {
			data, err := os.ReadFile(srcPath)
			if err != nil {
				continue
			}
			_ = os.WriteFile(dstPath, data, 0644)
		}
	}
	return nil
}
