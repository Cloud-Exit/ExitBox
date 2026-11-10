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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/subagents"
)

func writeFileAt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPickHarness(t *testing.T) {
	withSubagentsHome(t)

	// One enabled agent with subagents: no flag needed.
	if h, err := pickHarness("", hasSubagents); err != nil || h != subagents.Claude {
		t.Errorf("only claude enabled: %v, %v", h, err)
	}
	// Pi is enabled too, but has no subagents, so it does not count.
	enableAgents(t, "codex", "pi")
	if h, err := pickHarness("", hasSubagents); err != nil || h != subagents.Codex {
		t.Errorf("codex and pi enabled: %v, %v", h, err)
	}
	// It does count for the master skill.
	if _, err := pickHarness("", hasSkills); err == nil || !strings.Contains(err.Error(), "codex, pi") {
		t.Errorf("master skill with codex and pi enabled: err = %v, want both offered", err)
	}

	enableAgents(t, "codex", "opencode")
	_, err := pickHarness("", hasSubagents)
	if err == nil || !strings.Contains(err.Error(), "--agent (codex, opencode)") {
		t.Errorf("two enabled: err = %v, want the two offered", err)
	}
	enableAgents(t)
	_, err = pickHarness("", hasSubagents)
	if err == nil || !strings.Contains(err.Error(), "claude, codex, copilot, cursor, kimi, opencode, qwen") {
		t.Errorf("none enabled: err = %v, want every agent with subagents offered", err)
	}

	// --agent wins over what is enabled, and is checked.
	if h, err := pickHarness("qwen", hasSubagents); err != nil || h != subagents.Qwen {
		t.Errorf("--agent qwen: %v, %v", h, err)
	}
	if _, err := pickHarness("pi", hasSubagents); err == nil || !strings.Contains(err.Error(), "no native subagents") {
		t.Errorf("--agent pi: err = %v", err)
	}
	if _, err := pickHarness("copilot", hasSkills); err == nil || !strings.Contains(err.Error(), "links no skills") {
		t.Errorf("master skill for copilot: err = %v", err)
	}
	if _, err := pickHarness("gemini", hasSubagents); err == nil || !strings.Contains(err.Error(), "unknown agent") {
		t.Errorf("--agent gemini: err = %v", err)
	}
}

func TestSubagentsNew_PerAgentFormat(t *testing.T) {
	withSubagentsHome(t)
	for _, tc := range []struct {
		agent, name, file, want string
	}{
		{"codex", "pr_explorer", "pr_explorer.toml", `name = "pr_explorer"`},
		{"opencode", "reviewer", "reviewer.md", "mode: subagent"},
		{"copilot", "reviewer", "reviewer.agent.md", "description:"},
		{"kimi", "code-reviewer", "code-reviewer.md", "name: code-reviewer"},
	} {
		h := subagents.HarnessFor(tc.agent)
		out, _, err := runSubagents(t, "new", tc.name, "--agent", tc.agent, "-w", "work")
		if err != nil {
			t.Errorf("%s: %v", tc.agent, err)
			continue
		}
		path := filepath.Join(h.StoreDir("work"), tc.file)
		data, readErr := os.ReadFile(path)
		if readErr != nil || !strings.Contains(string(data), tc.want) || !strings.Contains(out, path) {
			t.Errorf("%s: file %s = %q (err %v), out %q; want it to contain %q", tc.agent, path, data, readErr, out, tc.want)
		}
	}
	// A name the agent would skip is refused before anything is stored.
	if _, _, err := runSubagents(t, "new", "Code_Reviewer", "--agent", "kimi", "-w", "work"); err == nil {
		t.Error("kimi: a non-kebab-case name should be refused")
	}
	// A flag the format has no field for is refused, not dropped.
	if _, _, err := runSubagents(t, "new", "x", "--agent", "codex", "--tools", "Read", "-w", "work"); err == nil || !strings.Contains(err.Error(), "--tools") {
		t.Errorf("codex --tools: err = %v", err)
	}
	if _, _, err := runSubagents(t, "new", "x", "--agent", "pi", "-w", "work"); err == nil {
		t.Error("pi has no subagents; new should fail")
	}
}

