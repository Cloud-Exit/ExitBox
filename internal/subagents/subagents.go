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

// Package subagents manages the per-workspace stores of subagent definitions,
// one per coding agent (see Harness). The entrypoint puts each stored file
// into the agent's own agents directory at container start; the stores are
// mounted read-only.
package subagents

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Source says where a subagent definition lives or came from.
type Source string

const (
	SourceManaged   Source = "managed"   // created or added with exitbox subagents
	SourceImported  Source = "imported"  // copied from the host's agents directory
	SourceContainer Source = "container" // real file in the workspace's agents directory
	SourceProject   Source = "project"   // the project's agents directory
)

// manifestFile records the source of imported and added files. Dotfiles are
// never put into the container.
const manifestFile = ".exitbox-sources.json"

// copiesFile is written by the entrypoint next to the files it copied into
// an agent's agents directory, one file name per line.
const copiesFile = ".exitbox-subagents"

// MaxFileSize bounds a single subagent file.
const MaxFileSize = 1 << 20

// ErrExists is returned when a subagent with the same name is already stored.
var ErrExists = errors.New("already exists")

// ErrNotFound is returned when no stored subagent matches.
var ErrNotFound = errors.New("not found")

// Entry is one subagent file.
type Entry struct {
	Agent    string      `json:"agent"`
	File     string      `json:"file"` // path relative to its directory
	Path     string      `json:"path"`
	Source   Source      `json:"source"`
	Origin   string      `json:"origin,omitempty"` // where an added or imported file came from
	Def      *Definition `json:"-"`
	Findings []Finding   `json:"findings,omitempty"`
	// Shadow explains why the agent may not load this store entry.
	Shadow string `json:"shadowed_by,omitempty"`
}

// Name returns the subagent's name, or "" when the file has none.
func (e Entry) Name() string {
	if e.Def == nil {
		return ""
	}
	return e.Def.Name
}

type manifestEntry struct {
	Source Source `json:"source"`
	Origin string `json:"origin,omitempty"`
}

// ValidWorkspaceName rejects names that would escape profiles/global.
func ValidWorkspaceName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name &&
		!strings.ContainsAny(name, `/\:`)
}

// ListStore returns every subagent file in the agent's store, validated,
// with duplicate-name errors.
func (h *Harness) ListStore(workspace string) ([]Entry, error) {
	if !h.HasSubagents() {
		return nil, nil
	}
	dir := h.StoreDir(workspace)
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	man, err := readManifest(dir)
	if err != nil {
		return nil, err
	}
	var out []Entry
	for _, de := range des {
		if !de.Type().IsRegular() || !h.isAgentFile(de.Name()) {
			continue
		}
		e := h.loadEntry(filepath.Join(dir, de.Name()), de.Name(), SourceManaged)
		e.Findings = h.withRoutes(workspace, e.Def, e.Findings)
		if m, ok := man[de.Name()]; ok {
			e.Source, e.Origin = m.Source, m.Origin
		}
		out = append(out, e)
	}
	checkDuplicates(h, out)
	return out, nil
}

// ScanDir returns the subagent files under root as the agent would find
// them: recursively when it scans subdirectories. Symlinks are skipped when
// skipLinks is set.
func (h *Harness) ScanDir(root string, src Source, skipLinks bool) ([]Entry, error) {
	var out []Entry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root && errors.Is(walkErr, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return walkErr
		}
		if d.IsDir() {
			if p != root && (!h.Recursive || strings.HasPrefix(d.Name(), ".")) {
				return fs.SkipDir
			}
			return nil
		}
		if !h.isAgentFile(d.Name()) {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 && skipLinks {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, h.loadEntry(p, filepath.ToSlash(rel), src))
		return nil
	})
	return out, err
}

// Inventory lists the store plus the container's and project's own agent
// files, and marks store entries that another file shadows.
func (h *Harness) Inventory(workspace, projectDir string) ([]Entry, error) {
	store, err := h.ListStore(workspace)
	if err != nil || !h.HasSubagents() {
		return nil, err
	}
	containerDir := h.ContainerAgentsDir(workspace)
	scanned, err := h.ScanDir(containerDir, SourceContainer, true)
	if err != nil {
		return nil, err
	}
	// Files the entrypoint copied in are the store's own, not the container's.
	copies := managedCopies(containerDir)
	var container []Entry
	for _, c := range scanned {
		if !copies[c.File] {
			container = append(container, c)
		}
	}
	var project []Entry
	if projectDir != "" && h.Project != nil {
		if project, err = h.ScanDir(filepath.Join(projectDir, filepath.Join(h.Project...)), SourceProject, false); err != nil {
			return nil, err
		}
	}
	for i := range store {
		store[i].Shadow = h.shadowOf(store[i], container, project)
	}
	// Container and project files run in the same session as the store's,
	// so the same routes decide whether their model can be served.
	for _, list := range [][]Entry{container, project} {
		for i := range list {
			list[i].Findings = h.withRoutes(workspace, list[i].Def, list[i].Findings)
		}
	}
	out := append(store, container...)
	return append(out, project...), nil
}

func (h *Harness) shadowOf(e Entry, container, project []Entry) string {
	name := e.Name()
	for _, c := range container {
		if c.File == e.File {
			return fmt.Sprintf("container file %s/%s (the managed one is skipped)", h.AgentsDirLabel(), c.File)
		}
	}
	if name == "" {
		return ""
	}
	for _, p := range project {
		if p.Name() == name {
			return fmt.Sprintf("project file %s/%s (project agents take precedence)", strings.Join(h.Project, "/"), p.File)
		}
	}
	for _, c := range container {
		if c.Name() == name {
			return fmt.Sprintf("container file %s/%s with the same name (%s loads only one of them)", h.AgentsDirLabel(), c.File, h.Label)
		}
	}
	return ""
}

