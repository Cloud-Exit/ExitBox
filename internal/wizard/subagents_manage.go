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
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/plugins/clauderouter"
	"github.com/cloud-exit/exitbox/internal/router"
	"github.com/cloud-exit/exitbox/internal/subagents"
)

// Kinds of items on an agent's page.
const (
	itemRouter = iota
	itemSubagent
	itemBackend
	itemKey
	itemMasterSkill
	itemReviewer
)

// The master skill's Reviewer line, replaced in tests.
var (
	masterSkillReviewer = func(h *subagents.Harness, workspace string) (string, error) {
		return h.MasterSkillReviewer(workspace)
	}
	setMasterSkillReviewer = func(h *subagents.Harness, workspace, reviewer string) error {
		return h.SetMasterSkillReviewer(workspace, reviewer)
	}
)

// pageItem is one selectable line of an agent's page.
type pageItem struct {
	kind  int
	row   int    // index into rows, for itemSubagent
	model string // for itemBackend and itemKey
}

// isClaudePage reports whether the open page is Claude Code's, the only
// agent claude-router serves.
func (m Model) isClaudePage() bool { return m.sub.agent == subagents.Claude }

// loadSubagentPage reads the open page's subagents and router state.
func (m Model) loadSubagentPage() Model {
	h, ws := m.sub.agent, m.sub.workspace
	rows, err := loadSubagentRows(h, ws)
	m.sub.rows = rows
	if err != nil {
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not read the subagents: %v", err), true
	}
	if !m.isClaudePage() {
		return m
	}
	m.sub.router = loadClaudeRouter(ws)
	m.sub.routerOn = m.sub.router.Enabled
	m.sub.routerPending = false
	// The router is on by default: without it a subagent on a non-Claude
	// model cannot run. It is only saved with the first change here.
	if !m.sub.router.Known && m.sub.router.Conflict == "" {
		m.sub.routerOn, m.sub.routerPending = true, true
	}
	return m
}

// routedModels returns the models that need a backend: those of the
// subagents in use, then those that already have a route.
func (m Model) routedModels() []string {
	seen := map[string]bool{}
	var out []string
	add := func(model string) {
		if model != "" && !seen[model] {
			seen[model] = true
			out = append(out, model)
		}
	}
	for _, r := range m.sub.rows {
		if r.Stored && needsRoute(r.Model) {
			add(r.Model)
		}
	}
	for _, r := range m.sub.router.Routes {
		add(r.ID)
	}
	return out
}

// modelInUse reports whether a subagent used in the workspace runs on model.
func (m Model) modelInUse(model string) bool {
	for _, r := range m.sub.rows {
		if r.Stored && r.Model == model {
			return true
		}
	}
	return false
}

func (m Model) route(model string) (router.Route, bool) {
	return router.Find(m.sub.router.Routes, model)
}

// pageItems lists the page's selectable lines in display order.
func (m Model) pageItems() []pageItem {
	var items []pageItem
	if m.isClaudePage() {
		items = append(items, pageItem{kind: itemRouter})
	}
	for i := range m.sub.rows {
		items = append(items, pageItem{kind: itemSubagent, row: i})
	}
	if m.isClaudePage() && m.sub.routerOn {
		for _, model := range m.routedModels() {
			items = append(items, pageItem{kind: itemBackend, model: model}, pageItem{kind: itemKey, model: model})
		}
	}
	if m.sub.agent.Skills {
		items = append(items, pageItem{kind: itemMasterSkill})
		if m.sub.infos[m.sub.agent.Agent].MasterSkill {
			items = append(items, pageItem{kind: itemReviewer})
		}
	}
	return items
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
	if m.sub.input != nil {
		return m.updateSubInput(key)
	}
	items := m.pageItems()
	switch key.String() {
	case "up", "k":
		if m.sub.cursor > 0 {
			m.sub.cursor--
		}
	case "down", "j":
		if m.sub.cursor < len(items)-1 {
			m.sub.cursor++
		}
	case "a":
		m = m.useAllHostSubagents()
	case " ", "enter":
		if m.sub.cursor < len(items) {
			return m.activateItem(items[m.sub.cursor])
		}
	case "esc", "q":
		m.sub.msg, m.sub.vaultPW, m.sub.pendingKey = "", "", ""
		m.step = stepSubagentAgents
		return m.reloadSubagentInfos(), nil
	}
	return m, nil
}