func TestSubagentsList_AllAgents(t *testing.T) {
	withSubagentsHome(t)
	enableAgents(t, "claude", "cursor")
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "reviewer.md"), "reviewer", "d")
	writeAgent(t, filepath.Join(subagents.Cursor.StoreDir("work"), "auditor.md"), "auditor", "d")

	out, _, err := runSubagents(t, "list", "-w", "work")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"AGENT", "claude  reviewer", "cursor  auditor", "note: cursor:"} {
		if !strings.Contains(out, want) {
			t.Errorf("list lacks %q:\n%s", want, out)
		}
	}
	out, _, err = runSubagents(t, "list", "-w", "work", "--agent", "claude")
	if err != nil || strings.Contains(out, "auditor") || strings.Contains(out, "note: cursor") {
		t.Errorf("list --agent claude shows another agent's entries: %q, %v", out, err)
	}

	out, _, err = runSubagents(t, "list", "-w", "work", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Subagents []struct {
			Agent, Name string
		} `json:"subagents"`
		Notes []string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Subagents) != 2 || got.Subagents[0].Agent != "claude" || got.Subagents[1].Agent != "cursor" || len(got.Notes) != 1 {
		t.Errorf("json = %+v", got)
	}
}

func TestSubagents_NameLookupAcrossAgents(t *testing.T) {
	withSubagentsHome(t)
	enableAgents(t, "claude", "qwen")
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "only-claude.md"), "only-claude", "d")
	writeFileAt(t, filepath.Join(subagents.Qwen.StoreDir("work"), "both.md"), "---\nname: both\ndescription: qwen one\n---\nA long enough prompt.\n")
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "both.md"), "both", "claude one")

	// Stored for one agent: found without --agent, although two are enabled.
	out, _, err := runSubagents(t, "show", "only-claude", "-w", "work")
	if err != nil || !strings.Contains(out, "name: only-claude") {
		t.Errorf("show only-claude = %q, %v", out, err)
	}
	// Stored for two: ask which.
	if _, _, err := runSubagents(t, "show", "both", "-w", "work"); err == nil || !strings.Contains(err.Error(), "claude and qwen") {
		t.Errorf("show both: err = %v, want both agents named", err)
	}
	out, _, err = runSubagents(t, "show", "both", "-w", "work", "--agent", "qwen")
	if err != nil || !strings.Contains(out, "qwen one") {
		t.Errorf("show both --agent qwen = %q, %v", out, err)
	}
	if _, _, err := runSubagents(t, "rm", "both", "-w", "work", "--agent", "qwen"); err != nil {
		t.Fatal(err)
	}
	if _, err := subagents.Claude.Find("work", "both"); err != nil {
		t.Error("rm --agent qwen removed Claude's subagent")
	}
	if _, _, err := runSubagents(t, "show", "nope", "-w", "work"); err == nil {
		t.Error("show of an unknown name should fail")
	}
}

func TestSubagentsValidate_AllAgents(t *testing.T) {
	withSubagentsHome(t)
	writeAgent(t, filepath.Join(subagents.Claude.StoreDir("work"), "reviewer.md"), "reviewer", "d")
	writeFileAt(t, filepath.Join(subagents.Codex.StoreDir("work"), "broken.toml"), "name = \"broken\"\n")
	if _, _, err := subagents.Pi.EnsureMasterSkill("work"); err != nil {
		t.Fatal(err)
	}

	out, _, err := runSubagents(t, "validate", "-w", "work")
	if err == nil {
		t.Error("a Codex file without its required keys must fail validation")
	}
	for _, want := range []string{"claude: reviewer (reviewer.md): ok", "codex: broken (broken.toml):", "developer_instructions", "pi: master skill: ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("validate lacks %q:\n%s", want, out)
		}
	}
	out, _, err = runSubagents(t, "validate", "-w", "work", "--agent", "claude")
	if err != nil || strings.Contains(out, "codex") {
		t.Errorf("validate --agent claude = %q, %v", out, err)
	}
	if out, _, err := runSubagents(t, "validate", "broken", "-w", "work"); err == nil || !strings.Contains(out, "codex: broken") {
		t.Errorf("validate broken = %q, %v", out, err)
	}
}