// managedCopies reads the names of the files the entrypoint copied into an
// agents directory.
func managedCopies(dir string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(dir, copiesFile))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out[line] = true
		}
	}
	return out
}

// Find returns the stored entry whose name matches, falling back to the
// file name (with or without extension) for files without a usable name.
func (h *Harness) Find(workspace, name string) (Entry, error) {
	entries, err := h.ListStore(workspace)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range entries {
		if e.Name() == name {
			return e, nil
		}
	}
	for _, e := range entries {
		if e.File == name || h.stem(e.File) == name {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("%s subagent %q %w", h.Label, name, ErrNotFound)
}

// Names returns the names in the store, for shell completion.
func (h *Harness) Names(workspace string) []string {
	entries, err := h.ListStore(workspace)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if n == "" {
			n = h.stem(e.File)
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Save writes a validated definition into the agent's store. file is the
// definition's file name, which names the subagent for agents that go by
// file name. An existing entry with the same name is replaced only when
// force is set; a new entry is written to <name><ext>, renamed on collision.
// It returns the stored file's path.
func (h *Harness) Save(workspace, file string, data []byte, src Source, origin string, force bool) (string, error) {
	if !h.HasSubagents() {
		return "", fmt.Errorf("%s has no native subagents", h.Label)
	}
	def, findings := h.Parse(file, data)
	if HasErrors(findings) || def == nil {
		return "", &InvalidError{Findings: findings}
	}
	dir := h.StoreDir(workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating subagent store: %w", err)
	}
	entries, err := h.ListStore(workspace)
	if err != nil {
		return "", err
	}

	stored := ""
	for _, e := range entries {
		if e.Name() == def.Name {
			if !force {
				return "", fmt.Errorf("%s subagent %q %w (%s)", h.Label, def.Name, ErrExists, e.File)
			}
			stored = e.File
			break
		}
	}
	if stored == "" {
		stored = h.freeFileName(dir, h.FileNameFor(def.Name))
	}

	path := filepath.Join(dir, stored)
	if err := writeFileAtomic(path, data); err != nil {
		return "", err
	}
	if err := setManifest(dir, stored, src, origin); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes a stored subagent, matched like Find.
func (h *Harness) Remove(workspace, name string) (Entry, error) {
	e, err := h.Find(workspace, name)
	if err != nil {
		return Entry{}, err
	}
	if err := os.Remove(e.Path); err != nil {
		return Entry{}, err
	}
	if err := setManifest(h.StoreDir(workspace), e.File, SourceManaged, ""); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// InvalidError carries the findings that stopped a save.
type InvalidError struct {
	Findings []Finding
}

func (e *InvalidError) Error() string {
	var msgs []string
	for _, f := range e.Findings {
		if f.Severity == Error {
			msgs = append(msgs, f.String())
		}
	}
	return "invalid subagent: " + strings.Join(msgs, "; ")
}

// FileNameFor maps a subagent name to a safe store file name.
func (h *Harness) FileNameFor(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '.'
		if ok {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	base := strings.Trim(b.String(), "-.")
	if base == "" {
		base = "subagent"
	}
	if len(base) > 200 {
		base = base[:200]
	}
	return base + h.Ext
}

func (h *Harness) freeFileName(dir, file string) string {
	stem := h.stem(file)
	for i := 1; ; i++ {
		cand := file
		if i > 1 {
			cand = fmt.Sprintf("%s-%d%s", stem, i, h.Ext)
		}
		if _, err := os.Lstat(filepath.Join(dir, cand)); errors.Is(err, fs.ErrNotExist) {
			return cand
		}
	}
}

func (h *Harness) loadEntry(path, rel string, src Source) Entry {
	e := Entry{Agent: h.Agent, File: rel, Path: path, Source: src}
	data, err := readAgentFile(path)
	if err != nil {
		e.Findings = []Finding{{Severity: Error, Message: err.Error()}}
		return e
	}
	e.Def, e.Findings = h.Parse(rel, data)
	return e
}

// ReadFile reads a subagent file, refusing directories and oversized files.
func ReadFile(path string) ([]byte, error) {
	return readAgentFile(path)
}

func readAgentFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	if info.Size() > MaxFileSize {
		return nil, fmt.Errorf("%s is larger than %d bytes", path, MaxFileSize)
	}
	return os.ReadFile(path)
}

func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Chmod(name, 0o644); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func readManifest(dir string) (map[string]manifestEntry, error) {
	m := map[string]manifestEntry{}
	data, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if errors.Is(err, fs.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("reading %s: %w", manifestFile, err)
	}
	return m, nil
}

// setManifest records a file's source; managed with no origin is the default
// and removes the record.
func setManifest(dir, file string, src Source, origin string) error {
	m, err := readManifest(dir)
	if err != nil {
		return err
	}
	if src == SourceManaged && origin == "" {
		if _, ok := m[file]; !ok {
			return nil
		}
		delete(m, file)
	} else {
		m[file] = manifestEntry{Source: src, Origin: origin}
	}
	if len(m) == 0 {
		err := os.Remove(filepath.Join(dir, manifestFile))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, manifestFile), append(data, '\n'))
}
