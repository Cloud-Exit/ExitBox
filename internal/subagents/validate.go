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
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// DocsURL is the Claude Code reference the validator rules come from.
const DocsURL = "https://code.claude.com/docs/en/sub-agents"

// CheckedVersion is the Claude Code release the rules were last checked against.
const CheckedVersion = "2.1.319"

// KnownFields are the frontmatter fields Claude Code reads from a subagent file.
// Claude Code silently ignores any other top-level field.
var KnownFields = []string{
	"name",
	"description",
	"tools",
	"disallowedTools",
	"model",
	"permissionMode",
	"maxTurns",
	"autoCompactWindow",
	"skills",
	"mcpServers",
	"hooks",
	"memory",
	"background",
	"omitClaudeMd",
	"effort",
	"isolation",
	"color",
	"initialPrompt",
	"experimental",
}

// ModelAliases are the non-ID values the model field accepts.
var ModelAliases = []string{"sonnet", "opus", "haiku", "fable", "inherit"}

// MinAutoCompactWindow is the smallest autoCompactWindow Claude Code accepts.
const MinAutoCompactWindow = minAutoCompactWindow

const (
	maxNameLen           = 256
	minAutoCompactWindow = 100000
	maxAutoCompactWindow = 1000000
	// ForceModelEnv makes Claude Code ignore every subagent's model field.
	ForceModelEnv = "CLAUDE_CODE_SUBAGENT_MODEL_FORCE"
)

// Severity ranks a finding.
type Severity int

const (
	// Warning: Claude Code loads the file but may not do what the author meant.
	Warning Severity = iota
	// Error: Claude Code skips the file without telling the user.
	Error
)

func (s Severity) String() string {
	if s == Error {
		return "error"
	}
	return "warning"
}

// MarshalText renders the severity by name in JSON output.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Finding is one validation result.
type Finding struct {
	Severity Severity `json:"severity"`
	Field    string   `json:"field,omitempty"`
	Message  string   `json:"message"`
	// code marks a finding that depends on more than the file, so it can
	// be settled once the workspace is known.
	code string
}

// codeNeedsRoute marks the finding for a model that is not a Claude model.
// Whether that is a problem depends on the workspace's claude-router routes.
const codeNeedsRoute = "model-needs-route"

// Routes is what validation needs to know about the claude-router plugin
// of a workspace.
type Routes struct {
	Enabled bool     // the plugin is enabled for Claude Code
	IDs     []string // model IDs that have a route
}

// RoutesFor reports the claude-router routes of a workspace. The command
// layer sets it from the plugin configuration; by default no workspace has
// routes, which keeps this package free of configuration loading.
var RoutesFor = func(workspace string) Routes { return Routes{} }

// withRoutes settles the findings that depend on the workspace's routes: a
// model that is not a Claude model is fine when a route serves it, and an
// error when the router is on but has no route for it, because Anthropic
// rejects the model and the subagent fails on its first request.
func (h *Harness) withRoutes(workspace string, def *Definition, fs []Finding) []Finding {
	if h != Claude || def == nil {
		return fs
	}
	out := fs[:0:0]
	for _, f := range fs {
		if f.code != codeNeedsRoute {
			out = append(out, f)
			continue
		}
		r := RoutesFor(workspace)
		switch {
		case !r.Enabled:
			out = append(out, f)
		case contains(r.IDs, def.Model):
			// Routed: nothing to report.
		default:
			have := "none configured"
			if len(r.IDs) > 0 {
				have = strings.Join(r.IDs, ", ")
			}
			out = append(out, Finding{Severity: Error, Field: "model", Message: fmt.Sprintf(
				"%q has no claude-router route (routes: %s), so the subagent fails on its first request; add one with: exitbox plugins edit claude claude-router",
				def.Model, have)})
		}
	}
	return out
}

func (f Finding) String() string {
	if f.Field != "" {
		return fmt.Sprintf("%s: %s: %s", f.Severity, f.Field, f.Message)
	}
	return fmt.Sprintf("%s: %s", f.Severity, f.Message)
}

// HasErrors reports whether any finding is an error.
func HasErrors(fs []Finding) bool {
	for _, f := range fs {
		if f.Severity == Error {
			return true
		}
	}
	return false
}

// Definition is what ExitBox reads from a subagent file.
type Definition struct {
	Name        string
	Description string
	Model       string
	Tools       []string
	// AutoCompactWindow is the autoCompactWindow field when Claude Code
	// accepts it, and 0 when the file has none or an ignored one.
	AutoCompactWindow int
	// Fields holds every top-level key, in file order.
	Fields []string
}

// ModelClass says how Claude Code will resolve a model value.
type ModelClass int

const (
	ModelUnset    ModelClass = iota // no model field
	ModelAlias                      // sonnet, opus, haiku, fable, inherit
	ModelClaudeID                   // claude-* model ID
	ModelOther                      // anything else: needs a gateway route
)

