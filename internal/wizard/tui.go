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

// Step identifies the current wizard step.
type Step int

const (
	stepWelcome Step = iota
	stepWorkspaceSelect
	stepRole
	stepLanguage
	stepTools
	stepProfile
	stepAgents
	stepSettings
	stepReview
	stepDone
)

// State holds accumulated user selections across wizard steps.
type State struct {
	Roles               []string
	Languages           []string
	ToolCategories      []string
	WorkspaceName       string
	MakeDefault         bool
	Agents              []string
	AutoUpdate          bool
	StatusBar           bool
	EnableFirewall      bool
	AutoResume          bool
	PassEnv             bool
	ReadOnly            bool
	OriginalDevelopment []string // non-nil when editing an existing workspace
}

// Model is the root bubbletea model for the wizard.
type Model struct {
	step           Step
	state          State
	cursor         int
	checked        map[string]bool
	workspaceInput string
	workspaceOnly  bool
	workspaces       []config.Workspace // populated when >1 workspace exists
	defaultWorkspace string             // the config's default workspace name
	editingExisting  bool               // true when editing an existing workspace (skip role→lang override)
	width          int
	height         int
	cancelled      bool
	confirmed      bool
}

// NewModel creates a new wizard model with defaults.
func NewModel() Model {
	checked := make(map[string]bool)
	// Default settings to on
	checked["setting:auto_update"] = true
	checked["setting:status_bar"] = true
	checked["setting:make_default"] = true
	checked["setting:firewall"] = true
	checked["setting:auto_resume"] = true
	checked["setting:pass_env"] = true
	checked["setting:read_only"] = false
	return Model{
		step:           stepWelcome,
		checked:        checked,
		workspaceInput: "default",
	}
}

// NewModelFromConfig creates a wizard model pre-populated from existing config.
func NewModelFromConfig(cfg *config.Config) Model {
	checked := make(map[string]bool)

	// Pre-check roles
	for _, r := range cfg.Roles {
		checked["role:"+r] = true
	}

	// Pre-check agents
	if cfg.Agents.Claude.Enabled {
		checked["agent:claude"] = true
	}
	if cfg.Agents.Codex.Enabled {
		checked["agent:codex"] = true
	}
	if cfg.Agents.OpenCode.Enabled {
		checked["agent:opencode"] = true
	}

	// Pre-check tool categories from saved selections (or fall back to role inference)
	if len(cfg.ToolCategories) > 0 {
		for _, tc := range cfg.ToolCategories {
			checked["tool:"+tc] = true
		}
	} else {
		for _, roleName := range cfg.Roles {
			if role := GetRole(roleName); role != nil {
				for _, t := range role.ToolCategories {
					checked["tool:"+t] = true
				}
			}
		}
	}

	// Pre-check languages from saved workspaces (or fall back to role inference)
	activeWorkspaceName := cfg.Workspaces.Active
	if activeWorkspaceName == "" && len(cfg.Workspaces.Items) > 0 {
		activeWorkspaceName = cfg.Workspaces.Items[0].Name
	}
	if activeWorkspaceName != "" {
		profileSet := make(map[string]bool)
		for _, w := range cfg.Workspaces.Items {
			if w.Name == activeWorkspaceName {
				for _, p := range w.Development {
					profileSet[p] = true
				}
				break
			}
		}
		for _, l := range AllLanguages {
			if profileSet[l.Profile] {
				checked["lang:"+l.Name] = true
			}
		}
	} else {
		for _, roleName := range cfg.Roles {
			if role := GetRole(roleName); role != nil {
				for _, l := range role.Languages {
					checked["lang:"+l] = true
				}
			}
		}
	}

	// Settings
	checked["setting:auto_update"] = cfg.Settings.AutoUpdate
	checked["setting:status_bar"] = cfg.Settings.StatusBar
	checked["setting:make_default"] = cfg.Settings.DefaultWorkspace == activeWorkspaceNameOrDefault(activeWorkspaceName)
	checked["setting:firewall"] = !cfg.Settings.DefaultFlags.NoFirewall
	checked["setting:auto_resume"] = cfg.Settings.DefaultFlags.AutoResume
	checked["setting:pass_env"] = !cfg.Settings.DefaultFlags.NoEnv
	checked["setting:read_only"] = cfg.Settings.DefaultFlags.ReadOnly

	startStep := stepWelcome
	var workspaces []config.Workspace
	activeCursor := 0
	if len(cfg.Workspaces.Items) > 1 {
		startStep = stepWorkspaceSelect
		workspaces = cfg.Workspaces.Items
		for i, w := range workspaces {
			if w.Name == activeWorkspaceNameOrDefault(activeWorkspaceName) {
				activeCursor = i
				break
			}
		}
	}

	return Model{
		step:             startStep,
		cursor:           activeCursor,
		checked:          checked,
		workspaceInput:   activeWorkspaceNameOrDefault(activeWorkspaceName),
		workspaces:       workspaces,
		defaultWorkspace: cfg.Settings.DefaultWorkspace,
	}
}

