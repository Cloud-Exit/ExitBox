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
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// MasterSkillName is the master skill's directory, and the name agents see.
// The Agent Skills format wants the frontmatter name to match the directory.
const MasterSkillName = "master-skill"

const (
	masterSkillFile = "SKILL.md"
	// maxSkillDescription is the Agent Skills limit on a description.
	maxSkillDescription = 1024
)

// MasterSkillDir returns the host directory of the agent's master skill in
// a workspace. It sits inside the agent's subagent store, so the container
// sees it read-only.
func (h *Harness) MasterSkillDir(workspace string) string {
	return filepath.Join(h.StoreDir(workspace), MasterSkillName)
}

// MasterSkillPath returns the host path of the agent's master SKILL.md.
func (h *Harness) MasterSkillPath(workspace string) string {
	return filepath.Join(h.MasterSkillDir(workspace), masterSkillFile)
}

// MasterSkillExists reports whether the agent has a master skill.
func (h *Harness) MasterSkillExists(workspace string) bool {
	info, err := os.Stat(h.MasterSkillPath(workspace))
	return err == nil && info.Mode().IsRegular()
}

// EnsureMasterSkill writes the default master skill when the agent has
// none, with a route for every stored subagent. An existing file is never
// touched. It returns the path and whether the file was created.
func (h *Harness) EnsureMasterSkill(workspace string) (string, bool, error) {
	if !h.Skills {
		return "", false, fmt.Errorf("ExitBox does not link skills for %s", h.Label)
	}
	path := h.MasterSkillPath(workspace)
	if _, err := os.Lstat(path); err == nil {
		return path, false, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", false, err
	}
	entries, err := h.ListStore(workspace)
	if err != nil {
		return "", false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", false, fmt.Errorf("creating master skill: %w", err)
	}
	if err := writeFileAtomic(path, h.DefaultMasterSkill(entries)); err != nil {
		return "", false, err
	}
	return path, true, nil
}

// ReadMasterSkill returns the agent's master skill, or ErrNotFound.
func (h *Harness) ReadMasterSkill(workspace string) ([]byte, error) {
	data, err := readAgentFile(h.MasterSkillPath(workspace))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%s master skill %w", h.Label, ErrNotFound)
	}
	return data, err
}

// RemoveMasterSkill deletes the agent's master skill, or returns ErrNotFound.
func (h *Harness) RemoveMasterSkill(workspace string) error {
	dir := h.MasterSkillDir(workspace)
	if _, err := os.Lstat(dir); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%s master skill %w", h.Label, ErrNotFound)
	} else if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}

// MasterSkillFindings validates the agent's master skill. It returns
// nothing when there is none.
func (h *Harness) MasterSkillFindings(workspace string) ([]Finding, error) {
	data, err := h.ReadMasterSkill(workspace)
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return CheckMasterSkill(data), nil
}

// CheckMasterSkill validates a master SKILL.md against the Agent Skills
// format. Errors are files that agents skip.
func CheckMasterSkill(data []byte) []Finding {
	const skipped = "; agents skip a skill without it"
	fm, _, problem := frontmatterBlock(data)
	if problem != "" {
		return []Finding{{Severity: Error, Message: problem + "; agents skip a skill without frontmatter"}}
	}
	var fields map[string]any
	if err := yaml.Unmarshal([]byte(fm), &fields); err != nil {
		return []Finding{{Severity: Error, Message: "frontmatter YAML does not parse: " + yamlErr(err)}}
	}

	var out []Finding
	name, ok := fields["name"].(string)
	switch {
	case !ok || strings.TrimSpace(name) == "":
		out = append(out, Finding{Severity: Error, Field: "name", Message: "missing or not a string" + skipped})
	case name != MasterSkillName:
		out = append(out, Finding{Severity: Warning, Field: "name", Message: fmt.Sprintf(
			"%q differs from the skill's directory %q; some agents skip a skill whose name does not match", name, MasterSkillName)})
	}
	desc, ok := fields["description"].(string)
	switch {
	case !ok || strings.TrimSpace(desc) == "":
		out = append(out, Finding{Severity: Error, Field: "description", Message: "missing or not a string" + skipped})
	case utf8.RuneCountInString(desc) > maxSkillDescription:
		out = append(out, Finding{Severity: Warning, Field: "description", Message: fmt.Sprintf(
			"is longer than %d characters; some agents truncate or skip it", maxSkillDescription)})
	}
	return out
}

const masterSkillHead = `---
name: ` + MasterSkillName + `
description: Workspace rules for which agent does which work. Read it before starting any task with more than one step, and before delegating to a subagent or a third-party agent.
---

# Master skill

The owner of this workspace decides here which agent handles which kind of
work. Follow these rules before your own judgement about delegation.

## How to route

1. Before starting a task, compare it with the routes below, top to bottom. The first match wins.
2. On a match, delegate the task to that agent. Write a brief that stands on its own: the goal, the files involved, the constraints, and what to return. The agent does not see this conversation.
3. Check the result before relying on it, and say which agent did the work.
4. If no route matches, or the agent is not available in this session, do the work yourself.

## Routes

| When the task is | Send it to | Notes |
|:--|:--|:--|
`

const masterSkillTail = `
## Third-party agents

A third-party agent is a subagent that runs on a model from another provider,
or a separate agent CLI. When a route sends work to one:

- Send only what the task needs. Never put secrets, credentials or vault values in the brief.
- Treat the result as a draft: read the changes and run the tests before building on them.
`

// DefaultMasterSkill renders the starting master skill, with one route per
// loadable subagent in entries.
func (h *Harness) DefaultMasterSkill(entries []Entry) []byte {
	var b strings.Builder
	b.WriteString(masterSkillHead)
	rows := 0
	for _, e := range entries {
		if e.Def == nil || e.Def.Name == "" || HasErrors(e.Findings) {
			continue
		}
		b.WriteString(fmt.Sprintf("| %s | `%s` | %s |\n", tableCell(e.Def.Description), tableCell(e.Def.Name), h.routeNote(e.Def.Model)))
		rows++
	}
	if rows == 0 {
		b.WriteString("| _No routes yet. Add one row per kind of task._ | | |\n")
	}
	b.WriteString(masterSkillTail)
	return []byte(b.String())
}

// routeNote describes the model a route runs on.
func (h *Harness) routeNote(model string) string {
	switch {
	case model == "" || model == "inherit":
		return "main model"
	case h == Claude && ClassifyModel(model) == ModelOther:
		return "third-party model " + tableCell(model)
	}
	return "model " + tableCell(model)
}

// tableCell flattens text into one Markdown table cell.
func tableCell(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return strings.ReplaceAll(s, "|", `\|`)
}