func (m Model) activateItem(it pageItem) (tea.Model, tea.Cmd) {
	m.sub.msgIsError = false
	switch it.kind {
	case itemRouter:
		return m.toggleRouter(), nil
	case itemSubagent:
		return m.toggleSubagent(it.row), nil
	case itemBackend:
		text := ""
		if r, ok := m.route(it.model); ok {
			text = r.URL
		}
		m.sub.input = &subInput{kind: inputBackend, model: it.model, text: text,
			prompt: fmt.Sprintf("Backend URL for %s (llama.cpp or vLLM, serving /v1/messages)", it.model)}
	case itemKey:
		if _, ok := m.route(it.model); !ok {
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Set the backend URL for %s first.", it.model), true
			return m, nil
		}
		m.sub.input = &subInput{kind: inputKey, model: it.model, masked: true,
			prompt: fmt.Sprintf("API key of the %s backend (stored in the workspace vault)", it.model)}
	case itemMasterSkill:
		h, ws := m.sub.agent, m.sub.workspace
		if m.sub.infos[h.Agent].MasterSkill {
			return m, editFile(h.MasterSkillPath(ws))
		}
		if _, err := createMasterSkill(h, ws); err != nil {
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not create the master skill: %v", err), true
		} else {
			m.sub.msg = "Master skill created, with a route for every stored subagent."
		}
		return m.reloadSubagentInfos(), nil
	case itemReviewer:
		return m.nextReviewer(), nil
	}
	return m, nil
}

// reviewerChoices lists who can review: the main agent, then every stored
// subagent the agent loads.
func (m Model) reviewerChoices() []string {
	out := []string{subagents.ReviewerSelf}
	for _, r := range m.sub.rows {
		if r.Stored && r.Problem == "" {
			out = append(out, r.Name)
		}
	}
	return out
}

// nextReviewer moves the master skill's Reviewer line to the next choice.
func (m Model) nextReviewer() Model {
	h, ws := m.sub.agent, m.sub.workspace
	current, err := masterSkillReviewer(h, ws)
	if err != nil {
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("%v. Edit the master skill to add the line \"Reviewer: %s\".", err, subagents.ReviewerSelf), true
		return m
	}
	choices := m.reviewerChoices()
	next := choices[0]
	for i, c := range choices {
		if c == current {
			next = choices[(i+1)%len(choices)]
		}
	}
	if err := setMasterSkillReviewer(h, ws, next); err != nil {
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not set the reviewer: %v", err), true
		return m
	}
	if next == subagents.ReviewerSelf {
		m.sub.msg, m.sub.msgIsError = "The main agent reviews the work of its subagents.", false
	} else {
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("%s reviews the work of the other subagents; the main agent reviews %s's own work.", next, next), false
	}
	return m
}

// toggleRouter flips the claude-router checkbox and saves it.
func (m Model) toggleRouter() Model {
	on := !m.sub.routerOn
	if on && m.sub.router.Conflict != "" {
		m.sub.msg, m.sub.msgIsError = m.sub.router.Conflict, true
		return m
	}
	m = m.saveRouter(on, m.sub.router.Routes, false)
	if m.sub.msgIsError {
		return m
	}
	if on {
		m.sub.msg = "claude-router enabled. The next `exitbox run claude` adds it to the image."
	} else {
		m.sub.msg = "claude-router disabled. Subagents on non-Claude models will fail."
	}
	return m
}

// saveRouter stores the router state and mirrors it into the wizard state
// when the page works on the workspace the wizard edits, so the review
// step does not write the old state back.
func (m Model) saveRouter(on bool, routes []router.Route, enableVault bool) Model {
	entry, err := saveClaudeRouter(m.sub.workspace, on, routes, enableVault)
	if err != nil {
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not save claude-router: %v", err), true
		return m
	}
	m.sub.routerOn, m.sub.routerPending = on, false
	m.sub.router.Known, m.sub.router.Enabled, m.sub.router.Routes = true, on, routes
	if m.sub.workspace == m.activeWorkspaceName() {
		m.setPluginEntry(subagents.Claude.Agent, clauderouter.Name, entry)
		if enableVault {
			m.state.VaultEnabled, m.vaultExisting = true, true
		}
	}
	return m
}

