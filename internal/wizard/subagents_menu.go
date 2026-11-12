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

package wizard

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/subagents"
)

// subagentMenu is the state of the Subagent Management pages: a main menu
// item of its own, with one page per coding agent. Unlike the workspace
// steps, what it does is saved at once, not at the review step.
type subagentMenu struct {
	workspace   string               // workspace the pages work on
	enabled     map[string]bool      // agents enabled in the config
	agents      []*subagents.Harness // picker rows: enabled agents first
	infos       map[string]subagentsInfo
	agentCursor int
	agent       *subagents.Harness // agent of the open page
	cursor      int                // highlighted item on the page
	msg         string             // result of the last action
	msgIsError  bool

	// The open page: its subagents and, for Claude Code, claude-router.
	rows     []subagentRow
	router   routerState
	routerOn bool // the router checkbox
	// routerPending: the checkbox shows the default (on) but the config
	// has no entry yet; the first change on the page saves it.
	routerPending bool
	input         *subInput
	vaultPW       string // vault password, kept while the page is open
	pendingKey    string // backend key waiting for the vault password
	pendingModel  string
	newPW         string // first entry of a new vault password
}

// subInput is a text field open on the page.
type subInput struct {
	kind   string // inputBackend, inputKey, inputVaultPW, inputVaultNew, inputVaultConfirm
	model  string // route the field is for
	prompt string
	text   string
	masked bool
}

const (
	inputBackend      = "backend"
	inputKey          = "key"
	inputVaultPW      = "vault-password"
	inputVaultNew     = "vault-new"
	inputVaultConfirm = "vault-confirm"
)

// subagentsInfo is what the pages show about one agent in one workspace.
type subagentsInfo struct {
	Stored      []string // names of the stored subagents
	Broken      int      // stored files the agent skips
	HostCount   int      // subagents in the agent's host agents directory
	MasterSkill bool     // the agent has a master skill here
}

// subEditDoneMsg arrives when $EDITOR exits.
type subEditDoneMsg struct{ err error }

// The pages reach the store and the host through these, replaced in tests.
var (
	agentEnabled = func(name string) bool {
		return config.LoadOrDefault().IsAgentEnabled(name)
	}

	// loadSubagentsInfo reads an agent's store and host agents directory.
	// Unreadable locations count as empty.
	loadSubagentsInfo = func(h *subagents.Harness, workspace string) subagentsInfo {
		var info subagentsInfo
		if !subagents.ValidWorkspaceName(workspace) {
			return info
		}
		if entries, err := h.ListStore(workspace); err == nil {
			for _, e := range entries {
				if subagents.SkipErrors(e.Findings) {
					info.Broken++
					continue
				}
				info.Stored = append(info.Stored, e.Name())
			}
		}
		if h.HasSubagents() {
			if dir, err := h.HostDir(); err == nil {
				info.HostCount = h.CountDir(dir)
			}
		}
		info.MasterSkill = h.MasterSkillExists(workspace)
		return info
	}

	createMasterSkill = func(h *subagents.Harness, workspace string) (string, error) {
		path, _, err := h.EnsureMasterSkill(workspace)
		return path, err
	}

	masterSkillFindings = func(h *subagents.Harness, workspace string) ([]subagents.Finding, error) {
		return h.MasterSkillFindings(workspace)
	}

	// editFile suspends the wizard and opens a file in $EDITOR (default vi).
	editFile = func(path string) tea.Cmd {
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "vi"
		}
		return tea.ExecProcess(exec.Command(editor, path), func(err error) tea.Msg {
			return subEditDoneMsg{err: err}
		})
	}
)

// openSubagentAgents opens the agent picker for the workspace setup is on.
func (m Model) openSubagentAgents() Model {
	ws := m.activeWorkspaceName()
	if ws == "" {
		ws = "default"
	}
	m.sub = subagentMenu{workspace: ws, enabled: map[string]bool{}}
	var rest []*subagents.Harness
	for _, h := range subagents.Harnesses() {
		if m.sub.enabled[h.Agent] = agentEnabled(h.Agent); m.sub.enabled[h.Agent] {
			m.sub.agents = append(m.sub.agents, h)
		} else {
			rest = append(rest, h)
		}
	}
	m.sub.agents = append(m.sub.agents, rest...)
	m.step = stepSubagentAgents
	return m.reloadSubagentInfos()
}

