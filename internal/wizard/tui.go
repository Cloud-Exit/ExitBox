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
	"github.com/charmbracelet/lipgloss"
	"github.com/cloud-exit/exitbox/internal/apk"
	"github.com/cloud-exit/exitbox/internal/config"
)

// Step identifies the current wizard step.
type Step int

const (
	stepWelcome Step = iota
	stepWorkspaceSelect
	stepTopMenu
	stepRole
	stepLanguage
	stepTools
	stepPackages
	stepProfile
	stepAgents
	stepSettings
	stepKeybindings
	stepDomains
	stepCopyCredentials
	stepVault
	stepReview
	stepDone
)

// State holds accumulated user selections across wizard steps.
type State struct {
	Roles               []string
	Languages           []string
	ToolCategories      []string
	CustomPackages      []string // user-selected extra Alpine packages
	WorkspaceName       string
	MakeDefault         bool
	DefaultWorkspace    string // set when d is toggled on workspace select screen
	Agents              []string
	AutoUpdate          bool
	StatusBar           bool
	EnableFirewall      bool
	AutoResume          bool
	PassEnv             bool
	ReadOnly            bool
	OriginalDevelopment []string          // non-nil when editing an existing workspace
	DomainCategories    []domainCategory  // editable allowlist categories
	CopyFrom            string            // workspace to copy credentials from (empty = none)
	VaultEnabled        bool              // enable encrypted vault for secrets
	VaultPassword       string            // vault encryption password (set during wizard init)
	Keybindings         map[string]string // configurable keybindings (e.g. "workspace_menu" -> "C-M-p")
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

	// Package search step fields
	pkgSearchInput string            // current search text
	pkgSearchMode  bool              // true = typing in search, false = browsing results
	pkgResults     []apk.Package     // current search results (max 50)
	pkgSelected    map[string]bool   // selected custom packages (persists across searches)
	pkgIndex       []apk.Package     // full in-memory index (loaded once)
	pkgLoading     bool              // true while fetching index
	pkgLoadErr     string            // error message if fetch failed

	// Sidebar navigation
	sidebarFocused bool
	sidebarCursor  int
	visitedSteps   map[Step]bool
	isFirstRun     bool // true = sidebar read-only

	// Domain step
	domainCategories []domainCategory // 5 allowlist categories
	domainCatCursor  int              // selected category tab (0-4)
	domainItemCursor int              // highlighted domain within category
	domainInputMode  bool             // typing new domain
	domainInput      string           // current input text

	// Top menu step (re-run only)
	topMenuCursor int // 0=workspace management, 1=general settings
	topMenuChoice int // -1=not chosen, 0=workspace, 1=settings

	// Keybindings step
	keybindings   map[string]string // current keybinding values
	kbCursor      int               // highlighted action index
	kbEditMode    bool              // true when editing a binding
	kbEditInput   string            // current text input for tmux notation
	kbEditErr     string            // validation error message

	// Vault step: 0=choice, 1=password, 2=confirm
	vaultPhase    int
	vaultPwInput  string // password being typed
	vaultPwConfirm string // confirmation password
	vaultPwErr    string // mismatch/empty error
}

// stepInfo describes a wizard step for sidebar display.
type stepInfo struct {
	Step  Step
	Label string
	Num   int
}

// sidebarSteps defines the steps shown in the sidebar.
var sidebarSteps = []stepInfo{
	{stepRole, "Roles", 1},
	{stepLanguage, "Languages", 2},
	{stepTools, "Tools", 3},
	{stepPackages, "Packages", 4},
	{stepProfile, "Workspace", 5},
	{stepCopyCredentials, "Credentials", 6},
	{stepAgents, "Agents", 7},
	{stepSettings, "Settings", 8},
	{stepKeybindings, "Keybindings", 9},
	{stepDomains, "Firewall", 10},
	{stepVault, "Vault", 11},
	{stepReview, "Review", 12},
}

const sidebarWidth = 22

// visibleSidebarSteps returns sidebar steps filtered by current state
// (e.g. hides Firewall when disabled, filters by top menu choice on re-run)
// with renumbered step labels.
func (m Model) visibleSidebarSteps() []stepInfo {
	var out []stepInfo
	num := 1
	for _, si := range sidebarSteps {
		if si.Step == stepDomains && !m.firewallEnabled() {
			continue
		}
		if si.Step == stepCopyCredentials && !m.hasCopyCredentialsStep() {
			continue
		}
		// Vault is hidden in general-settings-only re-run.
		if si.Step == stepVault && !m.isFirstRun && m.topMenuChoice == 1 {
			continue
		}
		// On re-run with a top menu choice, filter steps by section.
		if !m.isFirstRun && m.topMenuChoice >= 0 {
			if m.topMenuChoice == 0 {
				// Workspace management: hide Keybindings (Settings stays, it's workspace-bound)
				if si.Step == stepKeybindings {
					continue
				}
			} else if m.topMenuChoice == 1 {
				// General settings: only show Keybindings, Review
				if si.Step != stepKeybindings && si.Step != stepReview {
					continue
				}
			}
		}
		out = append(out, stepInfo{Step: si.Step, Label: si.Label, Num: num})
		num++
	}
	return out
}

