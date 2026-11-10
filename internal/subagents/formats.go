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
	"regexp"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// mdDoc is a Markdown agent file split into frontmatter fields and body.
type mdDoc struct {
	fields []string // top-level keys, in file order
	values map[string]*yaml.Node
	body   string
}

// readMarkdown parses the frontmatter of a Markdown agent file. A nil doc
// means the file has none that can be read.
func readMarkdown(h *Harness, data []byte) (*mdDoc, []Finding) {
	fm, body, problem := frontmatterBlock(data)
	if problem == noOpeningFence {
		problem += "; without frontmatter " + h.Label + " has no description to delegate by"
	}
	if problem != "" {
		return nil, []Finding{{Severity: Error, Message: problem}}
	}
	var root yaml.Node
	if err := yaml.Unmarshal([]byte(fm), &root); err != nil {
		return nil, []Finding{{Severity: Error, Message: "frontmatter YAML does not parse: " + yamlErr(err)}}
	}
	doc := &mdDoc{values: map[string]*yaml.Node{}, body: body}
	if len(root.Content) == 0 {
		return doc, nil
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		return nil, []Finding{{Severity: Error, Message: "frontmatter must be a YAML mapping of field: value"}}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		key := m.Content[i].Value
		doc.fields = append(doc.fields, key)
		doc.values[key] = m.Content[i+1]
	}
	return doc, nil
}

// text returns a field's scalar value. ok is false when the field is
// absent, null, or not a scalar.
func (d *mdDoc) text(key string) (string, bool) {
	n := d.values[key]
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" {
		return "", false
	}
	return n.Value, true
}

// definition fills in the fields every Markdown format shares.
func (d *mdDoc) definition() *Definition {
	def := &Definition{Fields: d.fields}
	def.Description, _ = d.text("description")
	def.Model, _ = d.text("model")
	if n := d.values["tools"]; n != nil {
		switch n.Kind {
		case yaml.ScalarNode:
			for _, t := range strings.Split(n.Value, ",") {
				if t = strings.TrimSpace(t); t != "" {
					def.Tools = append(def.Tools, t)
				}
			}
		case yaml.SequenceNode:
			for _, item := range n.Content {
				if item.Kind == yaml.ScalarNode {
					def.Tools = append(def.Tools, item.Value)
				}
			}
		}
	}
	return def
}

// missingDescription reports a description that is absent or blank.
func (d *mdDoc) missingDescription(sev Severity, why string) []Finding {
	if v, ok := d.text("description"); ok && strings.TrimSpace(v) != "" {
		return nil
	}
	return []Finding{{Severity: sev, Field: "description", Message: "missing; " + why}}
}

// unknownFields warns about every field outside known.
func (d *mdDoc) unknownFields(known []string, msg string) []Finding {
	var out []Finding
	for _, f := range d.fields {
		if !contains(known, f) {
			out = append(out, Finding{Severity: Warning, Field: f, Message: msg})
		}
	}
	return out
}

// --- Qwen Code ---

var (
	qwenNameRE   = regexp.MustCompile(`^[\p{L}\p{N}_-]+$`)
	qwenReserved = []string{"self", "system", "user", "model", "tool", "config", "default", "main"}
	// qwenUnsupported are Claude Code fields Qwen Code does not read yet.
	qwenUnsupported = []string{"effort", "skills", "initialPrompt", "memory", "isolation"}
)

const qwenMinPrompt = 10