// savePendingRouter saves the default router state once the page changes
// something else.
func (m Model) savePendingRouter() Model {
	if !m.sub.routerPending {
		return m
	}
	msg, isErr := m.sub.msg, m.sub.msgIsError
	m = m.saveRouter(true, m.sub.router.Routes, false)
	if !m.sub.msgIsError {
		m.sub.msg, m.sub.msgIsError = msg, isErr
	}
	return m
}

// toggleSubagent stores a host subagent in the workspace, or removes the
// workspace copy of one. The host file is never touched.
func (m Model) toggleSubagent(i int) Model {
	h, ws := m.sub.agent, m.sub.workspace
	row := m.sub.rows[i]
	switch {
	case row.Stored && !row.onHost():
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("%s was added with exitbox subagents and has no host copy to bring it back. Remove it with: exitbox subagents rm %s --agent %s -w %s",
			row.Name, row.Name, h.Agent, ws), true
		return m
	case row.Stored && row.Source != subagents.SourceImported:
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("The stored %s was added or edited with exitbox subagents, so it is kept. Remove it with: exitbox subagents rm %s --agent %s -w %s",
			row.Name, row.Name, h.Agent, ws), true
		return m
	case row.Stored:
		if err := unstoreSubagent(h, ws, row.Name); err != nil {
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not remove %s: %v", row.Name, err), true
			return m
		}
		m.sub.msg = fmt.Sprintf("%s is no longer used in this workspace. The host file is untouched.", row.Name)
	case row.Problem != "":
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("%s cannot be used: %s skips it (%s)", row.Name, h.Label, row.Problem), true
		return m
	default:
		if err := storeSubagent(h, ws, row); err != nil {
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not add %s: %v", row.Name, subagentError(err)), true
			return m
		}
		m.sub.msg = fmt.Sprintf("%s is used in this workspace from the next container start.", row.Name)
		if m.isClaudePage() && needsRoute(row.Model) {
			if _, ok := m.route(row.Model); !ok {
				m.sub.msg += fmt.Sprintf(" It runs on %s: set its backend below.", row.Model)
			}
		}
	}
	m = m.savePendingRouter()
	return m.reloadPage()
}

// useAllHostSubagents checks every host subagent that can be used.
func (m Model) useAllHostSubagents() Model {
	h, ws := m.sub.agent, m.sub.workspace
	added, failed := 0, 0
	for _, row := range m.sub.rows {
		if row.Stored || !row.onHost() || row.Problem != "" {
			continue
		}
		if err := storeSubagent(h, ws, row); err != nil {
			failed++
			continue
		}
		added++
	}
	m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Added %d subagent(s).", added), false
	if failed > 0 {
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Added %d subagent(s); %d could not be added.", added, failed), true
	}
	if added > 0 {
		m = m.savePendingRouter()
	}
	return m.reloadPage()
}

func (m Model) reloadPage() Model {
	rows, err := loadSubagentRows(m.sub.agent, m.sub.workspace)
	if err == nil {
		m.sub.rows = rows
	}
	m = m.reloadSubagentInfos()
	if n := len(m.pageItems()); m.sub.cursor >= n {
		m.sub.cursor = n - 1
	}
	return m
}

func subagentError(err error) string {
	var inv *subagents.InvalidError
	if errors.As(err, &inv) {
		return firstError(inv.Findings)
	}
	return err.Error()
}