func TestSubagentsAddImport_OtherAgents(t *testing.T) {
	withSubagentsHome(t)
	host := withHostAgents(t)
	writeFileAt(t, filepath.Join(host, "opencode", "reviewer.md"), "---\ndescription: Reviews\nmode: subagent\n---\nReview the diff.\n")
	writeFileAt(t, filepath.Join(host, "opencode", "README.md"), "# Notes\n")
	writeAgent(t, filepath.Join(host, "claude", "claude-only.md"), "claude-only", "d")

	out, _, err := runSubagents(t, "import", "--agent", "opencode", "-w", "work")
	if err != nil || !strings.Contains(out, "Imported 1 OpenCode subagent(s)") {
		t.Fatalf("import = %q, %v", out, err)
	}
	if _, err := subagents.OpenCode.Find("work", "reviewer"); err != nil {
		t.Error("reviewer not imported for opencode")
	}
	if entries, _ := subagents.Claude.ListStore("work"); len(entries) != 0 {
		t.Errorf("an opencode import filled Claude's store: %+v", entries)
	}
	if out, _, err := runSubagents(t, "import", "--agent", "kimi", "-w", "work"); err != nil || !strings.Contains(out, "No Kimi Code CLI subagent files") {
		t.Errorf("import without host files = %q, %v", out, err)
	}

	// add takes files with the agent's own extension from a directory.
	src := t.TempDir()
	writeFileAt(t, filepath.Join(src, "explorer.toml"), "name = \"explorer\"\ndescription = \"d\"\ndeveloper_instructions = \"Explore.\"\n")
	writeFileAt(t, filepath.Join(src, "notes.md"), "# not for codex\n")
	out, _, err = runSubagents(t, "add", src, "--agent", "codex", "-w", "work")
	if err != nil || !strings.Contains(out, "Added 1 OpenAI Codex subagent(s)") {
		t.Errorf("add = %q, %v", out, err)
	}
	// A single file is taken whatever its extension, and named by its stem.
	single := filepath.Join(t.TempDir(), "docs-reviewer.md")
	writeFileAt(t, single, "---\ndescription: Reviews docs\n---\nReview.\n")
	if _, _, err := runSubagents(t, "add", single, "--agent", "copilot", "-w", "work"); err != nil {
		t.Fatal(err)
	}
	if e, err := subagents.Copilot.Find("work", "docs-reviewer"); err != nil || e.File != "docs-reviewer.agent.md" {
		t.Errorf("copilot single file = %+v, %v", e, err)
	}
}

func TestAgentsSkill_PerAgent(t *testing.T) {
	withSubagentsHome(t)
	writeFileAt(t, filepath.Join(subagents.Codex.StoreDir("work"), "explorer.toml"), "name = \"explorer\"\ndescription = \"Explores\"\ndeveloper_instructions = \"Explore.\"\n")

	out, _, err := runAgents(t, "skill", "edit", "--agent", "codex", "-w", "work")
	if err != nil || !strings.Contains(out, subagents.Codex.MasterSkillPath("work")) {
		t.Fatalf("skill edit --agent codex = %q, %v", out, err)
	}
	shown, _, _ := runAgents(t, "skill", "show", "--agent", "codex", "-w", "work")
	if !strings.Contains(shown, "`explorer`") {
		t.Errorf("codex master skill has no route to its own subagent:\n%s", shown)
	}
	// Claude's master skill is a different file and still absent.
	if subagents.Claude.MasterSkillExists("work") {
		t.Error("creating Codex's master skill created Claude's")
	}
	if _, _, err := runAgents(t, "skill", "edit", "--agent", "qwen", "-w", "work"); err == nil {
		t.Error("ExitBox links no skills for Qwen Code; skill edit should fail")
	}
	// Pi has no subagents but can have a master skill.
	if _, _, err := runAgents(t, "skill", "edit", "--agent", "pi", "-w", "work"); err != nil {
		t.Errorf("skill edit --agent pi: %v", err)
	}
}
