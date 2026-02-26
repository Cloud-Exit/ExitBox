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
	"github.com/cloud-exit/exitbox/internal/container"
)

// Mount describes a volume mount for a container.
type Mount struct {
	Source string
	Target string
}

// Agent is the interface that all agent implementations must satisfy.
type Agent interface {
	Name() string
	DisplayName() string
	GetLatestVersion() (string, error)
	GetInstalledVersion(rt container.Runtime, img string) (string, error)
	GetDockerfileInstall(buildCtx string) (string, error)
	GetFullDockerfile(version string) (string, error)
	HostConfigPaths() []string
	ContainerMounts(cfgDir string) []Mount
	DetectHostConfig() (string, error)
	ImportConfig(src, dst string) error
}

// AgentNames is the list of all supported agent names.
var AgentNames = []string{"claude", "codex", "opencode"}

// DisplayName returns the human-readable name for an agent.
func DisplayName(name string) string {
	switch name {
	case "claude":
		return "Claude Code"
	case "codex":
		return "OpenAI Codex"
	case "opencode":
		return "OpenCode"
	case "qwencode":
		return "QwenCode"
	}
	return name
}

// IsValidAgent returns true if the name is a known agent.
func IsValidAgent(name string) bool {
	for _, a := range AgentNames {
		if a == name {
			return true
		}
	}
	return false
}

// registry holds all agent implementations.
var registry = map[string]Agent{}

// Register adds an agent to the registry.
func Register(a Agent) {
	registry[a.Name()] = a
}

// Get returns the agent implementation for a name.
func Get(name string) Agent {
	return registry[name]
}

func init() {
	Register(&Claude{})
	Register(&Codex{})
	Register(&OpenCode{})
}
