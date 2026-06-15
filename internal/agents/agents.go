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

package agents

import (
	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/agents/claude"
	"github.com/cloud-exit/exitbox/internal/agents/codex"
	"github.com/cloud-exit/exitbox/internal/agents/copilot"
	"github.com/cloud-exit/exitbox/internal/agents/cursor"
	"github.com/cloud-exit/exitbox/internal/agents/kimi"
	"github.com/cloud-exit/exitbox/internal/agents/opencode"
	"github.com/cloud-exit/exitbox/internal/agents/qwen"
)

func Get(name string) agent.Agent {
	return registry[name]
}

func All() []agent.Agent {
	result := make([]agent.Agent, 0, len(registry))
	for _, a := range registry {
		result = append(result, a)
	}
	return result
}

// Names returns agent names from the registry. If prefix is passed, those strings
// are prepended to the result.
func Names(prefix ...string) []string {
	names := make([]string, 0, len(prefix)+len(registry))
	names = append(names, prefix...)
	for name := range registry {
		names = append(names, name)
	}
	return names
}

// registry holds all agent implementations.
var registry = map[string]agent.Agent{}

// Register adds an agent to the registry.
func Register(a agent.Agent) {
	registry[a.Name()] = a
}

func init() {
	Register(&claude.Claude{})
	Register(&codex.Codex{})
	Register(&copilot.Copilot{})
	Register(&cursor.Cursor{})
	Register(&kimi.Kimi{})
	Register(&opencode.OpenCode{})
	Register(&qwen.Qwen{})
}