// NewWorkspaceModelFromConfig creates a blank wizard model for creating one workspace.
// It intentionally does not inherit role/language/settings selections.
func NewWorkspaceModelFromConfig(_ *config.Config, workspaceName string) Model {
	m := NewModel()
	m.workspaceOnly = true
	m.state.MakeDefault = false
	m.checked["setting:make_default"] = false
	m.checked["setting:auto_update"] = false
	m.checked["setting:status_bar"] = false
	m.state.Roles = nil
	m.state.Languages = nil
	m.state.ToolCategories = nil
	m.state.Agents = nil
	m.state.AutoUpdate = false
	m.state.StatusBar = false
	if strings.TrimSpace(workspaceName) != "" {
		m.workspaceInput = strings.TrimSpace(workspaceName)
		m.state.WorkspaceName = m.workspaceInput
	} else {
		m.workspaceInput = ""
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "q":
			if m.step == stepWelcome || m.step == stepWorkspaceSelect {
				m.cancelled = true
				return m, tea.Quit
			}
		}
	}

	switch m.step {
	case stepWelcome:
		return m.updateWelcome(msg)
	case stepWorkspaceSelect:
		return m.updateWorkspaceSelect(msg)
	case stepRole:
		return m.updateRole(msg)
	case stepLanguage:
		return m.updateLanguage(msg)
	case stepTools:
		return m.updateTools(msg)
	case stepProfile:
		return m.updateProfile(msg)
	case stepAgents:
		return m.updateAgents(msg)
	case stepSettings:
		return m.updateSettings(msg)
	case stepReview:
		return m.updateReview(msg)
	}

	return m, nil
}

func (m Model) View() string {
	switch m.step {
	case stepWelcome:
		return m.viewWelcome()
	case stepWorkspaceSelect:
		return m.viewWorkspaceSelect()
	case stepRole:
		return m.viewRole()
	case stepLanguage:
		return m.viewLanguage()
	case stepTools:
		return m.viewTools()
	case stepProfile:
		return m.viewProfile()
	case stepAgents:
		return m.viewAgents()
	case stepSettings:
		return m.viewSettings()
	case stepReview:
		return m.viewReview()
	case stepDone:
		return ""
	}
	return ""
}

// Cancelled returns true if the user cancelled the wizard.
func (m Model) Cancelled() bool { return m.cancelled }

// Confirmed returns true if the user confirmed their selections.
func (m Model) Confirmed() bool { return m.confirmed }

// Result returns the final wizard state.
func (m Model) Result() State { return m.state }

