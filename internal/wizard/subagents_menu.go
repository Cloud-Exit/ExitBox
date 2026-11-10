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
	cursor      int                // highlighted action on the page
	msg         string             // result of the last action
	msgIsError  bool
}

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
				if subagents.HasErrors(e.Findings) {
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

	importHostSubagents = func(h *subagents.Harness, workspace string) (added, skipped, failed int, err error) {
		dir, err := h.HostDir()
		if err != nil {
			return 0, 0, 0, err
		}
		outcomes, err := h.ImportDir(workspace, dir, false)
		if err != nil {
			return 0, 0, 0, err
		}
		for _, o := range outcomes {
			switch {
			case o.Status == subagents.Added:
				added++
			case o.Status == subagents.Failed:
				failed++
			case o.Reason != "":
				// Skipped without a reason is documentation, not a subagent.
				skipped++
			}
		}
		return added, skipped, failed, nil
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

// Actions of an agent's page.
const (
	subActionImport = "import"
	subActionCreate = "create"
	subActionEdit   = "edit"
)

type subAction struct {
	Key         string
	Label       string
	Description string
	Disabled    bool
}

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

// subagentActions returns what the open page can do for its agent.
func (m Model) subagentActions() []subAction {
	h := m.sub.agent
	info := m.sub.infos[h.Agent]
	var out []subAction
	if h.HasSubagents() {
		imp := subAction{
			Key:         subActionImport,
			Label:       "Import host subagents",
			Description: fmt.Sprintf("Copy %d from the host's %s (stored names are kept)", info.HostCount, h.AgentsDirLabel()),
		}
		if info.HostCount == 0 {
			imp.Description = "No subagents found in the host's " + h.AgentsDirLabel()
			imp.Disabled = true
		}
		out = append(out, imp)
	}
	switch {
	case !h.Skills:
		out = append(out, subAction{Key: subActionCreate, Label: "Master skill", Disabled: true,
			Description: "ExitBox links no skills for " + h.Label + " yet"})
	case info.MasterSkill:
		out = append(out, subAction{Key: subActionEdit, Label: "Edit master skill",
			Description: "Open the task routing rules in $EDITOR"})
	default:
		out = append(out, subAction{Key: subActionCreate, Label: "Create master skill",
			Description: "The editable skill that routes tasks to subagents and third-party agents"})
	}
	return out
}

func (m Model) updateSubagentManage(msg tea.Msg) (tea.Model, tea.Cmd) {
	h := m.sub.agent
	if done, ok := msg.(subEditDoneMsg); ok {
		m.sub.msg, m.sub.msgIsError = "Master skill saved. It applies from the next container start.", false
		if done.err != nil {
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Editor exited with an error: %v", done.err), true
		} else if fs, err := masterSkillFindings(h, m.sub.workspace); err != nil {
			m.sub.msg, m.sub.msgIsError = err.Error(), true
		} else if subagents.HasErrors(fs) {
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("The master skill has errors and %s will skip it: %s", h.Label, firstError(fs)), true
		}
		return m.reloadSubagentInfos(), nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	actions := m.subagentActions()
	switch key.String() {
	case "up", "k":
		if m.sub.cursor > 0 {
			m.sub.cursor--
		}
	case "down", "j":
		if m.sub.cursor < len(actions)-1 {
			m.sub.cursor++
		}
	case "enter":
		if m.sub.cursor >= len(actions) || actions[m.sub.cursor].Disabled {
			return m, nil
		}
		return m.runSubagentAction(actions[m.sub.cursor].Key)
	case "esc", "q":
		m.sub.msg = ""
		m.step = stepSubagentAgents
	}
	return m, nil
}

func (m Model) runSubagentAction(action string) (tea.Model, tea.Cmd) {
	h, ws := m.sub.agent, m.sub.workspace
	m.sub.msgIsError = false
	switch action {
	case subActionImport:
		added, skipped, failed, err := importHostSubagents(h, ws)
		switch {
		case err != nil:
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Import failed: %v", err), true
		case failed > 0:
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Imported %d, skipped %d, %d not added. Details: exitbox subagents import --agent %s -w %s",
				added, skipped, failed, h.Agent, ws), true
		default:
			m.sub.msg = fmt.Sprintf("Imported %d subagent(s).", added)
			if skipped > 0 {
				m.sub.msg = fmt.Sprintf("Imported %d subagent(s); %d already stored.", added, skipped)
			}
		}
	case subActionCreate:
		if _, err := createMasterSkill(h, ws); err != nil {
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not create the master skill: %v", err), true
		} else {
			m.sub.msg = "Master skill created, with a route for every stored subagent."
		}
	case subActionEdit:
		return m, editFile(h.MasterSkillPath(ws))
	}
	m = m.reloadSubagentInfos()
	if n := len(m.subagentActions()); m.sub.cursor >= n {
		m.sub.cursor = n - 1
	}
	return m, nil
}

func firstError(fs []subagents.Finding) string {
	for _, f := range fs {
		if f.Severity == subagents.Error {
			return f.String()
		}
	}
	return ""
}

func (m Model) viewSubagentManage() string {
	h := m.sub.agent
	info := m.sub.infos[h.Agent]
	ws := m.sub.workspace
	var b strings.Builder
	b.WriteString(titleStyle.Render("Subagent Management: " + h.Label))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Workspace: %s\n\n", selectedStyle.Render(ws)))

	switch {
	case !h.HasSubagents():
		b.WriteString(fmt.Sprintf("  Subagents     %s\n", dimStyle.Render(h.Label+" has no native subagents")))
	case len(info.Stored) == 0:
		b.WriteString(fmt.Sprintf("  Subagents     %s\n", dimStyle.Render("none stored yet")))
	default:
		b.WriteString(fmt.Sprintf("  Subagents     %s %s\n", selectedStyle.Render(fmt.Sprintf("%d stored", len(info.Stored))),
			dimStyle.Render("("+truncate(strings.Join(info.Stored, ", "), 60)+")")))
	}
	if info.Broken > 0 {
		b.WriteString("                " + warnStyle.Render(fmt.Sprintf("⚠ %d stored file(s) have errors: exitbox subagents validate --agent %s -w %s", info.Broken, h.Agent, ws)) + "\n")
	}
	if h.Skills {
		state := dimStyle.Render("not set up")
		if info.MasterSkill {
			state = selectedStyle.Render("set up")
		}
		b.WriteString(fmt.Sprintf("  Master skill  %s\n", state))
	}
	if h.Caveat != "" {
		b.WriteString("\n  " + warnStyle.Render("⚠ "+h.Caveat) + "\n")
	}
	b.WriteString("\n")

	for i, a := range m.subagentActions() {
		cursor := "  "
		if m.sub.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		label := fmt.Sprintf("%-23s", a.Label)
		switch {
		case a.Disabled:
			label = dimStyle.Render(label)
		case m.sub.cursor == i:
			label = selectedStyle.Render(label)
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, label, dimStyle.Render(a.Description)))
	}

	if m.sub.msg != "" {
		style := successStyle
		if m.sub.msgIsError {
			style = warnStyle
		}
		b.WriteString("\n  " + style.Render(m.sub.msg) + "\n")
	}

	if h.HasSubagents() {
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("More from the command line:"))
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  exitbox subagents new <name> --agent %s -w %s\n", h.Agent, ws))
		b.WriteString(fmt.Sprintf("  exitbox subagents add <source> --agent %s -w %s\n", h.Agent, ws))
		b.WriteString(fmt.Sprintf("  exitbox subagents list --agent %s -w %s\n", h.Agent, ws))
	}

	b.WriteString(helpStyle.Render("\nUp/Down to move, Enter to run (saved at once), Esc to go back"))
	return b.String()
}
