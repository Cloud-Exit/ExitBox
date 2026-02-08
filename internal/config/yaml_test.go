package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateProfilesToWorkspaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write a config using the OLD "profiles" / "default_profile" keys.
	oldConfig := `version: 1
roles:
  - fullstack
profiles:
  active: work
  items:
    - name: work
      development:
        - go
        - python
    - name: personal
      development:
        - node
agents:
  claude:
    enabled: true
  codex:
    enabled: false
  opencode:
    enabled: false
settings:
  auto_update: false
  status_bar: true
  default_profile: work
`
	if err := os.WriteFile(path, []byte(oldConfig), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	// Workspaces should be populated from the old profiles key.
	if len(cfg.Workspaces.Items) != 2 {
		t.Fatalf("expected 2 workspaces, got %d", len(cfg.Workspaces.Items))
	}
	if cfg.Workspaces.Active != "work" {
		t.Fatalf("expected active workspace 'work', got %q", cfg.Workspaces.Active)
	}
	if cfg.Workspaces.Items[0].Name != "work" {
		t.Fatalf("expected first workspace 'work', got %q", cfg.Workspaces.Items[0].Name)
	}
	if len(cfg.Workspaces.Items[0].Development) != 2 {
		t.Fatalf("expected 2 dev profiles for work, got %d", len(cfg.Workspaces.Items[0].Development))
	}
	if cfg.Settings.DefaultWorkspace != "work" {
		t.Fatalf("expected default workspace 'work', got %q", cfg.Settings.DefaultWorkspace)
	}
}

func TestNewConfigUsesWorkspacesKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	// Write a config using the NEW "workspaces" / "default_workspace" keys.
	newConfig := `version: 1
workspaces:
  active: myws
  items:
    - name: myws
      development:
        - rust
settings:
  default_workspace: myws
agents:
  claude:
    enabled: true
  codex:
    enabled: false
  opencode:
    enabled: false
`
	if err := os.WriteFile(path, []byte(newConfig), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfigFrom(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(cfg.Workspaces.Items) != 1 {
		t.Fatalf("expected 1 workspace, got %d", len(cfg.Workspaces.Items))
	}
	if cfg.Workspaces.Items[0].Name != "myws" {
		t.Fatalf("expected workspace 'myws', got %q", cfg.Workspaces.Items[0].Name)
	}
	if cfg.Settings.DefaultWorkspace != "myws" {
		t.Fatalf("expected default workspace 'myws', got %q", cfg.Settings.DefaultWorkspace)
	}
}
