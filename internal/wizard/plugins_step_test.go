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
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/plugins"
)

func modelWithAgents(agentNames ...string) Model {
	m := NewModel()
	for _, name := range agentNames {
		m.checked["agent:"+name] = true
	}
	return m
}

func key(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace}
	}
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestHasPluginsStep(t *testing.T) {
	if modelWithAgents("qwen").hasPluginsStep() {
		t.Error("qwen has no plugins, the step must be hidden")
	}
	if !modelWithAgents("codex").hasPluginsStep() {
		t.Error("codex has plugins, the step must be shown")
	}
}

func TestStepAfterAgents(t *testing.T) {
	if got := modelWithAgents("qwen").stepAfterAgents(); got != stepSettings {
		t.Errorf("stepAfterAgents() = %v, want stepSettings", got)
	}
	if got := modelWithAgents("codex").stepAfterAgents(); got != stepPlugins {
		t.Errorf("stepAfterAgents() = %v, want stepPlugins", got)
	}
}

// A step missing from View shows a blank screen.
func TestView_RendersPluginsStep(t *testing.T) {
	m := modelWithAgents("codex")
	m.step = stepPlugins
	if view := m.View(); !strings.Contains(view, "Agent plugins") {
		t.Errorf("View() at the plugins step = %q, want the plugin list", view)
	}
}

func TestPluginRowsCoverSelectedAgents(t *testing.T) {
	rows := modelWithAgents("codex", "claude").pluginRows()
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.Agent] = true
	}
	if !seen["codex"] || !seen["claude"] {
		t.Errorf("pluginRows() = %v, want rows for both agents", rows)
	}
}

func TestPluginsStepToggleAndContinue(t *testing.T) {
	m := modelWithAgents("codex")
	m.step = stepPlugins

	updated, _ := m.updatePlugins(key(" "))
	m = updated.(Model)

	rows := m.pluginRows()
	if len(rows) == 0 {
		t.Fatal("no plugin rows")
	}
	entry := m.pluginEntry(rows[0].Agent, rows[0].Plugin.Name)
	if !entry.Enabled {
		t.Error("space should enable the highlighted plugin")
	}
	if entry.Settings["port"] == "" {
		t.Errorf("enabling should seed defaults, got %v", entry.Settings)
	}

	updated, _ = m.updatePlugins(key("enter"))
	m = updated.(Model)
	if m.step != stepSettings {
		t.Errorf("enter should continue to settings, got %v", m.step)
	}
}

func TestPluginsStepEscGoesBack(t *testing.T) {
	m := modelWithAgents("codex")
	m.step = stepPlugins
	updated, _ := m.updatePlugins(key("esc"))
	if updated.(Model).step != stepAgents {
		t.Errorf("esc should return to the agents step")
	}
}

func TestPluginsStepOpensAndClosesForm(t *testing.T) {
	m := modelWithAgents("codex")
	m.step = stepPlugins

	updated, _ := m.updatePlugins(key("e"))
	m = updated.(Model)
	if m.pluginForm == nil {
		t.Fatal("e should open the plugin configuration menu")
	}
	if !strings.Contains(m.viewPlugins(), "settings") {
		t.Error("the step should render the open menu")
	}

	// Saving in the nested menu closes it without quitting the wizard.
	updated, cmd := m.updatePlugins(key("s"))
	m = updated.(Model)
	if m.pluginForm != nil {
		t.Error("saving should close the menu")
	}
	if cmd != nil {
		t.Error("the nested menu must not quit the wizard")
	}
	if m.step != stepPlugins {
		t.Errorf("step = %v, want the wizard to stay on the plugins step", m.step)
	}
}

func TestPrunePluginSettingsDropsDefaultsAndHidden(t *testing.T) {
	p := plugins.Get("codex-router")
	got := prunePluginSettings(p, map[string]string{
		"port":              "4202", // default
		"auto_start":        "false",
		"providers":         "deepseek",
		"anthropic_api_key": "vault:SOME_KEY", // hidden: provider not selected
	})
	if _, ok := got["port"]; ok {
		t.Errorf("default value should not be stored: %v", got)
	}
	if _, ok := got["anthropic_api_key"]; ok {
		t.Errorf("hidden setting should not be stored: %v", got)
	}
	if got["auto_start"] != "false" || got["providers"] != "deepseek" {
		t.Errorf("real choices lost: %v", got)
	}
}