func parseQwen(h *Harness, _ string, data []byte) (*Definition, []Finding) {
	doc, findings := readMarkdown(h, data)
	if doc == nil {
		return nil, findings
	}
	def := doc.definition()

	name, ok := doc.text("name")
	name = strings.TrimSpace(name)
	def.Name = name
	switch {
	case !ok || name == "":
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: "missing; Qwen Code skips the file"})
	case utf8.RuneCountInString(name) < 2:
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: "must be at least 2 characters"})
	case utf8.RuneCountInString(name) > 50:
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: "must be 50 characters or less"})
	case !qwenNameRE.MatchString(name):
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: "may only contain letters, digits, hyphens and underscores"})
	case strings.HasPrefix(name, "-") || strings.HasPrefix(name, "_") || strings.HasSuffix(name, "-") || strings.HasSuffix(name, "_"):
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: "must not start or end with a hyphen or underscore"})
	case contains(qwenReserved, strings.ToLower(name)):
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: fmt.Sprintf("%q is a reserved name", name)})
	}

	findings = append(findings, doc.missingDescription(Error, "Qwen Code skips the file")...)
	if n := utf8.RuneCountInString(strings.TrimSpace(doc.body)); n < qwenMinPrompt {
		findings = append(findings, Finding{Severity: Error, Message: fmt.Sprintf(
			"the prompt after the frontmatter must be at least %d characters; Qwen Code skips the file", qwenMinPrompt)})
	}
	for _, f := range doc.fields {
		if contains(qwenUnsupported, f) {
			findings = append(findings, Finding{Severity: Warning, Field: f, Message: "Qwen Code does not read this Claude Code field yet"})
		}
	}
	return def, findings
}

// --- Kimi Code CLI ---

var (
	kebabRE      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	kimiFields   = []string{"name", "description", "whenToUse", "override", "tools", "disallowedTools", "subagents"}
	kimiBuiltins = []string{"coder", "explore", "plan"}
)

func parseKimi(h *Harness, file string, data []byte) (*Definition, []Finding) {
	doc, findings := readMarkdown(h, data)
	if doc == nil {
		return nil, findings
	}
	def := doc.definition()

	name, explicit := doc.text("name")
	if !explicit {
		name = h.stem(file)
	}
	def.Name = name
	switch {
	case kebabRE.MatchString(name):
	case explicit:
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: "must be kebab-case (lowercase letters, digits, hyphens); Kimi Code skips the file"})
	default:
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: fmt.Sprintf(
			"missing, and the file name %q is not kebab-case; Kimi Code skips the file", name)})
	}
	if override, _ := doc.text("override"); contains(kimiBuiltins, name) && override != "true" {
		findings = append(findings, Finding{Severity: Error, Field: "name", Message: fmt.Sprintf(
			"%q is a built-in agent; Kimi Code keeps the built-in one unless override: true is set", name)})
	}

	findings = append(findings, doc.missingDescription(Error, "Kimi Code skips the file")...)
	findings = append(findings, doc.unknownFields(kimiFields, "unknown field; Kimi Code ignores it")...)
	return def, findings
}

// --- Cursor CLI ---

var (
	cursorNameRE = regexp.MustCompile(`^[a-z0-9-]+$`)
	cursorFields = []string{"name", "description", "model", "readonly", "is_background"}
)

func parseCursor(h *Harness, file string, data []byte) (*Definition, []Finding) {
	doc, findings := readMarkdown(h, data)
	if doc == nil {
		return nil, findings
	}
	def := doc.definition()

	name, explicit := doc.text("name")
	if !explicit {
		name = h.stem(file)
	}
	def.Name = name
	if !cursorNameRE.MatchString(name) {
		findings = append(findings, Finding{Severity: Warning, Field: "name", Message: "Cursor asks for lowercase letters and hyphens"})
	}
	findings = append(findings, doc.missingDescription(Warning, "Cursor uses it to decide when to delegate")...)
	findings = append(findings, doc.unknownFields(cursorFields, "not a documented Cursor subagent field")...)
	return def, findings
}

// --- GitHub Copilot CLI ---

var copilotFields = []string{
	"name", "description", "target", "tools", "model", "disable-model-invocation",
	"user-invocable", "infer", "mcp-servers", "metadata", "argument-hint", "handoffs",
}

const copilotMaxPrompt = 30000

