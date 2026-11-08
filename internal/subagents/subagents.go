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

// Package subagents manages the per-workspace store of Claude Code subagent
// definitions (agents/*.md files). The entrypoint links each stored file into
// ~/.claude/agents/ at container start; the store itself is mounted read-only.
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

	"github.com/cloud-exit/exitbox/internal/config"
)

// Source says where a subagent definition lives or came from.
type Source string

const (
	SourceManaged   Source = "managed"   // created or added with exitbox subagents
	SourceImported  Source = "imported"  // copied from the host ~/.claude/agents
	SourceContainer Source = "container" // real file in the workspace's ~/.claude/agents
	SourceProject   Source = "project"   // the project's .claude/agents
)

// manifestFile records the source of imported and added files. Dotfiles are
// never linked into the container.
const manifestFile = ".exitbox-sources.json"

// MaxFileSize bounds a single subagent file.
const MaxFileSize = 1 << 20

// ErrExists is returned when a subagent with the same name is already stored.
var ErrExists = errors.New("already exists")

// ErrNotFound is returned when no stored subagent matches.
var ErrNotFound = errors.New("not found")

// Entry is one subagent file.
type Entry struct {
	File     string      `json:"file"` // path relative to its directory
	Path     string      `json:"path"`
	Source   Source      `json:"source"`
	Origin   string      `json:"origin,omitempty"` // where an added or imported file came from
	Def      *Definition `json:"-"`
	Findings []Finding   `json:"findings,omitempty"`
	// Shadow explains why Claude Code may not load this store entry.
	Shadow string `json:"shadowed_by,omitempty"`
}

// Name returns the frontmatter name, or "" when the file has none.
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

// StoreDir returns the host path of a workspace's subagent store.
func StoreDir(workspace string) string {
	return filepath.Join(config.Home, "profiles", "global", workspace, "subagents")
}

// ContainerAgentsDir returns the host path of the workspace's Claude
// ~/.claude/agents directory as the container sees it.
func ContainerAgentsDir(workspace string) string {
	return filepath.Join(config.WorkspaceAgentDir(workspace, "claude"), ".claude", "agents")
}

// ValidWorkspaceName rejects names that would escape profiles/global.
func ValidWorkspaceName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name &&
		!strings.ContainsAny(name, `/\:`)
}

// ListStore returns every *.md file in the store, validated, with
// duplicate-name errors.
func ListStore(workspace string) ([]Entry, error) {
	dir := StoreDir(workspace)
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
		if !de.Type().IsRegular() || !isAgentFile(de.Name()) {
			continue
		}
		e := loadEntry(filepath.Join(dir, de.Name()), de.Name(), SourceManaged)
		if m, ok := man[de.Name()]; ok {
			e.Source, e.Origin = m.Source, m.Origin
		}
		out = append(out, e)
	}
	CheckDuplicates(out)
	return out, nil
}

// ScanDir returns every *.md file under root, recursively, as Claude Code
// scans an agents directory. Symlinks are skipped when skipLinks is set.
func ScanDir(root string, src Source, skipLinks bool) ([]Entry, error) {
	var out []Entry
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if p == root && errors.Is(walkErr, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return walkErr
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !isAgentFile(d.Name()) {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 && skipLinks {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, loadEntry(p, filepath.ToSlash(rel), src))
		return nil
	})
	return out, err
}

// Inventory lists the store plus the container's and project's own agent
// files, and marks store entries that another file shadows.
func Inventory(workspace, projectDir string) ([]Entry, error) {
	store, err := ListStore(workspace)
	if err != nil {
		return nil, err
	}
	container, err := ScanDir(ContainerAgentsDir(workspace), SourceContainer, true)
	if err != nil {
		return nil, err
	}
	var project []Entry
	if projectDir != "" {
		if project, err = ScanDir(filepath.Join(projectDir, ".claude", "agents"), SourceProject, false); err != nil {
			return nil, err
		}
	}
	for i := range store {
		store[i].Shadow = shadowOf(store[i], container, project)
	}
	out := append(store, container...)
	return append(out, project...), nil
}

