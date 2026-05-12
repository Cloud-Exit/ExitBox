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

package profile

import (
	"fmt"
	"os"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/fsutil"
)

const (
	ScopeGlobal    = "global"
	ScopeDirectory = "directory"
)

// ResolvedWorkspace wraps a workspace with its source scope.
type ResolvedWorkspace struct {
	Scope     string
	Workspace config.Workspace
}

// ResolveActiveWorkspace resolves the workspace used for this run.
// Resolution order:
//  1. overrideName (from --workspace flag)
//  2. Directory-scoped workspace matching projectDir
//  3. cfg.Settings.DefaultWorkspace
//  4. cfg.Workspaces.Active
//  5. First workspace in list
func ResolveActiveWorkspace(cfg *config.Config, projectDir string, overrideName string) (*ResolvedWorkspace, error) {
	if overrideName != "" {
		if w := findByName(cfg.Workspaces.Items, overrideName); w != nil {
			scope := ScopeGlobal
			if w.Directory != "" {
				scope = ScopeDirectory
			}
			return &ResolvedWorkspace{Scope: scope, Workspace: *w}, nil
		}
		return nil, fmt.Errorf("unknown workspace: %s", overrideName)
	}

	// Check directory-scoped workspaces
	if projectDir != "" {
		for _, w := range cfg.Workspaces.Items {
			if w.Directory != "" && w.Directory == projectDir {
				return &ResolvedWorkspace{Scope: ScopeDirectory, Workspace: w}, nil
			}
		}
	}

	if cfg.Settings.DefaultWorkspace != "" {
		if w := findByName(cfg.Workspaces.Items, cfg.Settings.DefaultWorkspace); w != nil {
			return &ResolvedWorkspace{Scope: ScopeGlobal, Workspace: *w}, nil
		}
	}

	if cfg.Workspaces.Active != "" {
		if w := findByName(cfg.Workspaces.Items, cfg.Workspaces.Active); w != nil {
			return &ResolvedWorkspace{Scope: ScopeGlobal, Workspace: *w}, nil
		}
	}

	if len(cfg.Workspaces.Items) > 0 {
		return &ResolvedWorkspace{Scope: ScopeGlobal, Workspace: cfg.Workspaces.Items[0]}, nil
	}

	return nil, nil
}

// ListWorkspaces returns all workspaces from the global config.
func ListWorkspaces(cfg *config.Config) []ResolvedWorkspace {
	var out []ResolvedWorkspace
	for _, w := range cfg.Workspaces.Items {
		scope := ScopeGlobal
		if w.Directory != "" {
			scope = ScopeDirectory
		}
		out = append(out, ResolvedWorkspace{Scope: scope, Workspace: w})
	}
	return out
}

// SetActiveWorkspace sets the active workspace in the global config.
// This only changes the current session's active workspace, NOT the
// default workspace. The default is only changed via the setup wizard
// or explicit CLI commands (e.g. `exitbox workspaces default <name>`).
func SetActiveWorkspace(name string, cfg *config.Config) error {
	w := findByName(cfg.Workspaces.Items, name)
	if w == nil {
		return fmt.Errorf("unknown workspace: %s", name)
	}
	cfg.Workspaces.Active = w.Name
	return config.SaveConfig(cfg)
}

// AddWorkspace adds a workspace to the global config.
// If dir is non-empty, it becomes a directory-scoped workspace.
func AddWorkspace(w config.Workspace, cfg *config.Config) error {
	if w.Name == "" {
		return fmt.Errorf("workspace name cannot be empty")
	}
	for _, dev := range w.Development {
		if !RoleExists(dev) {
			return &InvalidWorkspaceError{Name: dev}
		}
	}

	cfg.Workspaces.Items = upsertWorkspace(cfg.Workspaces.Items, w)
	return config.SaveConfig(cfg)
}

// RemoveWorkspace removes a workspace from the global config.
func RemoveWorkspace(name string, cfg *config.Config) error {
	cfg.Workspaces.Items = deleteByName(cfg.Workspaces.Items, name)
	if strings.EqualFold(cfg.Workspaces.Active, name) {
		cfg.Workspaces.Active = ""
	}
	if strings.EqualFold(cfg.Settings.DefaultWorkspace, name) {
		cfg.Settings.DefaultWorkspace = ""
	}
	return config.SaveConfig(cfg)
}

// WorkspaceAgentDir returns host path for workspace agent config.
func WorkspaceAgentDir(workspaceName, agent string) string {
	return config.WorkspaceAgentDir(workspaceName, agent)
}

// EnsureAgentConfig ensures agent config directories exist for active workspace.
func EnsureAgentConfig(workspaceName, agentName string) error {
	if workspaceName == "" {
		return nil
	}
	agt := agents.Get(agentName)
	if agt == nil {
		return nil
	}
	return agt.EnsureWorkspaceAgentConfig(workspaceName)
}

// CopyWorkspaceCredentials copies all agent credentials from one workspace to another.
// Only copies agents whose source directory exists and is non-empty.
func CopyWorkspaceCredentials(srcWorkspace, dstWorkspace string, agents []string) error {
	for _, agentName := range agents {
		srcDir := WorkspaceAgentDir(srcWorkspace, agentName)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			continue
		}
		entries, err := os.ReadDir(srcDir)
		if err != nil || len(entries) == 0 {
			continue
		}
		dstDir := WorkspaceAgentDir(dstWorkspace, agentName)
		_ = os.MkdirAll(dstDir, 0755)
		if err := fsutil.CopyDirRecursive(srcDir, dstDir); err != nil {
			return fmt.Errorf("copying %s credentials from '%s' to '%s': %w", agentName, srcWorkspace, dstWorkspace, err)
		}
	}
	return nil
}

func findByName(list []config.Workspace, name string) *config.Workspace {
	for i := range list {
		if strings.EqualFold(list[i].Name, name) {
			return &list[i]
		}
	}
	return nil
}

func upsertWorkspace(list []config.Workspace, w config.Workspace) []config.Workspace {
	for i := range list {
		if strings.EqualFold(list[i].Name, w.Name) {
			list[i] = w
			return list
		}
	}
	return append(list, w)
}

func deleteByName(list []config.Workspace, name string) []config.Workspace {
	var out []config.Workspace
	for _, w := range list {
		if !strings.EqualFold(w.Name, name) {
			out = append(out, w)
		}
	}
	return out
}

// FindWorkspace returns a workspace by name (case-insensitive), or nil.
func FindWorkspace(cfg *config.Config, name string) *config.Workspace {
	return findByName(cfg.Workspaces.Items, name)
}

// WorkspaceNames returns the names of all configured workspaces.
func WorkspaceNames(cfg *config.Config) []string {
	names := make([]string, len(cfg.Workspaces.Items))
	for i, w := range cfg.Workspaces.Items {
		names[i] = w.Name
	}
	return names
}

// InvalidWorkspaceError is returned when an invalid development profile is used.
type InvalidWorkspaceError struct {
	Name string
}

func (e *InvalidWorkspaceError) Error() string {
	return "unknown development profile: " + e.Name + ". Run 'exitbox setup' to configure your development stack."
}
