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

package pi

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/fsutil"
)

// Pi integrates the Pi Coding Agent (npm: @earendil-works/pi-coding-agent, command: pi).
// Pi stores all of its state — auth.json, settings.json, models.json, sessions, skills,
// extensions — under ~/.pi/agent. Custom/local OpenAI-compatible providers are declared
// in ~/.pi/agent/models.json.
type Pi struct{}

var _ agent.Agent = (*Pi)(nil)

func (p *Pi) Name() string        { return "pi" }
func (p *Pi) DisplayName() string { return "Pi Coding Agent" }
func (p *Pi) Description() string {
	return "Extensible multi-provider coding agent CLI (Pi)"
}

// OllamaEnvVars returns no variables: Pi configures local/OpenAI-compatible servers
// through ~/.pi/agent/models.json (see GenerateConfig), not shell environment proxies,
// so the local-server flow is driven by `exitbox generate pi`.
func (p *Pi) OllamaEnvVars(ollamaBaseURL string) []string {
	return nil
}

func (p *Pi) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{
		filepath.Join(home, ".pi"),
	}
}

func (p *Pi) ContainerMounts(cfgDir string) []agent.Mount {
	return []agent.Mount{
		{Source: filepath.Join(cfgDir, ".pi"), Target: "/home/user/.pi"},
	}
}

func (p *Pi) EnsureWorkspaceAgentConfig(workspaceName string) error {
	if workspaceName == "" {
		return nil
	}
	root := config.WorkspaceAgentDir(workspaceName, p.Name())
	_ = os.MkdirAll(root, 0755)
	home := os.Getenv("HOME")

	piDir := fsutil.EnsureDir(root, ".pi")
	fsutil.SeedDirOnce(filepath.Join(home, ".pi"), piDir)

	return nil
}

func (p *Pi) DetectHostConfig() (string, error) {
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".pi")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("no Pi Coding Agent config found")
}

func (p *Pi) ImportConfig(src, dst string) error {
	target := filepath.Join(dst, ".pi")
	_ = os.MkdirAll(target, 0755)
	return fsutil.CopyDir(src, target)
}

func (p *Pi) ImportFile(src, dst string) error {
	target := filepath.Join(dst, ".pi", filepath.Base(src))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(target, data, 0644)
}
