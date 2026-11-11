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

	"github.com/cloud-exit/exitbox/internal/router"
)

func fm(body string) []byte {
	return []byte("---\n" + body + "\n---\nYou are a test agent.\n")
}

func findingFor(fs []Finding, field string) (Finding, bool) {
	for _, f := range fs {
		if f.Field == field {
			return f, true
		}
	}
	return Finding{}, false
}

func TestParse_Valid(t *testing.T) {
	def, findings := Claude.Parse("", fm("name: reviewer\ndescription: Reviews code\nmodel: sonnet\ntools: Read, Grep,  Bash"))
	if len(findings) != 0 {
		t.Fatalf("findings = %v, want none", findings)
	}
	if def.Name != "reviewer" || def.Description != "Reviews code" || def.Model != "sonnet" {
		t.Errorf("def = %+v", def)
	}
	if strings.Join(def.Tools, "|") != "Read|Grep|Bash" {
		t.Errorf("tools = %q", def.Tools)
	}
}

func TestParse_ToolsList(t *testing.T) {
	def, findings := Claude.Parse("", fm("name: r\ndescription: d\ntools:\n  - Read\n  - Edit"))
	if len(findings) != 0 {
		t.Fatalf("findings = %v", findings)
	}
	if strings.Join(def.Tools, "|") != "Read|Edit" {
		t.Errorf("tools = %q", def.Tools)
	}
}

func TestParse_LineEndingsAndBOM(t *testing.T) {
	for name, data := range map[string]string{
		"crlf": "---\r\nname: r\r\ndescription: d\r\n---\r\nbody\r\n",
		"bom":  "\uFEFF---\nname: r\ndescription: d\n---\nbody\n",
	} {
		t.Run(name, func(t *testing.T) {
			def, findings := Claude.Parse("", []byte(data))
			if HasErrors(findings) || def == nil || def.Name != "r" {
				t.Errorf("def = %+v, findings = %v", def, findings)
			}
		})
	}
}

// Every case here is a file Claude Code skips without telling the user.
func TestParse_SkipRules(t *testing.T) {
	long := strings.Repeat("a", 257)
	cases := []struct {
		name  string
		data  string
		field string
		want  string
	}{
		{"blank first line", "\n---\nname: r\ndescription: d\n---\n", "", "line 1"},
		{"no frontmatter", "# Notes\n", "", "line 1"},
		{"unclosed", "---\nname: r\ndescription: d\n", "", "closing"},
		{"bad yaml", "---\nname: r\ndescription: [unclosed\n---\n", "", "does not parse"},
		{"not a mapping", "---\n- a\n- b\n---\n", "", "mapping"},
		{"empty frontmatter", "---\n---\n", "name", "empty"},
		{"missing name", "---\ndescription: d\n---\n", "name", "missing"},
		{"null name", "---\nname:\ndescription: d\n---\n", "name", "missing"},
		{"numeric name", "---\nname: 12345\ndescription: d\n---\n", "name", "must be a string"},
		{"leading dash", "---\nname: -r\ndescription: d\n---\n", "name", "start with -"},
		{"colon", "---\nname: \"a:b\"\ndescription: d\n---\n", "name", "contain :"},
		{"too long", "---\nname: " + long + "\ndescription: d\n---\n", "name", "256"},
		{"missing description", "---\nname: r\n---\n", "description", "missing"},
		{"empty description", "---\nname: r\ndescription: \"\"\n---\n", "description", "empty"},
		{"list description", "---\nname: r\ndescription: [a]\n---\n", "description", "must be a string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, findings := Claude.Parse("", []byte(tc.data))
			if !HasErrors(findings) {
				t.Fatalf("findings = %v, want an error", findings)
			}
			f, ok := findingFor(findings, tc.field)
			if !ok || f.Severity != Error || !strings.Contains(f.Message, tc.want) {
				t.Errorf("findings = %v, want %s error containing %q", findings, tc.field, tc.want)
			}
		})
	}
}

func TestParse_NameAtLimit(t *testing.T) {
	_, findings := Claude.Parse("", fm("name: "+strings.Repeat("é", 256)+"\ndescription: d"))
	if HasErrors(findings) {
		t.Errorf("256-character name rejected: %v", findings)
	}
}

func TestParse_UnknownFieldSuggestions(t *testing.T) {
	cases := map[string]string{
		"toolz":               "tools",
		"Model":               "model",
		"disallowed_tools":    "disallowedTools",
		"permissionmode":      "permissionMode",
		"auto_compact_window": "autoCompactWindow",
		"autoCompactWindw":    "autoCompactWindow",
		"zzzzzz":              "",
	}
	for field, want := range cases {
		t.Run(field, func(t *testing.T) {
			_, findings := Claude.Parse("", fm("name: r\ndescription: d\n"+field+": x"))
			f, ok := findingFor(findings, field)
			if !ok || f.Severity != Warning {
				t.Fatalf("findings = %v, want warning on %s", findings, field)
			}
			if want == "" {
				if strings.Contains(f.Message, "did you mean") {
					t.Errorf("unexpected suggestion: %s", f.Message)
				}
				return
			}
			if !strings.Contains(f.Message, `"`+want+`"`) {
				t.Errorf("message %q, want suggestion %q", f.Message, want)
			}
		})
	}
}