// wrapWords joins words with ", " and wraps to maxWidth, indenting
// continuation lines with the given indent string.
func wrapWords(words []string, indent string, maxWidth int) string {
	if maxWidth <= 0 {
		maxWidth = 80
	}
	if len(words) == 0 {
		return ""
	}

	var b strings.Builder
	lineLen := len(indent)
	b.WriteString(indent)

	for i, w := range words {
		seg := w
		if i < len(words)-1 {
			seg += ","
		}
		// +1 for the space before the word (except first on line)
		needed := len(seg)
		if lineLen > len(indent) {
			needed++ // space separator
		}

		if lineLen+needed > maxWidth && lineLen > len(indent) {
			b.WriteString("\n")
			b.WriteString(indent)
			lineLen = len(indent)
		}

		if lineLen > len(indent) {
			b.WriteString(" ")
			lineLen++
		}
		b.WriteString(seg)
		lineLen += len(seg)
	}
	return b.String()
}

func activeWorkspaceNameOrDefault(name string) string {
	if strings.TrimSpace(name) == "" {
		return "default"
	}
	return name
}

// --- Welcome Step ---

func (m Model) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.String() == "enter" {
			m.step = stepRole
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewWelcome() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(logo))
	b.WriteString("\n\n")
	b.WriteString(titleStyle.Render("Welcome to ExitBox Setup"))
	b.WriteString("\n\n")
	b.WriteString("This wizard will help you configure your development environment.\n")
	b.WriteString("You'll choose your role, languages, tools, and agents.\n\n")
	b.WriteString(helpStyle.Render("Press Enter to start, q to quit"))
	return b.String()
}