// updateSubInput edits the open text field.
func (m Model) updateSubInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	in := m.sub.input
	switch key.Type {
	case tea.KeyEsc:
		m.sub.input, m.sub.pendingKey, m.sub.newPW = nil, "", ""
		m.sub.msg, m.sub.msgIsError = "Cancelled.", false
		return m, nil
	case tea.KeyEnter:
		text := strings.TrimSpace(in.text)
		m.sub.input = nil
		return m.submitSubInput(in, text), nil
	case tea.KeyBackspace:
		if r := []rune(in.text); len(r) > 0 {
			in.text = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		in.text = ""
	case tea.KeySpace:
		in.text += " "
	case tea.KeyRunes:
		in.text += string(key.Runes)
	}
	return m, nil
}

func (m Model) submitSubInput(in *subInput, text string) Model {
	ws := m.sub.workspace
	m.sub.msgIsError = false
	switch in.kind {
	case inputBackend:
		return m.saveBackend(in.model, text)
	case inputKey:
		if text == "" {
			m.sub.msg = "No key entered; nothing changed."
			return m
		}
		m.sub.pendingKey, m.sub.pendingModel = text, in.model
		return m.storePendingKey()
	case inputVaultPW:
		if text == "" {
			m.sub.pendingKey, m.sub.msg = "", "No password entered; the key was not stored."
			return m
		}
		m.sub.vaultPW = text
		return m.storePendingKey()
	case inputVaultNew:
		if text == "" {
			m.sub.pendingKey, m.sub.msg = "", "No password entered; the key was not stored."
			return m
		}
		m.sub.newPW = text
		m.sub.input = &subInput{kind: inputVaultConfirm, masked: true, prompt: "Repeat the new vault password"}
	case inputVaultConfirm:
		if text != m.sub.newPW {
			m.sub.newPW = ""
			m.sub.msg, m.sub.msgIsError = "The passwords do not match. Try again.", true
			m.sub.input = &subInput{kind: inputVaultNew, masked: true, prompt: fmt.Sprintf("New vault password for workspace '%s'", ws)}
			return m
		}
		if err := vaultCreate(ws, m.sub.newPW); err != nil {
			m.sub.newPW, m.sub.pendingKey = "", ""
			m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not create the vault: %v", err), true
			return m
		}
		m.sub.vaultPW, m.sub.newPW = m.sub.newPW, ""
		return m.storePendingKey()
	}
	return m
}

// saveBackend sets the backend URL of a model's route. The served name and
// vault key of an existing route are kept.
func (m Model) saveBackend(model, url string) Model {
	routes := append([]router.Route(nil), m.sub.router.Routes...)
	idx := -1
	for i, r := range routes {
		if r.ID == model {
			idx = i
		}
	}
	if url == "" {
		if idx < 0 {
			m.sub.msg = "No URL entered; nothing changed."
			return m
		}
		routes = append(routes[:idx], routes[idx+1:]...)
		m = m.saveRouter(m.sub.routerOn, routes, false)
		if !m.sub.msgIsError {
			m.sub.msg = fmt.Sprintf("Route for %s removed.", model)
		}
		return m.reloadPage()
	}
	r := router.Route{ID: model}
	if idx >= 0 {
		r = routes[idx]
	}
	parsed, err := router.ParseRoutes(model + "=" + url)
	if err != nil {
		m.sub.msg, m.sub.msgIsError = err.Error(), true
		return m
	}
	r.URL = parsed[0].URL
	if idx >= 0 {
		routes[idx] = r
	} else {
		routes = append(routes, r)
	}
	m = m.saveRouter(m.sub.routerOn, routes, false)
	if m.sub.msgIsError {
		return m
	}
	m.sub.msg = fmt.Sprintf("%s runs on %s. If that server needs an API key, set it on the next line.", model, r.URL)
	if r.OnHostLoopback() {
		m.sub.msg += " It is on this machine and is relayed into the container."
	}
	return m.reloadPage()
}

