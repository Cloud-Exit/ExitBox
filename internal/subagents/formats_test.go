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
	"strings"
	"testing"
)

// formatCase is one file checked against one agent's rules. want is a
// substring of the single expected finding; "" means no findings at all.
type formatCase struct {
	name     string
	file     string
	data     string
	severity Severity
	want     string
}

func runFormatCases(t *testing.T, h *Harness, cases []formatCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, findings := h.Parse(tc.file, []byte(tc.data))
			if tc.want == "" {
				if len(findings) != 0 {
					t.Fatalf("findings = %v, want none", findings)
				}
				return
			}
			if len(findings) != 1 || findings[0].Severity != tc.severity || !strings.Contains(findings[0].String(), tc.want) {
				t.Fatalf("findings = %v, want one %s containing %q", findings, tc.severity, tc.want)
			}
		})
	}
}

func md(frontmatter, body string) string {
	return "---\n" + frontmatter + "\n---\n" + body
}

const prompt = "You review code and report what you find.\n"

func TestParseQwen(t *testing.T) {
	runFormatCases(t, Qwen, []formatCase{
		{"ok", "x.md", md("name: api-checker\ndescription: d\ntools:\n  - read_file\nmodel: fast", prompt), Warning, ""},
		{"no frontmatter", "x.md", "# Notes\n", Error, "line 1"},
		{"no name", "x.md", md("description: d", prompt), Error, "name: missing"},
		{"short name", "x.md", md("name: a\ndescription: d", prompt), Error, "at least 2"},
		{"long name", "x.md", md("name: "+strings.Repeat("a", 51)+"\ndescription: d", prompt), Error, "50 characters"},
		{"spaces in name", "x.md", md("name: bad name\ndescription: d", prompt), Error, "letters, digits"},
		{"leading hyphen", "x.md", md("name: \"-ab\"\ndescription: d", prompt), Error, "start or end"},
		{"reserved name", "x.md", md("name: Main\ndescription: d", prompt), Error, "reserved"},
		{"no description", "x.md", md("name: ab", prompt), Error, "description: missing"},
		{"short prompt", "x.md", md("name: ab\ndescription: d", "hi\n"), Error, "at least 10 characters"},
		{"claude-only field", "x.md", md("name: ab\ndescription: d\nskills: [a]", prompt), Warning, "skills"},
	})
	def, _ := Qwen.Parse("x.md", []byte(md("name: ab\ndescription: d\ntools: read_file, glob", prompt)))
	if def == nil || len(def.Tools) != 2 {
		t.Errorf("comma-separated tools = %+v, want 2", def)
	}
	// Qwen Code turns a numeric name into a string, so it loads.
	if def, findings := Qwen.Parse("x.md", []byte(md("name: 12345\ndescription: d", prompt))); HasErrors(findings) || def.Name != "12345" {
		t.Errorf("numeric name: def = %+v, findings = %v", def, findings)
	}
}

func TestParseKimi(t *testing.T) {
	runFormatCases(t, Kimi, []formatCase{
		{"ok", "x.md", md("name: reviewer\ndescription: d\nwhenToUse: before merging\ntools:\n  - Read", prompt), Warning, ""},
		{"name from file", "code-reviewer.md", md("description: d", prompt), Warning, ""},
		{"no frontmatter", "x.md", prompt, Error, "line 1"},
		{"name not kebab", "x.md", md("name: Reviewer\ndescription: d", prompt), Error, "kebab-case"},
		{"file not kebab", "My_Agent.md", md("description: d", prompt), Error, `file name "My_Agent"`},
		{"no description", "x.md", md("name: reviewer", prompt), Error, "description: missing"},
		{"builtin name", "x.md", md("name: explore\ndescription: d", prompt), Error, "built-in agent"},
		{"builtin with override", "x.md", md("name: explore\ndescription: d\noverride: true", prompt), Warning, ""},
		{"model is ignored", "x.md", md("name: reviewer\ndescription: d\nmodel: kimi-x", prompt), Warning, "model: unknown field; Kimi Code ignores it"},
	})
	if def, _ := Kimi.Parse("code-reviewer.md", []byte(md("description: d", prompt))); def.Name != "code-reviewer" {
		t.Errorf("name = %q, want it taken from the file name", def.Name)
	}
}

