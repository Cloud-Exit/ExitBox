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

package subagents

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func withHome(t *testing.T) string {
	t.Helper()
	old := config.Home
	config.Home = t.TempDir()
	t.Cleanup(func() { config.Home = old })
	return config.Home
}

func agentFile(name, desc string) []byte {
	return []byte("---\nname: " + name + "\ndescription: " + desc + "\n---\nPrompt.\n")
}

func TestSave_WritesNameFile(t *testing.T) {
	withHome(t)
	path, err := Claude.Save("work", "", agentFile("code-reviewer", "Reviews"), SourceManaged, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "code-reviewer.md" || filepath.Dir(path) != Claude.StoreDir("work") {
		t.Errorf("path = %s", path)
	}
	entries, err := Claude.ListStore("work")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "code-reviewer" || entries[0].Source != SourceManaged {
		t.Errorf("entries = %+v", entries)
	}
}

func TestSave_RefusesExistingName(t *testing.T) {
	withHome(t)
	if _, err := Claude.Save("w", "", agentFile("r", "one"), SourceManaged, "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := Claude.Save("w", "", agentFile("r", "two"), SourceManaged, "", false); !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	path, err := Claude.Save("w", "", agentFile("r", "two"), SourceManaged, "", true)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "two") {
		t.Errorf("force did not replace: %s", data)
	}
	entries, err := Claude.ListStore("w")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("force created a second file: %d entries", len(entries))
	}
}

func TestSave_ForceKeepsExistingFileName(t *testing.T) {
	withHome(t)
	dir := Claude.StoreDir("w")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "legacy.md"), agentFile("r", "old"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, err := Claude.Save("w", "", agentFile("r", "new"), SourceManaged, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "legacy.md" {
		t.Errorf("path = %s, want the existing legacy.md replaced", path)
	}
}

func TestSave_RenamesOnFileCollision(t *testing.T) {
	withHome(t)
	p1, err := Claude.Save("w", "", agentFile("a b", "one"), SourceManaged, "", false)
	if err != nil {
		t.Fatal(err)
	}
	p2, err := Claude.Save("w", "", agentFile("a-b", "two"), SourceManaged, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p1) != "a-b.md" || filepath.Base(p2) != "a-b-2.md" {
		t.Errorf("paths = %s, %s", p1, p2)
	}
}

func TestSave_RejectsInvalid(t *testing.T) {
	withHome(t)
	_, err := Claude.Save("w", "", []byte("---\nname: r\n---\n"), SourceManaged, "", false)
	var inv *InvalidError
	if !errors.As(err, &inv) || !strings.Contains(err.Error(), "description") {
		t.Fatalf("err = %v, want InvalidError about description", err)
	}
	if _, statErr := os.Stat(filepath.Join(Claude.StoreDir("w"), "r.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("invalid file was written")
	}
}

func TestSave_RecordsSource(t *testing.T) {
	withHome(t)
	if _, err := Claude.Save("w", "", agentFile("imp", "d"), SourceImported, "/home/u/.claude/agents/imp.md", false); err != nil {
		t.Fatal(err)
	}
	e, err := Claude.Find("w", "imp")
	if err != nil {
		t.Fatal(err)
	}
	if e.Source != SourceImported || e.Origin != "/home/u/.claude/agents/imp.md" {
		t.Errorf("entry = %+v", e)
	}
	if _, err := Claude.Remove("w", "imp"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(Claude.StoreDir("w"), manifestFile)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("manifest should be removed with its last record")
	}
}

func TestFindAndRemove_ByNameNotFile(t *testing.T) {
	withHome(t)
	dir := Claude.StoreDir("w")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file-name.md"), agentFile("real-name", "d"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.md"), []byte("no frontmatter"), 0o644); err != nil {
		t.Fatal(err)
	}
	if e, err := Claude.Find("w", "real-name"); err != nil || e.File != "file-name.md" {
		t.Errorf("Claude.Find(real-name) = %+v, %v", e, err)
	}
	if e, err := Claude.Find("w", "broken"); err != nil || e.File != "broken.md" {
		t.Errorf("Claude.Find(broken) by file stem = %+v, %v", e, err)
	}
	if _, err := Claude.Find("w", "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Claude.Find(missing) err = %v", err)
	}
	if _, err := Claude.Remove("w", "real-name"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "file-name.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("file not removed")
	}
}