func parseCopilot(h *Harness, file string, data []byte) (*Definition, []Finding) {
	doc, findings := readMarkdown(h, data)
	if doc == nil {
		return nil, findings
	}
	def := doc.definition()
	// Copilot identifies an agent by its file name; name is a display name.
	def.Name = h.stem(file)

	findings = append(findings, doc.missingDescription(Error, "Copilot requires it")...)
	if utf8.RuneCountInString(doc.body) > copilotMaxPrompt {
		findings = append(findings, Finding{Severity: Error, Message: fmt.Sprintf(
			"the prompt after the frontmatter is longer than %d characters, Copilot's limit", copilotMaxPrompt)})
	}
	if _, ok := doc.values["infer"]; ok {
		findings = append(findings, Finding{Severity: Warning, Field: "infer", Message: "retired; use disable-model-invocation and user-invocable"})
	}
	findings = append(findings, doc.unknownFields(copilotFields, "not a documented custom agent property")...)
	return def, findings
}

// --- OpenCode ---

var (
	openCodeModes = []string{"primary", "subagent", "all"}
	// openCodeLegacy maps fields the current docs call deprecated to their
	// replacement, when there is one.
	openCodeLegacy = map[string]string{
		"temperature": "", "top_p": "", "prompt": "the Markdown body", "disable": "disabled",
		"permission": "permissions", "tools": "permissions", "maxSteps": "steps",
	}
)

func parseOpenCode(h *Harness, file string, data []byte) (*Definition, []Finding) {
	doc, findings := readMarkdown(h, data)
	if doc == nil {
		return nil, findings
	}
	def := doc.definition()
	// OpenCode names an agent after its file.
	def.Name = h.stem(file)

	mode, ok := doc.text("mode")
	switch {
	case !ok || mode == "primary":
		findings = append(findings, Finding{Severity: Warning, Field: "mode", Message: "OpenCode loads this as a primary agent, not a subagent; set mode: subagent"})
	case !contains(openCodeModes, mode):
		findings = append(findings, Finding{Severity: Warning, Field: "mode", Message: fmt.Sprintf(
			"%q is not one of %s", mode, strings.Join(openCodeModes, ", "))})
	}
	findings = append(findings, doc.missingDescription(Warning, "OpenCode shows it to the model that picks a subagent")...)
	if def.Model != "" && !strings.Contains(def.Model, "/") {
		findings = append(findings, Finding{Severity: Warning, Field: "model", Message: fmt.Sprintf("%q is not in provider/model form", def.Model)})
	}
	for _, f := range doc.fields {
		repl, legacy := openCodeLegacy[f]
		if !legacy {
			continue
		}
		msg := "deprecated in the current OpenCode docs"
		if repl != "" {
			msg += "; use " + repl
		}
		findings = append(findings, Finding{Severity: Warning, Field: f, Message: msg})
	}
	return def, findings
}

// --- OpenAI Codex ---

var (
	codexRequired = []string{"name", "description", "developer_instructions"}
	// codexParentOnly are keys an agent file cannot override.
	codexParentOnly = []string{"sandbox_mode", "approval_policy", "model_provider", "mcp_servers"}
)

func parseCodex(_ *Harness, _ string, data []byte) (*Definition, []Finding) {
	doc, err := readTOML(data)
	if err != nil {
		return nil, []Finding{{Severity: Error, Message: "TOML does not parse: " + err.Error()}}
	}
	def := &Definition{Fields: doc.keys, Name: doc.strings["name"], Description: doc.strings["description"], Model: doc.strings["model"]}

	var findings []Finding
	for _, key := range codexRequired {
		if strings.TrimSpace(doc.strings[key]) == "" {
			findings = append(findings, Finding{Severity: Error, Field: key, Message: "missing or not a string; Codex requires it"})
		}
	}
	for _, key := range doc.keys {
		if contains(codexParentOnly, key) {
			findings = append(findings, Finding{Severity: Warning, Field: key, Message: "an agent file cannot override this; the parent session's setting applies"})
		}
	}
	return def, findings
}