// NewModel creates a new wizard model with defaults.
func NewModel() Model {
	checked := make(map[string]bool)
	// Default settings to on
	checked["setting:auto_update"] = true
	checked["setting:status_bar"] = true
	checked["setting:make_default"] = true
	checked["setting:firewall"] = true
	checked["setting:auto_resume"] = false
	checked["setting:pass_env"] = true
	checked["setting:read_only"] = false
	kb := config.DefaultKeybindings()
	return Model{
		step:             stepWelcome,
		checked:          checked,
		workspaceInput:   "default",
		pkgSelected:      make(map[string]bool),
		pkgSearchMode:    true,
		visitedSteps:     make(map[Step]bool),
		isFirstRun:       true,
		domainCategories: allowlistToCategories(config.DefaultAllowlist()),
		topMenuChoice:    -1,
		keybindings: map[string]string{
			"workspace_menu": kb.WorkspaceMenu,
			"session_menu":   kb.SessionMenu,
		},
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

	// On re-run, always start at the top menu so the user can choose
	// between workspace management and general settings.
	startStep := stepTopMenu
	var workspaces []config.Workspace
	activeCursor := 0
	if len(cfg.Workspaces.Items) > 1 {
		workspaces = cfg.Workspaces.Items
		for i, w := range workspaces {
			if w.Name == activeWorkspaceNameOrDefault(activeWorkspaceName) {
				activeCursor = i
				break
			}
		}
	}

	// Pre-populate custom packages from the active workspace.
	// Fall back to global Tools.User for configs that predate per-workspace packages.
	pkgSelected := make(map[string]bool)
	ws := findWorkspace(cfg.Workspaces.Items, activeWorkspaceNameOrDefault(activeWorkspaceName))
	if ws != nil && len(ws.Packages) > 0 {
		for _, p := range ws.Packages {
			pkgSelected[p] = true
		}
	} else {
		categoryPkgs := make(map[string]bool)
		for _, p := range ComputePackages(cfg.ToolCategories) {
			categoryPkgs[p] = true
		}
		for _, p := range cfg.Tools.User {
			if !categoryPkgs[p] {
				pkgSelected[p] = true
			}
		}
	}

	// Start with no steps visited — checkmarks are earned during this session.
	visited := make(map[Step]bool)

	// Keybindings: start from defaults, overlay config values.
	kb := config.DefaultKeybindings()
	if cfg.Settings.Keybindings.WorkspaceMenu != "" {
		kb.WorkspaceMenu = cfg.Settings.Keybindings.WorkspaceMenu
	}
	if cfg.Settings.Keybindings.SessionMenu != "" {
		kb.SessionMenu = cfg.Settings.Keybindings.SessionMenu
	}

	return Model{
		step:             startStep,
		cursor:           activeCursor,
		checked:          checked,
		workspaceInput:   activeWorkspaceNameOrDefault(activeWorkspaceName),
		workspaces:       workspaces,
		defaultWorkspace: cfg.Settings.DefaultWorkspace,
		pkgSelected:      pkgSelected,
		pkgSearchMode:    true,
		visitedSteps:     visited,
		isFirstRun:       false,
		domainCategories: allowlistToCategories(config.LoadAllowlistOrDefault()),
		topMenuChoice:    -1,
		keybindings: map[string]string{
			"workspace_menu": kb.WorkspaceMenu,
			"session_menu":   kb.SessionMenu,
		},
	}
}

// NewWorkspaceModelFromConfig creates a blank wizard model for creating one workspace.
// It intentionally does not inherit role/language/settings selections.
func NewWorkspaceModelFromConfig(cfg *config.Config, workspaceName string) Model {
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
	// Populate existing workspaces for the copy-credentials step.
	if cfg != nil {
		for _, w := range cfg.Workspaces.Items {
			// Exclude the workspace being created.
			if !strings.EqualFold(w.Name, workspaceName) {
				m.workspaces = append(m.workspaces, w)
			}
		}
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

		// Tab toggles sidebar focus (only on re-run, non-workspaceOnly, past welcome screens)
		if msg.String() == "tab" && !m.isFirstRun && !m.workspaceOnly && m.step >= stepRole && m.step <= stepReview && m.step != stepTopMenu {
			m.sidebarFocused = !m.sidebarFocused
			if m.sidebarFocused {
				// Position cursor on current step
				for i, si := range m.visibleSidebarSteps() {
					if si.Step == m.step {
						m.sidebarCursor = i
						break
					}
				}
			}
			return m, nil
		}

		// When sidebar is focused, intercept all keys
		if m.sidebarFocused {
			return m.updateSidebar(msg)
		}
	}

	switch m.step {
	case stepWelcome:
		return m.updateWelcome(msg)
	case stepWorkspaceSelect:
		return m.updateWorkspaceSelect(msg)
	case stepTopMenu:
		return m.updateTopMenu(msg)
	case stepRole:
		return m.updateRole(msg)
	case stepLanguage:
		return m.updateLanguage(msg)
	case stepTools:
		return m.updateTools(msg)
	case stepPackages:
		return m.updatePackages(msg)
	case stepProfile:
		return m.updateProfile(msg)
	case stepAgents:
		return m.updateAgents(msg)
	case stepSettings:
		return m.updateSettings(msg)
	case stepKeybindings:
		return m.updateKeybindings(msg)
	case stepCopyCredentials:
		return m.updateCopyCredentials(msg)
	case stepVault:
		return m.updateVault(msg)
	case stepDomains:
		return m.updateDomains(msg)
	case stepReview:
		return m.updateReview(msg)
	}

	return m, nil
}

func (m Model) View() string {
	var content string
	switch m.step {
	case stepWelcome:
		return m.viewWelcome()
	case stepWorkspaceSelect:
		return m.viewWorkspaceSelect()
	case stepTopMenu:
		return m.viewTopMenu()
	case stepRole:
		content = m.viewRole()
	case stepLanguage:
		content = m.viewLanguage()
	case stepTools:
		content = m.viewTools()
	case stepPackages:
		content = m.viewPackages()
	case stepProfile:
		content = m.viewProfile()
	case stepAgents:
		content = m.viewAgents()
	case stepSettings:
		content = m.viewSettings()
	case stepKeybindings:
		content = m.viewKeybindings()
	case stepCopyCredentials:
		content = m.viewCopyCredentials()
	case stepVault:
		content = m.viewVault()
	case stepDomains:
		content = m.viewDomains()
	case stepReview:
		content = m.viewReview()
	case stepDone:
		return ""
	default:
		return ""
	}

	// Compose with sidebar for non-workspaceOnly flow
	if !m.workspaceOnly {
		sidebar := m.renderSidebar()
		return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " "+content)
	}
	return content
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

func findWorkspace(items []config.Workspace, name string) *config.Workspace {
	for i := range items {
		if strings.EqualFold(items[i].Name, name) {
			return &items[i]
		}
	}
	return nil
}

// --- Welcome Step ---

func (m Model) updateWelcome(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.String() == "enter" {
			if m.isFirstRun {
				m.step = stepRole
			} else {
				m.step = stepTopMenu
				m.topMenuCursor = 0
			}
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
		case "d":
			if m.cursor < len(m.workspaces) {
				ws := m.workspaces[m.cursor]
				if m.defaultWorkspace == ws.Name {
					m.defaultWorkspace = ""
				} else {
					m.defaultWorkspace = ws.Name
				}
				newDefault := m.defaultWorkspace
				return m, func() tea.Msg {
					cfg := config.LoadOrDefault()
					cfg.Settings.DefaultWorkspace = newDefault
					_ = config.SaveConfig(cfg)
					return nil
				}
			}
		case "esc":
			if m.topMenuChoice >= 0 {
				m.step = stepTopMenu
				m.topMenuCursor = 0
				m.topMenuChoice = -1
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
				m.checked["setting:auto_resume"] = false
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

	// cursor(2) + name(20) + space(1) = 23 chars indent for wrapping
	const wsIndent = "                       "
	for i, ws := range m.workspaces {
		cursor := "  "
		if m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		prefix := " "
		if ws.Name == m.defaultWorkspace {
			prefix = "*"
		}
		paddedName := fmt.Sprintf("%s %-20s", prefix, ws.Name)
		var desc string
		if len(ws.Development) > 0 {
			wrapped := wrapWords(ws.Development, wsIndent, m.width)
			desc = strings.TrimLeft(wrapped, " ")
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

	if m.topMenuChoice >= 0 {
		b.WriteString(helpStyle.Render("\nEnter to select, d to toggle default, Esc to go back, q to quit"))
	} else {
		b.WriteString(helpStyle.Render("\nEnter to select, d to toggle default, q to quit"))
	}
	b.WriteString("\n\n" + dimStyle.Render("* Default workspace for new sessions"))
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
			m.visitedSteps[stepRole] = true
			m.step = stepLanguage
			m.cursor = 0
		case "esc":
			if !m.isFirstRun && m.topMenuChoice >= 0 {
				if m.topMenuChoice == 0 && len(m.workspaces) > 1 {
					m.step = stepWorkspaceSelect
				} else {
					m.step = stepTopMenu
					m.topMenuCursor = 0
				}
			} else if len(m.workspaces) > 1 {
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
		b.WriteString(titleStyle.Render(fmt.Sprintf("Step 1/%d — What kind of developer are you?", m.workspaceOnlyStepCount())))
	} else {
		b.WriteString(m.stepTitle(1, "What kind of developer are you?"))
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
		b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back" + m.tabHint()))
	} else {
		b.WriteString(helpStyle.Render("\nSpace to toggle, Esc to go back"+m.tabHint()) + "  " + dimStyle.Render("(select at least one role)"))
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
			m.visitedSteps[stepLanguage] = true
			if m.workspaceOnly {
				m = m.collectAllStepState()
				if len(m.workspaces) > 0 {
					m.step = stepCopyCredentials
				} else {
					m.step = stepVault
				}
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
		b.WriteString(titleStyle.Render(fmt.Sprintf("Step 2/%d — Which languages do you use?", m.workspaceOnlyStepCount())))
	} else {
		b.WriteString(m.stepTitle(2, "Which languages do you use?"))
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

	b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back" + m.tabHint()))
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
			m.visitedSteps[stepTools] = true
			if m.workspaceOnly {
				m.step = stepProfile
			} else {
				m.step = stepPackages
				m.pkgSearchMode = true
				if m.pkgIndex == nil && !m.pkgLoading && m.pkgLoadErr == "" {
					m.pkgLoading = true
					m.cursor = 0
					return m, loadPkgIndex
				}
			}
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
		b.WriteString(m.stepTitle(3, "Which tool categories do you need?"))
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
		wrapped := wrapWords(cat.Packages, "                      ", m.contentWidth())
		// First line starts after the padded name, so trim leading indent
		firstLine := strings.TrimLeft(wrapped, " ")
		pkgs := dimStyle.Render(firstLine)
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, check, paddedName, pkgs))
	}

	b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back" + m.tabHint()))
	return b.String()
}

// --- Packages Step (search + browse) ---

// pkgIndexLoadedMsg is sent when the APKINDEX has been fetched and parsed.
type pkgIndexLoadedMsg struct {
	index []apk.Package
	err   error
}

func loadPkgIndex() tea.Msg {
	index, err := apk.LoadIndex()
	return pkgIndexLoadedMsg{index: index, err: err}
}

func (m Model) updatePackages(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pkgIndexLoadedMsg:
		m.pkgLoading = false
		if msg.err != nil {
			m.pkgLoadErr = msg.err.Error()
		} else {
			m.pkgIndex = msg.index
			m.pkgLoadErr = ""
		}
		// Run initial search if there's already text
		if m.pkgSearchInput != "" {
			m.pkgResults = apk.Search(m.pkgIndex, m.pkgSearchInput, 50)
		}
		return m, nil

	case tea.KeyMsg:
		// If still loading, only allow esc and enter
		if m.pkgLoading {
			switch msg.String() {
			case "esc":
				m.step = stepTools
				m.cursor = 0
			}
			return m, nil
		}

		if m.pkgSearchMode {
			return m.updatePackagesSearchMode(msg)
		}
		return m.updatePackagesBrowseMode(msg)
	}
	return m, nil
}

func (m Model) updatePackagesSearchMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Confirm and move to next step
		m.state.CustomPackages = nil
		for pkg := range m.pkgSelected {
			if m.pkgSelected[pkg] {
				m.state.CustomPackages = append(m.state.CustomPackages, pkg)
			}
		}
		m.visitedSteps[stepPackages] = true
		m.step = stepProfile
		m.cursor = 0
		return m, nil
	case "esc":
		m.step = stepTools
		m.cursor = 0
		return m, nil
	case "down":
		if len(m.pkgResults) > 0 {
			m.pkgSearchMode = false
			m.cursor = 0
		}
		return m, nil
	case "backspace", "ctrl+h":
		if len(m.pkgSearchInput) > 0 {
			m.pkgSearchInput = m.pkgSearchInput[:len(m.pkgSearchInput)-1]
			if m.pkgSearchInput != "" && m.pkgIndex != nil {
				m.pkgResults = apk.Search(m.pkgIndex, m.pkgSearchInput, 50)
			} else {
				m.pkgResults = nil
			}
		}
		return m, nil
	default:
		s := msg.String()
		var added bool
		for _, c := range s {
			if c >= ' ' && c <= '~' {
				m.pkgSearchInput += string(c)
				added = true
			}
		}
		if added && m.pkgIndex != nil {
			m.pkgResults = apk.Search(m.pkgIndex, m.pkgSearchInput, 50)
		}
		return m, nil
	}
}

func (m Model) updatePackagesBrowseMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	maxVisible := m.pkgMaxVisible()
	resultCount := len(m.pkgResults)
	if resultCount > maxVisible {
		resultCount = maxVisible
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		} else {
			// At top of results, go back to search mode
			m.pkgSearchMode = true
		}
	case "down", "j":
		if m.cursor < resultCount-1 {
			m.cursor++
		}
	case " ", "x":
		if m.cursor < len(m.pkgResults) {
			pkg := m.pkgResults[m.cursor].Name
			m.pkgSelected[pkg] = !m.pkgSelected[pkg]
			if !m.pkgSelected[pkg] {
				delete(m.pkgSelected, pkg)
			}
		}
	case "enter":
		// Confirm and move to next step
		m.state.CustomPackages = nil
		for pkg := range m.pkgSelected {
			if m.pkgSelected[pkg] {
				m.state.CustomPackages = append(m.state.CustomPackages, pkg)
			}
		}
		m.visitedSteps[stepPackages] = true
		m.step = stepProfile
		m.cursor = 0
	case "esc":
		m.step = stepTools
		m.cursor = 0
	default:
		// Typed character → switch back to search mode and append
		s := msg.String()
		var added bool
		for _, c := range s {
			if c >= ' ' && c <= '~' {
				m.pkgSearchInput += string(c)
				added = true
			}
		}
		if added {
			m.pkgSearchMode = true
			if m.pkgIndex != nil {
				m.pkgResults = apk.Search(m.pkgIndex, m.pkgSearchInput, 50)
			}
			m.cursor = 0
		}
	}
	return m, nil
}

