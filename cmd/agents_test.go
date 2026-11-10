// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/subagents"
)

func TestAgentsMdPath(t *testing.T) {
	p := agentsMdPath("personal")
	expected := filepath.Join(config.Home, "profiles", "global", "personal", "agents.md")
	if p != expected {
		t.Errorf("agentsMdPath(personal) = %q, want %q", p, expected)
	}
}

func TestAgentsMdPath_DefaultWorkspace(t *testing.T) {
	p := agentsMdPath("default")
	if !strings.HasSuffix(p, filepath.Join("global", "default", "agents.md")) {
		t.Errorf("agentsMdPath(default) = %q, expected to end with global/default/agents.md", p)
	}
}

func runAgents(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	c := newAgentsCmd()
	var out, errb bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errb)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), errb.String(), err
}

func TestAgentsSkillEdit_CreatesWithRoutesAndKeepsEdits(t *testing.T) {
	withSubagentsHome(t)
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "reviewer.md"), "reviewer", "Reviews diffs")
	var edited string
	runEditor = func(p string) error { edited = p; return nil }

	out, _, err := runAgents(t, "skill", "edit", "-w", "work")
	if err != nil {
		t.Fatalf("skill edit: %v", err)
	}
	want := subagents.Claude.MasterSkillPath("work")
	if edited != want || !strings.Contains(out, "Created "+want) {
		t.Errorf("edited %q, out %q, want %q created", edited, out, want)
	}
	shown, _, err := runAgents(t, "skill", "show", "-w", "work")
	if err != nil || !strings.Contains(shown, "`reviewer`") {
		t.Errorf("show = %q, err %v; want a route to reviewer", shown, err)
	}

	// A second edit opens the same file and must not recreate it.
	runEditor = func(p string) error {
		return os.WriteFile(p, []byte("---\nname: master-skill\ndescription: mine\n---\nMy rules.\n"), 0o644)
	}
	out, _, err = runAgents(t, "skill", "edit", "-w", "work")
	if err != nil || strings.Contains(out, "Created") {
		t.Errorf("second edit: out %q, err %v", out, err)
	}
	if shown, _, _ := runAgents(t, "skill", "show", "-w", "work"); !strings.Contains(shown, "My rules.") {
		t.Errorf("edits lost: %q", shown)
	}
}

func TestAgentsSkillEdit_EditorBreaksFile(t *testing.T) {
	withSubagentsHome(t)
	runEditor = func(p string) error { return os.WriteFile(p, []byte("no frontmatter\n"), 0o644) }
	_, errOut, err := runAgents(t, "skill", "edit", "-w", "work")
	if err == nil || !strings.Contains(errOut, "line 1") {
		t.Errorf("err = %v, stderr = %q; want failure naming the missing frontmatter", err, errOut)
	}
}

func TestAgentsSkillShowPathRm(t *testing.T) {
	withSubagentsHome(t)
	out, _, err := runAgents(t, "skill", "show", "-w", "work")
	if err != nil || !strings.Contains(out, "master skill in workspace") {
		t.Errorf("show without a skill: out %q, err %v", out, err)
	}
	if _, _, err := runAgents(t, "skill", "rm", "-w", "work"); err == nil {
		t.Error("rm without a skill should fail")
	}
	if out, _, _ := runAgents(t, "skill", "path", "-w", "work"); strings.TrimSpace(out) != subagents.Claude.MasterSkillPath("work") {
		t.Errorf("path = %q", out)
	}
	if subagents.Claude.MasterSkillExists("work") {
		t.Fatal("show, rm or path created the master skill")
	}

	if _, _, err := runAgents(t, "skill", "edit", "-w", "work"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runAgents(t, "skill", "rm", "-w", "work"); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if subagents.Claude.MasterSkillExists("work") {
		t.Error("master skill still exists after rm")
	}
}

func TestAgentsSkill_RejectsBadWorkspace(t *testing.T) {
	withSubagentsHome(t)
	if _, _, err := runAgents(t, "skill", "edit", "-w", "../escape"); err == nil {
		t.Error("expected an error for a workspace name that leaves profiles/global")
	}
}

func TestAgentsSubagents_SameCommandsAsTopLevel(t *testing.T) {
	withSubagentsHome(t)
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "reviewer.md"), "reviewer", "Reviews diffs")
	out, _, err := runAgents(t, "subagents", "list", "-w", "work")
	if err != nil || !strings.Contains(out, "reviewer") {
		t.Errorf("agents subagents list = %q, err %v", out, err)
	}
}

func TestSubagentsValidate_ChecksMasterSkill(t *testing.T) {
	withSubagentsHome(t)
	if _, _, err := runAgents(t, "skill", "edit", "-w", "work"); err != nil {
		t.Fatal(err)
	}
	out, _, err := runSubagents(t, "validate", "-w", "work")
	if err != nil || !strings.Contains(out, "master skill: ok") {
		t.Errorf("validate = %q, err %v", out, err)
	}
	if err := os.WriteFile(subagents.Claude.MasterSkillPath("work"), []byte("---\nname: master-skill\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err = runSubagents(t, "validate", "-w", "work")
	if err == nil || !strings.Contains(out, "description") {
		t.Errorf("validate with a broken master skill = %q, err %v; want an error naming description", out, err)
	}
}