func TestParseCursor(t *testing.T) {
	runFormatCases(t, Cursor, []formatCase{
		{"ok", "x.md", md("name: security-auditor\ndescription: d\nmodel: inherit\nreadonly: true\nis_background: false", prompt), Warning, ""},
		{"name from file", "auditor.md", md("description: d", prompt), Warning, ""},
		{"no frontmatter", "x.md", prompt, Error, "line 1"},
		{"odd name", "x.md", md("name: Security_Auditor\ndescription: d", prompt), Warning, "lowercase letters and hyphens"},
		{"no description", "x.md", md("name: auditor", prompt), Warning, "description: missing"},
		{"undocumented field", "x.md", md("name: auditor\ndescription: d\ntools: Read", prompt), Warning, "tools: not a documented"},
	})
	// Nothing Cursor documents makes it skip a file, so warnings never block a save.
	if _, findings := Cursor.Parse("x.md", []byte(md("name: A B\ncolor: red", prompt))); HasErrors(findings) || len(findings) != 3 {
		t.Errorf("findings = %v, want 3 warnings", findings)
	}
}

func TestParseCopilot(t *testing.T) {
	runFormatCases(t, Copilot, []formatCase{
		{"ok", "reviewer.agent.md", md("name: Docs Reviewer\ndescription: d\ntools: [\"read\", \"search\"]\ndisable-model-invocation: true", prompt), Warning, ""},
		{"no frontmatter", "x.agent.md", prompt, Error, "line 1"},
		{"no description", "x.agent.md", md("name: X", prompt), Error, "description: missing"},
		{"prompt too long", "x.agent.md", md("description: d", strings.Repeat("x", copilotMaxPrompt+1)), Error, "30000 characters"},
		{"retired field", "x.agent.md", md("description: d\ninfer: false", prompt), Warning, "infer: retired"},
		{"undocumented field", "x.agent.md", md("description: d\ncolor: red", prompt), Warning, "color: not a documented"},
	})
	// Copilot goes by file name; the name field is only a display name.
	if def, _ := Copilot.Parse("docs-reviewer.agent.md", []byte(md("name: Docs Reviewer\ndescription: d", prompt))); def.Name != "docs-reviewer" {
		t.Errorf("name = %q, want the file name without .agent.md", def.Name)
	}
}

func TestParseOpenCode(t *testing.T) {
	runFormatCases(t, OpenCode, []formatCase{
		{"ok", "reviewer.md", md("description: d\nmode: subagent\nmodel: anthropic/claude-sonnet-5-5#high\nsteps: 8\npermissions:\n  - action: edit\n    resource: \"*\"\n    effect: deny", prompt), Warning, ""},
		{"mode all", "reviewer.md", md("description: d\nmode: all", prompt), Warning, ""},
		{"no frontmatter", "reviewer.md", prompt, Error, "line 1"},
		{"no mode", "reviewer.md", md("description: d", prompt), Warning, "primary agent, not a subagent"},
		{"primary", "reviewer.md", md("description: d\nmode: primary", prompt), Warning, "primary agent, not a subagent"},
		{"bad mode", "reviewer.md", md("description: d\nmode: helper", prompt), Warning, `"helper" is not one of`},
		{"no description", "reviewer.md", md("mode: subagent", prompt), Warning, "description: missing"},
		{"bare model", "reviewer.md", md("description: d\nmode: subagent\nmodel: sonnet", prompt), Warning, "provider/model"},
		{"legacy tools", "reviewer.md", md("description: d\nmode: subagent\ntools:\n  write: false", prompt), Warning, "tools: deprecated in the current OpenCode docs; use permissions"},
		{"legacy temperature", "reviewer.md", md("description: d\nmode: subagent\ntemperature: 0.1", prompt), Warning, "temperature: deprecated"},
	})
	if def, _ := OpenCode.Parse("reviewer.md", []byte(md("description: d\nmode: subagent", prompt))); def.Name != "reviewer" {
		t.Errorf("name = %q, want the file name", def.Name)
	}
}

const codexExample = `name = "pr_explorer"
description = "Read-only codebase explorer for gathering evidence before changes are proposed."
model = "gpt-6-luna"
model_reasoning_effort = "high"
developer_instructions = """
Stay in exploration mode.
Do not edit files.
"""
`

