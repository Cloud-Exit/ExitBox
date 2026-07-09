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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/fsutil"
)

type Codex struct{}

var _ agent.Agent = (*Codex)(nil)

func (c *Codex) Name() string        { return "codex" }
func (c *Codex) DisplayName() string { return "OpenAI Codex" }
func (c *Codex) Description() string { return "OpenAI's coding CLI" }

func (c *Codex) OllamaEnvVars(ollamaBaseURL string) []string {
	return []string{
		"OPENAI_BASE_URL=" + ollamaBaseURL + "/v1",
	}
}

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

// CodeModeHostBinaryName returns the release tarball for codex-code-mode-host, the
// sibling binary recent Codex versions spawn for code review / "code mode". It ships
// as a separate release asset and must be installed alongside the codex binary.
func (c *Codex) CodeModeHostBinaryName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "codex-code-mode-host-x86_64-unknown-linux-musl.tar.gz"
	case "arm64":
		return "codex-code-mode-host-aarch64-unknown-linux-musl.tar.gz"
	default:
		return ""
	}
}

func (c *Codex) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".codex"),
		filepath.Join(home, ".config", "codex"),
	}
}

func (c *Codex) ContainerMounts(cfgDir string) []agent.Mount {
	return []agent.Mount{
		{Source: filepath.Join(cfgDir, ".codex"), Target: "/home/user/.codex"},
		{Source: filepath.Join(cfgDir, ".config", "codex"), Target: "/home/user/.config/codex"},
	}
}

func (c *Codex) EnsureWorkspaceAgentConfig(workspaceName string) error {
	if workspaceName == "" {
		return nil
	}
	root := config.WorkspaceAgentDir(workspaceName, c.Name())
	_ = os.MkdirAll(root, 0755)
	home := os.Getenv("HOME")

	codexDir := fsutil.EnsureDir(root, ".codex")
	fsutil.SeedDirOnce(filepath.Join(home, ".codex"), codexDir)

	codexCfg := fsutil.EnsureDir(root, ".config", "codex")
	fsutil.SeedDirOnce(filepath.Join(home, ".config", "codex"), codexCfg)
	return nil
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
	if strings.Contains(src, filepath.Join(".config", "codex")) {
		target := filepath.Join(dst, ".config", "codex")
		_ = os.MkdirAll(target, 0755)
		return fsutil.CopyDir(src, target)
	}
	target := filepath.Join(dst, ".codex")
	_ = os.MkdirAll(target, 0755)
	return fsutil.CopyDir(src, target)
}

func (c *Codex) ImportFile(src, dst string) error {
	target := filepath.Join(dst, ".codex", filepath.Base(src))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0644)
}
