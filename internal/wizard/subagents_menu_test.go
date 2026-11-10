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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/subagents"
)

// subagentStubs replaces everything the Subagent Management pages reach
// outside the wizard, and records what they asked for.
type subagentStubs struct {
	infos    map[string]subagentsInfo // by agent
	enabled  []string
	imported []string // "agent@workspace"
	created  []string
	edited   []string
	findings []subagents.Finding
}

func stubSubagents(t *testing.T) *subagentStubs {
	t.Helper()
	st := &subagentStubs{infos: map[string]subagentsInfo{}}
	oldEnabled, oldLoad, oldImport, oldCreate, oldFindings, oldEdit :=
		agentEnabled, loadSubagentsInfo, importHostSubagents, createMasterSkill, masterSkillFindings, editFile
	t.Cleanup(func() {
		agentEnabled, loadSubagentsInfo, importHostSubagents, createMasterSkill, masterSkillFindings, editFile =
			oldEnabled, oldLoad, oldImport, oldCreate, oldFindings, oldEdit
	})
	agentEnabled = func(name string) bool {
		for _, n := range st.enabled {
			if n == name {
				return true
			}
		}
		return false
	}
	loadSubagentsInfo = func(h *subagents.Harness, _ string) subagentsInfo { return st.infos[h.Agent] }
	importHostSubagents = func(h *subagents.Harness, ws string) (int, int, int, error) {
		st.imported = append(st.imported, h.Agent+"@"+ws)
		info := st.infos[h.Agent]
		info.Stored = append(info.Stored, "imported")
		st.infos[h.Agent] = info
		return 2, 1, 0, nil
	}
	createMasterSkill = func(h *subagents.Harness, ws string) (string, error) {
		st.created = append(st.created, h.Agent+"@"+ws)
		info := st.infos[h.Agent]
		info.MasterSkill = true
		st.infos[h.Agent] = info
		return "/store/" + h.Agent + "/master-skill/SKILL.md", nil
	}
	masterSkillFindings = func(*subagents.Harness, string) ([]subagents.Finding, error) { return st.findings, nil }
	editFile = func(path string) tea.Cmd {
		st.edited = append(st.edited, path)
		return func() tea.Msg { return subEditDoneMsg{} }
	}
	return st
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		updated, _ := m.Update(key(k))
		m = updated.(Model)
	}
	return m
}

func rerunModel(workspaces ...string) Model {
	cfg := &config.Config{}
	for _, w := range workspaces {
		cfg.Workspaces.Items = append(cfg.Workspaces.Items, config.Workspace{Name: w})
	}
	if len(workspaces) > 0 {
		cfg.Workspaces.Active = workspaces[0]
	}
	return NewModelFromConfig(cfg)
}

func agentRow(m Model, agent string) int {
	for i, h := range m.sub.agents {
		if h.Agent == agent {
			return i
		}
	}
	return -1
}

func openAgentPage(t *testing.T, m Model, agent string) Model {
	t.Helper()
	m = m.openSubagentAgents()
	m.sub.agentCursor = agentRow(m, agent)
	return press(t, m, "enter")
}

func TestTopMenu_SubagentManagementIsAMainMenuItem(t *testing.T) {
	stubSubagents(t)
	m := rerunModel("work")
	if m.step != stepTopMenu {
		t.Fatalf("re-run starts at %v, want the main menu", m.step)
	}
	if !strings.Contains(m.View(), "Subagent Management") {
		t.Fatalf("main menu does not list Subagent Management:\n%s", m.View())
	}
	idx := -1
	for i, opt := range topMenuOptions {
		if opt.Label == topMenuSubagents {
			idx = i
		}
	}
	for i := 0; i < idx; i++ {
		m = press(t, m, "down")
	}
	m = press(t, m, "enter")
	if m.step != stepSubagentAgents || m.sub.workspace != "work" {
		t.Fatalf("step = %v, workspace = %q; want the agent picker on the active workspace", m.step, m.sub.workspace)
	}
	// It is not a workspace step: no sidebar, and Esc returns to the main menu.
	if strings.Contains(m.View(), "1. Roles") {
		t.Error("the agent picker shows the workspace sidebar")
	}
	m = press(t, m, "tab")
	if m.sidebarFocused {
		t.Error("Tab focused the workspace sidebar from Subagent Management")
	}
	m = press(t, m, "esc")
	if m.step != stepTopMenu || m.topMenuChoice != -1 {
		t.Errorf("esc: step = %v, topMenuChoice = %d; want the main menu", m.step, m.topMenuChoice)
	}
	// The workspace flow has no subagents step.
	for _, si := range sidebarSteps {
		if strings.Contains(strings.ToLower(si.Label), "subagent") {
			t.Errorf("sidebar has a workspace step %q", si.Label)
		}
	}
}