// pkgMaxVisible returns how many package results to display.
func (m Model) pkgMaxVisible() int {
	// Reserve lines for: title(1) + subtitle(1) + blank(1) + search(1) + blank(1) + added(1) + blank(1) + help(2) = ~9
	available := m.height - 9
	if available < 5 {
		available = 5
	}
	if available > 20 {
		available = 20
	}
	return available
}

func (m Model) viewPackages() string {
	var b strings.Builder
	b.WriteString(m.stepTitle(4, "Add extra Alpine packages"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Search for additional packages beyond the tool categories.\n"))
	b.WriteString("\n")

	if m.pkgLoading {
		b.WriteString("  Loading package database...\n")
		b.WriteString(helpStyle.Render("\nPlease wait, Esc to go back"))
		return b.String()
	}

	if m.pkgLoadErr != "" && m.pkgIndex == nil {
		b.WriteString(fmt.Sprintf("  %s\n", warnStyle.Render("Error: "+m.pkgLoadErr)))
		b.WriteString(dimStyle.Render("  You can type package names directly.\n"))
		b.WriteString("\n")
	}

	// Search input
	cursor := " "
	if m.pkgSearchMode {
		cursor = cursorStyle.Render(">")
	}
	b.WriteString(fmt.Sprintf("  %s Search: %s\n", cursor, selectedStyle.Render(m.pkgSearchInput+blinkCursor(m.pkgSearchMode))))
	b.WriteString("\n")

	// Results
	maxVisible := m.pkgMaxVisible()
	visibleResults := m.pkgResults
	if len(visibleResults) > maxVisible {
		visibleResults = visibleResults[:maxVisible]
	}

	for i, pkg := range visibleResults {
		cursor := "  "
		if !m.pkgSearchMode && m.cursor == i {
			cursor = cursorStyle.Render("> ")
		}
		check := "[ ]"
		if m.pkgSelected[pkg.Name] {
			check = selectedStyle.Render("[x]")
		}
		paddedName := fmt.Sprintf("%-20s", pkg.Name)
		if !m.pkgSearchMode && m.cursor == i {
			paddedName = selectedStyle.Render(paddedName)
		}
		desc := dimStyle.Render(truncate(pkg.Description, 50))
		b.WriteString(fmt.Sprintf("  %s%s %s %s\n", cursor, check, paddedName, desc))
	}

	if m.pkgSearchInput != "" && len(m.pkgResults) == 0 && m.pkgIndex != nil {
		b.WriteString(dimStyle.Render("    No packages found.\n"))
	}

	// Show selected packages
	var selected []string
	for pkg := range m.pkgSelected {
		if m.pkgSelected[pkg] {
			selected = append(selected, pkg)
		}
	}
	if len(selected) > 0 {
		// Sort for stable display
		sortStrings(selected)
		b.WriteString(fmt.Sprintf("\n  Added: %s\n", selectedStyle.Render(strings.Join(selected, ", "))))
	}

	b.WriteString(helpStyle.Render("\nType to search, Space to toggle, Enter to confirm, Esc to go back" + m.tabHint()))
	return b.String()
}

func blinkCursor(active bool) string {
	if active {
		return "█"
	}
	return ""
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// sortStrings sorts a string slice in place (simple insertion sort to avoid importing sort).
func sortStrings(ss []string) {
	for i := 1; i < len(ss); i++ {
		for j := i; j > 0 && ss[j] < ss[j-1]; j-- {
			ss[j], ss[j-1] = ss[j-1], ss[j]
		}
	}
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
			m.visitedSteps[stepProfile] = true
			if m.hasCopyCredentialsStep() {
				m.step = stepCopyCredentials
			} else {
				m.step = stepAgents
			}
			m.cursor = 0
		case "esc":
			if m.workspaceOnly {
				m.step = stepTools
			} else {
				m.step = stepPackages
				m.pkgSearchMode = true
			}
			m.cursor = 0
		case "backspace", "ctrl+h":
			if len(m.workspaceInput) > 0 {
				m.workspaceInput = m.workspaceInput[:len(m.workspaceInput)-1]
			}
		default:
			s := key.String()
			for _, c := range s {
				isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
				if isAlphaNum || c == '-' || c == '_' {
					m.workspaceInput += string(c)
				}
			}
		}
	}
	return m, nil
}

func (m Model) viewProfile() string {
	var b strings.Builder
	b.WriteString(m.stepTitle(5, "Name your workspace"))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("This workspace stores development stacks and separate agent configs.\n"))
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  Workspace name: %s\n", selectedStyle.Render(m.workspaceInput+"█")))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Examples: personal, work, client-a"))
	b.WriteString("\n")
	if strings.TrimSpace(m.workspaceInput) == "" {
		b.WriteString(helpStyle.Render("\nType a name, Esc to go back"+m.tabHint()) + "  " + dimStyle.Render("(name required)"))
	} else {
		b.WriteString(helpStyle.Render("\nType to edit, Enter to confirm, Esc to go back" + m.tabHint()))
	}
	return b.String()
}

