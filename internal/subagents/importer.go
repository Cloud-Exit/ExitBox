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
	"path/filepath"
	"sort"
)

// OutcomeStatus says what happened to one file of an add or import.
type OutcomeStatus int

const (
	Added OutcomeStatus = iota
	Skipped
	Failed
)

// Outcome reports one file of an add or import.
type Outcome struct {
	Source   string // the file's path relative to what was added
	Status   OutcomeStatus
	Name     string // subagent name, when the file has one
	File     string // store file name, when added
	Reason   string // why it was skipped or failed; empty for documentation
	Findings []Finding
}

// Tally counts outcomes by status.
func Tally(outcomes []Outcome) (added, skipped, failed int) {
	for _, o := range outcomes {
		switch o.Status {
		case Added:
			added++
		case Skipped:
			skipped++
		case Failed:
			failed++
		}
	}
	return
}

// StoreFiles saves fetched files into the agent's store. Files without a
// name are documentation and skipped without a reason, unless the source is
// a single file. With skipExisting, a name that is already stored is a
// skip, not a failure.
func (h *Harness) StoreFiles(workspace string, files map[string][]byte, origin func(rel string) string,
	src Source, force, skipExisting bool) []Outcome {
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)

	var out []Outcome
	seen := map[string]string{}
	for _, rel := range rels {
		data := files[rel]
		o := Outcome{Source: rel, Status: Failed}
		if len(data) > MaxFileSize {
			o.Reason = fmt.Sprintf("larger than %d bytes, skipped", MaxFileSize)
			out = append(out, o)
			continue
		}
		def, findings := h.Parse(rel, data)
		if def == nil || def.Name == "" {
			if len(files) == 1 {
				o.Reason, o.Findings = "not a subagent", findings
			} else {
				o.Status = Skipped
			}
			out = append(out, o)
			continue
		}
		o.Name = def.Name
		if prev, ok := seen[def.Name]; ok {
			o.Reason = fmt.Sprintf("name %q is also used by %s in this source, skipped", def.Name, prev)
			out = append(out, o)
			continue
		}
		seen[def.Name] = rel

		path, err := h.Save(workspace, rel, data, src, origin(rel), force)
		var inv *InvalidError
		switch {
		case errors.As(err, &inv):
			o.Reason, o.Findings = "invalid, skipped", inv.Findings
		case errors.Is(err, ErrExists) && skipExisting:
			o.Status = Skipped
			o.Reason = fmt.Sprintf("%v, skipped (use --force to replace)", err)
		case errors.Is(err, ErrExists):
			o.Reason = fmt.Sprintf("%v; use --force to replace", err)
		case err != nil:
			o.Reason = err.Error()
		default:
			o.Status, o.File, o.Findings = Added, filepath.Base(path), findings
		}
		out = append(out, o)
	}
	return out
}

// ImportDir copies every subagent file in dir, an agents directory in the
// agent's own layout, into the store. Names that are already stored are
// kept unless force is set.
func (h *Harness) ImportDir(workspace, dir string, force bool) ([]Outcome, error) {
	entries, err := h.ScanDir(dir, SourceImported, false)
	if err != nil {
		return nil, err
	}
	var out []Outcome
	files := map[string][]byte{}
	for _, e := range entries {
		data, err := readAgentFile(e.Path)
		if err != nil {
			out = append(out, Outcome{Source: e.File, Status: Failed, Reason: err.Error()})
			continue
		}
		files[e.File] = data
	}
	stored := h.StoreFiles(workspace, files, func(rel string) string {
		return filepath.Join(dir, filepath.FromSlash(rel))
	}, SourceImported, force, true)
	return append(out, stored...), nil
}

// CountDir returns how many subagents dir holds, for offering an import.
func (h *Harness) CountDir(dir string) int {
	entries, err := h.ScanDir(dir, SourceImported, false)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.Name() != "" {
			n++
		}
	}
	return n
}