func TestSubagentAgents_ListsEveryAgentEnabledFirst(t *testing.T) {
	st := stubSubagents(t)
	st.enabled = []string{"opencode", "codex"}
	st.infos["codex"] = subagentsInfo{Stored: []string{"a", "b"}, Broken: 1, MasterSkill: true}
	m := rerunModel("work").openSubagentAgents()

	var order []string
	for _, h := range m.sub.agents {
		order = append(order, h.Agent)
	}
	if got := strings.Join(order, ","); got != "codex,opencode,claude,copilot,cursor,kimi,pi,qwen" {
		t.Errorf("order = %s, want enabled agents first, each group by name", got)
	}
	view := m.View()
	for _, want := range []string{"OpenAI Codex", "3 subagent(s), 1 with errors, master skill", "Pi Coding Agent", "no native subagents", "(agent not enabled)"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(strings.SplitN(view, "OpenAI Codex", 2)[1][:80], "not enabled") {
		t.Error("an enabled agent is marked as not enabled")
	}
}

func TestSubagentManage_ImportAndMasterSkill(t *testing.T) {
	st := stubSubagents(t)
	st.enabled = []string{"qwen", "kimi"}
	st.infos["kimi"] = subagentsInfo{HostCount: 3}
	m := openAgentPage(t, rerunModel("work"), "kimi")
	if m.step != stepSubagentManage || m.sub.agent != subagents.Kimi {
		t.Fatalf("step = %v, agent = %v", m.step, m.sub.agent)
	}
	if view := m.View(); !strings.Contains(view, "Copy 3 from the host's ~/.kimi-code/agents") || !strings.Contains(view, "Create master skill") {
		t.Fatalf("page lacks its actions:\n%s", view)
	}

	m = press(t, m, "enter")
	if len(st.imported) != 1 || st.imported[0] != "kimi@work" {
		t.Fatalf("imported = %v, want kimi's host subagents into workspace work", st.imported)
	}
	if !strings.Contains(m.View(), "Imported 2 subagent(s); 1 already stored.") || !strings.Contains(m.View(), "1 stored") {
		t.Errorf("page does not show the import result:\n%s", m.View())
	}

	m = press(t, m, "down", "enter")
	if len(st.created) != 1 || st.created[0] != "kimi@work" {
		t.Fatalf("created = %v", st.created)
	}
	view := m.View()
	if !strings.Contains(view, "Master skill created") || !strings.Contains(view, "Edit master skill") || strings.Contains(view, "Create master skill") {
		t.Errorf("after creating, the action should turn into Edit:\n%s", view)
	}

	// Editing hands the file to $EDITOR and validates it afterwards.
	updated, cmd := m.Update(key("enter"))
	m = updated.(Model)
	if cmd == nil || len(st.edited) != 1 || st.edited[0] != subagents.Kimi.MasterSkillPath("work") {
		t.Fatalf("edit: cmd = %v, edited = %v", cmd, st.edited)
	}
	updated, _ = m.Update(cmd())
	m = updated.(Model)
	if !strings.Contains(m.View(), "Master skill saved") {
		t.Errorf("no confirmation after the editor:\n%s", m.View())
	}
	st.findings = []subagents.Finding{{Severity: subagents.Error, Field: "description", Message: "missing"}}
	updated, _ = m.Update(subEditDoneMsg{})
	if view := updated.(Model).View(); !strings.Contains(view, "has errors") || !strings.Contains(view, "description: missing") {
		t.Errorf("a broken master skill is not reported:\n%s", view)
	}
	updated, _ = m.Update(subEditDoneMsg{err: errors.New("exit status 1")})
	if view := updated.(Model).View(); !strings.Contains(view, "Editor exited with an error") {
		t.Errorf("an editor failure is not reported:\n%s", view)
	}

	m = press(t, m, "esc")
	if m.step != stepSubagentAgents {
		t.Errorf("esc: step = %v, want the agent picker", m.step)
	}
}

func TestSubagentManage_WhatAnAgentCannotDoIsNotOffered(t *testing.T) {
	st := stubSubagents(t)

	// Nothing on the host to import.
	m := openAgentPage(t, rerunModel("work"), "claude")
	m = press(t, m, "enter")
	if len(st.imported) != 0 {
		t.Error("import ran although the host has no subagents")
	}
	if !strings.Contains(m.View(), "No subagents found in the host's ~/.claude/agents") {
		t.Errorf("page should say there is nothing to import:\n%s", m.View())
	}

	// Pi has no subagents: only the master skill.
	m = openAgentPage(t, rerunModel("work"), "pi")
	if view := m.View(); strings.Contains(view, "Import host subagents") || !strings.Contains(view, "has no native subagents") || !strings.Contains(view, "Create master skill") {
		t.Errorf("Pi page:\n%s", view)
	}
	press(t, m, "enter")
	if len(st.created) != 1 || st.created[0] != "pi@work" {
		t.Errorf("created = %v, want Pi's master skill", st.created)
	}

	// ExitBox links no skills for Qwen Code: no master skill to create.
	st.infos["qwen"] = subagentsInfo{HostCount: 1}
	m = openAgentPage(t, rerunModel("work"), "qwen")
	m = press(t, m, "down", "enter")
	if len(st.created) != 1 {
		t.Errorf("created = %v, a master skill was created for an agent without skill linking", st.created)
	}
	if !strings.Contains(m.View(), "ExitBox links no skills for Qwen Code") {
		t.Errorf("Qwen page should explain the missing master skill:\n%s", m.View())
	}

	// Known limits of an agent are shown on its page.
	if view := openAgentPage(t, rerunModel("work"), "cursor").View(); !strings.Contains(view, ".cursor/agents") || !strings.Contains(view, "may not load") {
		t.Errorf("Cursor page lacks its caveat:\n%s", view)
	}
}

func TestSubagentAgents_SwitchWorkspace(t *testing.T) {
	st := stubSubagents(t)
	st.infos["claude"] = subagentsInfo{HostCount: 1}
	m := rerunModel("a", "b").openSubagentAgents()
	if m.sub.workspace != "a" {
		t.Fatalf("workspace = %q, want the active one", m.sub.workspace)
	}
	m = press(t, m, "w")
	if m.sub.workspace != "b" {
		t.Fatalf("w: workspace = %q, want b", m.sub.workspace)
	}
	m.sub.agentCursor = agentRow(m, "claude")
	press(t, m, "enter", "enter")
	if len(st.imported) != 1 || st.imported[0] != "claude@b" {
		t.Errorf("imported = %v, want the import to go to workspace b", st.imported)
	}
	if m = press(t, m, "w"); m.sub.workspace != "a" {
		t.Errorf("w wraps around: workspace = %q, want a", m.sub.workspace)
	}
	// One workspace: nothing to switch to.
	one := rerunModel("only").openSubagentAgents()
	if one = press(t, one, "w"); one.sub.workspace != "only" {
		t.Errorf("workspace = %q", one.sub.workspace)
	}
}

func TestLoadSubagentsInfo(t *testing.T) {
	old := config.Home
	config.Home = t.TempDir()
	t.Cleanup(func() { config.Home = old })
	host := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", host)
	t.Setenv("CODEX_HOME", filepath.Join(host, "codex-home"))

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(host, "agents", "a.md"), "---\nname: a\ndescription: d\n---\n")
	write(filepath.Join(host, "agents", "nested", "b.md"), "---\nname: b\ndescription: d\n---\n")
	write(filepath.Join(host, "agents", "README.md"), "# Notes\n")
	write(filepath.Join(host, "codex-home", "agents", "x.toml"), "name = \"x\"\ndescription = \"d\"\ndeveloper_instructions = \"i\"\n")
	store := subagents.Claude.StoreDir("work")
	write(filepath.Join(store, "reviewer.md"), "---\nname: reviewer\ndescription: d\n---\n")
	write(filepath.Join(store, "broken.md"), "---\nname: broken\n---\n")

	info := loadSubagentsInfo(subagents.Claude, "work")
	if len(info.Stored) != 1 || info.Stored[0] != "reviewer" || info.Broken != 1 || info.HostCount != 2 || info.MasterSkill {
		t.Errorf("claude info = %+v, want 1 stored, 1 broken, 2 on the host, no master skill", info)
	}
	// Each agent has its own store and host directory.
	if info := loadSubagentsInfo(subagents.Codex, "work"); len(info.Stored) != 0 || info.HostCount != 1 {
		t.Errorf("codex info = %+v, want an empty store and 1 on the host", info)
	}
	if _, _, err := subagents.Claude.EnsureMasterSkill("work"); err != nil {
		t.Fatal(err)
	}
	if !loadSubagentsInfo(subagents.Claude, "work").MasterSkill || loadSubagentsInfo(subagents.Codex, "work").MasterSkill {
		t.Error("the master skill belongs to Claude only")
	}
	if got := loadSubagentsInfo(subagents.Claude, "../escape"); got.HostCount != 0 || got.MasterSkill {
		t.Errorf("invalid workspace name read something: %+v", got)
	}

	// The real import and create go to the store.
	added, skipped, failed, err := importHostSubagents(subagents.Claude, "work")
	if err != nil || added != 2 || skipped != 0 || failed != 0 {
		t.Errorf("import = %d added, %d skipped, %d failed, %v; want 2 added and the README not counted", added, skipped, failed, err)
	}
	// A second import finds both names stored already.
	if added, skipped, _, _ := importHostSubagents(subagents.Claude, "work"); added != 0 || skipped != 2 {
		t.Errorf("second import = %d added, %d skipped; want 0 and 2", added, skipped)
	}
	if path, err := createMasterSkill(subagents.Codex, "work"); err != nil || path != subagents.Codex.MasterSkillPath("work") {
		t.Errorf("createMasterSkill = %q, %v", path, err)
	}
}