// --- Copy Credentials Step ---
// Shown in both the full wizard and workspace-only wizard when there are
// existing workspaces to copy from. Offers: copy from workspace, import
// from host, or skip.

// hasCopyCredentialsStep returns true when the copy-credentials step should be shown.
func (m Model) hasCopyCredentialsStep() bool {
	// Only for new workspaces (not editing), and only when other workspaces exist.
	return !m.editingExisting && len(m.copyCredentialOptions()) > 0
}

// copyCredentialOptions returns workspace names available to copy from.
// Excludes the workspace being created/edited.
func (m Model) copyCredentialOptions() []config.Workspace {
	var out []config.Workspace
	for _, w := range m.workspaces {
		if !strings.EqualFold(w.Name, m.state.WorkspaceName) {
			out = append(out, w)
		}
	}
	return out
}

func (m Model) updateCopyCredentials(msg tea.Msg) (tea.Model, tea.Cmd) {
	options := m.copyCredentialOptions()
	// Layout: [Skip] [Import from host] [workspace 0] ... [workspace N-1]
	itemCount := len(options) + 2

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
			if m.cursor == 0 {
				m.state.CopyFrom = ""
			} else if m.cursor == 1 {
				m.state.CopyFrom = "__host__"
			} else {
				m.state.CopyFrom = options[m.cursor-2].Name
			}
			if m.workspaceOnly {
				m.visitedSteps[stepCopyCredentials] = true
				m.step = stepVault
			} else {
				m.visitedSteps[stepCopyCredentials] = true
				m.step = stepAgents
			}
			m.cursor = 0
		case "esc":
			if m.workspaceOnly {
				m.step = stepLanguage
			} else {
				m.step = stepProfile
			}
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewCopyCredentials() string {
	options := m.copyCredentialOptions()
	var b strings.Builder

	if m.workspaceOnly {
		b.WriteString(titleStyle.Render(fmt.Sprintf("Step %d/%d — Import credentials", m.workspaceOnlyStepCount()-1, m.workspaceOnlyStepCount())))
	} else {
		b.WriteString(m.stepTitle(6, "Import credentials"))
	}
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Agent credentials (API keys, auth tokens) for the new workspace.\n"))
	b.WriteString("\n")

	wsName := strings.TrimSpace(m.state.WorkspaceName)
	if wsName == "" {
		wsName = strings.TrimSpace(m.workspaceInput)
	}

	idx := 0

	// "Skip" option (default)
	cursor := "  "
	if m.cursor == idx {
		cursor = cursorStyle.Render("> ")
	}
	b.WriteString(fmt.Sprintf("%sSkip  %s\n", cursor, dimStyle.Render(fmt.Sprintf("(can be done later with 'exitbox import --workspace %s')", wsName))))
	idx++

	// "Import from host" option
	cursor = "  "
	if m.cursor == idx {
		cursor = cursorStyle.Render("> ")
	}
	b.WriteString(fmt.Sprintf("%sImport from host  %s\n", cursor, dimStyle.Render("(~/.claude, ~/.codex, etc.)")))
	idx++

	b.WriteString("\n")

	for _, w := range options {
		cursor = "  "
		if m.cursor == idx {
			cursor = cursorStyle.Render("> ")
		}
		label := fmt.Sprintf("Copy from '%s'", w.Name)
		if len(w.Development) > 0 {
			label += dimStyle.Render("  (" + strings.Join(w.Development, ", ") + ")")
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, label))
		idx++
	}

	b.WriteString(helpStyle.Render("\nUp/Down to move, Enter to confirm, Esc to go back"))
	return b.String()
}

// workspaceOnlyStepCount returns the total step count for workspace-only mode.
func (m Model) workspaceOnlyStepCount() int {
	n := 3 // Role, Language, Review
	if m.hasCopyCredentialsStep() {
		n++ // + Copy Credentials
	}
	n++ // + Vault
	return n
}

// --- Vault Step (toggle) ---

func (m Model) updateVault(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch m.vaultPhase {
		case 0: // Choice: enable or skip
			return m.updateVaultChoice(key)
		case 1: // Password entry
			return m.updateVaultPassword(key)
		case 2: // Password confirmation
			return m.updateVaultConfirm(key)
		}
	}
	return m, nil
}

func (m Model) updateVaultChoice(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < 1 {
			m.cursor++
		}
	case "enter":
		if m.cursor == 0 {
			// Enable vault → enter password
			m.vaultPhase = 1
			m.vaultPwInput = ""
			m.vaultPwErr = ""
		} else {
			// Skip vault
			m.state.VaultEnabled = false
			m.state.VaultPassword = ""
			m.visitedSteps[stepVault] = true
			m = m.collectAllStepState()
			m.step = stepReview
			m.cursor = 0
		}
	case "esc":
		m = m.vaultGoBack()
	}
	return m, nil
}

func (m Model) updateVaultPassword(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		if len(m.vaultPwInput) < 1 {
			m.vaultPwErr = "Password cannot be empty"
			return m, nil
		}
		m.vaultPhase = 2
		m.vaultPwConfirm = ""
		m.vaultPwErr = ""
	case "esc":
		m.vaultPhase = 0
		m.vaultPwInput = ""
		m.vaultPwErr = ""
		m.cursor = 0
	case "backspace", "ctrl+h":
		if len(m.vaultPwInput) > 0 {
			m.vaultPwInput = m.vaultPwInput[:len(m.vaultPwInput)-1]
		}
	default:
		s := key.String()
		if len(s) == 1 && s[0] >= 32 && s[0] <= 126 {
			m.vaultPwInput += s
		}
	}
	return m, nil
}

func (m Model) updateVaultConfirm(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		if m.vaultPwConfirm != m.vaultPwInput {
			m.vaultPwErr = "Passwords do not match"
			m.vaultPwConfirm = ""
			return m, nil
		}
		// Passwords match — vault is ready
		m.state.VaultEnabled = true
		m.state.VaultPassword = m.vaultPwInput
		m.vaultPwInput = ""
		m.vaultPwConfirm = ""
		m.vaultPwErr = ""
		m.visitedSteps[stepVault] = true
		m = m.collectAllStepState()
		m.step = stepReview
		m.cursor = 0
	case "esc":
		m.vaultPhase = 1
		m.vaultPwConfirm = ""
		m.vaultPwErr = ""
	case "backspace", "ctrl+h":
		if len(m.vaultPwConfirm) > 0 {
			m.vaultPwConfirm = m.vaultPwConfirm[:len(m.vaultPwConfirm)-1]
		}
	default:
		s := key.String()
		if len(s) == 1 && s[0] >= 32 && s[0] <= 126 {
			m.vaultPwConfirm += s
		}
	}
	return m, nil
}

