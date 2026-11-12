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
)

func TestEnsureMasterSkill_CreatesOnceWithRoutes(t *testing.T) {
	withHome(t)
	if _, err := Claude.Save("w", "", agentFile("log-reader", "Reads long | logs"), SourceManaged, "", false); err != nil {
		t.Fatal(err)
	}
	local := []byte("---\nname: bulk\ndescription: Bulk edits\nmodel: qwen3-coder\n---\n")
	if _, err := Claude.Save("w", "", local, SourceManaged, "", false); err != nil {
		t.Fatal(err)
	}
	// A file Claude Code skips must not get a route.
	if err := os.WriteFile(filepath.Join(Claude.StoreDir("w"), "broken.md"), []byte("---\nname: broken\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, created, err := Claude.EnsureMasterSkill("w")
	if err != nil || !created {
		t.Fatalf("EnsureMasterSkill = %q, %v, %v; want created", path, created, err)
	}
	if path != filepath.Join(Claude.StoreDir("w"), "master-skill", "SKILL.md") {
		t.Errorf("path = %s, want it inside the read-only subagent store", path)
	}
	data, err := Claude.ReadMasterSkill("w")
	if err != nil {
		t.Fatal(err)
	}
	if fs := CheckMasterSkill(data); len(fs) != 0 {
		t.Errorf("default skill has findings: %v", fs)
	}
	text := string(data)
	for _, want := range []string{
		"| Reads long \\| logs | `log-reader` | main model |",
		"| Bulk edits | `bulk` | third-party model qwen3-coder |",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("default skill lacks route %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "`broken`") {
		t.Error("default skill routes to a subagent Claude Code skips")
	}

	// A second call must keep the user's edits.
	if err := os.WriteFile(path, []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, created, err := Claude.EnsureMasterSkill("w"); err != nil || created {
		t.Fatalf("second EnsureMasterSkill created = %v, err = %v", created, err)
	}
	if got, _ := os.ReadFile(path); string(got) != "edited" {
		t.Errorf("existing master skill overwritten: %q", got)
	}
}

func TestDefaultMasterSkill_NoSubagents(t *testing.T) {
	text := string(Claude.DefaultMasterSkill(nil))
	if !strings.Contains(text, "No routes yet: tasks go to the subagent whose description fits best") {
		t.Errorf("empty store should say there are no routes:\n%s", text)
	}
	if fs := CheckMasterSkill([]byte(text)); len(fs) != 0 {
		t.Errorf("findings: %v", fs)
	}
}

func TestMasterSkill_NotAStoreEntry(t *testing.T) {
	withHome(t)
	if _, _, err := Claude.EnsureMasterSkill("w"); err != nil {
		t.Fatal(err)
	}
	entries, err := Claude.ListStore("w")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("master skill listed as a subagent: %+v", entries)
	}
}

func TestRemoveMasterSkill(t *testing.T) {
	withHome(t)
	if err := Claude.RemoveMasterSkill("w"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if _, err := Claude.ReadMasterSkill("w"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("read err = %v, want ErrNotFound", err)
	}
	if fs, err := Claude.MasterSkillFindings("w"); err != nil || fs != nil {
		t.Fatalf("findings without a skill = %v, %v", fs, err)
	}
	if _, _, err := Claude.EnsureMasterSkill("w"); err != nil {
		t.Fatal(err)
	}
	if !Claude.MasterSkillExists("w") {
		t.Fatal("MasterSkillExists = false after create")
	}
	if err := Claude.RemoveMasterSkill("w"); err != nil {
		t.Fatal(err)
	}
	if Claude.MasterSkillExists("w") {
		t.Error("master skill still exists after remove")
	}
}

func TestCheckMasterSkill(t *testing.T) {
	long := strings.Repeat("x", maxSkillDescription+1)
	cases := []struct {
		name     string
		data     string
		severity Severity
		want     string // substring of the finding; "" = no findings
	}{
		{"ok", "---\nname: master-skill\ndescription: d\n---\nbody\n", Warning, ""},
		{"no frontmatter", "# Routes\n", Error, "line 1"},
		{"unclosed", "---\nname: master-skill\n", Error, "no closing"},
		{"bad yaml", "---\nname: [a\n---\n", Error, "does not parse"},
		{"no name", "---\ndescription: d\n---\n", Error, "name"},
		{"numeric name", "---\nname: 12\ndescription: d\n---\n", Error, "name"},
		{"no description", "---\nname: master-skill\n---\n", Error, "description"},
		{"renamed", "---\nname: routing\ndescription: d\n---\n", Warning, "differs from the skill's directory"},
		{"long description", "---\nname: master-skill\ndescription: " + long + "\n---\n", Warning, "longer than"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := CheckMasterSkill([]byte(tc.data))
			if tc.want == "" {
				if len(fs) != 0 {
					t.Fatalf("findings = %v, want none", fs)
				}
				return
			}
			if len(fs) != 1 || fs[0].Severity != tc.severity || !strings.Contains(fs[0].String(), tc.want) {
				t.Fatalf("findings = %v, want one %s containing %q", fs, tc.severity, tc.want)
			}
		})
	}
}

func TestDefaultMasterSkill_IsACoordinatorThatReviews(t *testing.T) {
	text := string(Claude.DefaultMasterSkill(nil))
	for _, want := range []string{
		"You are the coordinator.",
		"Plan first.",
		"Run independent tasks in parallel",
		"When no route matches, hand the task to the subagent whose description fits it best.",
		"Send every result to the reviewer below",
		"\nReviewer: main agent\n",
		"A subagent never reviews\nits own work",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("default skill lacks %q:\n%s", want, text)
		}
	}
	// The Review section sits between the routes and the third-party rules.
	if r, rv, tp := strings.Index(text, "## Routes"), strings.Index(text, "## Review"), strings.Index(text, "## Third-party agents"); !(r < rv && rv < tp) {
		t.Errorf("section order: routes %d, review %d, third-party %d", r, rv, tp)
	}
	// Exactly one line the reviewer setting reads.
	if n := strings.Count(text, "\n"+reviewerPrefix); n != 1 {
		t.Errorf("%d Reviewer lines, want 1", n)
	}
	if strings.ContainsAny(text, "\u2013\u2014") {
		t.Error("the skill contains a long dash")
	}
}

func TestMasterSkillReviewer(t *testing.T) {
	withHome(t)
	if _, err := Claude.MasterSkillReviewer("w"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no skill: %v, want ErrNotFound", err)
	}
	if _, err := Claude.Save("w", "", agentFile("code-reviewer", "Reviews diffs"), SourceManaged, "", false); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Claude.EnsureMasterSkill("w"); err != nil {
		t.Fatal(err)
	}
	if r, err := Claude.MasterSkillReviewer("w"); err != nil || r != ReviewerSelf {
		t.Fatalf("default reviewer = %q, %v; want the main agent", r, err)
	}

	before, _ := Claude.ReadMasterSkill("w")
	if err := Claude.SetMasterSkillReviewer("w", "code-reviewer"); err != nil {
		t.Fatal(err)
	}
	after, _ := Claude.ReadMasterSkill("w")
	if r, _ := Claude.MasterSkillReviewer("w"); r != "code-reviewer" {
		t.Errorf("reviewer = %q, want code-reviewer", r)
	}
	if !strings.Contains(string(after), "\nReviewer: `code-reviewer`\n") {
		t.Errorf("line not written as expected:\n%s", after)
	}
	// Only that line changed.
	bl, al := strings.Split(string(before), "\n"), strings.Split(string(after), "\n")
	if len(bl) != len(al) {
		t.Fatalf("line count changed: %d -> %d", len(bl), len(al))
	}
	changed := 0
	for i := range bl {
		if bl[i] != al[i] {
			changed++
		}
	}
	if changed != 1 {
		t.Errorf("%d lines changed, want 1", changed)
	}
	if fs := CheckMasterSkill(after); len(fs) != 0 {
		t.Errorf("findings after setting the reviewer: %v", fs)
	}

	// Back to the main agent, by any of its spellings.
	for _, v := range []string{"main", "", "Main Agent"} {
		if err := Claude.SetMasterSkillReviewer("w", "code-reviewer"); err != nil {
			t.Fatal(err)
		}
		if err := Claude.SetMasterSkillReviewer("w", v); err != nil {
			t.Fatal(err)
		}
		if r, _ := Claude.MasterSkillReviewer("w"); r != ReviewerSelf {
			t.Errorf("Set(%q): reviewer = %q", v, r)
		}
	}
	if got, _ := Claude.ReadMasterSkill("w"); string(got) != string(before) {
		t.Error("setting the reviewer back did not restore the file")
	}

	// A name that is not stored is refused.
	if err := Claude.SetMasterSkillReviewer("w", "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown subagent: %v, want ErrNotFound", err)
	}

	// The owner's own spelling of the line is read too, and a file
	// without the line says so.
	path := Claude.MasterSkillPath("w")
	edited := strings.Replace(string(before), "Reviewer: main agent", "  Reviewer: **`code-reviewer`**", 1)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	if r, _ := Claude.MasterSkillReviewer("w"); r != "code-reviewer" {
		t.Errorf("owner's spelling: reviewer = %q", r)
	}
	if err := Claude.SetMasterSkillReviewer("w", "main"); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); !strings.Contains(string(got), "\n  Reviewer: main agent\n") {
		t.Errorf("indentation not kept:\n%s", got)
	}
	without := strings.Replace(string(before), "Reviewer: main agent\n", "", 1)
	if err := os.WriteFile(path, []byte(without), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Claude.MasterSkillReviewer("w"); !errors.Is(err, ErrNoReviewerLine) {
		t.Errorf("no line: %v", err)
	}
	if err := Claude.SetMasterSkillReviewer("w", "main"); !errors.Is(err, ErrNoReviewerLine) {
		t.Errorf("set without a line: %v", err)
	}
}