func TestListStore_FlagsDuplicatesAndSkipsOthers(t *testing.T) {
	withHome(t)
	dir := Claude.StoreDir("w")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"one.md":     agentFile("dup", "a"),
		"two.md":     agentFile("dup", "b"),
		"notes.txt":  []byte("x"),
		".hidden.md": agentFile("hidden", "x"),
		"sub/x.md":   agentFile("nested", "x"),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Claude.ListStore("w")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v, want one.md and two.md only", entries)
	}
	for _, e := range entries {
		if !HasErrors(e.Findings) {
			t.Errorf("%s: duplicate not flagged", e.File)
		}
	}
}

func TestListStore_MissingDir(t *testing.T) {
	withHome(t)
	entries, err := Claude.ListStore("nope")
	if err != nil || entries != nil {
		t.Errorf("entries = %v, err = %v", entries, err)
	}
}

func TestInventory_Shadowing(t *testing.T) {
	withHome(t)
	for _, n := range []string{"same-file", "same-name", "project-wins", "clean"} {
		if _, err := Claude.Save("w", "", agentFile(n, "d"), SourceManaged, "", false); err != nil {
			t.Fatal(err)
		}
	}
	cdir := Claude.ContainerAgentsDir("w")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(cdir, "same-file.md"), agentFile("something-else", "d"))
	mustWrite(t, filepath.Join(cdir, "other.md"), agentFile("same-name", "d"))
	// A link created by the entrypoint is not a container-owned file.
	linked := true
	if err := os.Symlink(filepath.Join(Claude.StoreDir("w"), "clean.md"), filepath.Join(cdir, "clean.md")); err != nil {
		t.Logf("symlinks unavailable, skipping link check: %v", err)
		linked = false
	}
	project := t.TempDir()
	pdir := filepath.Join(project, ".claude", "agents", "team")
	if err := os.MkdirAll(pdir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(pdir, "p.md"), agentFile("project-wins", "d"))

	entries, err := Claude.Inventory("w", project)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Entry{}
	for _, e := range entries {
		got[string(e.Source)+":"+e.File] = e
	}
	checks := map[string]string{
		"managed:same-file.md":    "the managed one is skipped",
		"managed:same-name.md":    "loads only one of them",
		"managed:project-wins.md": "project file .claude/agents/team/p.md",
		"managed:clean.md":        "",
	}
	for key, want := range checks {
		e, ok := got[key]
		if !ok {
			t.Errorf("missing %s in %v", key, entries)
			continue
		}
		if (want == "") != (e.Shadow == "") || !strings.Contains(e.Shadow, want) {
			t.Errorf("%s shadow = %q, want %q", key, e.Shadow, want)
		}
	}
	if _, ok := got["container:clean.md"]; ok && linked {
		t.Errorf("symlink listed as a container file")
	}
	if _, ok := got["project:team/p.md"]; !ok {
		t.Errorf("nested project file not listed")
	}
}

func TestFileNameFor(t *testing.T) {
	cases := map[string]string{
		"code-reviewer":    "code-reviewer.md",
		"Code Reviewer":    "Code-Reviewer.md",
		"../../etc/passwd": "etc-passwd.md",
		"日本":               "subagent.md",
		"a/b\\c":           "a-b-c.md",
	}
	for in, want := range cases {
		if got := Claude.FileNameFor(in); got != want {
			t.Errorf("FileNameFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidWorkspaceName(t *testing.T) {
	for name, want := range map[string]bool{"work": true, "": false, "..": false, "a/b": false, `a\b`: false, "a:b": false} {
		if got := ValidWorkspaceName(name); got != want {
			t.Errorf("ValidWorkspaceName(%q) = %v", name, got)
		}
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNames_Dedup(t *testing.T) {
	withHome(t)
	dir := Claude.StoreDir("w")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(dir, "a.md"), agentFile("same", "d"))
	mustWrite(t, filepath.Join(dir, "b.md"), agentFile("same", "d"))
	mustWrite(t, filepath.Join(dir, "broken.md"), []byte("x"))
	if got := strings.Join(Claude.Names("w"), ","); got != "broken,same" {
		t.Errorf("Names = %s", got)
	}
}
