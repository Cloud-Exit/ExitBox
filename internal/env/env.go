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

// Package env stores named environment variable profiles in the workspace KV.
// Each profile is a set of KEY=VALUE pairs serialized as JSON under key
// "env.<profileName>".
package env

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/kvstore"
)

const keyPrefix = "env."
const defaultKey = "env.__default__"

// Profile holds a named set of environment variables.
type Profile struct {
	Name string
	Vars map[string]string
}

// Save persists the profile to the workspace KV.
func Save(workspace string, p *Profile) error {
	store, err := openStore(workspace)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	data, err := json.Marshal(p.Vars)
	if err != nil {
		return fmt.Errorf("marshal vars: %w", err)
	}
	return store.Set([]byte(keyPrefix+p.Name), data)
}

// Load returns the profile or an error if not found.
func Load(workspace, name string) (*Profile, error) {
	store, err := openStore(workspace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()

	val, err := store.Get([]byte(keyPrefix + name))
	if err != nil {
		if errors.Is(err, kvstore.ErrNotFound) {
			return nil, fmt.Errorf("env profile '%s' not found in workspace '%s'", name, workspace)
		}
		return nil, err
	}
	var vars map[string]string
	if err := json.Unmarshal(val, &vars); err != nil {
		return nil, fmt.Errorf("invalid profile data: %w", err)
	}
	return &Profile{Name: name, Vars: vars}, nil
}

// List returns all env profile names in the workspace, sorted.
func List(workspace string) ([]string, error) {
	store, err := openStore(workspace)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()

	var names []string
	err = store.Iterate([]byte(keyPrefix), func(key, _ []byte) error {
		if string(key) == defaultKey {
			return nil
		}
		names = append(names, strings.TrimPrefix(string(key), keyPrefix))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	return names, nil
}

// Delete removes a profile.
func Delete(workspace, name string) error {
	store, err := openStore(workspace)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return store.Delete([]byte(keyPrefix + name))
}

// Exists reports whether a profile with the given name exists.
func Exists(workspace, name string) bool {
	store, err := openStore(workspace)
	if err != nil {
		return false
	}
	defer func() { _ = store.Close() }()
	_, err = store.Get([]byte(keyPrefix + name))
	return err == nil
}

// FormatEnvFile renders vars as KEY=VALUE lines, sorted by key.
// Empty values render as `KEY=` (preserves explicit-empty semantics).
func FormatEnvFile(vars map[string]string) string {
	keys := make([]string, 0, len(vars))
	for k := range vars {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(vars[k])
		b.WriteByte('\n')
	}
	return b.String()
}

// ParseEnvFile parses KEY=VALUE lines into a map. Lines starting with `#`
// or empty lines are ignored. Values are taken verbatim (no shell expansion).
// Returns an error on malformed lines.
func ParseEnvFile(content string) (map[string]string, error) {
	vars := make(map[string]string)
	for i, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// Strip optional `export ` prefix.
		trimmed = strings.TrimPrefix(trimmed, "export ")
		idx := strings.Index(trimmed, "=")
		if idx <= 0 {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE, got %q", i+1, line)
		}
		key := strings.TrimSpace(trimmed[:idx])
		val := trimmed[idx+1:]
		// Strip matching outer quotes.
		val = stripQuotes(val)
		vars[key] = val
	}
	return vars, nil
}

func stripQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// SetDefault marks a profile as the workspace default.
func SetDefault(workspace, name string) error {
	store, err := openStore(workspace)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if _, err := store.Get([]byte(keyPrefix + name)); err != nil {
		if errors.Is(err, kvstore.ErrNotFound) {
			return fmt.Errorf("env profile '%s' not found in workspace '%s'", name, workspace)
		}
		return err
	}
	return store.Set([]byte(defaultKey), []byte(name))
}

// GetDefault returns the default profile name, or "" if none set.
func GetDefault(workspace string) (string, error) {
	store, err := openStore(workspace)
	if err != nil {
		return "", err
	}
	defer func() { _ = store.Close() }()

	val, err := store.Get([]byte(defaultKey))
	if err != nil {
		if errors.Is(err, kvstore.ErrNotFound) {
			return "", nil
		}
		return "", err
	}
	return string(val), nil
}

// ClearDefault removes the default profile setting.
func ClearDefault(workspace string) error {
	store, err := openStore(workspace)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	return store.Delete([]byte(defaultKey))
}

func openStore(workspace string) (*kvstore.Store, error) {
	if workspace == "" {
		return nil, fmt.Errorf("workspace required")
	}
	return kvstore.Open(kvstore.Options{Dir: config.KVDir(workspace)})
}
