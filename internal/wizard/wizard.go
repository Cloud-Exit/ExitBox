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
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
)

// SetupResult holds post-wizard actions for the caller to execute.
type SetupResult struct {
	WorkspaceName string   // the workspace that was created/edited
	CopyFrom      string   // workspace to copy credentials from ("__host__" = import from host, "" = skip)
	IsDefault     bool     // true if this workspace is the default
	Agents        []string // enabled agent names (e.g. ["claude", "codex"])
	VaultEnabled  bool     // enable encrypted vault for secrets
	VaultPassword string   // vault encryption password (non-empty when VaultEnabled)
}

// Run executes the setup wizard TUI and writes config files on completion.
// If existingCfg is non-nil, the wizard is pre-populated from it.
func Run(existingCfg *config.Config) (*SetupResult, error) {
	var model Model
	if existingCfg != nil {
		model = NewModelFromConfig(existingCfg)
	} else {
		model = NewModel()
	}
	p := tea.NewProgram(model, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("wizard error: %w", err)
	}

	wm := finalModel.(Model)
	if wm.Cancelled() {
		return nil, fmt.Errorf("setup cancelled")
	}
	if !wm.Confirmed() {
		return nil, fmt.Errorf("setup cancelled")
	}

	if err := applyResult(wm.Result(), existingCfg); err != nil {
		return nil, err
	}

	result := wm.Result()
	wsName := activeWorkspaceNameOrDefault(result.WorkspaceName)
	isDefault := result.MakeDefault || strings.EqualFold(result.DefaultWorkspace, wsName)

	return &SetupResult{
		WorkspaceName: wsName,
		CopyFrom:      result.CopyFrom,
		IsDefault:     isDefault,
		Agents:        result.Agents,
		VaultEnabled:  result.VaultEnabled,
		VaultPassword: result.VaultPassword,
	}, nil
}

// WorkspaceCreationResult holds the result of the workspace creation wizard.
type WorkspaceCreationResult struct {
	Workspace     *config.Workspace
	MakeDefault   bool
	CopyFrom      string // workspace to copy credentials from (empty = none)
	VaultEnabled  bool   // enable encrypted vault for secrets
	VaultPassword string // vault encryption password (non-empty when VaultEnabled)
}

// RunWorkspaceCreation runs the wizard from step 1 and returns a configured workspace.
func RunWorkspaceCreation(existingCfg *config.Config, workspaceName string) (*WorkspaceCreationResult, error) {
	model := NewWorkspaceModelFromConfig(existingCfg, workspaceName)

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("wizard error: %w", err)
	}

	wm := finalModel.(Model)
	if wm.Cancelled() || !wm.Confirmed() {
		return nil, fmt.Errorf("setup cancelled")
	}

	name := strings.TrimSpace(wm.Result().WorkspaceName)
	if name == "" {
		name = strings.TrimSpace(workspaceName)
	}
	if name == "" {
		name = "default"
	}

	return &WorkspaceCreationResult{
		Workspace: &config.Workspace{
			Name:        name,
			Development: ComputeProfiles(wm.Result().Roles, wm.Result().Languages),
			Packages:    wm.Result().CustomPackages,
			Vault:       config.VaultConfig{Enabled: wm.Result().VaultEnabled},
		},
		MakeDefault:   wm.Result().MakeDefault,
		CopyFrom:      wm.Result().CopyFrom,
		VaultEnabled:  wm.Result().VaultEnabled,
		VaultPassword: wm.Result().VaultPassword,
	}, nil
}