func shadowOf(e Entry, container, project []Entry) string {
	name := e.Name()
	for _, c := range container {
		if c.File == e.File {
			return fmt.Sprintf("container file ~/.claude/agents/%s (the link is skipped)", c.File)
		}
	}
	if name == "" {
		return ""
	}
	for _, p := range project {
		if p.Name() == name {
			return fmt.Sprintf("project file .claude/agents/%s (project agents take precedence)", p.File)
		}
	}
	for _, c := range container {
		if c.Name() == name {
			return fmt.Sprintf("container file ~/.claude/agents/%s with the same name (Claude Code loads one by filesystem order)", c.File)
		}
	}
	return ""
}

// Find returns the stored entry whose frontmatter name matches, falling back
// to the file name (with or without .md) for files without a usable name.
func Find(workspace, name string) (Entry, error) {
	entries, err := ListStore(workspace)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range entries {
		if e.Name() == name {
			return e, nil
		}
	}
	for _, e := range entries {
		if e.File == name || strings.TrimSuffix(e.File, ".md") == name {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("subagent %q %w", name, ErrNotFound)
}

// Names returns the frontmatter names in the store, for shell completion.
func Names(workspace string) []string {
	entries, err := ListStore(workspace)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if n == "" {
			n = strings.TrimSuffix(e.File, ".md")
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// Save writes a validated definition into the store. Identity is the
// frontmatter name: an existing entry with that name is replaced only when
// force is set; a new entry is written to <name>.md, renamed on collision.
// It returns the stored file's path.
func Save(workspace string, data []byte, src Source, origin string, force bool) (string, error) {
	def, findings := Parse(data)
	if HasErrors(findings) {
		return "", &InvalidError{Findings: findings}
	}
	dir := StoreDir(workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating subagent store: %w", err)
	}
	entries, err := ListStore(workspace)
	if err != nil {
		return "", err
	}

	file := ""
	for _, e := range entries {
		if e.Name() == def.Name {
			if !force {
				return "", fmt.Errorf("subagent %q %w (%s)", def.Name, ErrExists, e.File)
			}
			file = e.File
			break
		}
	}
	if file == "" {
		file = freeFileName(dir, FileNameFor(def.Name))
	}

	path := filepath.Join(dir, file)
	if err := writeFileAtomic(path, data); err != nil {
		return "", err
	}
	if err := setManifest(dir, file, src, origin); err != nil {
		return "", err
	}
	return path, nil
}

// Remove deletes a stored subagent, matched like Find.
func Remove(workspace, name string) (Entry, error) {
	e, err := Find(workspace, name)
	if err != nil {
		return Entry{}, err
	}
	if err := os.Remove(e.Path); err != nil {
		return Entry{}, err
	}
	if err := setManifest(StoreDir(workspace), e.File, SourceManaged, ""); err != nil {
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
func FileNameFor(name string) string {
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
	return base + ".md"
}

func freeFileName(dir, file string) string {
	stem := strings.TrimSuffix(file, ".md")
	for i := 1; ; i++ {
		cand := file
		if i > 1 {
			cand = fmt.Sprintf("%s-%d.md", stem, i)
		}
		if _, err := os.Lstat(filepath.Join(dir, cand)); errors.Is(err, fs.ErrNotExist) {
			return cand
		}
	}
}

func isAgentFile(name string) bool {
	return strings.HasSuffix(name, ".md") && !strings.HasPrefix(name, ".")
}

func loadEntry(path, rel string, src Source) Entry {
	e := Entry{File: rel, Path: path, Source: src}
	data, err := readAgentFile(path)
	if err != nil {
		e.Findings = []Finding{{Severity: Error, Message: err.Error()}}
		return e
	}
	e.Def, e.Findings = Parse(data)
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
