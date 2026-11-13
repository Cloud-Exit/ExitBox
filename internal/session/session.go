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

package session

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/kvstore"
	"github.com/cloud-exit/exitbox/internal/project"
)

// KV key helpers

// kvSessionPrefix returns the KV key prefix for sessions of a given agent/project.
func kvSessionPrefix(agentName, projectKey string) string {
	return "session:" + agentName + ":" + projectKey + ":"
}

// kvNameKey returns the KV key for a session's display name.
func kvNameKey(agentName, projectKey, sessionKey string) string {
	return kvSessionPrefix(agentName, projectKey) + sessionKey + ":.name"
}

// kvResumeTokenKey returns the KV key for a session's resume token.
func kvResumeTokenKey(agentName, projectKey, sessionKey string) string {
	return kvSessionPrefix(agentName, projectKey) + sessionKey + ":.resume-token"
}

// kvActiveSessionKey returns the KV key for the active session pointer.
func kvActiveSessionKey(agentName, projectKey string) string {
	return kvSessionPrefix(agentName, projectKey) + ".active-session"
}

// ProjectResumeDir returns the project-scoped resume directory for a workspace/agent.
func ProjectResumeDir(workspaceName, agentName, projectDir string) string {
	projectKey := project.GenerateFolderName(projectDir)
	return filepath.Join(
		config.Home,
		"profiles",
		"global",
		workspaceName,
		agentName,
		"projects",
		projectKey,
	)
}

// ProjectSessionsDir returns the project-scoped sessions directory.
func ProjectSessionsDir(workspaceName, agentName, projectDir string) string {
	return filepath.Join(ProjectResumeDir(workspaceName, agentName, projectDir), "sessions")
}

// SaveResumeToken writes a resume token to the KV store for the given session.
func SaveResumeToken(store *kvstore.Store, agentName, projectKey, sessionKey, token string) error {
	return store.Set([]byte(kvResumeTokenKey(agentName, projectKey, sessionKey)), []byte(token))
}

// LoadResumeToken reads a resume token from the KV store, falling back to
// the filesystem for backward compatibility.
func LoadResumeToken(store *kvstore.Store, agentName, projectKey, sessionKey, workspaceName, projectDir string) (string, error) {
	val, err := store.Get([]byte(kvResumeTokenKey(agentName, projectKey, sessionKey)))
	if err == nil {
		return string(val), nil
	}
	if !errors.Is(err, kvstore.ErrNotFound) {
		return "", err
	}

	// Filesystem fallback.
	sessionsDir := ProjectSessionsDir(workspaceName, agentName, projectDir)
	tokenFile := filepath.Join(sessionsDir, sessionKey, ".resume-token")
	raw, readErr := os.ReadFile(tokenFile)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", kvstore.ErrNotFound
		}
		return "", readErr
	}
	token := strings.TrimSpace(string(raw))
	if token == "" {
		return "", kvstore.ErrNotFound
	}
	// Migrate to KV.
	_ = store.Set([]byte(kvResumeTokenKey(agentName, projectKey, sessionKey)), []byte(token))
	return token, nil
}

// SetActiveSession writes the active session name to the KV store.
func SetActiveSession(store *kvstore.Store, agentName, projectKey, name string) error {
	return store.Set([]byte(kvActiveSessionKey(agentName, projectKey)), []byte(name))
}

// GetActiveSession reads the active session name from the KV store, falling
// back to the filesystem.
func GetActiveSession(store *kvstore.Store, agentName, projectKey, workspaceName, projectDir string) (string, error) {
	val, err := store.Get([]byte(kvActiveSessionKey(agentName, projectKey)))
	if err == nil {
		return string(val), nil
	}
	if !errors.Is(err, kvstore.ErrNotFound) {
		return "", err
	}

	// Filesystem fallback.
	activeFile := filepath.Join(ProjectResumeDir(workspaceName, agentName, projectDir), ".active-session")
	raw, readErr := os.ReadFile(activeFile)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return "", kvstore.ErrNotFound
		}
		return "", readErr
	}
	name := strings.TrimSpace(string(raw))
	if name == "" {
		return "", kvstore.ErrNotFound
	}
	// Migrate to KV.
	_ = store.Set([]byte(kvActiveSessionKey(agentName, projectKey)), []byte(name))
	return name, nil
}

// SaveSessionName writes the session name to the KV store for a given session key.
func SaveSessionName(store *kvstore.Store, agentName, projectKey, sessionKey, name string) error {
	return store.Set([]byte(kvNameKey(agentName, projectKey, sessionKey)), []byte(name))
}