func (m Model) vaultGoBack() Model {
	m.vaultPhase = 0
	m.vaultPwInput = ""
	m.vaultPwConfirm = ""
	m.vaultPwErr = ""
	if m.workspaceOnly {
		if m.hasCopyCredentialsStep() {
			m.step = stepCopyCredentials
		} else {
			m.step = stepLanguage
		}
	} else if m.firewallEnabled() {
		m.step = stepDomains
		m.domainCatCursor = 0
		m.domainItemCursor = 0
	} else {
		m.step = stepKeybindings
		m.kbCursor = 0
	}
	m.cursor = 0
	return m
}

func (m Model) viewVault() string {
	var b strings.Builder

	if m.workspaceOnly {
		b.WriteString(titleStyle.Render(fmt.Sprintf("Step %d/%d — Encrypted Vault", m.workspaceOnlyStepCount()-1, m.workspaceOnlyStepCount())))
	} else {
		total := len(m.visibleSidebarSteps())
		b.WriteString(m.stepTitle(total-1, "Encrypted Vault"))
	}
	b.WriteString("\n")

	switch m.vaultPhase {
	case 0:
		b.WriteString(subtitleStyle.Render("Encrypt secrets (API keys, tokens, credentials) in an encrypted vault."))
		b.WriteString("\n")
		b.WriteString(subtitleStyle.Render("Agents request secrets by name; each read shows an approval popup."))
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("  How it works:"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  1. Your secrets are stored encrypted on disk (AES-256 + Argon2id)"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  2. First access in a session prompts for the vault password"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  3. Each secret read shows a y/n approval popup"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  4. .env files are masked (hidden) inside the container"))
		b.WriteString("\n")
		b.WriteString(dimStyle.Render("  5. Agents use: exitbox-vault get <KEY> to fetch secrets"))
		b.WriteString("\n\n")

		options := []string{"Enable vault (set password now)", "Skip (use .env files directly)"}
		for i, opt := range options {
			cursor := "  "
			if m.cursor == i {
				cursor = cursorStyle.Render("> ")
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, opt))
		}
		b.WriteString(helpStyle.Render("\nUp/Down to move, Enter to confirm, Esc to go back"))

	case 1:
		b.WriteString(subtitleStyle.Render("Choose a password to encrypt the vault."))
		b.WriteString("\n")
		b.WriteString(subtitleStyle.Render("You'll need this password when the agent first accesses secrets."))
		b.WriteString("\n\n")
		mask := strings.Repeat("*", len(m.vaultPwInput))
		b.WriteString(fmt.Sprintf("  Password: %s\n", selectedStyle.Render(mask+"█")))
		if m.vaultPwErr != "" {
			b.WriteString(fmt.Sprintf("\n  %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.vaultPwErr)))
		}
		b.WriteString(helpStyle.Render("\nType your password, Enter to confirm, Esc to go back"))

	case 2:
		b.WriteString(subtitleStyle.Render("Confirm your vault password."))
		b.WriteString("\n\n")
		mask := strings.Repeat("*", len(m.vaultPwConfirm))
		b.WriteString(fmt.Sprintf("  Confirm:  %s\n", selectedStyle.Render(mask+"█")))
		if m.vaultPwErr != "" {
			b.WriteString(fmt.Sprintf("\n  %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.vaultPwErr)))
		}
		b.WriteString(helpStyle.Render("\nType your password again, Enter to confirm, Esc to go back"))
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
			m.visitedSteps[stepAgents] = true
			m.step = stepSettings
			m.cursor = 0
		case "esc":
			if m.hasCopyCredentialsStep() {
				m.step = stepCopyCredentials
			} else {
				m.step = stepProfile
			}
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) viewAgents() string {
	var b strings.Builder
	b.WriteString(m.stepTitle(6, "Which agents do you want to enable?"))
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
		b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back" + m.tabHint()))
	} else {
		b.WriteString(helpStyle.Render("\nSpace to toggle, Esc to go back"+m.tabHint()) + "  " + dimStyle.Render("(select at least one agent)"))
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
	{"setting:firewall", "Network firewall", "Restrict outbound network to allowlisted domains only (disabling decreases security)"},
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
			m.state.EnableFirewall = m.checked["setting:firewall"]
			m.state.AutoResume = m.checked["setting:auto_resume"]
			m.state.PassEnv = m.checked["setting:pass_env"]
			m.state.ReadOnly = m.checked["setting:read_only"]
			m.state.MakeDefault = m.checked["setting:make_default"]
			m.visitedSteps[stepSettings] = true
			if !m.isFirstRun && m.topMenuChoice == 0 {
				// Workspace management: Settings → Domains or Vault
				if m.firewallEnabled() {
					m.step = stepDomains
					m.domainCatCursor = 0
					m.domainItemCursor = 0
				} else {
					m.step = stepVault
					m.cursor = 0
				}
			} else {
				// First-run: Settings → Keybindings
				m.step = stepKeybindings
				m.kbCursor = 0
				m.kbEditMode = false
			}
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
	b.WriteString(m.stepTitle(7, "Settings"))
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
		desc := dimStyle.Render(opt.Description)
		if opt.Key == "setting:firewall" && !m.checked[opt.Key] {
			desc = warnStyle.Render("⚠ " + opt.Description)
		}
		b.WriteString(fmt.Sprintf("%s%s %s %s\n", cursor, check, paddedLabel, desc))
	}

	defaultWs := m.defaultWorkspace
	if m.checked["setting:make_default"] {
		defaultWs = activeWorkspaceNameOrDefault(m.workspaceInput)
	}
	if defaultWs == "" {
		defaultWs = "none"
	}
	b.WriteString(fmt.Sprintf("\n  Default workspace: %s\n", dimStyle.Render(defaultWs)))

	b.WriteString(helpStyle.Render("\nSpace to toggle, Enter to confirm, Esc to go back" + m.tabHint()))
	return b.String()
}

// --- Top Menu Step (re-run only) ---

var topMenuOptions = []struct {
	Label       string
	Description string
}{
	{"Workspace management", "Configure roles, languages, tools, packages, and workspace settings"},
	{"General settings", "Configure keybindings and global preferences"},
}

func (m Model) updateTopMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "up", "k":
			if m.topMenuCursor > 0 {
				m.topMenuCursor--
			}
		case "down", "j":
			if m.topMenuCursor < len(topMenuOptions)-1 {
				m.topMenuCursor++
			}
		case "enter":
			m.topMenuChoice = m.topMenuCursor
			if m.topMenuChoice == 0 {
				// Workspace management flow
				if len(m.workspaces) > 1 {
					m.step = stepWorkspaceSelect
				} else {
					m.step = stepRole
				}
			} else {
				// General settings flow — single screen: keybindings
				m.step = stepKeybindings
				m.kbCursor = 0
			}
			m.cursor = 0
		case "esc", "q":
			m.cancelled = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) viewTopMenu() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(logo))
	b.WriteString("\n\n")
	b.WriteString(titleStyle.Render("ExitBox Setup — What would you like to configure?"))
	b.WriteString("\n\n")

	for i, opt := range topMenuOptions {
		cursor := "  "
		if m.topMenuCursor == i {
			cursor = cursorStyle.Render("> ")
		}
		label := opt.Label
		if m.topMenuCursor == i {
			label = selectedStyle.Render(label)
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, label))
		b.WriteString(fmt.Sprintf("    %s\n\n", dimStyle.Render(opt.Description)))
	}

	b.WriteString(helpStyle.Render("Up/Down to move, Enter to select, q to quit"))
	return b.String()
}

// --- Keybindings Step ---

var allKeybindingActions = []struct {
	Key, Label, Description, Default string
}{
	{"workspace_menu", "Workspace menu", "Open workspace switcher", "C-M-p"},
	{"session_menu", "Session menu", "Open session manager", "C-M-s"},
}