func TestParseCodex(t *testing.T) {
	runFormatCases(t, Codex, []formatCase{
		{"docs example", "x.toml", codexExample, Warning, ""},
		{"extra config", "x.toml", codexExample + "nickname_candidates = [\n  \"scout\", # one per line\n  \"ranger\",\n]\nfeatures.shell_tool = false\n\n[[skills.config]]\npath = '/x/SKILL.md'\nenabled = false\n", Warning, ""},
		{"no instructions", "x.toml", "name = \"x\"\ndescription = \"d\"\n", Error, "developer_instructions: missing"},
		{"name is not a string", "x.toml", "name = 5\ndescription = \"d\"\ndeveloper_instructions = \"i\"\n", Error, "name: missing or not a string"},
		{"empty description", "x.toml", "name = \"x\"\ndescription = \"  \"\ndeveloper_instructions = \"i\"\n", Error, "description: missing"},
		{"parent-only key", "x.toml", codexExample + "sandbox_mode = \"read-only\"\n", Warning, "sandbox_mode: an agent file cannot override"},
		{"parent-only table", "x.toml", codexExample + "\n[mcp_servers.docs]\ncommand = \"x\"\n", Warning, "mcp_servers: an agent file cannot override"},
		{"unclosed string", "x.toml", "name = \"x\ndescription = \"d\"\n", Error, "TOML does not parse: line 1"},
		{"not toml", "x.toml", "---\nname: x\n---\n", Error, "TOML does not parse"},
	})
	def, _ := Codex.Parse("renamed.toml", []byte(codexExample))
	if def == nil || def.Name != "pr_explorer" || def.Model != "gpt-6-luna" {
		t.Errorf("def = %+v, want the name field to win over the file name", def)
	}
	// A required key inside a table is not a top-level key.
	if _, findings := Codex.Parse("x.toml", []byte("description = \"d\"\ndeveloper_instructions = \"i\"\n[extra]\nname = \"x\"\n")); !HasErrors(findings) {
		t.Error("name under a [table] must not count as the agent's name")
	}
}

