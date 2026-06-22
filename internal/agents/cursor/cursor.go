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

package cursor

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/fsutil"
)

// Cursor integrates the Cursor CLI (command: cursor-agent). The agent authenticates
// against Cursor's backend (interactive login) and stores its config, sessions and
// credentials under ~/.cursor, overridable via the CURSOR_CONFIG_DIR environment
// variable. The primary config file is ~/.cursor/cli-config.json.
type Cursor struct{}

var _ agent.Agent = (*Cursor)(nil)

func (c *Cursor) Name() string        { return "cursor" }
func (c *Cursor) DisplayName() string { return "Cursor CLI" }
func (c *Cursor) Description() string {
	return "Cursor's terminal coding agent (cursor-agent)"
}

// OllamaEnvVars returns no variables: the Cursor CLI authenticates against Cursor's
// backend and does not support pointing at an arbitrary OpenAI-compatible endpoint.
func (c *Cursor) OllamaEnvVars(ollamaBaseURL string) []string {
	return nil
}

func (c *Cursor) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".cursor"),
	}
}

func (c *Cursor) ContainerMounts(cfgDir string) []agent.Mount {
	return []agent.Mount{
		{Source: filepath.Join(cfgDir, ".cursor"), Target: "/home/user/.cursor"},
	}
}

func (c *Cursor) EnsureWorkspaceAgentConfig(workspaceName string) error {
	if workspaceName == "" {
		return nil
	}
	root := config.WorkspaceAgentDir(workspaceName, c.Name())
	_ = os.MkdirAll(root, 0755)
	home := os.Getenv("HOME")

	cursorDir := fsutil.EnsureDir(root, ".cursor")
	fsutil.SeedDirOnce(filepath.Join(home, ".cursor"), cursorDir)

	return nil
}

func (c *Cursor) DetectHostConfig() (string, error) {
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".cursor")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("no Cursor CLI config found")
}

func (c *Cursor) ImportConfig(src, dst string) error {
	target := filepath.Join(dst, ".cursor")
	_ = os.MkdirAll(target, 0755)
	return fsutil.CopyDir(src, target)
}

func (c *Cursor) ImportFile(src, dst string) error {
	target := filepath.Join(dst, ".cursor", filepath.Base(src))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0644)
}