func TestParse_KnownFieldsNoWarnings(t *testing.T) {
	var b strings.Builder
	b.WriteString("name: r\ndescription: d\n")
	for _, f := range KnownFields {
		if f == "name" || f == "description" || f == "model" || f == "tools" || f == "disallowedTools" || f == "autoCompactWindow" {
			continue
		}
		b.WriteString(f + ": x\n")
	}
	_, findings := Claude.Parse("", fm(b.String()))
	if len(findings) != 0 {
		t.Errorf("known fields produced findings: %v", findings)
	}
}

func TestParse_AutoCompactWindow(t *testing.T) {
	cases := []struct {
		value string
		ok    bool
	}{
		{"200000", true},
		{"100000", true},
		{"1000000", true},
		{"200k", false},
		{"\"200000\"", false},
		{"99999", false},
		{"1000001", false},
		{"2.5e5", false},
	}
	for _, tc := range cases {
		t.Run(tc.value, func(t *testing.T) {
			_, findings := Claude.Parse("", fm("name: r\ndescription: d\nautoCompactWindow: "+tc.value))
			if HasErrors(findings) {
				t.Fatalf("autoCompactWindow must only warn: %v", findings)
			}
			_, warned := findingFor(findings, "autoCompactWindow")
			if warned == tc.ok {
				t.Errorf("value %s: warned=%v, findings=%v", tc.value, warned, findings)
			}
		})
	}
}

func TestClassifyModel(t *testing.T) {
	cases := map[string]ModelClass{
		"":                ModelUnset,
		"sonnet":          ModelAlias,
		"opus":            ModelAlias,
		"haiku":           ModelAlias,
		"fable":           ModelAlias,
		"inherit":         ModelAlias,
		"claude-opus-5-5": ModelClaudeID,
		"qwen3.6-27b":     ModelOther,
		"Opus":            ModelOther,
		"gpt-5":           ModelOther,
	}
	for model, want := range cases {
		if got := ClassifyModel(model); got != want {
			t.Errorf("ClassifyModel(%q) = %d, want %d", model, got, want)
		}
	}
}

func TestParse_ModelFindings(t *testing.T) {
	cases := []struct {
		model string
		want  string // "" = no finding
	}{
		{"sonnet", ""},
		{"inherit", ""},
		{"claude-sonnet-5", ""},
		{"qwen3.6-27b", "needs a claude-router route"},
		{"Opus", `did you mean "opus"`},
	}
	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			_, findings := Claude.Parse("", fm("name: r\ndescription: d\nmodel: "+tc.model))
			f, ok := findingFor(findings, "model")
			if tc.want == "" {
				if ok {
					t.Errorf("unexpected finding %v", f)
				}
				return
			}
			if !ok || f.Severity != Warning || !strings.Contains(f.Message, tc.want) {
				t.Errorf("findings = %v, want warning containing %q", findings, tc.want)
			}
		})
	}
}

func TestCheckDuplicates(t *testing.T) {
	entries := []Entry{
		{File: "a.md", Def: &Definition{Name: "dup"}},
		{File: "b.md", Def: &Definition{Name: "dup"}},
		{File: "c.md", Def: &Definition{Name: "solo"}},
		{File: "d.md"},
	}
	checkDuplicates(Claude, entries)
	for i, wantErr := range []bool{true, true, false, false} {
		if got := HasErrors(entries[i].Findings); got != wantErr {
			t.Errorf("%s: error=%v, want %v (%v)", entries[i].File, got, wantErr, entries[i].Findings)
		}
	}
	if !strings.Contains(entries[0].Findings[0].Message, "b.md") {
		t.Errorf("message should name the other file: %s", entries[0].Findings[0].Message)
	}
}

func TestEnvFindings(t *testing.T) {
	findings := EnvFindings(map[string]map[string]string{
		"env profile 'on'":    {ForceModelEnv: "1"},
		"env profile 'off'":   {ForceModelEnv: "0"},
		"env profile 'other'": {"FOO": "1"},
	})
	if len(findings) != 1 || !strings.Contains(findings[0].Message, "'on'") {
		t.Errorf("findings = %v, want one for profile 'on'", findings)
	}
}

// withRoutesFor stubs the workspace's claude-router routes for one test.
func withRoutesFor(t *testing.T, r Routes) {
	t.Helper()
	old := RoutesFor
	RoutesFor = func(string) Routes { return r }
	t.Cleanup(func() { RoutesFor = old })
}