// --- scaffolds ---

func scaffoldBody(name string) string {
	return "You are " + name + ". Describe the role, the steps to follow, and what to return.\n"
}

// markdownAgent renders frontmatter, given as a struct, and a prompt body.
func markdownAgent(frontmatter any, name string) ([]byte, error) {
	fm, err := yaml.Marshal(frontmatter)
	if err != nil {
		return nil, err
	}
	return []byte("---\n" + string(fm) + "---\n\n" + scaffoldBody(name)), nil
}

// toolList splits --tools into the YAML list most agents want.
func toolList(tools string) []string {
	var out []string
	for _, t := range strings.Split(tools, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// noField rejects a --flag the agent's format has no field for.
func noField(h *Harness, flag, value string) error {
	if value == "" {
		return nil
	}
	return fmt.Errorf("%s subagents have no %s field; drop --%s", h.Label, flag, flag)
}

func scaffoldClaude(_ *Harness, o ScaffoldOptions) ([]byte, error) {
	return markdownAgent(struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Tools       string `yaml:"tools,omitempty"`
		Model       string `yaml:"model,omitempty"`
	}{o.Name, o.Description, o.Tools, o.Model}, o.Name)
}

func scaffoldQwen(_ *Harness, o ScaffoldOptions) ([]byte, error) {
	return markdownAgent(struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Tools       []string `yaml:"tools,omitempty"`
		Model       string   `yaml:"model,omitempty"`
	}{o.Name, o.Description, toolList(o.Tools), o.Model}, o.Name)
}

func scaffoldKimi(h *Harness, o ScaffoldOptions) ([]byte, error) {
	if err := noField(h, "model", o.Model); err != nil {
		return nil, err
	}
	return markdownAgent(struct {
		Name        string   `yaml:"name"`
		Description string   `yaml:"description"`
		Tools       []string `yaml:"tools,omitempty"`
	}{o.Name, o.Description, toolList(o.Tools)}, o.Name)
}

func scaffoldCursor(h *Harness, o ScaffoldOptions) ([]byte, error) {
	if err := noField(h, "tools", o.Tools); err != nil {
		return nil, err
	}
	return markdownAgent(struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Model       string `yaml:"model,omitempty"`
	}{o.Name, o.Description, o.Model}, o.Name)
}

func scaffoldCopilot(_ *Harness, o ScaffoldOptions) ([]byte, error) {
	return markdownAgent(struct {
		Description string   `yaml:"description"`
		Tools       []string `yaml:"tools,omitempty"`
		Model       string   `yaml:"model,omitempty"`
	}{o.Description, toolList(o.Tools), o.Model}, o.Name)
}

func scaffoldOpenCode(h *Harness, o ScaffoldOptions) ([]byte, error) {
	if err := noField(h, "tools", o.Tools); err != nil {
		return nil, err
	}
	return markdownAgent(struct {
		Description string `yaml:"description"`
		Mode        string `yaml:"mode"`
		Model       string `yaml:"model,omitempty"`
	}{o.Description, "subagent", o.Model}, o.Name)
}

func scaffoldCodex(h *Harness, o ScaffoldOptions) ([]byte, error) {
	if err := noField(h, "tools", o.Tools); err != nil {
		return nil, err
	}
	var b strings.Builder
	b.WriteString("name = " + tomlQuote(o.Name) + "\n")
	b.WriteString("description = " + tomlQuote(o.Description) + "\n")
	if o.Model != "" {
		b.WriteString("model = " + tomlQuote(o.Model) + "\n")
	}
	b.WriteString("developer_instructions = \"\"\"\n" + strings.ReplaceAll(scaffoldBody(o.Name), `\`, `\\`) + "\"\"\"\n")
	return []byte(b.String()), nil
}
