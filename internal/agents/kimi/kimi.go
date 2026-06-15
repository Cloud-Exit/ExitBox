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

package kimi

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/fsutil"
)

// Kimi integrates the Kimi Code CLI from Moonshot AI (npm: @moonshot-ai/kimi-code,
// command: kimi). The CLI stores its agent/runtime settings in ~/.kimi-code/config.toml
// (TOML, overridable via KIMI_CODE_HOME) and works out of the box with Moonshot's Kimi
// models after an interactive /login, or against any compatible provider configured in
// config.toml.
type Kimi struct{}

var _ agent.Agent = (*Kimi)(nil)

func (k *Kimi) Name() string        { return "kimi" }
func (k *Kimi) DisplayName() string { return "Kimi Code CLI" }
func (k *Kimi) Description() string {
	return "Moonshot AI's terminal coding agent (Kimi Code)"
}

// OllamaEnvVars returns no variables: the Kimi Code CLI reads provider credentials
// only from config.toml (not the shell environment), so a local OpenAI-compatible
// server is configured via `exitbox generate kimi` rather than environment variables.
func (k *Kimi) OllamaEnvVars(ollamaBaseURL string) []string {
	return nil
}

func (k *Kimi) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".kimi-code"),
	}
}

func (k *Kimi) ContainerMounts(cfgDir string) []agent.Mount {
	return []agent.Mount{
		{Source: filepath.Join(cfgDir, ".kimi-code"), Target: "/home/user/.kimi-code"},
	}
}

func (k *Kimi) EnsureWorkspaceAgentConfig(workspaceName string) error {
	if workspaceName == "" {
		return nil
	}
	root := config.WorkspaceAgentDir(workspaceName, k.Name())
	_ = os.MkdirAll(root, 0755)
	home := os.Getenv("HOME")

	kimiDir := fsutil.EnsureDir(root, ".kimi-code")
	fsutil.SeedDirOnce(filepath.Join(home, ".kimi-code"), kimiDir)

	return nil
}

func (k *Kimi) DetectHostConfig() (string, error) {
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".kimi-code")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("no Kimi Code CLI config found")
}

func (k *Kimi) ImportConfig(src, dst string) error {
	target := filepath.Join(dst, ".kimi-code")
	_ = os.MkdirAll(target, 0755)
	return fsutil.CopyDir(src, target)
}

func (k *Kimi) ImportFile(src, dst string) error {
	target := filepath.Join(dst, ".kimi-code", filepath.Base(src))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0644)
}
