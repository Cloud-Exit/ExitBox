// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package profile

import (
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestResolveActiveWorkspace_Override(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Active: "personal",
			Items: []config.Workspace{
				{Name: "personal", Development: []string{"python"}},
				{Name: "work", Development: []string{"go"}},
			},
		},
	}

	active, err := ResolveActiveWorkspace(cfg, "/some/dir", "work")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil {
		t.Fatal("expected active workspace")
	}
	if active.Workspace.Name != "work" {
		t.Fatalf("expected work, got %s", active.Workspace.Name)
	}
}

func TestResolveActiveWorkspace_OverrideNotFound(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Items: []config.Workspace{
				{Name: "personal"},
			},
		},
	}

	_, err := ResolveActiveWorkspace(cfg, "/some/dir", "nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown workspace override")
	}
}

func TestResolveActiveWorkspace_DirectoryScoped(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Active: "personal",
			Items: []config.Workspace{
				{Name: "personal", Development: []string{"python"}},
				{Name: "project-x", Development: []string{"go"}, Directory: "/home/user/project-x"},
			},
		},
	}

	active, err := ResolveActiveWorkspace(cfg, "/home/user/project-x", "")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil {
		t.Fatal("expected active workspace")
	}
	if active.Scope != ScopeDirectory || active.Workspace.Name != "project-x" {
		t.Fatalf("expected directory/project-x, got %s/%s", active.Scope, active.Workspace.Name)
	}
}

func TestResolveActiveWorkspace_DefaultFallback(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Items: []config.Workspace{
				{Name: "personal"},
				{Name: "work"},
			},
		},
		Settings: config.SettingsConfig{
			DefaultWorkspace: "work",
		},
	}

	active, err := ResolveActiveWorkspace(cfg, "/some/dir", "")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil {
		t.Fatal("expected active workspace")
	}
	if active.Workspace.Name != "work" {
		t.Fatalf("expected work, got %s", active.Workspace.Name)
	}
}

func TestResolveActiveWorkspace_FirstFallback(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Items: []config.Workspace{
				{Name: "first"},
				{Name: "second"},
			},
		},
	}

	active, err := ResolveActiveWorkspace(cfg, "/some/dir", "")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil {
		t.Fatal("expected active workspace")
	}
	if active.Workspace.Name != "first" {
		t.Fatalf("expected first, got %s", active.Workspace.Name)
	}
}

func TestResolveActiveWorkspace_Empty(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{},
	}

	active, err := ResolveActiveWorkspace(cfg, "/some/dir", "")
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Fatalf("expected nil, got %+v", active)
	}
}

func TestResolveActiveWorkspace_OverrideCaseInsensitive(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Items: []config.Workspace{
				{Name: "Personal", Development: []string{"python"}},
				{Name: "work", Development: []string{"go"}},
			},
		},
	}

	// "personal" should match "Personal"
	active, err := ResolveActiveWorkspace(cfg, "/some/dir", "personal")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil {
		t.Fatal("expected active workspace")
	}
	if active.Workspace.Name != "Personal" {
		t.Fatalf("expected canonical name 'Personal', got %s", active.Workspace.Name)
	}

	// "WORK" should match "work"
	active, err = ResolveActiveWorkspace(cfg, "/some/dir", "WORK")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil {
		t.Fatal("expected active workspace")
	}
	if active.Workspace.Name != "work" {
		t.Fatalf("expected canonical name 'work', got %s", active.Workspace.Name)
	}
}

func TestResolveActiveWorkspace_DefaultFallbackCaseInsensitive(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Items: []config.Workspace{
				{Name: "personal"},
				{Name: "Work"},
			},
		},
		Settings: config.SettingsConfig{
			DefaultWorkspace: "work", // lowercase, stored name is "Work"
		},
	}

	active, err := ResolveActiveWorkspace(cfg, "/some/dir", "")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil {
		t.Fatal("expected active workspace")
	}
	if active.Workspace.Name != "Work" {
		t.Fatalf("expected canonical name 'Work', got %s", active.Workspace.Name)
	}
}

func TestFindWorkspace(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Items: []config.Workspace{
				{Name: "MyWorkspace"},
			},
		},
	}

	// Case-insensitive lookup
	if w := FindWorkspace(cfg, "myworkspace"); w == nil {
		t.Fatal("expected to find workspace")
	} else if w.Name != "MyWorkspace" {
		t.Fatalf("expected canonical name 'MyWorkspace', got %s", w.Name)
	}

	// Non-existent
	if w := FindWorkspace(cfg, "nonexistent"); w != nil {
		t.Fatalf("expected nil, got %+v", w)
	}
}

func TestWorkspaceNames(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Items: []config.Workspace{
				{Name: "personal"},
				{Name: "work"},
			},
		},
	}

	names := WorkspaceNames(cfg)
	if len(names) != 2 {
		t.Fatalf("expected 2 names, got %d", len(names))
	}
	if names[0] != "personal" || names[1] != "work" {
		t.Fatalf("unexpected names: %v", names)
	}
}

func TestResolveActiveWorkspace_ActiveFallback(t *testing.T) {
	cfg := &config.Config{
		Workspaces: config.WorkspaceCatalog{
			Active: "second",
			Items: []config.Workspace{
				{Name: "first"},
				{Name: "second"},
			},
		},
	}

	active, err := ResolveActiveWorkspace(cfg, "/some/dir", "")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil {
		t.Fatal("expected active workspace")
	}
	if active.Workspace.Name != "second" {
		t.Fatalf("expected second, got %s", active.Workspace.Name)
	}
}

// The base image builds a pinned tmux from source; a profile installing the
// distro package would add a second, older tmux to project images.
func TestAllRoles_NoDistroTmux(t *testing.T) {
	for _, r := range AllRoles() {
		for _, pkg := range strings.Fields(r.Packages) {
			if pkg == "tmux" {
				t.Errorf("profile %q installs the distro tmux package", r.Name)
			}
		}
	}
}
