// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package profile

import (
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