func (m Model) updateKeybindings(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if m.kbEditMode {
			return m.updateKeybindingEdit(key)
		}

		switch key.String() {
		case "up", "k":
			if m.kbCursor > 0 {
				m.kbCursor--
			}
		case "down", "j":
			if m.kbCursor < len(allKeybindingActions)-1 {
				m.kbCursor++
			}
		case " ", "e":
			// Enter edit mode — pre-fill with current value
			m.kbEditMode = true
			m.kbEditInput = m.keybindings[allKeybindingActions[m.kbCursor].Key]
		case "enter":
			m.visitedSteps[stepKeybindings] = true
			if !m.isFirstRun && m.topMenuChoice == 1 {
				// General settings flow — go to review
				m = m.collectAllStepState()
				m.step = stepReview
			} else if m.firewallEnabled() {
				m.step = stepDomains
				m.domainCatCursor = 0
				m.domainItemCursor = 0
			} else {
				m.step = stepVault
				m.cursor = 0
			}
			m.cursor = 0
		case "esc":
			if !m.isFirstRun && m.topMenuChoice == 1 {
				// General settings: back to top menu
				m.step = stepTopMenu
				m.topMenuCursor = 0
			} else {
				// First-run or workspace management (via sidebar): back to Settings
				m.step = stepSettings
			}
			m.cursor = 0
		}
	}
	return m, nil
}

func (m Model) updateKeybindingEdit(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		val := strings.TrimSpace(m.kbEditInput)
		if val == "" {
			m.kbEditMode = false
			m.kbEditInput = ""
			m.kbEditErr = ""
			break
		}
		if err := validTmuxKey(val); err != "" {
			m.kbEditErr = err
			break
		}
		action := allKeybindingActions[m.kbCursor]
		m.keybindings[action.Key] = val
		m.kbEditMode = false
		m.kbEditInput = ""
		m.kbEditErr = ""
	case "esc":
		m.kbEditMode = false
		m.kbEditInput = ""
		m.kbEditErr = ""
	case "backspace", "ctrl+h":
		m.kbEditErr = ""
		if len(m.kbEditInput) > 0 {
			m.kbEditInput = m.kbEditInput[:len(m.kbEditInput)-1]
		}
	default:
		m.kbEditErr = ""
		s := key.String()
		for _, c := range s {
			if c >= ' ' && c <= '~' {
				m.kbEditInput += string(c)
			}
		}
	}
	return m, nil
}

// validTmuxKey validates tmux key notation. Returns an error message if
// invalid, or empty string if valid.
// Valid: optional prefixes C- M- S- followed by a base key (single char,
// function key F1-F20, or special name like Enter, Tab, Space, etc.).
func validTmuxKey(s string) string {
	rest := s

	// Strip valid modifier prefixes (in any order, each at most once)
	sawC, sawM, sawS := false, false, false
	for {
		if strings.HasPrefix(rest, "C-") && !sawC {
			sawC = true
			rest = rest[2:]
		} else if strings.HasPrefix(rest, "M-") && !sawM {
			sawM = true
			rest = rest[2:]
		} else if strings.HasPrefix(rest, "S-") && !sawS {
			sawS = true
			rest = rest[2:]
		} else {
			break
		}
	}

	if rest == "" {
		return "Missing key after modifier prefix"
	}

	// Single printable character (a-z, 0-9, punctuation)
	if len(rest) == 1 {
		c := rest[0]
		if c >= '!' && c <= '~' {
			return ""
		}
		return fmt.Sprintf("Invalid key: %q", rest)
	}

	// Function keys F1-F20
	if rest[0] == 'F' && len(rest) >= 2 && len(rest) <= 3 {
		num := rest[1:]
		valid := false
		for _, fk := range []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20"} {
			if num == fk {
				valid = true
				break
			}
		}
		if valid {
			return ""
		}
	}

	// Special named keys
	specials := map[string]bool{
		"Enter": true, "Tab": true, "Space": true, "BSpace": true,
		"Escape": true, "Up": true, "Down": true, "Left": true,
		"Right": true, "Home": true, "End": true, "PPage": true,
		"NPage": true, "DC": true, "IC": true,
	}
	if specials[rest] {
		return ""
	}

	return fmt.Sprintf("Invalid key %q. Use: single char, F1-F20, or Enter/Tab/Space/Up/Down/etc.", rest)
}

func (m Model) viewKeybindings() string {
	var b strings.Builder
	total := len(m.visibleSidebarSteps())
	// Find our step number in the visible sidebar
	num := 0
	for _, si := range m.visibleSidebarSteps() {
		if si.Step == stepKeybindings {
			num = si.Num
			break
		}
	}
	if num == 0 {
		num = total
	}
	b.WriteString(titleStyle.Render(fmt.Sprintf("Step %d/%d — Keybindings", num, total)))
	b.WriteString("\n")
	b.WriteString(subtitleStyle.Render("Customize tmux key shortcuts. Notation: C- = Ctrl, M- = Alt, S- = Shift\n"))
	b.WriteString(subtitleStyle.Render("Examples: C-M-p (Ctrl+Alt+p), C-b (Ctrl+b), F2, S-Tab (Shift+Tab)\n"))
	b.WriteString("\n")

	for i, action := range allKeybindingActions {
		cursor := "  "
		if m.kbCursor == i && !m.kbEditMode {
			cursor = cursorStyle.Render("> ")
		}

		currentVal := m.keybindings[action.Key]
		if currentVal == "" {
			currentVal = action.Default
		}

		label := fmt.Sprintf("%-20s", action.Label)
		if m.kbCursor == i && !m.kbEditMode {
			label = selectedStyle.Render(label)
		}

		binding := selectedStyle.Render(currentVal)
		if currentVal != action.Default {
			binding = successStyle.Render(currentVal)
		}

		desc := dimStyle.Render(action.Description)
		b.WriteString(fmt.Sprintf("%s%s %s  %s\n", cursor, label, binding, desc))

		if m.kbCursor == i && m.kbEditMode {
			b.WriteString(fmt.Sprintf("    Tmux notation: %s\n", selectedStyle.Render(m.kbEditInput+"█")))
			if m.kbEditErr != "" {
				b.WriteString(fmt.Sprintf("    %s\n", warnStyle.Render(m.kbEditErr)))
			}
		}
	}

	b.WriteString(helpStyle.Render("\nSpace to edit, Enter to confirm, Esc to go back" + m.tabHint()))
	return b.String()
}

// --- Review Step ---

func (m Model) updateReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter", "y":
			m.state.DefaultWorkspace = m.defaultWorkspace
			m.confirmed = true
			m.step = stepDone
			return m, tea.Quit
		case "d":
			m.state.MakeDefault = !m.state.MakeDefault
			if m.state.MakeDefault {
				m.defaultWorkspace = activeWorkspaceNameOrDefault(m.state.WorkspaceName)
			} else if m.defaultWorkspace == activeWorkspaceNameOrDefault(m.state.WorkspaceName) {
				m.defaultWorkspace = ""
			}
		case "esc":
			if m.workspaceOnly {
				if len(m.workspaces) > 0 {
					m.step = stepCopyCredentials
				} else {
					m.step = stepLanguage
				}
			} else if !m.isFirstRun && m.topMenuChoice == 0 {
				// Workspace management: previous step is Domains or Settings
				if m.firewallEnabled() {
					m.step = stepDomains
				} else {
					m.step = stepSettings
				}
			} else if !m.isFirstRun && m.topMenuChoice == 1 {
				// General settings: previous step is Keybindings
				m.step = stepKeybindings
			} else if m.firewallEnabled() {
				m.step = stepDomains
			} else {
				m.step = stepKeybindings
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
	if !m.isFirstRun && m.topMenuChoice == 1 {
		return m.viewGeneralSettingsReview()
	}

	var b strings.Builder
	total := len(m.visibleSidebarSteps())
	b.WriteString(m.stepTitle(total, "Review your configuration"))
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
		firewallStr = warnStyle.Render("⚠ no")
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
	vaultStr := dimStyle.Render("no (.env files)")
	if m.state.VaultEnabled {
		vaultStr = successStyle.Render("yes (encrypted, password set)")
	}
	b.WriteString(fmt.Sprintf("  Vault:        %s\n", vaultStr))

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
		wrapped := wrapWords(packages, "              ", m.contentWidth())
		firstLine := strings.TrimLeft(wrapped, " ")
		b.WriteString(fmt.Sprintf("  Packages:   %s\n", dimStyle.Render(firstLine)))
	}

	if len(m.state.CustomPackages) > 0 {
		sorted := make([]string, len(m.state.CustomPackages))
		copy(sorted, m.state.CustomPackages)
		sortStrings(sorted)
		wrapped := wrapWords(sorted, "              ", m.contentWidth())
		firstLine := strings.TrimLeft(wrapped, " ")
		b.WriteString(fmt.Sprintf("  Extra pkgs: %s\n", selectedStyle.Render(firstLine)))
	}

	if len(m.state.DomainCategories) > 0 {
		total := countDomains(m.state.DomainCategories)
		custom := countCustomDomains(m.state.DomainCategories)
		domainStr := fmt.Sprintf("%d domains", total)
		if custom > 0 {
			domainStr += fmt.Sprintf(" (%d custom)", custom)
		}
		b.WriteString(fmt.Sprintf("  Allowlist:  %s\n", selectedStyle.Render(domainStr)))
	}

	// Keybindings
	if len(m.state.Keybindings) > 0 {
		b.WriteString("\n")
		for _, action := range allKeybindingActions {
			val := m.state.Keybindings[action.Key]
			if val == "" {
				val = action.Default
			}
			style := dimStyle
			if val != action.Default {
				style = successStyle
			}
			b.WriteString(fmt.Sprintf("  %-14s %s\n", action.Label+":", style.Render(val)))
		}
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Enter to confirm, d to toggle default, Esc to go back, q to cancel" + m.tabHint()))
	return b.String()
}