func (m Model) reloadSubagentInfos() Model {
	m.sub.infos = map[string]subagentsInfo{}
	for _, h := range m.sub.agents {
		m.sub.infos[h.Agent] = loadSubagentsInfo(h, m.sub.workspace)
	}
	return m
}

// nextSubagentWorkspace moves the pages to the next configured workspace.
func (m Model) nextSubagentWorkspace() Model {
	if len(m.workspaces) < 2 {
		return m
	}
	next := 0
	for i, w := range m.workspaces {
		if w.Name == m.sub.workspace {
			next = (i + 1) % len(m.workspaces)
			break
		}
	}
	m.sub.workspace = m.workspaces[next].Name
	m.sub.msg = ""
	return m.reloadSubagentInfos()
}

func (m Model) updateSubagentAgents(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if m.sub.agentCursor > 0 {
			m.sub.agentCursor--
		}
	case "down", "j":
		if m.sub.agentCursor < len(m.sub.agents)-1 {
			m.sub.agentCursor++
		}
	case "w":
		m = m.nextSubagentWorkspace()
	case "enter":
		m.sub.agent = m.sub.agents[m.sub.agentCursor]
		m.sub.cursor = 0
		m.sub.msg = ""
		m.sub.vaultPW, m.sub.pendingKey, m.sub.input = "", "", nil
		m = m.loadSubagentPage()
		m.step = stepSubagentManage
	case "esc", "q":
		m.step = stepTopMenu
		m.topMenuChoice = -1
	}
	return m, nil
}

// subagentSummary is an agent's line in the picker.
func subagentSummary(h *subagents.Harness, info subagentsInfo) string {
	var parts []string
	switch {
	case !h.HasSubagents():
		parts = append(parts, "no native subagents")
	case len(info.Stored)+info.Broken == 0 && info.HostCount > 0:
		parts = append(parts, fmt.Sprintf("%d on the host, none used yet", info.HostCount))
	case len(info.Stored)+info.Broken == 0:
		parts = append(parts, "no subagents stored")
	default:
		s := fmt.Sprintf("%d subagent(s)", len(info.Stored)+info.Broken)
		if info.Broken > 0 {
			s += fmt.Sprintf(", %d with errors", info.Broken)
		}
		parts = append(parts, s)
	}
	if info.MasterSkill {
		parts = append(parts, "master skill")
	}
	return strings.Join(parts, ", ")
}

func (m Model) subWorkspaceLine() string {
	line := fmt.Sprintf("Workspace: %s", selectedStyle.Render(m.sub.workspace))
	if len(m.workspaces) > 1 {
		line += dimStyle.Render("   (w: next workspace)")
	}
	return line
}

func (m Model) viewSubagentAgents() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Subagent Management"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Every coding agent has its own subagents and its own master skill, stored per workspace."))
	b.WriteString("\n\n")
	b.WriteString(m.subWorkspaceLine())
	b.WriteString("\n\n")

	for i, h := range m.sub.agents {
		cursor := "  "
		if m.sub.agentCursor == i {
			cursor = cursorStyle.Render("> ")
		}
		label := fmt.Sprintf("%-20s", h.Label)
		summary := subagentSummary(h, m.sub.infos[h.Agent])
		switch {
		case m.sub.agentCursor == i:
			label = selectedStyle.Render(label)
		case !m.sub.enabled[h.Agent]:
			label = dimStyle.Render(label)
		}
		if !m.sub.enabled[h.Agent] {
			summary += "  (agent not enabled)"
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, label, dimStyle.Render(summary)))
	}

	help := "Up/Down to move, Enter to manage, Esc to go back"
	if len(m.workspaces) > 1 {
		help = "Up/Down to move, Enter to manage, w to switch workspace, Esc to go back"
	}
	b.WriteString(helpStyle.Render("\n" + help))
	return b.String()
}
