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
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/config"
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
func SetActiveWorkspace(name string, cfg *config.Config) error {
	if findByName(cfg.Workspaces.Items, name) == nil {
		return fmt.Errorf("unknown workspace: %s", name)
	}
	cfg.Workspaces.Active = name
	cfg.Settings.DefaultWorkspace = name
	return config.SaveConfig(cfg)
}

// AddWorkspace adds a workspace to the global config.
// If dir is non-empty, it becomes a directory-scoped workspace.
func AddWorkspace(w config.Workspace, cfg *config.Config) error {
	if w.Name == "" {
		return fmt.Errorf("workspace name cannot be empty")
	}
	for _, dev := range w.Development {
		if !Exists(dev) {
			return &InvalidWorkspaceError{Name: dev}
		}
	}

	cfg.Workspaces.Items = upsertWorkspace(cfg.Workspaces.Items, w)
	return config.SaveConfig(cfg)
}

// RemoveWorkspace removes a workspace from the global config.
func RemoveWorkspace(name string, cfg *config.Config) error {
	cfg.Workspaces.Items = deleteByName(cfg.Workspaces.Items, name)
	if cfg.Workspaces.Active == name {
		cfg.Workspaces.Active = ""
	}
	if cfg.Settings.DefaultWorkspace == name {
		cfg.Settings.DefaultWorkspace = ""
	}
	return config.SaveConfig(cfg)
}

// WorkspaceAgentDir returns host path for workspace agent config.
func WorkspaceAgentDir(workspaceName, agent string) string {
	return filepath.Join(config.Home, "profiles", ScopeGlobal, workspaceName, agent)
}

// EnsureAgentConfig ensures agent config directories exist for active workspace.
func EnsureAgentConfig(workspaceName, agent string) error {
	if workspaceName == "" {
		return nil
	}
	root := WorkspaceAgentDir(workspaceName, agent)
	_ = os.MkdirAll(root, 0755)

	home := os.Getenv("HOME")
	switch agent {
	case "claude":
		claudeDir := ensureDir(root, ".claude")
		seedDirOnce(filepath.Join(home, ".claude"), claudeDir)

		claudeJSON := ensureFile(root, ".claude.json")
		seedFileOnce(filepath.Join(home, ".claude.json"), claudeJSON)

		cfgDir := ensureDir(root, ".config")
		seedDirOnce(filepath.Join(home, ".config"), cfgDir)
	case "codex":
		codexDir := ensureDir(root, ".codex")
		seedDirOnce(filepath.Join(home, ".codex"), codexDir)

		codexCfg := ensureDir(root, ".config", "codex")
		seedDirOnce(filepath.Join(home, ".config", "codex"), codexCfg)
	case "opencode":
		ocDir := ensureDir(root, ".opencode")
		seedDirOnce(filepath.Join(home, ".opencode"), ocDir)

		ocCfg := ensureDir(root, ".config", "opencode")
		seedDirOnce(filepath.Join(home, ".config", "opencode"), ocCfg)

		ocShare := ensureDir(root, ".local", "share", "opencode")
		seedDirOnce(filepath.Join(home, ".local", "share", "opencode"), ocShare)

		ocState := ensureDir(root, ".local", "state")
		seedDirOnce(filepath.Join(home, ".local", "state"), ocState)

		ocCache := ensureDir(root, ".cache", "opencode")
		seedDirOnce(filepath.Join(home, ".cache", "opencode"), ocCache)
	}
	return nil
}

func findByName(list []config.Workspace, name string) *config.Workspace {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

func upsertWorkspace(list []config.Workspace, w config.Workspace) []config.Workspace {
	for i := range list {
		if list[i].Name == w.Name {
			list[i] = w
			return list
		}
	}
	return append(list, w)
}

func deleteByName(list []config.Workspace, name string) []config.Workspace {
	var out []config.Workspace
	for _, w := range list {
		if w.Name != name {
			out = append(out, w)
		}
	}
	return out
}

func ensureDir(parts ...string) string {
	p := filepath.Join(parts...)
	_ = os.MkdirAll(p, 0755)
	return p
}

func ensureFile(parts ...string) string {
	p := filepath.Join(parts...)
	_ = os.MkdirAll(filepath.Dir(p), 0755)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		_ = os.WriteFile(p, []byte("{}\n"), 0644)
	}
	return p
}

func seedDirOnce(host, managed string) {
	if _, err := os.Stat(host); os.IsNotExist(err) {
		return
	}
	entries, err := os.ReadDir(managed)
	if err == nil && len(entries) > 0 {
		return
	}
	_ = os.MkdirAll(managed, 0755)
	_ = copyDirRecursive(host, managed)
}

func copyDirRecursive(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func seedFileOnce(host, managed string) {
	if _, err := os.Stat(host); os.IsNotExist(err) {
		return
	}
	if _, err := os.Stat(managed); err == nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(managed), 0755)
	data, err := os.ReadFile(host)
	if err == nil {
		_ = os.WriteFile(managed, data, 0644)
	}
}

// InvalidWorkspaceError is returned when an invalid development profile is used.
type InvalidWorkspaceError struct {
	Name string
}

func (e *InvalidWorkspaceError) Error() string {
	return "unknown development profile: " + e.Name + ". Run 'exitbox setup' to configure your development stack."
}