func TestPluginMapConversions(t *testing.T) {
	ws := config.Workspace{Name: "default"}
	ws.SetPluginEntry("codex", "codex-router", config.PluginEntry{Enabled: true, Settings: map[string]string{"port": "4300"}})

	copied := copyPluginMap(ws.Plugins)
	copied["codex"]["codex-router"].Settings["port"] = "9999"
	if ws.PluginSetting("codex", "codex-router", "port") != "4300" {
		t.Error("copyPluginMap must not alias the config's settings")
	}

	back := toPluginMap(copied)
	entry := back["codex"]["codex-router"]
	if !entry.Enabled || entry.Settings["port"] != "9999" {
		t.Errorf("toPluginMap lost data: %+v", entry)
	}
	if toPluginMap(nil) != nil {
		t.Error("toPluginMap(nil) should be nil")
	}
	// Entries that are neither enabled nor configured are dropped.
	empty := map[string]map[string]config.PluginEntry{"codex": {"codex-router": {}}}
	if toPluginMap(empty) != nil {
		t.Error("empty entries should not be persisted")
	}
}

func TestPluginSummaryHidesSecretValues(t *testing.T) {
	p := plugins.Get("codex-router")
	summary := pluginSummary(p, config.PluginEntry{Settings: map[string]string{
		"providers":         "anthropic-api",
		"anthropic_api_key": "vault:MY_KEY",
	}})
	if !strings.Contains(summary, "vault:MY_KEY") {
		t.Errorf("summary = %q, want the vault key shown", summary)
	}
	if strings.Contains(pluginSummary(p, config.PluginEntry{}), "vault") {
		t.Error("an unconfigured plugin should just prompt for configuration")
	}
}

// pluginRowIndex returns the row of a plugin in the plugins step.
func pluginRowIndex(t *testing.T, m Model, agentName, pluginName string) int {
	t.Helper()
	for i, row := range m.pluginRows() {
		if row.Agent == agentName && row.Plugin.Name == pluginName {
			return i
		}
	}
	t.Fatalf("no row for %s / %s", agentName, pluginName)
	return -1
}

// claude-router and codex-router both set Claude Code's base URL, so the
// step lets only one of them be enabled for Claude at a time.
func TestPluginsStepRefusesTwoRoutersForClaude(t *testing.T) {
	m := modelWithAgents("claude", "codex")
	m.step = stepPlugins
	toggle := func(agentName, pluginName string) {
		t.Helper()
		m.pluginCursor = pluginRowIndex(t, m, agentName, pluginName)
		updated, _ := m.updatePlugins(key(" "))
		m = updated.(Model)
	}

	toggle("claude", "claude-router")
	if !m.pluginEntry("claude", "claude-router").Enabled || m.pluginNote != "" {
		t.Fatalf("claude-router alone must enable, note %q", m.pluginNote)
	}
	toggle("claude", "codex-router")
	if m.pluginEntry("claude", "codex-router").Enabled {
		t.Error("codex-router was enabled next to claude-router")
	}
	if !strings.Contains(m.pluginNote, "cannot be enabled together") {
		t.Errorf("no reason shown: %q", m.pluginNote)
	}
	if !strings.Contains(m.viewPlugins(), "cannot be enabled together") {
		t.Error("the reason is not on screen")
	}

	// The same plugin for another agent is a different matter.
	toggle("codex", "codex-router")
	if !m.pluginEntry("codex", "codex-router").Enabled || m.pluginNote != "" {
		t.Errorf("codex-router for codex must enable, note %q", m.pluginNote)
	}

	// Turning the first one off frees the second.
	toggle("claude", "claude-router")
	toggle("claude", "codex-router")
	if !m.pluginEntry("claude", "codex-router").Enabled || m.pluginNote != "" {
		t.Errorf("codex-router must enable once claude-router is off, note %q", m.pluginNote)
	}
	toggle("claude", "claude-router")
	if m.pluginEntry("claude", "claude-router").Enabled || m.pluginNote == "" {
		t.Error("claude-router was enabled next to codex-router")
	}
}

// Enabling from inside the configuration menu is the same switch.
func TestPluginsStepRefusesTwoRoutersFromTheForm(t *testing.T) {
	m := modelWithAgents("claude")
	m.step = stepPlugins
	m.setPluginEntry("claude", "codex-router", config.PluginEntry{Enabled: true})
	m.pluginCursor = pluginRowIndex(t, m, "claude", "claude-router")

	updated, _ := m.updatePlugins(key("e"))
	m = updated.(Model)
	if m.pluginForm == nil {
		t.Fatal("the configuration menu did not open")
	}
	m.pluginForm.enabled = true
	m.pluginForm.settings["routes"] = "qwen=http://10.0.0.21:8080"
	updated, _ = m.updatePlugins(key("s")) // save
	m = updated.(Model)
	if m.pluginForm != nil {
		t.Fatal("saving did not close the menu")
	}

	entry := m.pluginEntry("claude", "claude-router")
	if entry.Enabled {
		t.Error("claude-router was enabled from its menu next to codex-router")
	}
	if entry.Settings["routes"] != "qwen=http://10.0.0.21:8080" {
		t.Errorf("the settings typed in the menu were lost: %v", entry.Settings)
	}
	if m.pluginNote == "" {
		t.Error("no reason shown")
	}
}