func (m Model) viewGeneralSettingsReview() string {
	var b strings.Builder
	total := len(m.visibleSidebarSteps())
	b.WriteString(m.stepTitle(total, "Review your settings"))
	b.WriteString("\n\n")

	for _, action := range allKeybindingActions {
		val := m.state.Keybindings[action.Key]
		if val == "" {
			val = action.Default
		}
		style := dimStyle
		if val != action.Default {
			style = successStyle
		}
		b.WriteString(fmt.Sprintf("  %-14s %s  %s\n", action.Label+":", style.Render(val), dimStyle.Render(action.Description)))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Enter to confirm, Esc to go back, q to cancel" + m.tabHint()))
	return b.String()
}

func (m Model) viewWorkspaceOnlyReview() string {
	var b strings.Builder
	totalSteps := m.workspaceOnlyStepCount()
	b.WriteString(titleStyle.Render(fmt.Sprintf("Step %d/%d — Review new workspace", totalSteps, totalSteps)))
	b.WriteString("\n\n")

	name := strings.TrimSpace(m.state.WorkspaceName)
	if name == "" {
		name = strings.TrimSpace(m.workspaceInput)
	}
	if name == "" {
		name = "default"
	}
	b.WriteString(fmt.Sprintf("  Workspace:    %s\n", selectedStyle.Render(name)))

	if len(m.state.Roles) > 0 {
		b.WriteString(fmt.Sprintf("  Roles:        %s\n", successStyle.Render(strings.Join(m.state.Roles, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Roles:        %s\n", dimStyle.Render("none")))
	}

	if len(m.state.Languages) > 0 {
		b.WriteString(fmt.Sprintf("  Languages:    %s\n", selectedStyle.Render(strings.Join(m.state.Languages, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Languages:    %s\n", dimStyle.Render("none")))
	}

	dev := ComputeProfiles(m.state.Roles, m.state.Languages)
	if len(dev) > 0 {
		b.WriteString(fmt.Sprintf("  Development:  %s\n", selectedStyle.Render(strings.Join(dev, ", "))))
	} else {
		b.WriteString(fmt.Sprintf("  Development:  %s\n", dimStyle.Render("none")))
	}

	if m.state.CopyFrom != "" {
		b.WriteString(fmt.Sprintf("  Credentials:  %s\n", selectedStyle.Render("copy from "+m.state.CopyFrom)))
	} else {
		b.WriteString(fmt.Sprintf("  Credentials:  %s\n", dimStyle.Render("fresh (seeded from host)")))
	}

	vaultStr := dimStyle.Render("no (.env files)")
	if m.state.VaultEnabled {
		vaultStr = successStyle.Render("yes (encrypted, password set)")
	}
	b.WriteString(fmt.Sprintf("  Vault:        %s\n", vaultStr))

	defaultStr := dimStyle.Render("no")
	if m.state.MakeDefault {
		defaultStr = successStyle.Render("yes")
	}
	b.WriteString(fmt.Sprintf("  Make default: %s\n", defaultStr))

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Enter to create workspace, d to toggle default, Esc to go back, q to cancel"))
	return b.String()
}

// --- Content Width ---

// contentWidth returns the available width for step content, accounting for sidebar.
func (m Model) contentWidth() int {
	if !m.workspaceOnly && m.step >= stepRole && m.step <= stepReview {
		w := m.width - sidebarWidth - 2 // 2 for border + gap
		if w < 40 {
			w = 40
		}
		return w
	}
	return m.width
}

// stepTitle formats a step title like "Step 3/9 — Some title".
// It adjusts the total count when firewall is disabled (8 instead of 9).
func (m Model) stepTitle(num int, title string) string {
	total := len(m.visibleSidebarSteps())
	return titleStyle.Render(fmt.Sprintf("Step %d/%d — %s", num, total, title))
}

// tabHint returns ", Tab: sidebar" if the sidebar is available.
func (m Model) tabHint() string {
	if !m.isFirstRun && !m.workspaceOnly {
		return ", Tab: sidebar"
	}
	return ""
}

// firewallEnabled returns true if the firewall setting is currently checked.
func (m Model) firewallEnabled() bool {
	return m.checked["setting:firewall"]
}

// --- Sidebar Navigation ---

// renderSidebar returns the sidebar panel string.
func (m Model) renderSidebar() string {
	var b strings.Builder
	b.WriteString("\n")

	visible := m.visibleSidebarSteps()
	for i, si := range visible {
		var line string
		isCurrent := si.Step == m.step
		isVisited := m.visitedSteps[si.Step]

		// Cursor prefix when sidebar is focused
		prefix := "  "
		if m.sidebarFocused && m.sidebarCursor == i {
			prefix = cursorStyle.Render("> ")
		}

		label := fmt.Sprintf("%d. %s", si.Num, si.Label)

		if isCurrent {
			line = prefix + sidebarActiveStyle.Render(">> "+label)
		} else if isVisited {
			line = prefix + sidebarVisitedStyle.Render(label+" ✓")
		} else {
			line = prefix + dimStyle.Render(label)
		}

		b.WriteString(line + "\n")
	}

	style := sidebarStyle
	if m.sidebarFocused {
		style = sidebarFocusedStyle
	}
	return style.Render(b.String())
}

// updateSidebar handles keys when the sidebar is focused.
func (m Model) updateSidebar(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	visible := m.visibleSidebarSteps()

	switch key.String() {
	case "up", "k":
		if m.sidebarCursor > 0 {
			m.sidebarCursor--
		}
	case "down", "j":
		if m.sidebarCursor < len(visible)-1 {
			m.sidebarCursor++
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(key.String()[0]-'0') - 1
		if idx >= 0 && idx < len(visible) {
			m.sidebarCursor = idx
		}
	case "enter":
		target := visible[m.sidebarCursor].Step
		if target == m.step {
			m.sidebarFocused = false
			return m, nil
		}
		m = m.collectCurrentStepState()
		m.visitedSteps[m.step] = true
		if target == stepReview {
			m = m.collectAllStepState()
		}
		m.step = target
		m.cursor = 0
		m.sidebarFocused = false
		// If jumping to packages, trigger index load if needed
		if target == stepPackages && m.pkgIndex == nil && !m.pkgLoading && m.pkgLoadErr == "" {
			m.pkgLoading = true
			m.pkgSearchMode = true
			return m, loadPkgIndex
		}
		// Reset domain cursors when jumping to domains
		if target == stepDomains {
			m.domainCatCursor = 0
			m.domainItemCursor = 0
		}
	case "tab", "esc":
		m.sidebarFocused = false
	}
	return m, nil
}

// collectAllStepState snapshots every step's data into m.state from m.checked.
// Call this before entering Review to ensure the state is complete regardless
// of which steps were visited or skipped via sidebar navigation.
func (m Model) collectAllStepState() Model {
	m.state.Roles = nil
	for _, role := range Roles {
		if m.checked["role:"+role.Name] {
			m.state.Roles = append(m.state.Roles, role.Name)
		}
	}
	m.state.Languages = nil
	for _, l := range AllLanguages {
		if m.checked["lang:"+l.Name] {
			m.state.Languages = append(m.state.Languages, l.Name)
		}
	}
	m.state.ToolCategories = nil
	for _, t := range AllToolCategories {
		if m.checked["tool:"+t.Name] {
			m.state.ToolCategories = append(m.state.ToolCategories, t.Name)
		}
	}
	m.state.CustomPackages = nil
	for pkg := range m.pkgSelected {
		if m.pkgSelected[pkg] {
			m.state.CustomPackages = append(m.state.CustomPackages, pkg)
		}
	}
	m.state.WorkspaceName = strings.TrimSpace(m.workspaceInput)
	m.state.Agents = nil
	for _, a := range AllAgents {
		if m.checked["agent:"+a.Name] {
			m.state.Agents = append(m.state.Agents, a.Name)
		}
	}
	m.state.AutoUpdate = m.checked["setting:auto_update"]
	m.state.StatusBar = m.checked["setting:status_bar"]
	m.state.EnableFirewall = m.checked["setting:firewall"]
	m.state.AutoResume = m.checked["setting:auto_resume"]
	m.state.PassEnv = m.checked["setting:pass_env"]
	m.state.ReadOnly = m.checked["setting:read_only"]
	m.state.MakeDefault = m.checked["setting:make_default"]
	m.state.DomainCategories = m.domainCategories
	m.state.Keybindings = copyMap(m.keybindings)
	return m
}

func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// collectCurrentStepState saves current step data from model fields into m.state.
func (m Model) collectCurrentStepState() Model {
	switch m.step {
	case stepRole:
		m.state.Roles = nil
		for _, role := range Roles {
			if m.checked["role:"+role.Name] {
				m.state.Roles = append(m.state.Roles, role.Name)
			}
		}
	case stepLanguage:
		m.state.Languages = nil
		for _, l := range AllLanguages {
			if m.checked["lang:"+l.Name] {
				m.state.Languages = append(m.state.Languages, l.Name)
			}
		}
	case stepTools:
		m.state.ToolCategories = nil
		for _, t := range AllToolCategories {
			if m.checked["tool:"+t.Name] {
				m.state.ToolCategories = append(m.state.ToolCategories, t.Name)
			}
		}
	case stepPackages:
		m.state.CustomPackages = nil
		for pkg := range m.pkgSelected {
			if m.pkgSelected[pkg] {
				m.state.CustomPackages = append(m.state.CustomPackages, pkg)
			}
		}
	case stepProfile:
		m.state.WorkspaceName = strings.TrimSpace(m.workspaceInput)
	case stepAgents:
		m.state.Agents = nil
		for _, a := range AllAgents {
			if m.checked["agent:"+a.Name] {
				m.state.Agents = append(m.state.Agents, a.Name)
			}
		}
	case stepSettings:
		m.state.AutoUpdate = m.checked["setting:auto_update"]
		m.state.StatusBar = m.checked["setting:status_bar"]
		m.state.EnableFirewall = m.checked["setting:firewall"]
		m.state.AutoResume = m.checked["setting:auto_resume"]
		m.state.PassEnv = m.checked["setting:pass_env"]
		m.state.ReadOnly = m.checked["setting:read_only"]
		m.state.MakeDefault = m.checked["setting:make_default"]
	case stepKeybindings:
		m.state.Keybindings = copyMap(m.keybindings)
	case stepDomains:
		m.state.DomainCategories = m.domainCategories
	}
	return m
}

// --- Domains Step ---

func (m Model) updateDomains(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.domainInputMode {
		return m.updateDomainInput(key)
	}

	cats := m.domainCategories
	catCount := len(cats)
	if catCount == 0 {
		return m, nil
	}

	var domCount int
	if m.domainCatCursor < catCount {
		domCount = len(cats[m.domainCatCursor].Domains)
	}

	switch key.String() {
	case "left", "h":
		if m.domainCatCursor > 0 {
			m.domainCatCursor--
			m.domainItemCursor = 0
		}
	case "right", "l":
		if m.domainCatCursor < catCount-1 {
			m.domainCatCursor++
			m.domainItemCursor = 0
		}
	case "up", "k":
		if m.domainItemCursor > 0 {
			m.domainItemCursor--
		}
	case "down", "j":
		if m.domainItemCursor < domCount-1 {
			m.domainItemCursor++
		}
	case "a":
		m.domainInputMode = true
		m.domainInput = ""
	case "d":
		if domCount > 0 && m.domainItemCursor < domCount {
			doms := cats[m.domainCatCursor].Domains
			m.domainCategories[m.domainCatCursor].Domains = append(doms[:m.domainItemCursor], doms[m.domainItemCursor+1:]...)
			if m.domainItemCursor >= len(m.domainCategories[m.domainCatCursor].Domains) && m.domainItemCursor > 0 {
				m.domainItemCursor--
			}
		}
	case "enter":
		m.state.DomainCategories = m.domainCategories
		m.visitedSteps[stepDomains] = true
		m.step = stepVault
		m.cursor = 0
	case "esc":
		if !m.isFirstRun && m.topMenuChoice == 0 {
			// Workspace management: previous step is Settings
			m.step = stepSettings
		} else {
			// First-run: previous step is Keybindings
			m.step = stepKeybindings
		}
		m.cursor = 0
	}
	return m, nil
}

func (m Model) updateDomainInput(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "enter":
		domain := strings.TrimSpace(m.domainInput)
		if domain != "" {
			// Dedup check
			dup := false
			for _, d := range m.domainCategories[m.domainCatCursor].Domains {
				if d == domain {
					dup = true
					break
				}
			}
			if !dup {
				m.domainCategories[m.domainCatCursor].Domains = append(
					m.domainCategories[m.domainCatCursor].Domains, domain)
			}
		}
		m.domainInputMode = false
		m.domainInput = ""
	case "esc":
		m.domainInputMode = false
		m.domainInput = ""
	case "backspace", "ctrl+h":
		if len(m.domainInput) > 0 {
			m.domainInput = m.domainInput[:len(m.domainInput)-1]
		}
	default:
		s := key.String()
		for _, c := range s {
			isValid := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
				c == '.' || c == '-' || c == '_' || c == ':' || c == '*'
			if isValid {
				m.domainInput += string(c)
			}
		}
	}
	return m, nil
}

func (m Model) viewDomains() string {
	var b strings.Builder
	b.WriteString(m.stepTitle(8, "Network allowlist"))
	b.WriteString("\n\n")

	// Category tabs
	b.WriteString("  ")
	for i, cat := range m.domainCategories {
		label := cat.Name
		if i == m.domainCatCursor {
			b.WriteString(selectedStyle.Render("[" + label + "]"))
		} else {
			b.WriteString(dimStyle.Render(" " + label + " "))
		}
		if i < len(m.domainCategories)-1 {
			b.WriteString("  ")
		}
	}
	b.WriteString("\n\n")

	// Domains in selected category
	if m.domainCatCursor < len(m.domainCategories) {
		domains := m.domainCategories[m.domainCatCursor].Domains
		if len(domains) == 0 {
			b.WriteString(dimStyle.Render("    (no domains)\n"))
		}
		for i, d := range domains {
			cursor := "    "
			if !m.domainInputMode && m.domainItemCursor == i {
				cursor = cursorStyle.Render("  > ")
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, d))
		}
	}

	// Input mode
	if m.domainInputMode {
		b.WriteString(fmt.Sprintf("\n  Add domain: %s\n", selectedStyle.Render(m.domainInput+"█")))
	} else {
		b.WriteString(dimStyle.Render("\n  Press 'a' to add a domain\n"))
	}

	b.WriteString(helpStyle.Render("\nLeft/Right: category, a: add, d: delete, Enter: confirm, Esc: back" + m.tabHint()))
	return b.String()
}
