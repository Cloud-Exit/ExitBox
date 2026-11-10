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

package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/subagents"
)

// withSubagentsHome points ExitBox at an empty config home with Claude as
// the only enabled agent, so commands without --agent work on Claude.
func withSubagentsHome(t *testing.T) {
	t.Helper()
	old := config.Home
	config.Home = t.TempDir()
	t.Cleanup(func() { config.Home = old })
	enableAgents(t, "claude")
	oldEditor := runEditor
	runEditor = func(string) error { return nil }
	t.Cleanup(func() { runEditor = oldEditor })
}

// enableAgents writes a config in which exactly these agents are enabled.
func enableAgents(t *testing.T, names ...string) {
	t.Helper()
	cfg := config.DefaultConfig()
	cfg.Agents = config.AgentConfig{}
	for _, n := range names {
		cfg.SetAgentEnabled(n, true)
	}
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

// withHostAgents makes every agent's host agents directory a temp dir.
func withHostAgents(t *testing.T) string {
	t.Helper()
	host := t.TempDir()
	old := hostAgentsDir
	hostAgentsDir = func(h *subagents.Harness) (string, error) { return filepath.Join(host, h.Agent), nil }
	t.Cleanup(func() { hostAgentsDir = old })
	return host
}

func runSubagents(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	c := newSubagentsCmd()
	var out, errb bytes.Buffer
	c.SetOut(&out)
	c.SetErr(&errb)
	c.SetArgs(args)
	err := c.Execute()
	return out.String(), errb.String(), err
}

func writeAgent(t *testing.T, path, name, desc string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := "---\nname: " + name + "\ndescription: " + desc + "\n---\nPrompt.\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSubagentsNew(t *testing.T) {
	withSubagentsHome(t)
	var edited string
	runEditor = func(p string) error { edited = p; return nil }

	out, _, err := runSubagents(t, "new", "code-reviewer", "-w", "work", "--model", "haiku", "--tools", "Read, Grep", "--description", "Reviews: diffs")
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	want := filepath.Join(subagents.Claude.StoreDir("work"), "code-reviewer.md")
	if edited != want || !strings.Contains(out, want) {
		t.Errorf("edited %q, out %q, want %q", edited, out, want)
	}
	e, err := subagents.Claude.Find("work", "code-reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Findings) != 0 || e.Def.Model != "haiku" || e.Def.Description != "Reviews: diffs" || len(e.Def.Tools) != 2 {
		t.Errorf("scaffold = %+v, findings %v", e.Def, e.Findings)
	}

	if _, _, err := runSubagents(t, "new", "code-reviewer", "-w", "work"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second new: err = %v, want already exists", err)
	}
}

func TestSubagentsNew_RejectsBadName(t *testing.T) {
	withSubagentsHome(t)
	if _, _, err := runSubagents(t, "new", "a:b", "-w", "work"); err == nil {
		t.Fatal("expected error for name with ':'")
	}
	if entries, _ := subagents.Claude.ListStore("work"); len(entries) != 0 {
		t.Errorf("invalid scaffold stored: %v", entries)
	}
}

func TestSubagentsNew_EditorBreaksFile(t *testing.T) {
	withSubagentsHome(t)
	runEditor = func(p string) error { return os.WriteFile(p, []byte("---\nname: x\n---\n"), 0o644) }
	_, errOut, err := runSubagents(t, "new", "x", "-w", "work")
	if err == nil || !strings.Contains(errOut, "description") {
		t.Errorf("err = %v, stderr = %q; want failure naming description", err, errOut)
	}
}

func TestSubagentsAdd(t *testing.T) {
	withSubagentsHome(t)
	src := t.TempDir()
	writeAgent(t, filepath.Join(src, "one.md"), "one", "first")
	writeAgent(t, filepath.Join(src, "team", "two.md"), "two", "second")
	if err := os.WriteFile(filepath.Join(src, "README.md"), []byte("# docs\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _, err := runSubagents(t, "add", src, "-w", "work")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if !strings.Contains(out, "Added 2") || !strings.Contains(out, "skipped 1") || !strings.Contains(out, "next container start") {
		t.Errorf("out = %q", out)
	}

	// Same names again: refused without --force, replaced with it.
	if _, _, err := runSubagents(t, "add", filepath.Join(src, "one.md"), "-w", "work"); err == nil {
		t.Error("re-adding without --force should fail")
	}
	if _, _, err := runSubagents(t, "add", filepath.Join(src, "one.md"), "-w", "work", "--force"); err != nil {
		t.Errorf("--force: %v", err)
	}
	if entries, _ := subagents.Claude.ListStore("work"); len(entries) != 2 {
		t.Errorf("entries = %d, want 2", len(entries))
	}
}

func TestSubagentsAdd_InvalidFileFails(t *testing.T) {
	withSubagentsHome(t)
	src := filepath.Join(t.TempDir(), "bad.md")
	if err := os.WriteFile(src, []byte("---\nname: bad\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, errOut, err := runSubagents(t, "add", src, "-w", "work"); err == nil || !strings.Contains(errOut, "description") {
		t.Errorf("err = %v, stderr = %q", err, errOut)
	}
}

func TestSubagentsImport(t *testing.T) {
	withSubagentsHome(t)
	host := filepath.Join(withHostAgents(t), "claude")

	writeAgent(t, filepath.Join(host, "a.md"), "a", "from host")
	writeAgent(t, filepath.Join(host, "nested", "b.md"), "b", "from host")
	if _, err := subagents.Claude.Save("work", "", []byte("---\nname: a\ndescription: mine\n---\n"), subagents.SourceManaged, "", false); err != nil {
		t.Fatal(err)
	}

	out, _, err := runSubagents(t, "import", "-w", "work")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if !strings.Contains(out, "Imported 1") || !strings.Contains(out, "skipped 1") {
		t.Errorf("out = %q", out)
	}
	if e, _ := subagents.Claude.Find("work", "a"); e.Def.Description != "mine" {
		t.Errorf("existing name overwritten without --force")
	}
	b, err := subagents.Claude.Find("work", "b")
	if err != nil || b.Source != subagents.SourceImported {
		t.Errorf("b = %+v, err = %v", b, err)
	}

	if _, _, err := runSubagents(t, "import", "-w", "work", "--force"); err != nil {
		t.Fatalf("import --force: %v", err)
	}
	if e, _ := subagents.Claude.Find("work", "a"); e.Def.Description != "from host" || e.Source != subagents.SourceImported {
		t.Errorf("--force did not replace: %+v", e)
	}
}

func TestSubagentsRm(t *testing.T) {
	withSubagentsHome(t)
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "file.md"), "by-name", "d")
	if _, _, err := runSubagents(t, "rm", "missing", "-w", "work"); err == nil {
		t.Error("rm missing should fail")
	}
	out, _, err := runSubagents(t, "rm", "by-name", "-w", "work")
	if err != nil || !strings.Contains(out, "subagent by-name (file.md)") {
		t.Errorf("rm: out = %q, err = %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(subagents.Claude.StoreDir("work"), "file.md")); !os.IsNotExist(err) {
		t.Errorf("file still present")
	}
}

func TestSubagentsValidate_ExitCodes(t *testing.T) {
	withSubagentsHome(t)
	dir := subagents.Claude.StoreDir("work")
	writeAgent(t, filepath.Join(dir, "good.md"), "good", "d")

	out, _, err := runSubagents(t, "validate", "-w", "work")
	if err != nil || !strings.Contains(out, "good (good.md): ok") {
		t.Fatalf("clean store: out = %q, err = %v", out, err)
	}

	// Warnings alone do not fail.
	if err := os.WriteFile(filepath.Join(dir, "warn.md"), []byte("---\nname: warn\ndescription: d\ntoolz: Read\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _, err := runSubagents(t, "validate", "-w", "work"); err != nil || !strings.Contains(out, `did you mean "tools"`) {
		t.Fatalf("warnings: out = %q, err = %v", out, err)
	}

	// A duplicate name is an error, also when validating one subagent.
	writeAgent(t, filepath.Join(dir, "copy.md"), "good", "d")
	if _, _, err := runSubagents(t, "validate", "-w", "work"); err == nil {
		t.Error("duplicate names should fail validation")
	}
	if out, _, err := runSubagents(t, "validate", "warn", "-w", "work"); err != nil {
		t.Errorf("validate warn: out = %q, err = %v", out, err)
	}
	if out, _, err := runSubagents(t, "validate", "copy", "-w", "work"); err == nil || !strings.Contains(out, "also used by") {
		t.Errorf("validate copy: out = %q, err = %v", out, err)
	}
}

func TestSubagentsList_JSON(t *testing.T) {
	withSubagentsHome(t)
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "r.md"), "r", "d")
	writeAgent(t, filepath.Join(subagents.Claude.ContainerAgentsDir("work"), "r.md"), "container-r", "d")

	out, _, err := runSubagents(t, "list", "-w", "work", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Workspace string `json:"workspace"`
		Subagents []struct {
			Name       string `json:"name"`
			Source     string `json:"source"`
			Status     string `json:"status"`
			ShadowedBy string `json:"shadowed_by"`
		} `json:"subagents"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("bad JSON %q: %v", out, err)
	}
	if got.Workspace != "work" || len(got.Subagents) != 2 {
		t.Fatalf("got = %+v", got)
	}
	if s := got.Subagents[0]; s.Name != "r" || s.Source != "managed" || s.Status != "ok" || !strings.Contains(s.ShadowedBy, "the managed one is skipped") {
		t.Errorf("store entry = %+v", s)
	}
	if s := got.Subagents[1]; s.Name != "container-r" || s.Source != "container" {
		t.Errorf("container entry = %+v", s)
	}
}

func TestSubagentsList_Empty(t *testing.T) {
	withSubagentsHome(t)
	out, _, err := runSubagents(t, "list", "-w", "work")
	if err != nil || !strings.Contains(out, "No subagents") {
		t.Errorf("out = %q, err = %v", out, err)
	}
}

func TestSubagents_RejectsTraversalWorkspace(t *testing.T) {
	withSubagentsHome(t)
	if _, _, err := runSubagents(t, "list", "-w", "../escape"); err == nil {
		t.Error("expected invalid workspace error")
	}
}

func TestSubagentsShow(t *testing.T) {
	withSubagentsHome(t)
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "x.md"), "shown", "d")
	out, _, err := runSubagents(t, "show", "shown", "-w", "work")
	if err != nil || !strings.Contains(out, "name: shown") {
		t.Errorf("out = %q, err = %v", out, err)
	}
}
