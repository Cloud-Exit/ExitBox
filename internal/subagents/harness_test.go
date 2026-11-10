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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/agents"
)

func TestHarnesses_CoverEveryAgentInStableOrder(t *testing.T) {
	var names []string
	for _, h := range Harnesses() {
		names = append(names, h.Agent)
		if HarnessFor(h.Agent) != h {
			t.Errorf("HarnessFor(%q) does not return its harness", h.Agent)
		}
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("Harnesses() = %v, want sorted by agent name", names)
	}
	registered := agents.Names()
	sort.Strings(registered)
	if strings.Join(names, ",") != strings.Join(registered, ",") {
		t.Errorf("harnesses = %v, registered agents = %v; every agent needs a harness", names, registered)
	}
	if HarnessFor("nope") != nil {
		t.Error("HarnessFor(unknown) should be nil")
	}
}

func TestWithSubagents_LeavesOutPi(t *testing.T) {
	for _, h := range WithSubagents() {
		if h == Pi {
			t.Fatal("Pi has no native subagents")
		}
	}
	if Pi.HasSubagents() || !Pi.Skills {
		t.Error("Pi: want no subagents, but a master skill")
	}
	withHome(t)
	if _, err := Pi.Save("w", "x.md", agentFile("x", "d"), SourceManaged, "", false); err == nil {
		t.Error("saving a subagent for Pi should fail")
	}
	if entries, err := Pi.Inventory("w", ""); err != nil || entries != nil {
		t.Errorf("Pi inventory = %v, %v", entries, err)
	}
}

func TestHarness_Paths(t *testing.T) {
	home := withHome(t)
	if got, want := Codex.StoreDir("w"), filepath.Join(home, "profiles", "global", "w", "subagents", "codex"); got != want {
		t.Errorf("StoreDir = %s, want %s", got, want)
	}
	// The container's agents directory follows each agent's config layout.
	for h, want := range map[*Harness]string{
		Claude:   filepath.Join("claude", ".claude", "agents"),
		OpenCode: filepath.Join("opencode", ".config", "opencode", "agents"),
		Kimi:     filepath.Join("kimi", ".kimi-code", "agents"),
	} {
		if got := h.ContainerAgentsDir("w"); !strings.HasSuffix(got, filepath.Join("global", "w", want)) {
			t.Errorf("%s ContainerAgentsDir = %s, want it to end in %s", h.Agent, got, want)
		}
	}
	if got := OpenCode.AgentsDirLabel(); got != "~/.config/opencode/agents" {
		t.Errorf("AgentsDirLabel = %s", got)
	}
}

func TestHarness_HostDir(t *testing.T) {
	userHome := t.TempDir()
	t.Setenv("HOME", userHome)
	t.Setenv("USERPROFILE", userHome)
	for _, env := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "KIMI_CODE_HOME", "XDG_CONFIG_HOME"} {
		t.Setenv(env, "")
	}
	hostDir := func(h *Harness) string {
		t.Helper()
		dir, err := h.HostDir()
		if err != nil {
			t.Fatal(err)
		}
		return dir
	}
	if got, want := hostDir(Qwen), filepath.Join(userHome, ".qwen", "agents"); got != want {
		t.Errorf("qwen = %s, want %s", got, want)
	}
	if got, want := hostDir(OpenCode), filepath.Join(userHome, ".config", "opencode", "agents"); got != want {
		t.Errorf("opencode = %s, want %s", got, want)
	}

	other := t.TempDir()
	t.Setenv("CODEX_HOME", other)
	t.Setenv("KIMI_CODE_HOME", other)
	t.Setenv("CLAUDE_CONFIG_DIR", other)
	t.Setenv("XDG_CONFIG_HOME", other)
	for _, h := range []*Harness{Codex, Kimi, Claude} {
		if got, want := hostDir(h), filepath.Join(other, "agents"); got != want {
			t.Errorf("%s with its env override = %s, want %s", h.Agent, got, want)
		}
	}
	if got, want := hostDir(OpenCode), filepath.Join(other, "opencode", "agents"); got != want {
		t.Errorf("opencode with XDG_CONFIG_HOME = %s, want %s", got, want)
	}
}

// A scaffold must pass its own agent's rules and round-trip through the store.
func TestScaffold_ValidForEveryHarness(t *testing.T) {
	withHome(t)
	for _, h := range WithSubagents() {
		t.Run(h.Agent, func(t *testing.T) {
			data, err := h.Scaffold(ScaffoldOptions{Name: "demo-agent"})
			if err != nil {
				t.Fatal(err)
			}
			file := "demo-agent" + h.Ext
			def, findings := h.Parse(file, data)
			if def == nil || HasErrors(findings) || def.Name != "demo-agent" {
				t.Fatalf("scaffold does not parse cleanly: def = %+v, findings = %v\n%s", def, findings, data)
			}
			if def.Description == "" {
				t.Error("scaffold has no description")
			}
			path, err := h.Save("w", file, data, SourceManaged, "", false)
			if err != nil {
				t.Fatal(err)
			}
			if filepath.Base(path) != file || filepath.Dir(path) != h.StoreDir("w") {
				t.Errorf("stored at %s, want %s in %s", path, file, h.StoreDir("w"))
			}
			if e, err := h.Find("w", "demo-agent"); err != nil || e.Agent != h.Agent {
				t.Errorf("Find = %+v, %v", e, err)
			}
			// Another agent's store must not see it.
			for _, other := range WithSubagents() {
				if other == h {
					continue
				}
				if _, err := other.Find("w", "demo-agent"); err == nil && other.StoreDir("w") == h.StoreDir("w") {
					t.Errorf("%s shares a store with %s", other.Agent, h.Agent)
				}
			}
		})
	}
}

