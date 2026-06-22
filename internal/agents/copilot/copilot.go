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

package copilot

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/fsutil"
)

// Copilot integrates the GitHub Copilot CLI (npm: @github/copilot, command: copilot).
// Copilot CLI stores all of its state (config, sessions, logs, auth) under a single
// directory, ~/.copilot, overridable via the COPILOT_HOME environment variable.
type Copilot struct{}

var _ agent.Agent = (*Copilot)(nil)

func (c *Copilot) Name() string        { return "copilot" }
func (c *Copilot) DisplayName() string { return "GitHub Copilot CLI" }
func (c *Copilot) Description() string {
	return "GitHub's AI coding agent in the terminal (Copilot CLI)"
}

// OllamaEnvVars returns no variables: Copilot CLI authenticates against GitHub and
// does not support pointing at an arbitrary OpenAI-compatible / Ollama endpoint.
func (c *Copilot) OllamaEnvVars(ollamaBaseURL string) []string {
	return nil
}

func (c *Copilot) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".copilot"),
	}
}

func (c *Copilot) ContainerMounts(cfgDir string) []agent.Mount {
	return []agent.Mount{
		{Source: filepath.Join(cfgDir, ".copilot"), Target: "/home/user/.copilot"},
	}
}

func (c *Copilot) EnsureWorkspaceAgentConfig(workspaceName string) error {
	if workspaceName == "" {
		return nil
	}
	root := config.WorkspaceAgentDir(workspaceName, c.Name())
	_ = os.MkdirAll(root, 0755)
	home := os.Getenv("HOME")

	copilotDir := fsutil.EnsureDir(root, ".copilot")
	fsutil.SeedDirOnce(filepath.Join(home, ".copilot"), copilotDir)

	return nil
}

func (c *Copilot) DetectHostConfig() (string, error) {
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".copilot")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("no GitHub Copilot CLI config found")
}

func (c *Copilot) ImportConfig(src, dst string) error {
	target := filepath.Join(dst, ".copilot")
	_ = os.MkdirAll(target, 0755)
	return fsutil.CopyDir(src, target)
}

func (c *Copilot) ImportFile(src, dst string) error {
	target := filepath.Join(dst, ".copilot", filepath.Base(src))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0644)
}
