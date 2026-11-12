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
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/plugin"
	"github.com/cloud-exit/exitbox/internal/plugins"
)

// pluginRow is one line of the plugins step: a plugin offered for one of
// the agents selected in the previous step.
type pluginRow struct {
	Agent  string
	Plugin *plugin.Plugin
}

// selectedAgents returns the agents ticked in the agents step.
func (m Model) selectedAgents() []string {
	var out []string
	for _, a := range AllAgents {
		if m.checked["agent:"+a.Name] {
			out = append(out, a.Name)
		}
	}
	return out
}

// pluginRows returns the plugins configurable for the selected agents.
func (m Model) pluginRows() []pluginRow {
	var rows []pluginRow
	for _, agentName := range m.selectedAgents() {
		for _, p := range plugins.For(agentName) {
			rows = append(rows, pluginRow{Agent: agentName, Plugin: p})
		}
	}
	return rows
}

// hasPluginsStep reports whether any selected agent has plugins to offer.
func (m Model) hasPluginsStep() bool {
	return len(m.pluginRows()) > 0
}

// stepAfterAgents returns the step following the agents step, skipping
// plugins when no selected agent has any.
func (m Model) stepAfterAgents() Step {
	if m.hasPluginsStep() {
		return stepPlugins
	}
	return stepSettings
}

// enabledPlugins returns the plugins enabled for an agent in wizard state.
func (m Model) enabledPlugins(agentName string) []string {
	var out []string
	for name, entry := range m.state.Plugins[agentName] {
		if entry.Enabled {
			out = append(out, name)
		}
	}
	return out
}

// pluginEntry returns the stored configuration of one plugin.
func (m Model) pluginEntry(agentName, pluginName string) config.PluginEntry {
	if m.state.Plugins == nil {
		return config.PluginEntry{}
	}
	return m.state.Plugins[agentName][pluginName]
}

// setPluginEntry stores the configuration of one plugin in wizard state.
func (m *Model) setPluginEntry(agentName, pluginName string, entry config.PluginEntry) {
	if m.state.Plugins == nil {
		m.state.Plugins = map[string]map[string]config.PluginEntry{}
	}
	if m.state.Plugins[agentName] == nil {
		m.state.Plugins[agentName] = map[string]config.PluginEntry{}
	}
	m.state.Plugins[agentName][pluginName] = entry
}