// storePendingKey writes the entered backend key into the workspace vault,
// asking for the vault password (or creating the vault) first.
func (m Model) storePendingKey() Model {
	ws, model := m.sub.workspace, m.sub.pendingModel
	if m.sub.vaultPW == "" {
		if vaultExists(ws) {
			m.sub.input = &subInput{kind: inputVaultPW, masked: true, prompt: fmt.Sprintf("Vault password for workspace '%s'", ws)}
		} else {
			m.sub.input = &subInput{kind: inputVaultNew, masked: true,
				prompt: fmt.Sprintf("Workspace '%s' has no vault yet. New vault password", ws)}
		}
		return m
	}
	routes := append([]router.Route(nil), m.sub.router.Routes...)
	idx := -1
	for i, r := range routes {
		if r.ID == model {
			idx = i
		}
	}
	if idx < 0 {
		m.sub.pendingKey = ""
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Set the backend URL for %s first.", model), true
		return m
	}
	keyName := routes[idx].VaultKey
	if keyName == "" {
		keyName = routeVaultKey(model)
	}
	if err := vaultStore(ws, m.sub.vaultPW, keyName, m.sub.pendingKey); err != nil {
		// Most likely a wrong password: ask again, keep the key.
		m.sub.vaultPW = ""
		m.sub.msg, m.sub.msgIsError = fmt.Sprintf("Could not store the key: %v", err), true
		m.sub.input = &subInput{kind: inputVaultPW, masked: true, prompt: fmt.Sprintf("Vault password for workspace '%s'", ws)}
		return m
	}
	m.sub.pendingKey = ""
	routes[idx].VaultKey = keyName
	m = m.saveRouter(m.sub.routerOn, routes, true)
	if m.sub.msgIsError {
		return m
	}
	m.sub.msg = fmt.Sprintf("Key for %s stored in the vault as %s. Each container start asks you to approve reading it.", model, keyName)
	return m.reloadPage()
}

func firstError(fs []subagents.Finding) string {
	for _, f := range fs {
		if f.Severity == subagents.Error {
			return f.String()
		}
	}
	return ""
}

func checkbox(on bool) string {
	if on {
		return selectedStyle.Render("[x]")
	}
	return "[ ]"
}

