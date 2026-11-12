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
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/router"
	"github.com/cloud-exit/exitbox/internal/subagents"
	"github.com/cloud-exit/exitbox/internal/vault"
)

// subagentStubs replaces everything the Subagent Management pages reach
// outside the wizard, and records what they asked for.
type subagentStubs struct {
	infos    map[string]subagentsInfo // by agent
	rows     map[string][]subagentRow // by agent
	router   routerState
	enabled  []string
	stored   []string // "agent:name@workspace"
	removed  []string
	saved    []string // "enabled|routes|vault@workspace"
	created  []string
	edited   []string
	findings []subagents.Finding
}

func stubSubagents(t *testing.T) *subagentStubs {
	t.Helper()
	st := &subagentStubs{infos: map[string]subagentsInfo{}, rows: map[string][]subagentRow{}}
	oldEnabled, oldLoad, oldCreate, oldFindings, oldEdit :=
		agentEnabled, loadSubagentsInfo, createMasterSkill, masterSkillFindings, editFile
	oldRows, oldStore, oldUnstore, oldRouter, oldSave := loadSubagentRows, storeSubagent, unstoreSubagent, loadClaudeRouter, saveClaudeRouter
	t.Cleanup(func() {
		agentEnabled, loadSubagentsInfo, createMasterSkill, masterSkillFindings, editFile =
			oldEnabled, oldLoad, oldCreate, oldFindings, oldEdit
		loadSubagentRows, storeSubagent, unstoreSubagent, loadClaudeRouter, saveClaudeRouter = oldRows, oldStore, oldUnstore, oldRouter, oldSave
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
	loadSubagentRows = func(h *subagents.Harness, _ string) ([]subagentRow, error) {
		return append([]subagentRow(nil), st.rows[h.Agent]...), nil
	}
	storeSubagent = func(h *subagents.Harness, ws string, row subagentRow) error {
		st.stored = append(st.stored, h.Agent+":"+row.Name+"@"+ws)
		for i, r := range st.rows[h.Agent] {
			if r.Name == row.Name {
				st.rows[h.Agent][i].Stored, st.rows[h.Agent][i].Source = true, subagents.SourceImported
			}
		}
		return nil
	}
	unstoreSubagent = func(h *subagents.Harness, ws, name string) error {
		st.removed = append(st.removed, h.Agent+":"+name+"@"+ws)
		for i, r := range st.rows[h.Agent] {
			if r.Name == name {
				st.rows[h.Agent][i].Stored = false
			}
		}
		return nil
	}
	loadClaudeRouter = func(string) routerState { return st.router }
	saveClaudeRouter = func(ws string, enabled bool, routes []router.Route, enableVault bool) (config.PluginEntry, error) {
		st.saved = append(st.saved, fmt.Sprintf("%v|%s|%v@%s", enabled, joinRoutes(routes), enableVault, ws))
		st.router = routerState{Known: true, Enabled: enabled, Routes: routes}
		return config.PluginEntry{Enabled: enabled, Settings: map[string]string{"routes": joinRoutes(routes)}}, nil
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
	st.infos["claude"] = subagentsInfo{HostCount: 4}
	m := rerunModel("work").openSubagentAgents()

	var order []string
	for _, h := range m.sub.agents {
		order = append(order, h.Agent)
	}
	if got := strings.Join(order, ","); got != "codex,opencode,claude,copilot,cursor,kimi,pi,qwen" {
		t.Errorf("order = %s, want enabled agents first, each group by name", got)
	}
	view := m.View()
	for _, want := range []string{"OpenAI Codex", "3 subagent(s), 1 with errors, master skill", "Pi Coding Agent", "no native subagents", "(agent not enabled)", "4 on the host, none used yet"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(strings.SplitN(view, "OpenAI Codex", 2)[1][:80], "not enabled") {
		t.Error("an enabled agent is marked as not enabled")
	}
}

// itemIndex returns the position of the first page item of a kind (and
// row or model), or fails.
func itemIndex(t *testing.T, m Model, kind int, match string) int {
	t.Helper()
	for i, it := range m.pageItems() {
		if it.kind != kind {
			continue
		}
		switch kind {
		case itemSubagent:
			if m.sub.rows[it.row].Name == match {
				return i
			}
		case itemBackend, itemKey:
			if it.model == match {
				return i
			}
		default:
			return i
		}
	}
	t.Fatalf("no page item %d %q in %+v", kind, match, m.pageItems())
	return -1
}

func at(t *testing.T, m Model, kind int, match string) Model {
	t.Helper()
	m.sub.cursor = itemIndex(t, m, kind, match)
	return m
}

func typeText(t *testing.T, m Model, text string) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
	return updated.(Model)
}

func TestSubagentManage_HostSubagentsAreCheckboxes(t *testing.T) {
	st := stubSubagents(t)
	st.enabled = []string{"kimi"}
	st.rows["kimi"] = []subagentRow{
		{Name: "reviewer", Path: "/host/reviewer.md", File: "reviewer.md"},
		{Name: "used", Path: "/host/used.md", File: "used.md", Stored: true, Source: subagents.SourceImported},
		{Name: "broken", Path: "/host/broken.md", File: "broken.md", Problem: "error: description: missing"},
		{Name: "mine", File: "mine.md", Stored: true, Source: subagents.SourceManaged},
	}
	m := openAgentPage(t, rerunModel("work"), "kimi")
	if m.step != stepSubagentManage || m.sub.agent != subagents.Kimi {
		t.Fatalf("step = %v, agent = %v", m.step, m.sub.agent)
	}
	view := m.View()
	for _, want := range []string{"[ ] reviewer", "[x] used", "skipped by Kimi Code CLI", "added with exitbox subagents", "~/.kimi-code/agents"} {
		if !strings.Contains(view, want) {
			t.Errorf("page lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "claude-router") {
		t.Error("claude-router is offered on another agent's page")
	}

	// Checking stores the host file in the workspace.
	m = press(t, at(t, m, itemSubagent, "reviewer"), " ")
	if strings.Join(st.stored, ",") != "kimi:reviewer@work" || !strings.Contains(m.View(), "[x] reviewer") {
		t.Fatalf("stored = %v\n%s", st.stored, m.View())
	}
	// Unchecking removes only the workspace copy.
	m = press(t, at(t, m, itemSubagent, "used"), "enter")
	if strings.Join(st.removed, ",") != "kimi:used@work" || !strings.Contains(m.View(), "host file is untouched") {
		t.Fatalf("removed = %v\n%s", st.removed, m.View())
	}
	// A file the agent would skip cannot be checked.
	m = press(t, at(t, m, itemSubagent, "broken"), " ")
	if len(st.stored) != 1 || !strings.Contains(m.View(), "cannot be used") {
		t.Errorf("broken: stored = %v\n%s", st.stored, m.View())
	}
	// A subagent made with exitbox subagents has no host copy to bring
	// back, so the page does not delete it.
	m = press(t, at(t, m, itemSubagent, "mine"), " ")
	if len(st.removed) != 1 || !strings.Contains(m.View(), "exitbox subagents rm mine --agent kimi -w work") {
		t.Errorf("mine: removed = %v\n%s", st.removed, m.View())
	}
	// a uses every host subagent that can be used.
	m = press(t, m, "a")
	if strings.Join(st.stored, ",") != "kimi:reviewer@work,kimi:used@work" || !strings.Contains(m.View(), "Added 1 subagent(s).") {
		t.Errorf("a: stored = %v\n%s", st.stored, m.View())
	}
	if len(st.saved) != 0 {
		t.Errorf("claude-router was saved from the Kimi page: %v", st.saved)
	}
	m = press(t, m, "esc")
	if m.step != stepSubagentAgents {
		t.Errorf("esc: step = %v, want the agent picker", m.step)
	}
}

func TestSubagentManage_ClaudeRouterIsOnByDefault(t *testing.T) {
	st := stubSubagents(t)
	st.rows["claude"] = []subagentRow{
		{Name: "local-helper", Model: "qwen3.6-27b", Path: "/host/l.md", File: "l.md"},
		{Name: "reviewer", Model: "sonnet", Path: "/host/r.md", File: "r.md"},
	}
	m := openAgentPage(t, rerunModel("work"), "claude")
	view := m.View()
	for _, want := range []string{"[x] Enable claude-router", "Without it they do not work", "saved with your first change"} {
		if !strings.Contains(view, want) {
			t.Errorf("page lacks %q:\n%s", want, view)
		}
	}
	if len(st.saved) != 0 {
		t.Fatalf("opening the page saved %v", st.saved)
	}

	// The first change saves the default together with it.
	m = press(t, at(t, m, itemSubagent, "local-helper"), " ")
	if strings.Join(st.saved, ";") != "true||false@work" {
		t.Fatalf("saved = %v, want the router enabled with the first change", st.saved)
	}
	if !strings.Contains(m.View(), "set its backend below") || !strings.Contains(m.View(), "needs a backend below") {
		t.Errorf("the page does not ask for the backend:\n%s", m.View())
	}
	// The wizard state follows, so the review step keeps it.
	if !m.pluginEntry("claude", "claude-router").Enabled {
		t.Error("the wizard state does not have the router enabled")
	}
	// Turning it off warns about the subagents that now fail.
	m = press(t, at(t, m, itemRouter, ""), " ")
	if !strings.Contains(m.View(), "[ ] Enable claude-router") || !strings.Contains(m.View(), "1 subagent(s) here use a non-Claude model and will fail") {
		t.Errorf("router off:\n%s", m.View())
	}
	if _, err := m.pageItems(), error(nil); err != nil || itemCount(m, itemBackend) != 0 {
		t.Error("backend rows are shown with the router off")
	}
	m = press(t, at(t, m, itemRouter, ""), "enter")
	if !m.sub.routerOn || itemCount(m, itemBackend) != 1 {
		t.Errorf("router back on: on=%v backends=%d", m.sub.routerOn, itemCount(m, itemBackend))
	}
}

func itemCount(m Model, kind int) int {
	n := 0
	for _, it := range m.pageItems() {
		if it.kind == kind {
			n++
		}
	}
	return n
}

func TestSubagentManage_ClaudeRouterRespectsAStoredChoice(t *testing.T) {
	st := stubSubagents(t)
	st.router = routerState{Known: true, Enabled: false}
	m := openAgentPage(t, rerunModel("work"), "claude")
	if m.sub.routerOn || m.sub.routerPending {
		t.Error("a router the user disabled was turned back on")
	}
	// codex-router owns the base URL: the default is off and turning it on
	// says why it cannot be.
	st.router = routerState{Conflict: "claude-router cannot be enabled together with codex-router"}
	m = openAgentPage(t, rerunModel("work"), "claude")
	if m.sub.routerOn {
		t.Error("router on next to codex-router")
	}
	m = press(t, at(t, m, itemRouter, ""), " ")
	if m.sub.routerOn || len(st.saved) != 0 || !strings.Contains(m.View(), "together with codex-router") {
		t.Errorf("conflict: on=%v saved=%v\n%s", m.sub.routerOn, st.saved, m.View())
	}
}

func TestSubagentManage_BackendAndKey(t *testing.T) {
	st := stubSubagents(t)
	st.router = routerState{Known: true, Enabled: true}
	st.rows["claude"] = []subagentRow{{Name: "local-helper", Model: "qwen3.6-27b", Path: "/h/l.md", File: "l.md", Stored: true, Source: subagents.SourceImported}}

	var stores []string
	exists := false
	oldExists, oldCreate, oldStore := vaultExists, vaultCreate, vaultStore
	t.Cleanup(func() { vaultExists, vaultCreate, vaultStore = oldExists, oldCreate, oldStore })
	vaultExists = func(string) bool { return exists }
	vaultCreate = func(ws, pw string) error { exists = true; stores = append(stores, "init:"+ws+":"+pw); return nil }
	vaultStore = func(ws, pw, k, v string) error {
		if pw != "right" {
			return errors.New("wrong password")
		}
		stores = append(stores, ws+":"+k+"="+v)
		return nil
	}

	m := openAgentPage(t, rerunModel("work"), "claude")
	// The key needs a backend first.
	m = press(t, at(t, m, itemKey, "qwen3.6-27b"), "enter")
	if m.sub.input != nil || !strings.Contains(m.View(), "Set the backend URL for qwen3.6-27b first") {
		t.Fatalf("key before backend:\n%s", m.View())
	}
	// A bad URL is refused and nothing is saved.
	m = press(t, at(t, m, itemBackend, "qwen3.6-27b"), "enter")
	m = press(t, typeText(t, m, "10.0.0.21:8080"), "enter")
	if len(st.saved) != 0 || !strings.Contains(m.View(), "must start with http://") {
		t.Fatalf("bad URL: saved=%v\n%s", st.saved, m.View())
	}
	m = press(t, at(t, m, itemBackend, "qwen3.6-27b"), "enter")
	m = press(t, typeText(t, m, "http://10.0.0.21:8080/v1"), "enter")
	if strings.Join(st.saved, ";") != "true|qwen3.6-27b=http://10.0.0.21:8080|false@work" {
		t.Fatalf("saved = %v", st.saved)
	}
	if !strings.Contains(m.View(), "local-helper") || !strings.Contains(m.View(), "qwen3.6-27b on http://10.0.0.21:8080") {
		t.Errorf("subagent row does not show its backend:\n%s", m.View())
	}

	// The key is typed masked; the workspace has no vault, so one is made.
	m = press(t, at(t, m, itemKey, "qwen3.6-27b"), "enter")
	m = typeText(t, m, "sk-local-123")
	if strings.Contains(m.View(), "sk-local-123") || !strings.Contains(m.View(), "••••••••••••") {
		t.Fatalf("the key is not masked:\n%s", m.View())
	}
	m = press(t, m, "enter")
	if m.sub.input == nil || m.sub.input.kind != inputVaultNew {
		t.Fatalf("no new vault password asked for: %+v", m.sub.input)
	}
	m = press(t, typeText(t, m, "right"), "enter")
	m = press(t, typeText(t, m, "wrong"), "enter")
	if !strings.Contains(m.View(), "do not match") || m.sub.input.kind != inputVaultNew {
		t.Fatalf("mismatch:\n%s", m.View())
	}
	m = press(t, typeText(t, m, "right"), "enter")
	m = press(t, typeText(t, m, "right"), "enter")
	if strings.Join(stores, ";") != "init:work:right;work:CLAUDE_ROUTER_QWEN3_6_27B_KEY=sk-local-123" {
		t.Fatalf("vault = %v", stores)
	}
	last := st.saved[len(st.saved)-1]
	if last != "true|qwen3.6-27b=http://10.0.0.21:8080=vault:CLAUDE_ROUTER_QWEN3_6_27B_KEY|true@work" {
		t.Errorf("saved = %q, want the vault reference and the vault enabled", last)
	}
	view := m.View()
	if !strings.Contains(view, "in vault as CLAUDE_ROUTER_QWEN3_6_27B_KEY") || strings.Contains(view, "sk-local-123") {
		t.Errorf("after storing:\n%s", view)
	}
	if !m.state.VaultEnabled {
		t.Error("the wizard state does not have the vault enabled")
	}

	// A second key on the same page reuses the password; a wrong cached
	// one asks again and keeps the key.
	m.sub.vaultPW = "stale"
	m = press(t, at(t, m, itemKey, "qwen3.6-27b"), "enter")
	m = press(t, typeText(t, m, "sk-2"), "enter")
	if m.sub.input == nil || m.sub.input.kind != inputVaultPW || !strings.Contains(m.View(), "wrong password") {
		t.Fatalf("wrong password not handled:\n%s", m.View())
	}
	m = press(t, typeText(t, m, "right"), "enter")
	if stores[len(stores)-1] != "work:CLAUDE_ROUTER_QWEN3_6_27B_KEY=sk-2" || m.sub.pendingKey != "" {
		t.Errorf("vault = %v, pending = %q", stores, m.sub.pendingKey)
	}

	// Esc cancels a field without saving.
	n := len(st.saved)
	m = press(t, at(t, m, itemBackend, "qwen3.6-27b"), "enter")
	m = press(t, typeText(t, m, "x"), "esc")
	if len(st.saved) != n || m.sub.input != nil {
		t.Error("Esc saved the field")
	}
	// An empty URL removes the route.
	m = press(t, at(t, m, itemBackend, "qwen3.6-27b"), "enter")
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	m = press(t, updated.(Model), "enter")
	if !strings.HasPrefix(st.saved[len(st.saved)-1], "true||") || !strings.Contains(m.View(), "Route for qwen3.6-27b removed") {
		t.Errorf("remove route: %v\n%s", st.saved, m.View())
	}
}

func TestSubagentManage_MasterSkill(t *testing.T) {
	st := stubSubagents(t)
	m := openAgentPage(t, rerunModel("work"), "pi")
	if view := m.View(); !strings.Contains(view, "has no native subagents") || !strings.Contains(view, "Create master skill") {
		t.Errorf("Pi page:\n%s", view)
	}
	m = press(t, at(t, m, itemMasterSkill, ""), "enter")
	if strings.Join(st.created, ",") != "pi@work" || !strings.Contains(m.View(), "Edit master skill") {
		t.Fatalf("created = %v\n%s", st.created, m.View())
	}
	updated, cmd := m.Update(key("enter"))
	m = updated.(Model)
	if cmd == nil || len(st.edited) != 1 || st.edited[0] != subagents.Pi.MasterSkillPath("work") {
		t.Fatalf("edit: cmd = %v, edited = %v", cmd, st.edited)
	}
	updated, _ = m.Update(cmd())
	if !strings.Contains(updated.(Model).View(), "Master skill saved") {
		t.Errorf("no confirmation after the editor")
	}
	st.findings = []subagents.Finding{{Severity: subagents.Error, Field: "description", Message: "missing"}}
	updated, _ = m.Update(subEditDoneMsg{})
	if view := updated.(Model).View(); !strings.Contains(view, "has errors") || !strings.Contains(view, "description: missing") {
		t.Errorf("a broken master skill is not reported:\n%s", view)
	}
	updated, _ = m.Update(subEditDoneMsg{err: errors.New("exit status 1")})
	if !strings.Contains(updated.(Model).View(), "Editor exited with an error") {
		t.Errorf("an editor failure is not reported")
	}

	// ExitBox links no skills for Qwen Code.
	m = openAgentPage(t, rerunModel("work"), "qwen")
	if itemCount(m, itemMasterSkill) != 0 || !strings.Contains(m.View(), "ExitBox links no skills for Qwen Code") {
		t.Errorf("Qwen page:\n%s", m.View())
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
	st.rows["kimi"] = []subagentRow{{Name: "r", Path: "/h/r.md", File: "r.md"}}
	m.sub.agentCursor = agentRow(m, "kimi")
	press(t, m, "enter", " ")
	if len(st.stored) != 1 || st.stored[0] != "kimi:r@b" {
		t.Errorf("stored = %v, want the subagent stored in workspace b", st.stored)
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

	if path, err := createMasterSkill(subagents.Codex, "work"); err != nil || path != subagents.Codex.MasterSkillPath("work") {
		t.Errorf("createMasterSkill = %q, %v", path, err)
	}
}

// The page's real store, config and vault access, without stubs.
func TestSubagentPage_RealStoreConfigAndVault(t *testing.T) {
	old := config.Home
	config.Home = t.TempDir()
	t.Cleanup(func() { config.Home = old })
	host := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", host)
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	write := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(host, "agents", "local.md"), "---\nname: local-helper\ndescription: d\nmodel: qwen3.6-27b\n---\nP.\n")
	write(filepath.Join(host, "agents", "review.md"), "---\nname: reviewer\ndescription: d\nmodel: sonnet\n---\nP.\n")
	write(filepath.Join(host, "agents", "bad.md"), "---\nname: bad\n---\n")
	write(filepath.Join(host, "agents", "README.md"), "# notes\n")
	write(filepath.Join(subagents.Claude.StoreDir("work"), "mine.md"), "---\nname: mine\ndescription: d\n---\nP.\n")

	cfg := config.DefaultConfig()
	cfg.Workspaces.Items = []config.Workspace{{Name: "work"}}
	cfg.Workspaces.Active = "work"
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}

	rows, err := loadSubagentRows(subagents.Claude, "work")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s:%v:%v:%v", r.Name, r.Stored, r.onHost(), r.Problem != ""))
	}
	if want := "bad:false:true:true,local-helper:false:true:false,reviewer:false:true:false,mine:true:false:false"; strings.Join(got, ",") != want {
		t.Fatalf("rows = %v, want %s", got, want)
	}

	if err := storeSubagent(subagents.Claude, "work", rows[1]); err != nil {
		t.Fatal(err)
	}
	rows, _ = loadSubagentRows(subagents.Claude, "work")
	if !rows[1].Stored || rows[1].Source != subagents.SourceImported {
		t.Fatalf("stored row = %+v", rows[1])
	}
	if err := unstoreSubagent(subagents.Claude, "work", "local-helper"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(host, "agents", "local.md")); err != nil {
		t.Errorf("the host file was touched: %v", err)
	}

	// Router: unknown first, then saved with defaults kept.
	st := loadClaudeRouter("work")
	if st.Known || st.Enabled || st.Conflict != "" {
		t.Fatalf("fresh router = %+v", st)
	}
	routes, _ := router.ParseRoutes("qwen3.6-27b=http://10.0.0.21:8080=vault:CLAUDE_ROUTER_QWEN3_6_27B_KEY")
	if _, err := saveClaudeRouter("work", true, routes, true); err != nil {
		t.Fatal(err)
	}
	saved := config.LoadOrDefault().Workspace("work")
	entry, _ := saved.PluginEntry("claude", "claude-router")
	if !entry.Enabled || entry.Settings["routes"] != routes[0].String() || entry.Settings["port"] != "4203" || !saved.Vault.Enabled {
		t.Errorf("saved entry = %+v, vault = %+v", entry, saved.Vault)
	}
	if st := loadClaudeRouter("work"); !st.Known || !st.Enabled || len(st.Routes) != 1 {
		t.Errorf("reloaded router = %+v", st)
	}
	// codex-router owns the base URL: enabling is refused, the state says so.
	saved.SetPluginEntry("claude", "codex-router", config.PluginEntry{Enabled: true})
	c := config.LoadOrDefault()
	*c.Workspace("work") = *saved
	if err := config.SaveConfig(c); err != nil {
		t.Fatal(err)
	}
	if _, err := saveClaudeRouter("work", true, routes, false); err == nil {
		t.Error("claude-router was saved enabled next to codex-router")
	}
	if loadClaudeRouter("work").Conflict == "" {
		t.Error("the conflict is not reported")
	}
	if _, err := saveClaudeRouter("nope", true, nil, false); err == nil {
		t.Error("saved into a workspace that is not in the config")
	}

	// Vault: created and written through the real functions.
	if vaultExists("work") {
		t.Fatal("vault exists before it was created")
	}
	if err := vaultCreate("work", "pw"); err != nil {
		t.Fatal(err)
	}
	if err := vaultStore("work", "pw", routeVaultKey("qwen3.6-27b"), "sk-1"); err != nil {
		t.Fatal(err)
	}
	if err := vaultStore("work", "wrong", "K", "v"); err == nil {
		t.Error("a wrong vault password was accepted")
	}
	if v, err := vault.QuickGet("work", "pw", "CLAUDE_ROUTER_QWEN3_6_27B_KEY"); err != nil || v != "sk-1" {
		t.Errorf("vault value = %q, %v", v, err)
	}
}

func TestRouteVaultKey(t *testing.T) {
	for model, want := range map[string]string{
		"qwen3.6-27b":      "CLAUDE_ROUTER_QWEN3_6_27B_KEY",
		"gemma":            "CLAUDE_ROUTER_GEMMA_KEY",
		"org/model:Q4_K_M": "CLAUDE_ROUTER_ORG_MODEL_Q4_K_M_KEY",
		"-x-":              "CLAUDE_ROUTER_X_KEY",
	} {
		if got := routeVaultKey(model); got != want {
			t.Errorf("routeVaultKey(%q) = %q, want %q", model, got, want)
		}
		if _, err := router.ParseRoutes("m=http://h:1=vault:" + routeVaultKey(model)); err != nil {
			t.Errorf("%q is not a valid vault key in a route: %v", routeVaultKey(model), err)
		}
	}
}

func TestSubagentManage_Reviewer(t *testing.T) {
	st := stubSubagents(t)
	st.rows["claude"] = []subagentRow{
		{Name: "code-reviewer", Path: "/h/r.md", File: "r.md", Stored: true, Source: subagents.SourceImported},
		{Name: "unused", Path: "/h/u.md", File: "u.md"},
		{Name: "coder", Path: "/h/c.md", File: "c.md", Stored: true, Source: subagents.SourceImported},
	}
	reviewer, set := subagents.ReviewerSelf, []string{}
	oldGet, oldSet := masterSkillReviewer, setMasterSkillReviewer
	t.Cleanup(func() { masterSkillReviewer, setMasterSkillReviewer = oldGet, oldSet })
	masterSkillReviewer = func(*subagents.Harness, string) (string, error) { return reviewer, nil }
	setMasterSkillReviewer = func(_ *subagents.Harness, ws, r string) error {
		reviewer, set = r, append(set, r+"@"+ws)
		return nil
	}

	// No master skill yet: no reviewer to set.
	m := openAgentPage(t, rerunModel("work"), "claude")
	if itemCount(m, itemReviewer) != 0 {
		t.Fatal("reviewer offered without a master skill")
	}
	m = press(t, at(t, m, itemMasterSkill, ""), "enter")
	if !strings.Contains(m.View(), "Reviewer") || !strings.Contains(m.View(), "main agent") {
		t.Fatalf("no reviewer row after creating the skill:\n%s", m.View())
	}
	// Enter cycles: main agent, then each subagent in use, then back.
	m = at(t, m, itemReviewer, "")
	for _, want := range []string{"code-reviewer", "coder", subagents.ReviewerSelf} {
		m = press(t, m, "enter")
		if reviewer != want {
			t.Fatalf("reviewer = %q, want %q (unused subagents are not offered)", reviewer, want)
		}
	}
	if strings.Join(set, ",") != "code-reviewer@work,coder@work,main agent@work" {
		t.Errorf("set = %v", set)
	}
	if !strings.Contains(m.View(), "The main agent reviews the work of its subagents") {
		t.Errorf("view:\n%s", m.View())
	}

	// A file without the line: say how to fix it, change nothing.
	masterSkillReviewer = func(*subagents.Harness, string) (string, error) { return "", subagents.ErrNoReviewerLine }
	m = press(t, m, "enter")
	if len(set) != 3 || !strings.Contains(m.View(), `add the line "Reviewer: main agent"`) {
		t.Errorf("no line: set = %v\n%s", set, m.View())
	}
}

// The model of a subagent was renamed after its backend was set up: the
// page offers to hand the old route's URL and key to the new name.
func TestSubagentManage_MoveRouteAfterModelRename(t *testing.T) {
	st := stubSubagents(t)
	const oldID, newID = "cloud-exit/mesh-ai-coder-v1[1m]", "cloud-exit/mesh-ai-coder-v1"
	old, _ := router.ParseRoutes(oldID + "=https://ai.exitmesh.com=vault:CLAUDE_ROUTER_CLOUD_EXIT_MESH_AI_CODER_V1_1M_KEY")
	st.router = routerState{Known: true, Enabled: true, Routes: old}
	st.rows["claude"] = []subagentRow{{Name: "meshai", Model: newID, Path: "/h/m.md", File: "m.md", Stored: true, Source: subagents.SourceImported}}

	m := openAgentPage(t, rerunModel("default"), "claude")
	view := m.View()
	if !strings.Contains(view, "Use backend of "+oldID+" for "+newID) || !strings.Contains(view, "(https://ai.exitmesh.com, key kept; press Enter)") {
		t.Fatalf("no move offered:\n%s", view)
	}
	// Long model names keep the value column apart from the label.
	if !strings.Contains(view, oldID+" API key ") {
		t.Errorf("label runs into its value:\n%s", view)
	}

	m = press(t, at(t, m, itemMoveRoute, newID), "enter")
	want := "true|" + newID + "=https://ai.exitmesh.com=vault:CLAUDE_ROUTER_CLOUD_EXIT_MESH_AI_CODER_V1_1M_KEY|false@default"
	if len(st.saved) != 1 || st.saved[0] != want {
		t.Fatalf("saved = %v, want %s", st.saved, want)
	}
	view = m.View()
	if strings.Contains(view, "Use backend of") || strings.Contains(view, oldID+" URL") || !strings.Contains(view, newID+" on https://ai.exitmesh.com") {
		t.Errorf("after the move:\n%s", view)
	}
	if !strings.Contains(view, "with the key in the vault as CLAUDE_ROUTER_CLOUD_EXIT_MESH_AI_CODER_V1_1M_KEY") {
		t.Errorf("message does not say the key was kept:\n%s", view)
	}
	if itemCount(m, itemMoveRoute) != 0 {
		t.Error("move still offered")
	}
}