func applyResult(state State, existingCfg *config.Config) error {
	cfg := config.DefaultConfig()
	if existingCfg != nil {
		copyCfg := *existingCfg
		cfg = &copyCfg
	}

	workspaceName := activeWorkspaceNameOrDefault(state.WorkspaceName)
	var development []string
	if state.OriginalDevelopment != nil {
		// Editing an existing workspace: preserve the original development
		// stack and apply language selection changes on top.
		development = applyLanguageDelta(state.OriginalDevelopment, state.Languages)
	} else {
		development = ComputeProfiles(state.Roles, state.Languages)
	}

	cfg.Roles = state.Roles
	cfg.ToolCategories = state.ToolCategories
	cfg.Settings.AutoUpdate = state.AutoUpdate
	cfg.Settings.StatusBar = state.StatusBar
	cfg.Settings.DefaultFlags = config.DefaultFlags{
		NoFirewall: !state.EnableFirewall,
		AutoResume: state.AutoResume,
		NoEnv:      !state.PassEnv,
		ReadOnly:   state.ReadOnly,
	}

	// Persist keybindings — only store non-default values (omitempty in YAML).
	if len(state.Keybindings) > 0 {
		defaults := config.DefaultKeybindings()
		kb := config.KeybindingsConfig{}
		if v := state.Keybindings["workspace_menu"]; v != "" && v != defaults.WorkspaceMenu {
			kb.WorkspaceMenu = v
		}
		if v := state.Keybindings["session_menu"]; v != "" && v != defaults.SessionMenu {
			kb.SessionMenu = v
		}
		cfg.Settings.Keybindings = kb
	}
	cfg.Workspaces.Active = workspaceName
	cfg.Workspaces.Items = upsertWorkspace(cfg.Workspaces.Items, config.Workspace{
		Name:        workspaceName,
		Development: development,
		Packages:    state.CustomPackages,
		Vault:       config.VaultConfig{Enabled: state.VaultEnabled},
	})
	cfg.Settings.DefaultWorkspace = state.DefaultWorkspace

	cfg.Agents = config.AgentConfig{}
	for _, name := range state.Agents {
		cfg.SetAgentEnabled(name, true)
	}

	cfg.Tools.User = ComputePackages(state.ToolCategories)
	cfg.Tools.Binaries = nil
	for _, b := range ComputeBinaries(state.ToolCategories) {
		cfg.Tools.Binaries = append(cfg.Tools.Binaries, config.BinaryConfig{
			Name:       b.Name,
			URLPattern: b.URLPattern,
		})
	}

	if err := config.SaveConfig(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	var al *config.Allowlist
	if len(state.DomainCategories) > 0 {
		al = categoriesToAllowlist(state.DomainCategories)
	} else {
		al = config.LoadAllowlistOrDefault()
	}
	if err := config.SaveAllowlist(al); err != nil {
		return fmt.Errorf("saving allowlist: %w", err)
	}

	config.EnsureDirs()

	return nil
}

// applyLanguageDelta takes an existing development stack and applies language
// selection changes on top. Non-language profiles (e.g. "web", "database",
// "build-tools") are preserved; language profiles are added/removed based on
// the user's current selections.
func applyLanguageDelta(original []string, selectedLanguages []string) []string {
	// Build a set of all known language profiles.
	langProfiles := make(map[string]bool)
	for _, l := range AllLanguages {
		langProfiles[l.Profile] = true
	}

	// Build a set of selected language profiles.
	selectedProfiles := make(map[string]bool)
	for _, langName := range selectedLanguages {
		for _, l := range AllLanguages {
			if l.Name == langName {
				selectedProfiles[l.Profile] = true
				break
			}
		}
	}

	// Start with non-language profiles from the original stack.
	seen := make(map[string]bool)
	var result []string
	for _, p := range original {
		if langProfiles[p] {
			// This is a language profile — only keep if still selected.
			if selectedProfiles[p] && !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		} else {
			// Non-language profile — always preserve.
			if !seen[p] {
				seen[p] = true
				result = append(result, p)
			}
		}
	}

	// Add newly selected language profiles that weren't in the original.
	for _, l := range AllLanguages {
		if selectedProfiles[l.Profile] && !seen[l.Profile] {
			seen[l.Profile] = true
			result = append(result, l.Profile)
		}
	}

	return result
}


func upsertWorkspace(list []config.Workspace, item config.Workspace) []config.Workspace {
	for i := range list {
		if list[i].Name == item.Name {
			list[i] = item
			return list
		}
	}
	return append(list, item)
}