func (m Model) viewSubagentManage() string {
	h := m.sub.agent
	ws := m.sub.workspace
	info := m.sub.infos[h.Agent]
	var b strings.Builder
	b.WriteString(titleStyle.Render("Subagent Management: " + h.Label))
	b.WriteString("\n\n")
	b.WriteString(fmt.Sprintf("Workspace: %s\n", selectedStyle.Render(ws)))

	items := m.pageItems()
	line := func(idx int, text string) {
		cursor := "  "
		if idx == m.sub.cursor {
			cursor = cursorStyle.Render("> ")
		}
		b.WriteString(cursor + text + "\n")
	}
	label := func(idx int, s string, width int, dim bool) string {
		s = fmt.Sprintf("%-*s", width, s)
		switch {
		case idx == m.sub.cursor:
			return selectedStyle.Render(s)
		case dim:
			return dimStyle.Render(s)
		}
		return s
	}

	unrouted := 0
	for _, r := range m.sub.rows {
		if r.Stored && needsRoute(r.Model) {
			unrouted++
		}
	}

	for idx, it := range items {
		switch it.kind {
		case itemRouter:
			b.WriteString("\n" + dimStyle.Render("  Local models for subagents") + "\n")
			line(idx, fmt.Sprintf("%s %s %s", checkbox(m.sub.routerOn), label(idx, "Enable claude-router", 24, false),
				dimStyle.Render("Required for subagents on non-Claude models (Qwen, Gemma, ...). Without it they do not work.")))
			switch {
			case m.sub.routerPending:
				b.WriteString("      " + dimStyle.Render("On by default; saved with your first change on this page.") + "\n")
			case !m.sub.routerOn && unrouted > 0:
				b.WriteString("      " + warnStyle.Render(fmt.Sprintf("⚠ %d subagent(s) here use a non-Claude model and will fail.", unrouted)) + "\n")
			}
			if m.sub.router.Conflict != "" && !m.sub.routerOn {
				b.WriteString("      " + warnStyle.Render(m.sub.router.Conflict) + "\n")
			}
		case itemSubagent:
			if it.row == 0 {
				b.WriteString("\n" + dimStyle.Render(fmt.Sprintf("  Subagents (from the host's %s; checked = used in this workspace)", h.AgentsDirLabel())) + "\n")
			}
			r := m.sub.rows[it.row]
			model := r.Model
			if model == "" {
				model = "default model"
			}
			note := model
			switch {
			case r.Problem != "":
				note = warnStyle.Render("skipped by " + h.Label + ": " + truncate(r.Problem, 70))
			case !r.onHost():
				note = dimStyle.Render(model + ", added with exitbox subagents")
			case m.isClaudePage() && r.Stored && needsRoute(r.Model):
				if rt, ok := m.route(r.Model); ok && m.sub.routerOn {
					note = dimStyle.Render(model+" on ") + rt.URL
				} else {
					note = warnStyle.Render(model + ": needs a backend below")
				}
			default:
				note = dimStyle.Render(note)
			}
			line(idx, fmt.Sprintf("%s %s %s", checkbox(r.Stored), label(idx, r.Name, 24, r.Problem != ""), note))
		case itemBackend:
			if idx == 0 || items[idx-1].kind != itemKey {
				b.WriteString("\n" + dimStyle.Render("  Backends (llama.cpp or vLLM serving the Anthropic Messages API)") + "\n")
			}
			value := warnStyle.Render("not set: press Enter")
			if r, ok := m.route(it.model); ok {
				value = r.URL
				if !m.modelInUse(it.model) {
					value += dimStyle.Render("   (no subagent here uses this model; Enter, Ctrl+U, Enter removes it)")
				}
			}
			line(idx, fmt.Sprintf("    %s %s", label(idx, it.model+" URL", 28, false), value))
		case itemKey:
			value := dimStyle.Render("none (press Enter if the server needs one)")
			if r, ok := m.route(it.model); ok && r.VaultKey != "" {
				value = selectedStyle.Render("in vault") + dimStyle.Render(" as "+r.VaultKey)
			}
			line(idx, fmt.Sprintf("    %s %s", label(idx, it.model+" API key", 28, false), value))
		case itemMasterSkill:
			b.WriteString("\n" + dimStyle.Render("  Master skill") + "\n")
			text, desc := "Create master skill", "The main agent coordinates: it hands tasks to subagents and reviews their work"
			if info.MasterSkill {
				text, desc = "Edit master skill", "Open the coordination rules in $EDITOR"
			}
			line(idx, fmt.Sprintf("%s %s", label(idx, text, 28, false), dimStyle.Render(desc)))
		case itemReviewer:
			value := dimStyle.Render("no Reviewer line in the file")
			if r, err := masterSkillReviewer(h, ws); err == nil {
				value = selectedStyle.Render(r) + dimStyle.Render("   (Enter: next choice)")
			}
			line(idx, fmt.Sprintf("%s %s", label(idx, "Reviewer", 28, false), value))
		}
	}

	if !h.HasSubagents() {
		b.WriteString("\n  " + dimStyle.Render(h.Label+" has no native subagents.") + "\n")
	} else if len(m.sub.rows) == 0 {
		b.WriteString("\n  " + dimStyle.Render("No subagents in the host's "+h.AgentsDirLabel()+" and none stored.") + "\n")
	}
	if !h.Skills {
		b.WriteString("  " + dimStyle.Render("Master skill: ExitBox links no skills for "+h.Label+" yet.") + "\n")
	}
	if h.Caveat != "" {
		b.WriteString("\n  " + warnStyle.Render("⚠ "+h.Caveat) + "\n")
	}

	if m.sub.msg != "" {
		style := successStyle
		if m.sub.msgIsError {
			style = warnStyle
		}
		b.WriteString("\n  " + style.Render(m.sub.msg) + "\n")
	}

	if in := m.sub.input; in != nil {
		shown := in.text
		if in.masked {
			shown = strings.Repeat("•", len([]rune(in.text)))
		}
		b.WriteString("\n  " + in.prompt + ":\n  > " + shown + cursorStyle.Render("_") + "\n")
		b.WriteString(helpStyle.Render("\n  Enter to save, Esc to cancel, Ctrl+U to clear"))
		return b.String()
	}

	help := "Up/Down to move, Space/Enter to toggle or edit, a to use every host subagent, Esc to go back. Changes are saved at once."
	b.WriteString(helpStyle.Render("\n" + help))
	return b.String()
}
