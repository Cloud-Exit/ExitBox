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

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/plugins"
	"github.com/cloud-exit/exitbox/internal/plugins/clauderouter"
	"github.com/cloud-exit/exitbox/internal/router"
	"github.com/cloud-exit/exitbox/internal/subagents"
	"github.com/cloud-exit/exitbox/internal/vault"
)

// subagentRow is one subagent on an agent's page: found in the host's
// agents directory, stored in the workspace, or both.
type subagentRow struct {
	Name    string
	Model   string
	File    string // file name relative to the directory it was found in
	Path    string // host path; "" when the subagent is only in the store
	Stored  bool   // the workspace uses it
	Source  subagents.Source
	Problem string // why the agent would skip the host file; it cannot be stored
}

// onHost reports whether the row has a host file to copy.
func (r subagentRow) onHost() bool { return r.Path != "" }

// routerState is the claude-router configuration of a workspace.
type routerState struct {
	Known    bool // the plugin has an entry in the workspace config
	Enabled  bool
	Routes   []router.Route
	Conflict string // why it cannot be enabled now; "" when it can
}

// The page reaches the store, the config and the vault through these,
// replaced in tests.
var (
	// loadSubagentRows lists the host's subagents and the stored ones.
	loadSubagentRows = func(h *subagents.Harness, workspace string) ([]subagentRow, error) {
		if !h.HasSubagents() || !subagents.ValidWorkspaceName(workspace) {
			return nil, nil
		}
		stored, err := h.ListStore(workspace)
		if err != nil {
			return nil, err
		}
		var host []subagents.Entry
		if dir, err := h.HostDir(); err == nil {
			if host, err = h.ScanDir(dir, subagents.SourceImported, false); err != nil {
				return nil, err
			}
		}
		return mergeSubagentRows(host, stored), nil
	}

	storeSubagent = func(h *subagents.Harness, workspace string, row subagentRow) error {
		data, err := subagents.ReadFile(row.Path)
		if err != nil {
			return err
		}
		_, err = h.Save(workspace, row.File, data, subagents.SourceImported, row.Path, false)
		return err
	}

	unstoreSubagent = func(h *subagents.Harness, workspace, name string) error {
		_, err := h.Remove(workspace, name)
		return err
	}

	loadClaudeRouter = func(workspace string) routerState {
		ws := config.LoadOrDefault().Workspace(workspace)
		if ws == nil {
			return routerState{Conflict: fmt.Sprintf("workspace '%s' is not in the config yet; finish setting it up first", workspace)}
		}
		entry, known := ws.PluginEntry(subagents.Claude.Agent, clauderouter.Name)
		st := routerState{Known: known, Enabled: entry.Enabled}
		if p := plugins.Get(clauderouter.Name); p != nil {
			st.Routes = clauderouter.Routes(plugins.Settings(ws, subagents.Claude.Agent, p))
		}
		if err := plugins.Conflict(subagents.Claude.Agent, clauderouter.Name, ws.EnabledPlugins(subagents.Claude.Agent)); err != nil {
			st.Conflict = err.Error()
		}
		return st
	}

	// saveClaudeRouter stores the plugin state and routes, and with
	// enableVault turns on the workspace vault the backend keys live in.
	// It returns the stored entry.
	saveClaudeRouter = func(workspace string, enabled bool, routes []router.Route, enableVault bool) (config.PluginEntry, error) {
		cfg := config.LoadOrDefault()
		ws := cfg.Workspace(workspace)
		if ws == nil {
			return config.PluginEntry{}, fmt.Errorf("workspace '%s' is not in the config", workspace)
		}
		p := plugins.Get(clauderouter.Name)
		if p == nil {
			return config.PluginEntry{}, errors.New("this ExitBox has no claude-router plugin")
		}
		if enabled {
			if err := plugins.Conflict(subagents.Claude.Agent, p.Name, ws.EnabledPlugins(subagents.Claude.Agent)); err != nil {
				return config.PluginEntry{}, err
			}
		}
		entry, known := ws.PluginEntry(subagents.Claude.Agent, p.Name)
		settings := map[string]string{}
		if !known {
			for k, v := range p.DefaultSettings() {
				settings[k] = v
			}
		}
		for k, v := range entry.Settings {
			settings[k] = v
		}
		if len(routes) > 0 {
			settings["routes"] = joinRoutes(routes)
		} else {
			delete(settings, "routes")
		}
		entry.Enabled, entry.Settings = enabled, settings
		ws.SetPluginEntry(subagents.Claude.Agent, p.Name, entry)
		if enableVault {
			ws.Vault.Enabled = true
		}
		if err := config.SaveConfig(cfg); err != nil {
			return config.PluginEntry{}, err
		}
		return entry, nil
	}

	vaultExists = vault.IsInitialized
	vaultCreate = vault.Init
	vaultStore  = vault.QuickSet
)

// mergeSubagentRows joins the host's files and the store by subagent name:
// host files first, in the order the agent finds them, then what is only
// in the store.
func mergeSubagentRows(host, stored []subagents.Entry) []subagentRow {
	byName := map[string]subagents.Entry{}
	for _, e := range stored {
		if e.Name() != "" {
			byName[e.Name()] = e
		}
	}
	seen := map[string]bool{}
	var rows []subagentRow
	for _, e := range host {
		name := e.Name()
		if name == "" || seen[name] {
			// No name: documentation in the agents directory, not a subagent.
			continue
		}
		seen[name] = true
		row := subagentRow{Name: name, File: e.File, Path: e.Path}
		if e.Def != nil {
			row.Model = e.Def.Model
		}
		if subagents.HasErrors(e.Findings) {
			row.Problem = firstError(e.Findings)
		}
		if s, ok := byName[name]; ok {
			row.Stored, row.Source = true, s.Source
			if s.Def != nil {
				row.Model = s.Def.Model
			}
		}
		rows = append(rows, row)
	}
	for _, e := range stored {
		name := e.Name()
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		row := subagentRow{Name: name, File: e.File, Stored: true, Source: e.Source}
		if e.Def != nil {
			row.Model = e.Def.Model
		}
		rows = append(rows, row)
	}
	return rows
}

func joinRoutes(routes []router.Route) string {
	parts := make([]string, 0, len(routes))
	for _, r := range routes {
		parts = append(parts, r.String())
	}
	return strings.Join(parts, ",")
}

// routeVaultKey is the vault key a model's backend key is stored under,
// e.g. CLAUDE_ROUTER_QWEN3_6_27B_KEY.
func routeVaultKey(model string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(model) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return "CLAUDE_ROUTER_" + strings.Trim(b.String(), "_") + "_KEY"
}

// needsRoute reports whether Claude Code can only reach a model through
// claude-router.
func needsRoute(model string) bool {
	return subagents.ClassifyModel(model) == subagents.ModelOther
}
