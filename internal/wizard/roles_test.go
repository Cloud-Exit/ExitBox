// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package wizard

import (
	"os"
	"path/filepath"
	"testing"
)

func TestComputeExternalToolPackages_Valid(t *testing.T) {
	pkgs := ComputeExternalToolPackages([]string{"Bun", "GitHub CLI"})
	want := []string{"nodejs", "npm", "github-cli"}
	if len(pkgs) != len(want) {
		t.Fatalf("ComputeExternalToolPackages(Bun, GitHub CLI) = %v, want %v", pkgs, want)
	}
	for i := range want {
		if pkgs[i] != want[i] {
			t.Fatalf("ComputeExternalToolPackages(Bun, GitHub CLI) = %v, want %v", pkgs, want)
		}
	}
}

func TestComputeExternalToolInstallSteps_Valid(t *testing.T) {
	steps := ComputeExternalToolInstallSteps([]string{"Bun", "GitHub CLI"})
	if len(steps) != 1 {
		t.Fatalf("ComputeExternalToolInstallSteps(Bun, GitHub CLI) = %v, want one step", steps)
	}
	if steps[0] != "RUN npm install -g bun\n" {
		t.Fatalf("ComputeExternalToolInstallSteps(Bun, GitHub CLI) = %q, want bun install step", steps[0])
	}
}

func TestComputeExternalToolInstallSteps_Unknown(t *testing.T) {
	steps := ComputeExternalToolInstallSteps([]string{"Unknown Tool"})
	if steps != nil {
		t.Errorf("ComputeExternalToolInstallSteps(Unknown Tool) = %v, want nil", steps)
	}
}

func TestComputeExternalToolPackages_Empty(t *testing.T) {
	pkgs := ComputeExternalToolPackages(nil)
	if pkgs != nil {
		t.Errorf("ComputeExternalToolPackages(nil) = %v, want nil", pkgs)
	}
}

func TestComputeExternalToolPackages_Unknown(t *testing.T) {
	pkgs := ComputeExternalToolPackages([]string{"Unknown Tool"})
	if pkgs != nil {
		t.Errorf("ComputeExternalToolPackages(Unknown Tool) = %v, want nil", pkgs)
	}
}

func TestDetectExternalToolConfigs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	// Create the .config/gh directory to simulate GitHub CLI installed.
	ghDir := filepath.Join(tmpHome, ".config", "gh")
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}

	detected := DetectExternalToolConfigs()
	if detected == nil {
		t.Fatal("DetectExternalToolConfigs() returned nil, expected map with GitHub CLI")
	}
	paths, ok := detected["GitHub CLI"]
	if !ok || len(paths) == 0 {
		t.Errorf("expected GitHub CLI to be detected, got %v", detected)
	}
	if paths[0] != ".config/gh" {
		t.Errorf("expected .config/gh, got %q", paths[0])
	}
}

func TestDetectExternalToolConfigs_NoHome(t *testing.T) {
	t.Setenv("HOME", "")
	detected := DetectExternalToolConfigs()
	if detected != nil {
		t.Errorf("DetectExternalToolConfigs() with empty HOME = %v, want nil", detected)
	}
}

func TestDetectExternalToolConfigs_NothingDetected(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	detected := DetectExternalToolConfigs()
	if detected != nil {
		t.Errorf("DetectExternalToolConfigs() with empty home = %v, want nil", detected)
	}
}

func TestAIDeveloperRole(t *testing.T) {
	role := GetRole("AI Developer")
	if role == nil {
		t.Fatal("AI Developer role not found")
	}

	// Verify python comes before ml so the venv exists when ml's pip install runs.
	pythonIdx, mlIdx := -1, -1
	for i, p := range role.Roles {
		if p == "python" {
			pythonIdx = i
		}
		if p == "ml" {
			mlIdx = i
		}
	}
	if pythonIdx < 0 {
		t.Error("AI Developer role must include 'python' profile")
	}
	if mlIdx < 0 {
		t.Error("AI Developer role must include 'ml' profile")
	}
	if pythonIdx >= 0 && mlIdx >= 0 && pythonIdx >= mlIdx {
		t.Errorf("python profile (%d) must come before ml profile (%d)", pythonIdx, mlIdx)
	}
}

func TestComputeRoles_AIDeveloper(t *testing.T) {
	profiles := ComputeRoles([]string{"AI Developer"}, nil)
	wantContains := []string{"python", "ml", "build-tools"}
	for _, want := range wantContains {
		found := false
		for _, p := range profiles {
			if p == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("ComputeRoles(AI Developer) missing %q, got %v", want, profiles)
		}
	}
}