const logo = `  _____      _ _   ____
 | ____|_  _(_) |_| __ )  _____  __
 |  _| \ \/ / | __|  _ \ / _ \ \/ /
 | |___ >  <| | |_| |_) | (_) >  <
 |_____/_/\_\_|\__|____/ \___/_/\_\`

// --- Workspace Select Step (single-select) ---

func (m Model) updateWorkspaceSelect(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Items: one per workspace + "Create new workspace" at the end
	itemCount := len(m.workspaces) + 1

	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < itemCount-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor < len(m.workspaces) {
				// Selected an existing workspace — re-populate from it
				ws := m.workspaces[m.cursor]
				m.workspaceInput = ws.Name
				m.state.WorkspaceName = ws.Name
				m.editingExisting = true
				// Preserve original development stack for delta-based updates
				m.state.OriginalDevelopment = make([]string, len(ws.Development))
				copy(m.state.OriginalDevelopment, ws.Development)

				devSet := make(map[string]bool)
				for _, p := range ws.Development {
					devSet[p] = true
				}

				// Re-check roles: only check roles whose profiles all exist
				// in this workspace's development stack.
				for _, role := range Roles {
					match := len(role.Profiles) > 0
					for _, p := range role.Profiles {
						if !devSet[p] {
							match = false
							break
						}
					}
					m.checked["role:"+role.Name] = match
				}

				// Re-populate language checks from this workspace's dev stack
				for _, l := range AllLanguages {
					m.checked["lang:"+l.Name] = devSet[l.Profile]
				}

				// Re-check tool categories: only check categories whose
				// packages overlap with the workspace's tool set.
				// (Tools are global, but we infer from development stack context.)
				for _, tc := range AllToolCategories {
					match := false
					for _, role := range Roles {
						if m.checked["role:"+role.Name] {
							for _, rt := range role.ToolCategories {
								if rt == tc.Name {
									match = true
									break
								}
							}
						}
						if match {
							break
						}
					}
					m.checked["tool:"+tc.Name] = match
				}

				// Only check "make default" if this workspace is already the default.
				m.checked["setting:make_default"] = ws.Name == m.defaultWorkspace
			} else {
				// "Create new workspace" — start with a clean slate
				m.workspaceInput = ""
				m.state.WorkspaceName = ""
				m.editingExisting = false
				m.state.OriginalDevelopment = nil

				// Clear all selections
				for _, role := range Roles {
					m.checked["role:"+role.Name] = false
				}
				for _, l := range AllLanguages {
					m.checked["lang:"+l.Name] = false
				}
				for _, tc := range AllToolCategories {
					m.checked["tool:"+tc.Name] = false
				}
				for _, a := range AllAgents {
					m.checked["agent:"+a.Name] = false
				}

				// Reset settings to defaults
				m.checked["setting:auto_update"] = true
				m.checked["setting:status_bar"] = true
				m.checked["setting:make_default"] = false
				m.checked["setting:firewall"] = true
				m.checked["setting:auto_resume"] = true
				m.checked["setting:pass_env"] = true
				m.checked["setting:read_only"] = false
			}
			m.step = stepRole
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewWorkspaceSelect() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(logo))
	b.WriteString("\n\n")
	b.WriteString(titleStyle.Render("ExitBox Setup — Select Workspace"))
	b.WriteString("\n\n")
	b.WriteString("Which workspace do you want to configure?\n\n")

	for i, ws := range m.workspaces {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		paddedName := fmt.Sprintf("%-20s", ws.Name)
		desc := ""
		if len(ws.Development) > 0 {
			desc = strings.Join(ws.Development, ", ")
		} else {
			desc = "no development stack"
		}
		if m.cursor == i {
			paddedName = selectedStyle.Render(paddedName)
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, paddedName, dimStyle.Render(desc)))
	}

	// "Create new workspace" option
	cursor := "  "
	createIdx := len(m.workspaces)
	if m.cursor == createIdx {
		cursor = cursorStyle.Render("> ")
	}
	label := "+ Create new workspace"
	if m.cursor == createIdx {
		label = selectedStyle.Render(label)
	}
	b.WriteString(fmt.Sprintf("\n%s%s\n", cursor, label))

	b.WriteString(helpStyle.Render("\nEnter to select, q to quit"))
	return b.String()
}

// --- Role Step (multi-select) ---

func (m Model) updateRole(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(Roles)-1 {
				m.cursor++
			}
		case " ", "x":
			k := "role:" + Roles[m.cursor].Name
			m.checked[k] = !m.checked[k]
		case "enter":
			// Require at least one role selected
			hasRole := false
			for _, role := range Roles {
				if m.checked["role:"+role.Name] {
					hasRole = true
					break
				}
			}
			if !hasRole {
				return m, nil
			}
			m.state.Roles = nil
			// Pre-check languages and tools from all selected roles.
			// When editing an existing workspace, skip language overrides
			// so the workspace's development stack is preserved.
			for _, role := range Roles {
				if m.checked["role:"+role.Name] {
					m.state.Roles = append(m.state.Roles, role.Name)
					if !m.editingExisting {
						for _, l := range role.Languages {
							m.checked["lang:"+l] = true
						}
					}
					for _, t := range role.ToolCategories {
						m.checked["tool:"+t] = true
					}
				}
			}
			m.step = stepLanguage
			m.cursor = 0
		case "esc":
			if len(m.workspaces) > 1 {
				m.step = stepWorkspaceSelect
			} else {
				m.step = stepWelcome
			}
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewRole() string {
	var b strings.Builder
	if m.workspaceOnly {
		b.WriteString(titleStyle.Render("Step 1/3 — What kind of developer are you?"))
	} else {
		b.WriteString(titleStyle.Render("Step 1/7 — What kind of developer are you?"))
	}
	b.WriteString("\n")
	if m.editingExisting {
		b.WriteString(subtitleStyle.Render(fmt.Sprintf("Workspace: %s — Select all that apply. Space to toggle.\n", m.workspaceInput)))
	} else {
		b.WriteString(subtitleStyle.Render("Select all that apply. Space to toggle.\n"))
	}
	b.WriteString("\n")

	for i, role := range Roles {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		check := "[ ]"
		if m.checked["role:"+role.Name] {
			check = selectedStyle.Render("[x]")
		}
		// Pad name to fixed width before styling to prevent layout shift
		paddedName := fmt.Sprintf("%-15s", role.Name)
		if m.cursor == i {
			paddedName = selectedStyle.Render(paddedName)
		}
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, check, paddedName, dimStyle.Render(role.Description)))
	}

	hasRole := false
	for _, role := range Roles {
		if m.checked["role:"+role.Name] {
			hasRole = true
			break
		}
	}
	if hasRole {
		b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back"))
	} else {
		b.WriteString(helpStyle.Render("\nSpace to toggle, Esc to go back") + "  " + dimStyle.Render("(select at least one role)"))
	}
	return b.String()
}

// --- Language Step (multi-select) ---

func (m Model) updateLanguage(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(AllLanguages)-1 {
				m.cursor++
			}
		case " ", "x":
			k := "lang:" + AllLanguages[m.cursor].Name
			m.checked[k] = !m.checked[k]
		case "enter":
			m.state.Languages = nil
			for _, l := range AllLanguages {
				if m.checked["lang:"+l.Name] {
					m.state.Languages = append(m.state.Languages, l.Name)
				}
			}
			if m.workspaceOnly {
				m.step = stepReview
			} else {
				m.step = stepTools
			}
			m.cursor = 0
		case "esc":
			m.step = stepRole
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewLanguage() string {
	var b strings.Builder
	if m.workspaceOnly {
		b.WriteString(titleStyle.Render("Step 2/3 — Which languages do you use?"))
	} else {
		b.WriteString(titleStyle.Render("Step 2/7 — Which languages do you use?"))
	}
	b.WriteString("\n")
	if m.editingExisting {
		b.WriteString(subtitleStyle.Render(fmt.Sprintf("Workspace: %s — These become the development stack. Space to toggle.\n", m.workspaceInput)))
	} else {
		b.WriteString(subtitleStyle.Render("These become the development stack for your workspace. Space to toggle.\n"))
	}
	b.WriteString("\n")

	for i, lang := range AllLanguages {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		check := "[ ]"
		if m.checked["lang:"+lang.Name] {
			check = selectedStyle.Render("[x]")
		}
		paddedName := fmt.Sprintf("%-15s", lang.Name)
		if m.cursor == i {
			paddedName = selectedStyle.Render(paddedName)
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, check, paddedName))
	}

	b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back"))
	return b.String()
}

// --- Tools Step (multi-select) ---

func (m Model) updateTools(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(AllToolCategories)-1 {
				m.cursor++
			}
		case " ", "x":
			k := "tool:" + AllToolCategories[m.cursor].Name
			m.checked[k] = !m.checked[k]
		case "enter":
			m.state.ToolCategories = nil
			for _, t := range AllToolCategories {
				if m.checked["tool:"+t.Name] {
					m.state.ToolCategories = append(m.state.ToolCategories, t.Name)
				}
			}
			m.step = stepProfile
			m.cursor = 0
		case "esc":
			m.step = stepLanguage
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewTools() string {
	var b strings.Builder
	if m.workspaceOnly {
		b.WriteString(titleStyle.Render("Step 3/3 — Which tool categories do you need?"))
	} else {
		b.WriteString(titleStyle.Render("Step 3/7 — Which tool categories do you need?"))
	}
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Pre-selected based on your role. Space to toggle.\n"))
	b.WriteString("\n")

	for i, cat := range AllToolCategories {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		check := "[ ]"
		if m.checked["tool:"+cat.Name] {
			check = selectedStyle.Render("[x]")
		}
		paddedName := fmt.Sprintf("%-15s", cat.Name)
		if m.cursor == i {
			paddedName = selectedStyle.Render(paddedName)
		}
		// prefix: cursor(2) + check(3) + space(1) + name(15) + space(1) = 22 chars
		wrapped := wrapWords(cat.Packages, "                      ", m.width)
		// First line starts after the padded name, so trim leading indent
		firstLine := strings.TrimLeft(wrapped, " ")
		pkgs := dimStyle.Render(firstLine)
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, check, paddedName, pkgs))
	}

	b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back"))
	return b.String()
}

// --- Workspace Step ---

func (m Model) updateProfile(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			name := strings.TrimSpace(m.workspaceInput)
			if name == "" {
				return m, nil
			}
			m.workspaceInput = name
			m.state.WorkspaceName = name
			if m.workspaceOnly {
				m.state.MakeDefault = false
			}
			m.step = stepAgents
			m.cursor = 0
		case "esc":
			m.step = stepTools
			m.cursor = 0
		case "backspace", "ctrl+h":
			if len(m.workspaceInput) > 0 {
				m.workspaceInput = m.workspaceInput[:len(m.workspaceInput)-1]
			}
		default:
			s := key.String()
			if len(s) == 1 {
				c := s[0]
				isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
				if isAlphaNum || c == '-' || c == '_' {
					m.workspaceInput += s
				}
			}
		}
	}
	return m, nil
}

func (m Model) viewProfile() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Step 4/7 — Name your workspace"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("This workspace stores development stacks and separate agent configs.\n"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  Workspace name: %s\n", selectedStyle.Render(m.workspaceInput+"█")))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Examples: personal, work, client-a"))
	b.WriteString("\n")
	if strings.TrimSpace(m.workspaceInput) == "" {
		b.WriteString(helpStyle.Render("\nType a name, Esc to go back") + "  " + dimStyle.Render("(name required)"))
	} else {
		b.WriteString(helpStyle.Render("\nType to edit, Enter to confirm, Esc to go back"))
	}
	return b.String()
}

// --- Agents Step (multi-select) ---

func (m Model) updateAgents(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(AllAgents)-1 {
				m.cursor++
			}
		case " ", "x":
			k := "agent:" + AllAgents[m.cursor].Name
			m.checked[k] = !m.checked[k]
		case "enter":
			// Require at least one agent selected
			hasAgent := false
			for _, a := range AllAgents {
				if m.checked["agent:"+a.Name] {
					hasAgent = true
					break
				}
			}
			if !hasAgent {
				return m, nil
			}
			m.state.Agents = nil
			for _, a := range AllAgents {
				if m.checked["agent:"+a.Name] {
					m.state.Agents = append(m.state.Agents, a.Name)
				}
			}
			m.step = stepSettings
			m.cursor = 0
		case "esc":
			m.step = stepProfile
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewAgents() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Step 5/7 — Which agents do you want to enable?"))
	b.WriteString("\n\n")

	for i, agent := range AllAgents {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		check := "[ ]"
		if m.checked["agent:"+agent.Name] {
			check = selectedStyle.Render("[x]")
		}
		paddedName := fmt.Sprintf("%-18s", agent.DisplayName)
		if m.cursor == i {
			paddedName = selectedStyle.Render(paddedName)
		}
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, check, paddedName, dimStyle.Render(agent.Description)))
	}

	hasAgent := false
	for _, a := range AllAgents {
		if m.checked["agent:"+a.Name] {
			hasAgent = true
			break
		}
	}
	if hasAgent {
		b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back"))
	} else {
		b.WriteString(helpStyle.Render("\nSpace to toggle, Esc to go back") + "  " + dimStyle.Render("(select at least one agent)"))
	}
	return b.String()
}

// --- Settings Step ---

var settingsOptions = []struct {
	Key         string
	Label       string
	Description string
}{
	{"setting:auto_update", "Auto-update agents", "Check for new versions on every launch (slows down startup)"},
	{"setting:status_bar", "Status bar", "Show a status bar with version and agent info during sessions"},
	{"setting:make_default", "Make workspace default", "Use this workspace by default in new sessions"},
	{"setting:firewall", "Network firewall", "Restrict outbound network to allowlisted domains only"},
	{"setting:auto_resume", "Auto-resume sessions", "Automatically resume the last agent conversation"},
	{"setting:pass_env", "Pass host environment", "Forward host environment variables into the container"},
	{"setting:read_only", "Read-only workspace", "Mount workspace as read-only (agents cannot modify files)"},
}

func (m Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(settingsOptions)-1 {
				m.cursor++
			}
		case " ", "x":
			k := settingsOptions[m.cursor].Key
			m.checked[k] = !m.checked[k]
		case "enter":
			m.state.AutoUpdate = m.checked["setting:auto_update"]
			m.state.StatusBar = m.checked["setting:status_bar"]
			m.state.MakeDefault = m.checked["setting:make_default"]
			m.state.EnableFirewall = m.checked["setting:firewall"]
			m.state.AutoResume = m.checked["setting:auto_resume"]
			m.state.PassEnv = m.checked["setting:pass_env"]
			m.state.ReadOnly = m.checked["setting:read_only"]
			m.step = stepReview
			m.cursor = 0
		case "esc":
			m.step = stepAgents
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewSettings() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Step 6/7 — Settings"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Space to toggle. Use 'exitbox rebuild <agent>' to update manually.\n"))
	b.WriteString("\n")

	for i, opt := range settingsOptions {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		check := "[ ]"
		if m.checked[opt.Key] {
			check = selectedStyle.Render("[x]")
		}
		paddedLabel := fmt.Sprintf("%-25s", opt.Label)
		if m.cursor == i {
			paddedLabel = selectedStyle.Render(paddedLabel)
		}
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, check, paddedLabel, dimStyle.Render(opt.Description)))
	}

	b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back"))
	return b.String()
}

// --- Review Step ---

func (m Model) updateReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter", "y":
			m.confirmed = true
			m.step = stepDone
			return m, tea.Quit
		case "d":
			m.state.MakeDefault = !m.state.MakeDefault
		case "esc":
			if m.workspaceOnly {
				m.step = stepLanguage
			} else {
				m.step = stepSettings
			}
			m.cursor = 0
		case "q", "n":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) viewReview() string {
	if m.workspaceOnly {
		return m.viewWorkspaceOnlyReview()
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Step 7/7 — Review your configuration"))
	b.WriteString("\n\n")

	if len(m.state.Roles) > 0 {
		b.WriteString(fmt.Sprintf("  Roles:      %s\n", successStyle.Render(strings.Join(m.state.Roles, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Roles:      %s\n", dimStyle.Render("none")))
	}

	if len(m.state.Languages) > 0 {
		b.WriteString(fmt.Sprintf("  Languages:  %s\n", selectedStyle.Render(strings.Join(m.state.Languages, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Languages:  %s\n", dimStyle.Render("none")))
	}

	if len(m.state.ToolCategories) > 0 {
		b.WriteString(fmt.Sprintf("  Tools:      %s\n", selectedStyle.Render(strings.Join(m.state.ToolCategories, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Tools:      %s\n", dimStyle.Render("none")))
	}

	b.WriteString(fmt.Sprintf("  Workspace:  %s\n", selectedStyle.Render(activeWorkspaceNameOrDefault(m.state.WorkspaceName))))

	if len(m.state.Agents) > 0 {
		names := make([]string, len(m.state.Agents))
		for i, a := range m.state.Agents {
			for _, opt := range AllAgents {
				if opt.Name == a {
					names[i] = opt.DisplayName
					break
				}
			}
		}
		b.WriteString(fmt.Sprintf("  Agents:     %s\n", selectedStyle.Render(strings.Join(names, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Agents:     %s\n", dimStyle.Render("none")))
	}

	autoUpdateStr := successStyle.Render("yes")
	if !m.state.AutoUpdate {
		autoUpdateStr = dimStyle.Render("no")
	}
	statusBarStr := successStyle.Render("yes")
	if !m.state.StatusBar {
		statusBarStr = dimStyle.Render("no")
	}
	b.WriteString(fmt.Sprintf("  Auto-update:  %s\n", autoUpdateStr))
	b.WriteString(fmt.Sprintf("  Status bar:   %s\n", statusBarStr))
	defaultStr := dimStyle.Render("no")
	if m.state.MakeDefault {
		defaultStr = successStyle.Render("yes")
	}
	b.WriteString(fmt.Sprintf("  Make default: %s\n", defaultStr))
	firewallStr := successStyle.Render("yes")
	if !m.state.EnableFirewall {
		firewallStr = dimStyle.Render("no")
	}
	b.WriteString(fmt.Sprintf("  Firewall:     %s\n", firewallStr))
	autoResumeStr := successStyle.Render("yes")
	if !m.state.AutoResume {
		autoResumeStr = dimStyle.Render("no")
	}
	b.WriteString(fmt.Sprintf("  Auto-resume:  %s\n", autoResumeStr))
	passEnvStr := successStyle.Render("yes")
	if !m.state.PassEnv {
		passEnvStr = dimStyle.Render("no")
	}
	b.WriteString(fmt.Sprintf("  Pass env:     %s\n", passEnvStr))
	readOnlyStr := dimStyle.Render("no")
	if m.state.ReadOnly {
		readOnlyStr = successStyle.Render("yes")
	}
	b.WriteString(fmt.Sprintf("  Read-only:    %s\n", readOnlyStr))

	var profiles []string
	if m.state.OriginalDevelopment != nil {
		profiles = applyLanguageDelta(m.state.OriginalDevelopment, m.state.Languages)
	} else {
		profiles = ComputeProfiles(m.state.Roles, m.state.Languages)
	}
	if len(profiles) > 0 {
		b.WriteString(fmt.Sprintf("\n  Development stack: %s\n", selectedStyle.Render(strings.Join(profiles, ", "))))
		b.WriteString(dimStyle.Render("  (saved inside the workspace)"))
		b.WriteString("\n")
	}

	packages := ComputePackages(m.state.ToolCategories)
	if len(packages) > 0 {
		// "  Packages:   " = 14 chars indent
		wrapped := wrapWords(packages, "              ", m.width)
		firstLine := strings.TrimLeft(wrapped, " ")
		b.WriteString(fmt.Sprintf("  Packages:   %s\n", dimStyle.Render(firstLine)))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Enter to confirm, d to toggle default, Esc to go back, q to cancel"))
	return b.String()
}

func (m Model) viewWorkspaceOnlyReview() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("Step 3/3 — Review new workspace"))
	b.WriteString("\n\n")

	name := strings.TrimSpace(m.state.WorkspaceName)
	if name == "" {
		name = strings.TrimSpace(m.workspaceInput)
	}
	if name == "" {
		name = "default"
	}
	b.WriteString(fmt.Sprintf("  Workspace:  %s\n", selectedStyle.Render(name)))

	if len(m.state.Roles) > 0 {
		b.WriteString(fmt.Sprintf("  Roles:      %s\n", successStyle.Render(strings.Join(m.state.Roles, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Roles:      %s\n", dimStyle.Render("none")))
	}

	if len(m.state.Languages) > 0 {
		b.WriteString(fmt.Sprintf("  Languages:  %s\n", selectedStyle.Render(strings.Join(m.state.Languages, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Languages:  %s\n", dimStyle.Render("none")))
	}

	dev := ComputeProfiles(m.state.Roles, m.state.Languages)
	if len(dev) > 0 {
		b.WriteString(fmt.Sprintf("  Development: %s\n", selectedStyle.Render(strings.Join(dev, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Development: %s\n", dimStyle.Render("none")))
	}
	defaultStr := dimStyle.Render("no")
	if m.state.MakeDefault {
		defaultStr = successStyle.Render("yes")
	}
	b.WriteString(fmt.Sprintf("  Make default: %s\n", defaultStr))

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Enter to create workspace, d to toggle default, Esc to go back, q to cancel"))
	return b.String()
}