// ClassifyModel classifies a frontmatter model value.
func ClassifyModel(model string) ModelClass {
	switch {
	case model == "":
		return ModelUnset
	case contains(ModelAliases, model):
		return ModelAlias
	case strings.HasPrefix(model, "claude-"):
		return ModelClaudeID
	default:
		return ModelOther
	}
}

// parseClaude validates a Claude Code subagent file. A nil Definition means
// Claude Code reads no fields from the file.
func parseClaude(_ *Harness, _ string, data []byte) (*Definition, []Finding) {
	fm, ok, findings := splitFrontmatter(data)
	if !ok {
		return nil, findings
	}

	var root yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &root); err != nil {
		return nil, []Finding{{Severity: Error, Message: "frontmatter YAML does not parse: " + yamlErr(err)}}
	}
	if len(root.Content) == 0 {
		return nil, []Finding{{Severity: Error, Field: "name", Message: "frontmatter is empty; Claude Code treats the file as documentation"}}
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, []Finding{{Severity: Error, Message: "frontmatter must be a YAML mapping of field: value"}}
	}

	def := &Definition{}
	values := map[string]*yaml.Node{}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key := m.Content[i].Value
		def.Fields = append(def.Fields, key)
		values[key] = m.Content[i+1]
	}

	findings = append(findings, checkName(def, values["name"])...)
	findings = append(findings, checkDescription(def, values["description"])...)
	findings = append(findings, checkModel(def, values["model"])...)
	findings = append(findings, checkTools(def, "tools", values["tools"])...)
	findings = append(findings, checkTools(nil, "disallowedTools", values["disallowedTools"])...)
	findings = append(findings, checkAutoCompactWindow(def, values["autoCompactWindow"])...)
	findings = append(findings, checkUnknownFields(def.Fields)...)
	return def, findings
}

const (
	noOpeningFence = "the opening --- must be on line 1"
	noClosingFence = "frontmatter has no closing ---"
)

// frontmatterBlock returns the YAML between the opening and closing ---
// and the text after it, or the reason the file has no frontmatter.
func frontmatterBlock(data []byte) (fm, body, problem string) {
	content := strings.TrimPrefix(string(data), "\uFEFF")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return "", "", noOpeningFence
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], " \t") == "---" {
			return strings.Join(lines[1:i], "\n"), strings.Join(lines[i+1:], "\n"), ""
		}
	}
	return "", "", noClosingFence
}

// splitFrontmatter returns a Claude Code subagent file's frontmatter YAML.
func splitFrontmatter(data []byte) (string, bool, []Finding) {
	fm, _, problem := frontmatterBlock(data)
	switch problem {
	case "":
		return fm, true, nil
	case noOpeningFence:
		problem += "; Claude Code treats the file as documentation"
	}
	return "", false, []Finding{{Severity: Error, Message: problem}}
}

func checkName(def *Definition, n *yaml.Node) []Finding {
	if n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
		return []Finding{{Severity: Error, Field: "name", Message: "missing; Claude Code treats the file as documentation"}}
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return []Finding{{Severity: Error, Field: "name", Message: fmt.Sprintf("must be a string, got %s; quote the value", nodeType(n))}}
	}
	def.Name = n.Value
	switch {
	case strings.TrimSpace(n.Value) == "":
		return []Finding{{Severity: Error, Field: "name", Message: "is empty"}}
	case strings.HasPrefix(n.Value, "-"):
		return []Finding{{Severity: Error, Field: "name", Message: "must not start with -"}}
	case strings.Contains(n.Value, ":"):
		return []Finding{{Severity: Error, Field: "name", Message: "must not contain : (reserved for plugin-scoped names)"}}
	case utf8.RuneCountInString(n.Value) > maxNameLen:
		return []Finding{{Severity: Error, Field: "name", Message: fmt.Sprintf("is longer than %d characters", maxNameLen)}}
	}
	return nil
}

func checkDescription(def *Definition, n *yaml.Node) []Finding {
	if n == nil || (n.Kind == yaml.ScalarNode && n.Tag == "!!null") {
		return []Finding{{Severity: Error, Field: "description", Message: "missing; Claude Code skips the file"}}
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return []Finding{{Severity: Error, Field: "description", Message: fmt.Sprintf("must be a string, got %s", nodeType(n))}}
	}
	if strings.TrimSpace(n.Value) == "" {
		return []Finding{{Severity: Error, Field: "description", Message: "is empty; Claude Code skips the file"}}
	}
	def.Description = n.Value
	return nil
}

func checkModel(def *Definition, n *yaml.Node) []Finding {
	if n == nil {
		return nil
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return []Finding{{Severity: Warning, Field: "model", Message: fmt.Sprintf("must be a string, got %s", nodeType(n))}}
	}
	def.Model = n.Value
	if ClassifyModel(n.Value) != ModelOther {
		return nil
	}
	if lower := strings.ToLower(n.Value); ClassifyModel(lower) != ModelOther {
		return []Finding{{Severity: Warning, Field: "model", Message: fmt.Sprintf("%q is not recognized; did you mean %q?", n.Value, lower)}}
	}
	return []Finding{{Severity: Warning, Field: "model", code: codeNeedsRoute, Message: fmt.Sprintf(
		"%q is not a Claude model (%s, or a claude-* ID); it needs a claude-router route: exitbox plugins enable claude claude-router",
		n.Value, strings.Join(ModelAliases, ", "))}}
}