func (m Model) updatePlugins(msg tea.Msg) (tea.Model, tea.Cmd) {
	// A plugin's configuration menu runs nested inside this step; its quit
	// command closes the menu, not the wizard.
	if m.pluginForm != nil {
		updated, _ := m.pluginForm.Update(msg)
		form, ok := updated.(*PluginForm)
		if !ok {
			m.pluginForm = nil
			return m, nil
		}
		m.pluginForm = form
		if form.Done() {
			result := form.Result()
			if result.Saved {
				rows := m.pluginRows()
				if m.pluginCursor < len(rows) {
					row := rows[m.pluginCursor]
					entry := m.pluginEntry(row.Agent, row.Plugin.Name)
					m.pluginNote = ""
					if result.Enabled && !entry.Enabled {
						if err := plugins.Conflict(row.Agent, row.Plugin.Name, m.enabledPlugins(row.Agent)); err != nil {
							// Keep the settings, refuse only the switch.
							m.pluginNote = err.Error()
							result.Enabled = false
						}
					}
					entry.Enabled = result.Enabled
					entry.Settings = prunePluginSettings(row.Plugin, result.Settings)
					m.setPluginEntry(row.Agent, row.Plugin.Name, entry)
				}
			}
			m.pluginForm = nil
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	rows := m.pluginRows()
	switch key.String() {
	case "up", "k":
		if m.pluginCursor > 0 {
			m.pluginCursor--
		}
	case "down", "j":
		if m.pluginCursor < len(rows)-1 {
			m.pluginCursor++
		}
	case " ", "x":
		if m.pluginCursor < len(rows) {
			row := rows[m.pluginCursor]
			entry := m.pluginEntry(row.Agent, row.Plugin.Name)
			m.pluginNote = ""
			if !entry.Enabled {
				if err := plugins.Conflict(row.Agent, row.Plugin.Name, m.enabledPlugins(row.Agent)); err != nil {
					m.pluginNote = err.Error()
					return m, nil
				}
			}
			if !entry.Enabled && entry.Settings == nil {
				entry.Settings = row.Plugin.DefaultSettings()
			}
			entry.Enabled = !entry.Enabled
			m.setPluginEntry(row.Agent, row.Plugin.Name, entry)
		}
	case "e":
		if m.pluginCursor < len(rows) {
			row := rows[m.pluginCursor]
			m.pluginForm = NewPluginForm(row.Agent, row.Plugin, m.pluginEntry(row.Agent, row.Plugin.Name))
		}
	case "enter":
		m.visitedSteps[stepPlugins] = true
		m.step = stepSettings
		m.cursor = 0
	case "esc":
		m.step = stepAgents
		m.cursor = 0
	}
	return m, nil
}

func (m Model) viewPlugins() string {
	if m.pluginForm != nil {
		return m.pluginForm.View()
	}

	var b strings.Builder
	b.WriteString(m.stepTitle(7, "Agent plugins"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Plugins are installed into the agent image and configured per workspace.\n"))
	b.WriteString("\n")

	for i, row := range m.pluginRows() {
		cursor := "  "
		if m.pluginCursor == i {
			cursor = cursorStyle.Render("> ")
		}
		entry := m.pluginEntry(row.Agent, row.Plugin.Name)
		check := "[ ]"
		if entry.Enabled {
			check = selectedStyle.Render("[x]")
		}
		label := fmt.Sprintf("%-26s", row.Agent+" / "+row.Plugin.Name)
		if m.pluginCursor == i {
			label = selectedStyle.Render(label)
		}
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, check, label, dimStyle.Render(row.Plugin.Description)))
		if m.pluginCursor == i && entry.Enabled {
			b.WriteString(dimStyle.Render("      "+pluginSummary(row.Plugin, entry)) + "\n")
		}
	}

	if m.pluginNote != "" {
		b.WriteString("\n" + warnStyle.Render(m.pluginNote) + "\n")
	}
	b.WriteString(helpStyle.Render("\nSpace to toggle, e to configure, Enter to continue, Esc to go back" + m.tabHint()))
	return b.String()
}

// pluginSummary renders a one-line summary of a plugin's settings for the
// wizard list. Secrets show their vault key, never a value.
func pluginSummary(p *plugin.Plugin, entry config.PluginEntry) string {
	if len(entry.Settings) == 0 {
		return "not configured yet - press e"
	}
	var parts []string
	for _, s := range p.Settings {
		value := entry.Settings[s.Key]
		if value == "" || !s.Visible(entry.Settings) {
			continue
		}
		if s.Type == plugin.TypeSecret {
			if key, ok := plugin.VaultKey(value); ok {
				value = "vault:" + key
			}
		}
		parts = append(parts, s.Key+"="+value)
	}
	if len(parts) == 0 {
		return "not configured yet - press e"
	}
	return strings.Join(parts, "  ")
}

// prunePluginSettings drops values equal to the plugin's defaults or hidden
// by another setting, so the wizard writes only real choices.
func prunePluginSettings(p *plugin.Plugin, settings map[string]string) map[string]string {
	defaults := p.DefaultSettings()
	out := map[string]string{}
	for _, s := range p.Settings {
		value := settings[s.Key]
		if value == "" || value == defaults[s.Key] || !s.Visible(settings) {
			continue
		}
		out[s.Key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// copyPluginMap deep-copies a workspace plugin map so wizard edits do not
// mutate the loaded config before the user confirms.
func copyPluginMap(in config.PluginMap) map[string]map[string]config.PluginEntry {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]map[string]config.PluginEntry, len(in))
	for agentName, byPlugin := range in {
		copied := make(map[string]config.PluginEntry, len(byPlugin))
		for name, entry := range byPlugin {
			settings := map[string]string{}
			for k, v := range entry.Settings {
				settings[k] = v
			}
			if len(settings) == 0 {
				settings = nil
			}
			copied[name] = config.PluginEntry{Enabled: entry.Enabled, Version: entry.Version, Settings: settings}
		}
		out[agentName] = copied
	}
	return out
}

// toPluginMap converts wizard state back into the config representation.
func toPluginMap(in map[string]map[string]config.PluginEntry) config.PluginMap {
	if len(in) == 0 {
		return nil
	}
	out := config.PluginMap{}
	for agentName, byPlugin := range in {
		for name, entry := range byPlugin {
			if !entry.Enabled && len(entry.Settings) == 0 {
				continue
			}
			if out[agentName] == nil {
				out[agentName] = map[string]config.PluginEntry{}
			}
			out[agentName][name] = entry
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// pluginsSummary renders the enabled plugins for the review step, as
// "agent/plugin" entries. Empty when nothing is enabled.
func (m Model) pluginsSummary() string {
	var out []string
	for agentName, byPlugin := range m.state.Plugins {
		for name, entry := range byPlugin {
			if entry.Enabled {
				out = append(out, agentName+"/"+name)
			}
		}
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}