// ListNamesKV returns all named sessions from the KV store for an agent/project,
// falling back to the filesystem when KV has no entries.
func ListNamesKV(store *kvstore.Store, agentName, projectKey, workspaceName, projectDir string) ([]string, error) {
	prefix := kvSessionPrefix(agentName, projectKey)
	seen := make(map[string]struct{})
	var names []string

	err := store.Iterate([]byte(prefix), func(key, value []byte) error {
		k := string(key)
		if strings.HasSuffix(k, ":.name") {
			name := string(value)
			if name != "" {
				if _, ok := seen[name]; !ok {
					seen[name] = struct{}{}
					names = append(names, name)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if len(names) > 0 {
		sort.Strings(names)
		return names, nil
	}

	// Filesystem fallback.
	return ListNames(workspaceName, agentName, projectDir)
}

// RemoveByNameKV removes a named session from the KV store and filesystem.
func RemoveByNameKV(store *kvstore.Store, agentName, projectKey, workspaceName, projectDir, sessionName string) (bool, error) {
	sessionName = strings.TrimSpace(sessionName)
	if sessionName == "" {
		return false, fmt.Errorf("session name cannot be empty")
	}

	// Find and remove matching KV entries.
	prefix := kvSessionPrefix(agentName, projectKey)
	var keysToDelete [][]byte
	kvRemoved := false

	err := store.Iterate([]byte(prefix), func(key, value []byte) error {
		k := string(key)
		if strings.HasSuffix(k, ":.name") && string(value) == sessionName {
			// Found a matching session. Collect all keys for this session key.
			sessionKey := strings.TrimSuffix(strings.TrimPrefix(k, prefix), ":.name")
			keysToDelete = append(keysToDelete,
				[]byte(kvNameKey(agentName, projectKey, sessionKey)),
				[]byte(kvResumeTokenKey(agentName, projectKey, sessionKey)),
			)
			kvRemoved = true
		}
		return nil
	})
	if err != nil {
		return false, err
	}

	for _, key := range keysToDelete {
		_ = store.Delete(key)
	}

	// Clear active session pointer if it matches.
	active, getErr := store.Get([]byte(kvActiveSessionKey(agentName, projectKey)))
	if getErr == nil && string(active) == sessionName {
		_ = store.Delete([]byte(kvActiveSessionKey(agentName, projectKey)))
	}

	// Also remove from filesystem.
	fsRemoved, fsErr := RemoveByName(workspaceName, agentName, projectDir, sessionName)
	if fsErr != nil {
		return kvRemoved || fsRemoved, fsErr
	}

	return kvRemoved || fsRemoved, nil
}

// ListNames returns all named sessions for a workspace/agent/project.
func ListNames(workspaceName, agentName, projectDir string) ([]string, error) {
	dirs, err := NamedDirs(workspaceName, agentName, projectDir)
	if err != nil {
		return nil, err
	}
	var out []string
	for name := range dirs {
		out = append(out, name)
	}
	sort.Strings(out)
	return out, nil
}

// NamedDirs maps each named session of a workspace/agent/project to its
// directories under the sessions dir.
func NamedDirs(workspaceName, agentName, projectDir string) (map[string][]string, error) {
	sessionsDir := ProjectSessionsDir(workspaceName, agentName, projectDir)
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	out := make(map[string][]string)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(sessionsDir, e.Name())
		raw, readErr := os.ReadFile(filepath.Join(dir, ".name"))
		if readErr != nil {
			continue
		}
		name := strings.TrimSpace(string(raw))
		if name == "" {
			continue
		}
		out[name] = append(out[name], dir)
	}
	return out, nil
}

// RemoveByName removes all stored instances of a named session.
// Returns true when at least one session directory was removed.
func RemoveByName(workspaceName, agentName, projectDir, sessionName string) (bool, error) {
	sessionName = strings.TrimSpace(sessionName)
	if sessionName == "" {
		return false, fmt.Errorf("session name cannot be empty")
	}

	sessionsDir := ProjectSessionsDir(workspaceName, agentName, projectDir)
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("read sessions dir: %w", err)
	}

	removed := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(sessionsDir, e.Name())
		nameFile := filepath.Join(dirPath, ".name")
		raw, readErr := os.ReadFile(nameFile)
		if readErr != nil {
			continue
		}
		name := strings.TrimSpace(string(raw))
		if name != sessionName {
			continue
		}
		if rmErr := os.RemoveAll(dirPath); rmErr != nil {
			return false, fmt.Errorf("remove session '%s': %w", sessionName, rmErr)
		}
		removed = true
	}

	if removed {
		activeFile := filepath.Join(ProjectResumeDir(workspaceName, agentName, projectDir), ".active-session")
		raw, readErr := os.ReadFile(activeFile)
		if readErr == nil && strings.TrimSpace(string(raw)) == sessionName {
			_ = os.Remove(activeFile)
		}
	}
	return removed, nil
}

// ResolveSelector resolves a session selector to the canonical session name.
// Selector may be:
//  1. exact session name
//  2. exact session directory key/id
//  3. unique prefix of a session directory key/id
func ResolveSelector(workspaceName, agentName, projectDir, selector string) (string, bool, error) {
	selector = strings.TrimSpace(selector)
	if selector == "" {
		return "", false, nil
	}

	sessionsDir := ProjectSessionsDir(workspaceName, agentName, projectDir)
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read sessions dir: %w", err)
	}

	var prefixMatches []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirName := e.Name()
		nameFile := filepath.Join(sessionsDir, dirName, ".name")
		raw, readErr := os.ReadFile(nameFile)
		if readErr != nil {
			continue
		}
		name := strings.TrimSpace(string(raw))
		if name == "" {
			continue
		}
		if name == selector || dirName == selector {
			return name, true, nil
		}
		if strings.HasPrefix(dirName, selector) {
			prefixMatches = append(prefixMatches, name)
		}
	}

	if len(prefixMatches) == 1 {
		return prefixMatches[0], true, nil
	}
	if len(prefixMatches) > 1 {
		return "", false, fmt.Errorf("session id prefix '%s' is ambiguous", selector)
	}
	return "", false, nil
}