func TestListStore_ModelNeedsARoute(t *testing.T) {
	withHome(t)
	save := func(name, model string) {
		t.Helper()
		data := fm("name: " + name + "\ndescription: d\nmodel: " + model)
		if _, err := Claude.Save("default", name+".md", data, SourceManaged, "", false); err != nil {
			t.Fatal(err)
		}
	}
	save("local", "qwen3.6-27b")
	save("cloud", "sonnet")
	save("typo", "Opus")

	modelFinding := func(name string) (Finding, bool) {
		t.Helper()
		entries, err := Claude.ListStore("default")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.Name() == name {
				return findingFor(e.Findings, "model")
			}
		}
		t.Fatalf("no entry %q", name)
		return Finding{}, false
	}

	// Plugin off: a hint, as before.
	withRoutesFor(t, Routes{})
	if f, ok := modelFinding("local"); !ok || f.Severity != Warning || !strings.Contains(f.Message, "needs a claude-router route") {
		t.Errorf("plugin off: %v %v", f, ok)
	}

	// Plugin on with a route for the model: nothing to report.
	withRoutesFor(t, Routes{Enabled: true, IDs: []string{"gemma", "qwen3.6-27b"}})
	if f, ok := modelFinding("local"); ok {
		t.Errorf("routed model: unexpected finding %v", f)
	}

	// Plugin on, other routes: the subagent cannot work.
	withRoutesFor(t, Routes{Enabled: true, IDs: []string{"gemma"}})
	f, ok := modelFinding("local")
	if !ok || f.Severity != Error || !strings.Contains(f.Message, "no claude-router route") || !strings.Contains(f.Message, "routes: gemma") {
		t.Errorf("unrouted model: %v %v", f, ok)
	}

	// Plugin on without any route.
	withRoutesFor(t, Routes{Enabled: true})
	if f, ok := modelFinding("local"); !ok || f.Severity != Error || !strings.Contains(f.Message, "none configured") {
		t.Errorf("no routes: %v %v", f, ok)
	}
	// Claude models never need a route, and a misspelt alias stays a hint
	// about the spelling.
	if f, ok := modelFinding("cloud"); ok {
		t.Errorf("claude model: unexpected finding %v", f)
	}
	if f, ok := modelFinding("typo"); !ok || f.Severity != Warning || !strings.Contains(f.Message, "did you mean") {
		t.Errorf("misspelt alias: %v %v", f, ok)
	}
}

// A model without a route must not keep a file out of the store: the route
// can be added afterwards.
func TestSave_UnroutedModelIsStored(t *testing.T) {
	withHome(t)
	withRoutesFor(t, Routes{Enabled: true})
	data := fm("name: local\ndescription: d\nmodel: qwen3.6-27b")
	if _, err := Claude.Save("default", "local.md", data, SourceManaged, "", false); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func TestRoutesOnlyApplyToClaudeCode(t *testing.T) {
	withHome(t)
	withRoutesFor(t, Routes{Enabled: true})
	// OpenCode names models provider/model; claude-router has no say there.
	data := []byte("---\ndescription: d\nmode: subagent\nmodel: local/qwen3.6-27b\n---\nbody\n")
	if _, err := OpenCode.Save("default", "helper.md", data, SourceManaged, "", false); err != nil {
		t.Fatal(err)
	}
	entries, err := OpenCode.ListStore("default")
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListStore = %v, %v", entries, err)
	}
	if HasErrors(entries[0].Findings) {
		t.Errorf("claude-router routes were applied to OpenCode: %v", entries[0].Findings)
	}
}

func TestParse_AutoCompactWindowValue(t *testing.T) {
	cases := map[string]int{
		"autoCompactWindow: 200000":  200000,
		"autoCompactWindow: 100000":  100000,
		"autoCompactWindow: 200k":    0, // not an integer: ignored
		"autoCompactWindow: 50000":   0, // below the range: ignored
		"autoCompactWindow: 2000000": 0, // above the range: ignored
		"color: blue":                0, // not set
	}
	for field, want := range cases {
		def, _ := Claude.Parse("", fm("name: r\ndescription: d\n"+field))
		if def == nil || def.AutoCompactWindow != want {
			t.Errorf("%s: AutoCompactWindow = %+v, want %d", field, def, want)
		}
	}
}

// The router refuses a route named like a Claude Code alias; its list has
// to cover every alias this package knows.
func TestRouterRefusesEveryModelAlias(t *testing.T) {
	for _, alias := range ModelAliases {
		if _, err := router.ParseRoutes(alias + "=http://10.0.0.1:8080"); err == nil {
			t.Errorf("a route named %q was accepted", alias)
		}
	}
}

func TestSkipErrorsLeavesOutRouteErrors(t *testing.T) {
	withHome(t)
	withRoutesFor(t, Routes{Enabled: true, IDs: []string{"other"}})
	if _, err := Claude.Save("default", "m.md", fm("name: m\ndescription: d\nmodel: local-x"), SourceManaged, "", false); err != nil {
		t.Fatal(err)
	}
	entries, _ := Claude.ListStore("default")
	if len(entries) != 1 || !HasErrors(entries[0].Findings) || SkipErrors(entries[0].Findings) {
		t.Fatalf("findings = %v", entries[0].Findings)
	}
	f, _ := findingFor(entries[0].Findings, "model")
	if !f.NeedsRoute() {
		t.Error("the route error is not marked as one")
	}
	if !SkipErrors([]Finding{{Severity: Error, Field: "name", Message: "missing"}}) {
		t.Error("a missing name is a skip error")
	}
}