func checkTools(def *Definition, field string, n *yaml.Node) []Finding {
	if n == nil {
		return nil
	}
	var tools []string
	switch {
	case n.Kind == yaml.ScalarNode && n.Tag == "!!str":
		for _, t := range strings.Split(n.Value, ",") {
			if t = strings.TrimSpace(t); t != "" {
				tools = append(tools, t)
			}
		}
	case n.Kind == yaml.SequenceNode:
		for _, item := range n.Content {
			if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
				return []Finding{{Severity: Warning, Field: field, Message: "list entries must be tool names"}}
			}
			tools = append(tools, item.Value)
		}
	default:
		return []Finding{{Severity: Warning, Field: field, Message: "must be a comma-separated string or a YAML list"}}
	}
	if def != nil {
		def.Tools = tools
	}
	return nil
}

func checkAutoCompactWindow(def *Definition, n *yaml.Node) []Finding {
	if n == nil {
		return nil
	}
	if n.Kind != yaml.ScalarNode || n.Tag != "!!int" {
		return []Finding{{Severity: Warning, Field: "autoCompactWindow", Message: fmt.Sprintf(
			"%q is not a plain integer and Claude Code ignores it (200k reads as 200); write e.g. 200000", n.Value)}}
	}
	var v int64
	if err := n.Decode(&v); err != nil {
		return []Finding{{Severity: Warning, Field: "autoCompactWindow", Message: "not a valid integer: " + yamlErr(err)}}
	}
	if v < minAutoCompactWindow || v > maxAutoCompactWindow {
		return []Finding{{Severity: Warning, Field: "autoCompactWindow", Message: fmt.Sprintf(
			"%d is outside %d to %d and Claude Code ignores it", v, minAutoCompactWindow, maxAutoCompactWindow)}}
	}
	def.AutoCompactWindow = int(v)
	return nil
}

func checkUnknownFields(fields []string) []Finding {
	var out []Finding
	for _, f := range fields {
		if contains(KnownFields, f) {
			continue
		}
		msg := "unknown field; Claude Code ignores it"
		if s := suggestField(f); s != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", s)
		}
		out = append(out, Finding{Severity: Warning, Field: f, Message: msg})
	}
	return out
}

// suggestField returns the known field closest to an unknown one, or "".
func suggestField(f string) string {
	lf := strings.ToLower(f)
	best, bestDist := "", 0
	for _, k := range KnownFields {
		lk := strings.ToLower(k)
		if lk == lf || strings.ReplaceAll(lk, "_", "") == strings.ReplaceAll(lf, "_", "") {
			return k
		}
		d := levenshtein(lf, lk)
		if best == "" || d < bestDist {
			best, bestDist = k, d
		}
	}
	// Only suggest close matches: at most a third of the name changed.
	if bestDist <= max(2, len(f)/3) {
		return best
	}
	return ""
}

// checkDuplicates adds an error to every entry whose name another entry also uses.
func checkDuplicates(h *Harness, entries []Entry) {
	byName := map[string][]int{}
	for i, e := range entries {
		if e.Def != nil && e.Def.Name != "" {
			byName[e.Def.Name] = append(byName[e.Def.Name], i)
		}
	}
	names := make([]string, 0, len(byName))
	for n := range byName {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		idx := byName[n]
		if len(idx) < 2 {
			continue
		}
		for _, i := range idx {
			var others []string
			for _, j := range idx {
				if j != i {
					others = append(others, entries[j].File)
				}
			}
			entries[i].Findings = append(entries[i].Findings, Finding{
				Severity: Error, Field: "name",
				Message: fmt.Sprintf("%q is also used by %s; %s loads only one of them", n, strings.Join(others, ", "), h.Label),
			})
		}
	}
}

// EnvFindings checks environment variable sets (keyed by a label such as an
// env profile name) for settings that override every subagent.
func EnvFindings(envs map[string]map[string]string) []Finding {
	labels := make([]string, 0, len(envs))
	for l := range envs {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	var out []Finding
	for _, l := range labels {
		v, ok := envs[l][ForceModelEnv]
		if !ok || v == "" || v == "0" || strings.EqualFold(v, "false") {
			continue
		}
		out = append(out, Finding{Severity: Warning, Field: ForceModelEnv, Message: fmt.Sprintf(
			"set in %s; Claude Code ignores the model field of every subagent", l)})
	}
	return out
}

func nodeType(n *yaml.Node) string {
	switch n.Kind {
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a mapping"
	}
	return strings.TrimPrefix(n.Tag, "!!")
}

func yamlErr(err error) string {
	return strings.TrimPrefix(err.Error(), "yaml: ")
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}