func TestReadTOML(t *testing.T) {
	doc, err := readTOML([]byte("\uFEFF# comment\r\n" +
		"a = \"one \\\"quoted\\\" \\u00e9\" # trailing\r\n" +
		"b = 'lit\\eral'\n" +
		"c = \"\"\"\nfirst\nsecond \"\"quoted\"\"\n\"\"\"\n" +
		"d = '''\nraw \\n\n'''\n" +
		"n = 42\nt = true\n" +
		"arr = [ \"x]\", { k = \"}\" } ]\n" +
		"\"quoted key\" = \"q\"\n" +
		"dotted.key = \"not top level\"\n" +
		"[table]\na = \"shadow\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"a": "one \"quoted\" é", "b": `lit\eral`, "c": "first\nsecond \"\"quoted\"\"\n",
		"d": "raw \\n\n", "quoted key": "q",
	}
	for k, v := range want {
		if doc.strings[k] != v {
			t.Errorf("%s = %q, want %q", k, doc.strings[k], v)
		}
	}
	if _, ok := doc.strings["dotted"]; ok {
		t.Error("a dotted key must not count as a top-level string")
	}
	if got := strings.Join(doc.keys, ","); got != "a,b,c,d,n,t,arr,quoted key,dotted,table" {
		t.Errorf("keys = %s", got)
	}

	for name, bad := range map[string]string{
		"no value":       "a =\n",
		"no equals":      "a b\n",
		"trailing text":  "a = \"x\" y\n",
		"open multiline": "a = \"\"\"\nx\n",
		"open array":     "a = [1, 2\n",
		"open table":     "[t\n",
	} {
		if _, err := readTOML([]byte(bad)); err == nil {
			t.Errorf("%s: expected an error for %q", name, bad)
		}
	}
}

func TestTomlQuote_RoundTrips(t *testing.T) {
	for _, v := range []string{`plain`, `say "hi" <now> & \then`, "tab\tand\nnewline", "é ✓"} {
		doc, err := readTOML([]byte("k = " + tomlQuote(v) + "\n"))
		if err != nil || doc.strings["k"] != v {
			t.Errorf("tomlQuote(%q) read back as %q, err %v", v, doc.strings["k"], err)
		}
	}
}

func TestStoreFiles_Outcomes(t *testing.T) {
	withHome(t)
	files := map[string][]byte{
		"reviewer.md":  []byte(md("description: Reviews\nmode: subagent", prompt)),
		"README.md":    []byte("# How these agents work\n"),
		"primary.md":   []byte(md("description: Plans", prompt)),
		"big.md":       []byte(md("description: d\nmode: subagent", strings.Repeat("x", MaxFileSize))),
		"nested/x.md":  []byte(md("description: d\nmode: subagent", prompt)),
		"nested2/x.md": []byte(md("description: d\nmode: subagent", prompt)),
	}
	out := OpenCode.StoreFiles("w", files, func(rel string) string { return "src/" + rel }, SourceManaged, false, false)
	got := map[string]Outcome{}
	for _, o := range out {
		got[o.Source] = o
	}
	if o := got["reviewer.md"]; o.Status != Added || o.File != "reviewer.md" || o.Name != "reviewer" {
		t.Errorf("reviewer.md = %+v, want added", o)
	}
	if o := got["README.md"]; o.Status != Skipped || o.Reason != "" {
		t.Errorf("README.md = %+v, want it skipped quietly as documentation", o)
	}
	if o := got["primary.md"]; o.Status != Added || len(o.Findings) == 0 {
		t.Errorf("primary.md = %+v, want added with its warning", o)
	}
	if o := got["big.md"]; o.Status != Failed || !strings.Contains(o.Reason, "larger than") {
		t.Errorf("big.md = %+v, want a size failure", o)
	}
	if o := got["nested2/x.md"]; o.Status != Failed || !strings.Contains(o.Reason, "also used by nested/x.md") {
		t.Errorf("nested2/x.md = %+v, want a duplicate-name failure", o)
	}
	if a, s, f := Tally(out); a != 3 || s != 1 || f != 2 {
		t.Errorf("tally = %d added, %d skipped, %d failed", a, s, f)
	}
	if e, err := OpenCode.Find("w", "reviewer"); err != nil || e.Origin != "src/reviewer.md" {
		t.Errorf("stored reviewer = %+v, %v", e, err)
	}

	// A second pass: existing names fail, or are skipped when importing.
	again := OpenCode.StoreFiles("w", map[string][]byte{"reviewer.md": files["reviewer.md"]}, func(string) string { return "" }, SourceManaged, false, false)
	if again[0].Status != Failed || !strings.Contains(again[0].Reason, "--force") {
		t.Errorf("existing name = %+v, want a failure pointing at --force", again[0])
	}
	again = OpenCode.StoreFiles("w", map[string][]byte{"reviewer.md": files["reviewer.md"]}, func(string) string { return "" }, SourceImported, false, true)
	if again[0].Status != Skipped || again[0].Reason == "" {
		t.Errorf("existing name on import = %+v, want a skip with a reason", again[0])
	}
	// A single file that is no subagent is an error, not documentation.
	single := OpenCode.StoreFiles("w", map[string][]byte{"README.md": files["README.md"]}, func(string) string { return "" }, SourceManaged, false, false)
	if single[0].Status != Failed || single[0].Reason != "not a subagent" {
		t.Errorf("single README = %+v", single[0])
	}
}

func TestImportDir_UsesTheAgentsOwnLayout(t *testing.T) {
	withHome(t)
	host := t.TempDir()
	writeFile := func(rel, content string) {
		t.Helper()
		writeTestFile(t, host, rel, content)
	}
	writeFile("reviewer.agent.md", md("description: Reviews", prompt))
	writeFile("notes.md", md("description: Not a Copilot agent file", prompt))
	writeFile("team/nested.agent.md", md("description: Nested", prompt))

	if n := Copilot.CountDir(host); n != 1 {
		t.Errorf("CountDir = %d, want 1 (Copilot does not use plain .md, and the store is flat)", n)
	}
	out, err := Copilot.ImportDir("w", host, false)
	if err != nil {
		t.Fatal(err)
	}
	if a, s, f := Tally(out); a != 1 || s != 0 || f != 0 {
		t.Errorf("tally = %d/%d/%d, outcomes %+v", a, s, f, out)
	}
	e, err := Copilot.Find("w", "reviewer")
	if err != nil || e.Source != SourceImported || e.File != "reviewer.agent.md" {
		t.Errorf("imported = %+v, %v", e, err)
	}
	// Claude Code scans subdirectories, so its import does too.
	writeFile("deep/claude.md", "---\nname: deep\ndescription: d\n---\n")
	if n := Claude.CountDir(host); n != 1 {
		t.Errorf("Claude CountDir = %d, want the one named file, found in a subdirectory", n)
	}
	if n := Claude.CountDir(host + "-missing"); n != 0 {
		t.Errorf("CountDir of a missing dir = %d", n)
	}
}