func TestScaffold_FillsSupportedFields(t *testing.T) {
	cases := []struct {
		h    *Harness
		opts ScaffoldOptions
		want []string
	}{
		{Claude, ScaffoldOptions{Name: "r", Tools: "Read, Grep", Model: "haiku"}, []string{"tools: Read, Grep", "model: haiku"}},
		{Qwen, ScaffoldOptions{Name: "rr", Tools: "read_file, glob", Model: "fast"}, []string{"- read_file", "- glob", "model: fast"}},
		{Kimi, ScaffoldOptions{Name: "r", Tools: "Read"}, []string{"- Read"}},
		{Cursor, ScaffoldOptions{Name: "r", Model: "composer-2"}, []string{"model: composer-2"}},
		{Copilot, ScaffoldOptions{Name: "r", Tools: "read"}, []string{"- read"}},
		{OpenCode, ScaffoldOptions{Name: "r", Model: "anthropic/x"}, []string{"mode: subagent", "model: anthropic/x"}},
		{Codex, ScaffoldOptions{Name: "r", Model: "gpt-x", Description: `say "hi"`}, []string{`model = "gpt-x"`, `description = "say \"hi\""`, "developer_instructions = \"\"\""}},
	}
	for _, tc := range cases {
		data, err := tc.h.Scaffold(tc.opts)
		if err != nil {
			t.Errorf("%s: %v", tc.h.Agent, err)
			continue
		}
		for _, want := range tc.want {
			if !strings.Contains(string(data), want) {
				t.Errorf("%s scaffold lacks %q:\n%s", tc.h.Agent, want, data)
			}
		}
	}
}

func TestScaffold_RejectsFieldsTheFormatLacks(t *testing.T) {
	cases := []struct {
		h    *Harness
		opts ScaffoldOptions
		flag string
	}{
		{Kimi, ScaffoldOptions{Name: "r", Model: "x"}, "--model"},
		{Cursor, ScaffoldOptions{Name: "r", Tools: "Read"}, "--tools"},
		{OpenCode, ScaffoldOptions{Name: "r", Tools: "Read"}, "--tools"},
		{Codex, ScaffoldOptions{Name: "r", Tools: "Read"}, "--tools"},
	}
	for _, tc := range cases {
		if _, err := tc.h.Scaffold(tc.opts); err == nil || !strings.Contains(err.Error(), tc.flag) {
			t.Errorf("%s: err = %v, want it to name %s", tc.h.Agent, err, tc.flag)
		}
	}
}

func TestListStore_OnlyTheAgentsOwnExtension(t *testing.T) {
	withHome(t)
	dir := Copilot.StoreDir("w")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"reviewer.agent.md": "---\ndescription: d\n---\nReview.\n",
		"notes.md":          "---\ndescription: d\n---\nNot an agent file for Copilot.\n",
		".agent.md":         "---\ndescription: d\n---\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Copilot.ListStore("w")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "reviewer" {
		t.Errorf("entries = %+v, want only reviewer.agent.md", entries)
	}
}

func TestInventory_CopiedFilesAreNotContainerFiles(t *testing.T) {
	withHome(t)
	if _, err := Qwen.Save("w", "mine.md", []byte("---\nname: mine\ndescription: d\n---\nA long enough prompt.\n"), SourceManaged, "", false); err != nil {
		t.Fatal(err)
	}
	cdir := Qwen.ContainerAgentsDir("w")
	if err := os.MkdirAll(cdir, 0o755); err != nil {
		t.Fatal(err)
	}
	copyOf := []byte("---\nname: mine\ndescription: d\n---\nA long enough prompt.\n")
	for name, content := range map[string][]byte{
		"mine.md":            copyOf,
		"own.md":             []byte("---\nname: own\ndescription: d\n---\nMade in the container.\n"),
		".exitbox-subagents": []byte("mine.md\n"),
	} {
		if err := os.WriteFile(filepath.Join(cdir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := Qwen.Inventory("w", "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, string(e.Source)+":"+e.File)
		if e.Source == SourceManaged && e.Shadow != "" {
			t.Errorf("the store's own copy shadows it: %s", e.Shadow)
		}
	}
	if strings.Join(got, " ") != "managed:mine.md container:own.md" {
		t.Errorf("inventory = %v, want the copy left out and the container's own file listed", got)
	}
}

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
