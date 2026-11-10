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

package subagents

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
)

// Harness describes how one coding agent loads subagent files. Every agent
// has its own format, so each gets its own store, rules and scaffold.
type Harness struct {
	Agent string // ExitBox agent name
	Label string // display name
	// Ext is the extension of a subagent file. Empty when the agent has no
	// native subagents.
	Ext     string
	Home    []string // the agent's config directory under the user's home
	HomeEnv string   // host environment variable that replaces Home
	// Project is the project directory whose agents win over user-level
	// ones of the same name. Nil when they do not, or the docs do not say.
	Project   []string
	Recursive bool // the agent scans subdirectories of its agents directory
	// Skills says ExitBox links skills, and so the master skill, for this agent.
	Skills  bool
	DocsURL string
	Checked string // what the rules were checked against
	Caveat  string // known limitation, shown by list and validate

	parse    func(h *Harness, file string, data []byte) (*Definition, []Finding)
	scaffold func(h *Harness, o ScaffoldOptions) ([]byte, error)
}

// ScaffoldOptions are the fields `exitbox subagents new` can fill in.
type ScaffoldOptions struct {
	Name        string
	Description string
	Tools       string // comma-separated
	Model       string
}

// The coding agents ExitBox knows, with their subagent formats.
var (
	Claude = &Harness{
		Agent: "claude", Label: "Claude Code", Ext: ".md",
		Home: []string{".claude"}, HomeEnv: "CLAUDE_CONFIG_DIR",
		Project: []string{".claude", "agents"}, Recursive: true, Skills: true,
		DocsURL: DocsURL, Checked: "Claude Code " + CheckedVersion,
		parse: parseClaude, scaffold: scaffoldClaude,
	}
	Codex = &Harness{
		Agent: "codex", Label: "OpenAI Codex", Ext: ".toml",
		Home: []string{".codex"}, HomeEnv: "CODEX_HOME", Skills: true,
		DocsURL: "https://developers.openai.com/codex/subagents", Checked: docsChecked,
		parse: parseCodex, scaffold: scaffoldCodex,
	}
	Copilot = &Harness{
		Agent: "copilot", Label: "GitHub Copilot CLI", Ext: ".agent.md",
		Home:    []string{".copilot"},
		DocsURL: "https://docs.github.com/en/copilot/reference/custom-agents-configuration", Checked: docsChecked,
		parse: parseCopilot, scaffold: scaffoldCopilot,
	}
	Cursor = &Harness{
		Agent: "cursor", Label: "Cursor CLI", Ext: ".md",
		Home: []string{".cursor"}, Project: []string{".cursor", "agents"}, Recursive: true,
		DocsURL: "https://cursor.com/docs/subagents", Checked: docsChecked,
		Caveat: "Cursor staff report that the CLI (seen in 2026.07.23) loads only a project's .cursor/agents, " +
			"not ~/.cursor/agents, so these subagents may not load until Cursor fixes that",
		parse: parseCursor, scaffold: scaffoldCursor,
	}
	Kimi = &Harness{
		Agent: "kimi", Label: "Kimi Code CLI", Ext: ".md",
		Home: []string{".kimi-code"}, HomeEnv: "KIMI_CODE_HOME",
		Project: []string{".kimi-code", "agents"}, Recursive: true, Skills: true,
		DocsURL: "https://www.kimi.com/code/docs/en/kimi-code-cli/customization/agents.html", Checked: docsChecked,
		parse: parseKimi, scaffold: scaffoldKimi,
	}
	OpenCode = &Harness{
		Agent: "opencode", Label: "OpenCode", Ext: ".md",
		Home: []string{".config", "opencode"}, Skills: true,
		DocsURL: "https://opencode.ai/docs/agents/", Checked: docsChecked,
		parse: parseOpenCode, scaffold: scaffoldOpenCode,
	}
	// Pi has no native subagents; it is listed for the master skill.
	Pi = &Harness{
		Agent: "pi", Label: "Pi Coding Agent",
		Home: []string{".pi"}, Skills: true,
	}
	Qwen = &Harness{
		Agent: "qwen", Label: "Qwen Code", Ext: ".md",
		Home: []string{".qwen"}, Project: []string{".qwen", "agents"},
		DocsURL: "https://qwenlm.github.io/qwen-code-docs/en/users/features/sub-agents/",
		Checked: "Qwen Code source, 2026-11",
		parse:   parseQwen, scaffold: scaffoldQwen,
	}
)

