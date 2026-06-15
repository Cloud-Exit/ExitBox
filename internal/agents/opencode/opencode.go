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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/fsutil"
)

type OpenCode struct{}

var _ agent.Agent = (*OpenCode)(nil)

func (o *OpenCode) Name() string        { return "opencode" }
func (o *OpenCode) DisplayName() string { return "OpenCode" }
func (o *OpenCode) Description() string { return "Open-source AI code assistant" }

func (o *OpenCode) OllamaEnvVars(ollamaBaseURL string) []string {
	return []string{
		"OLLAMA_HOST=" + ollamaBaseURL,
	}
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

// NpmPackageName returns the platform-specific npm package that contains the
// actual OpenCode binary for this architecture.
func (o *OpenCode) NpmPackageName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "opencode-linux-x64"
	case "arm64":
		return "opencode-linux-arm64"
	default:
		return ""
	}
}

func (o *OpenCode) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".opencode"),
		filepath.Join(home, ".config", "opencode"),
	}
}

func (o *OpenCode) ContainerMounts(cfgDir string) []agent.Mount {
	return []agent.Mount{
		{Source: filepath.Join(cfgDir, ".opencode"), Target: "/home/user/.opencode"},
		{Source: filepath.Join(cfgDir, ".config", "opencode"), Target: "/home/user/.config/opencode"},
		{Source: filepath.Join(cfgDir, ".local", "share", "opencode"), Target: "/home/user/.local/share/opencode"},
	}
}

func (o *OpenCode) EnsureWorkspaceAgentConfig(workspaceName string) error {
	if workspaceName == "" {
		return nil
	}
	root := config.WorkspaceAgentDir(workspaceName, o.Name())
	_ = os.MkdirAll(root, 0755)
	home := os.Getenv("HOME")

	ocDir := fsutil.EnsureDir(root, ".opencode")
	fsutil.SeedDirOnce(filepath.Join(home, ".opencode"), ocDir)

	ocCfg := fsutil.EnsureDir(root, ".config", "opencode")
	fsutil.SeedDirOnce(filepath.Join(home, ".config", "opencode"), ocCfg)

	ocShare := fsutil.EnsureDir(root, ".local", "share", "opencode")
	fsutil.SeedDirOnce(filepath.Join(home, ".local", "share", "opencode"), ocShare)

	ocState := fsutil.EnsureDir(root, ".local", "state")
	fsutil.SeedDirOnce(filepath.Join(home, ".local", "state"), ocState)

	ocCache := fsutil.EnsureDir(root, ".cache", "opencode")
	fsutil.SeedDirOnce(filepath.Join(home, ".cache", "opencode"), ocCache)
	return nil
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
		return fsutil.CopyDir(src, target)
	}
	target := filepath.Join(dst, ".opencode")
	_ = os.MkdirAll(target, 0755)
	return fsutil.CopyDir(src, target)
}

func (o *OpenCode) ImportFile(src, dst string) error {
	target := filepath.Join(dst, ".config", "opencode", filepath.Base(src))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0644)
}