// docsChecked marks rules taken from an agent's documentation only: no
// release of that agent was run against them.
const docsChecked = "its docs, 2026-11"

// harnesses is sorted by agent name, so every listing has a stable order.
var harnesses = []*Harness{Claude, Codex, Copilot, Cursor, Kimi, OpenCode, Pi, Qwen}

// Harnesses returns every coding agent, sorted by name.
func Harnesses() []*Harness {
	return append([]*Harness(nil), harnesses...)
}

// WithSubagents returns the coding agents that have native subagents.
func WithSubagents() []*Harness {
	var out []*Harness
	for _, h := range harnesses {
		if h.HasSubagents() {
			out = append(out, h)
		}
	}
	return out
}

// HarnessFor returns the coding agent with that name, or nil.
func HarnessFor(agent string) *Harness {
	for _, h := range harnesses {
		if h.Agent == agent {
			return h
		}
	}
	return nil
}

// HasSubagents reports whether the agent loads subagent files at all.
func (h *Harness) HasSubagents() bool {
	return h.Ext != ""
}

// Parse reads a subagent file and validates it on its own. file is the
// file's name, which some agents take the subagent's name from. A nil
// Definition means the agent reads no fields from the file.
func (h *Harness) Parse(file string, data []byte) (*Definition, []Finding) {
	return h.parse(h, filepath.Base(file), data)
}

// Scaffold renders a new, valid subagent file.
func (h *Harness) Scaffold(o ScaffoldOptions) ([]byte, error) {
	if o.Description == "" {
		o.Description = "Describe when " + h.Label + " should delegate to this subagent."
	}
	return h.scaffold(h, o)
}

// StoreDir returns the host path of the agent's subagent store in a workspace.
func (h *Harness) StoreDir(workspace string) string {
	return filepath.Join(storeRoot(workspace), h.Agent)
}

// ContainerAgentsDir returns the host path of the agents directory the
// agent reads inside the container.
func (h *Harness) ContainerAgentsDir(workspace string) string {
	return filepath.Join(config.WorkspaceAgentDir(workspace, h.Agent), filepath.Join(h.Home...), "agents")
}

// AgentsDirLabel is how the agent's user-level agents directory is written
// in messages, e.g. ~/.claude/agents.
func (h *Harness) AgentsDirLabel() string {
	return "~/" + strings.Join(h.Home, "/") + "/agents"
}

// HostDir returns the host's user-level agents directory for the agent.
func (h *Harness) HostDir() (string, error) {
	if h.HomeEnv != "" {
		if dir := os.Getenv(h.HomeEnv); dir != "" {
			return filepath.Join(dir, "agents"), nil
		}
	}
	if h.Home[0] == ".config" {
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, filepath.Join(h.Home[1:]...), "agents"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, filepath.Join(h.Home...), "agents"), nil
}

// stem returns a file name without the agent's extension, or without its
// own when a single file was added under another one.
func (h *Harness) stem(file string) string {
	base := filepath.Base(file)
	if strings.HasSuffix(base, h.Ext) {
		return strings.TrimSuffix(base, h.Ext)
	}
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// isAgentFile reports whether the agent would read a file with that name.
func (h *Harness) isAgentFile(name string) bool {
	return strings.HasSuffix(name, h.Ext) && !strings.HasPrefix(name, ".") && name != h.Ext
}

// storeRoot holds one directory per agent. It is mounted read-only.
func storeRoot(workspace string) string {
	return filepath.Join(config.Home, "profiles", "global", workspace, "subagents")
}
