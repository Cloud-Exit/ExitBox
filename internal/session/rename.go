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
	"strconv"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/kvstore"
	"github.com/cloud-exit/exitbox/internal/project"
)

// Key returns the storage key of a session name, as the container
// entrypoint computes it (session_key_for_name): every byte outside
// [A-Za-z0-9._-] becomes "_", then "_" and the POSIX cksum of the name.
func Key(name string) string {
	slug := []byte(name)
	for i, c := range slug {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == '-'
		if !ok {
			slug[i] = '_'
		}
	}
	s := string(slug)
	if s == "" {
		s = "session"
	}
	return s + "_" + strconv.FormatUint(uint64(project.POSIXCksumString(name)), 10)
}

// codexSessionState lists the per-session Codex directories the entrypoint
// keeps under the shared ~/.codex (configure_codex_session_storage).
func codexSessionState(workspaceName, key string) []string {
	root := filepath.Join(config.WorkspaceAgentDir(workspaceName, "codex"), ".codex", ".exitbox")
	return []string{filepath.Join(root, "session-home", key), filepath.Join(root, "session-data", key)}
}

// findSessionDirs returns the directories under the sessions dir whose
// .name is name.
func findSessionDirs(sessionsDir, name string) ([]string, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(sessionsDir, e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, ".name"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(raw)) == name {
			out = append(out, dir)
		}
	}
	return out, nil
}

// kvKeyOf returns the KV session key whose stored name is name, or "".
func kvKeyOf(store *kvstore.Store, agentName, projectKey, name string) (string, error) {
	prefix := kvSessionPrefix(agentName, projectKey)
	found := ""
	err := store.Iterate([]byte(prefix), func(key, value []byte) error {
		k := string(key)
		if found == "" && strings.HasSuffix(k, ":.name") && string(value) == name {
			found = strings.TrimSuffix(strings.TrimPrefix(k, prefix), ":.name")
		}
		return nil
	})
	return found, err
}

// Rename gives a saved session a new name. It moves the session's files,
// its KV entries (store may be nil when the KV store cannot be opened), the
// active-session pointer and Codex's per-session state, so `--name new`
// resumes the conversation. The session must not be running.
func Rename(store *kvstore.Store, workspaceName, agentName, projectDir, oldName, newName string) error {
	oldName, newName = strings.TrimSpace(oldName), strings.TrimSpace(newName)
	if oldName == "" || newName == "" {
		return errors.New("session name cannot be empty")
	}
	if oldName == newName {
		return nil
	}
	projectKey := project.GenerateFolderName(projectDir)
	sessionsDir := ProjectSessionsDir(workspaceName, agentName, projectDir)
	newKey := Key(newName)

	// Check everything before moving anything.
	oldDirs, err := findSessionDirs(sessionsDir, oldName)
	if err != nil {
		return err
	}
	clash, err := findSessionDirs(sessionsDir, newName)
	if err != nil {
		return err
	}
	if len(clash) > 0 {
		return fmt.Errorf("a session named '%s' already exists", newName)
	}
	if _, err := os.Stat(filepath.Join(sessionsDir, newKey)); err == nil {
		return fmt.Errorf("a session named '%s' already exists", newName)
	}
	oldKVKey := ""
	if store != nil {
		if oldKVKey, err = kvKeyOf(store, agentName, projectKey, oldName); err != nil {
			return err
		}
		if k, err := kvKeyOf(store, agentName, projectKey, newName); err != nil {
			return err
		} else if k != "" {
			return fmt.Errorf("a session named '%s' already exists", newName)
		}
	}
	if len(oldDirs) == 0 && oldKVKey == "" {
		return fmt.Errorf("no saved session named '%s'", oldName)
	}
	var codexMoves [][2]string
	if agentName == "codex" {
		from := codexSessionState(workspaceName, Key(oldName))
		to := codexSessionState(workspaceName, newKey)
		for i := range from {
			if _, err := os.Stat(from[i]); err != nil {
				continue
			}
			if _, err := os.Stat(to[i]); err == nil {
				return fmt.Errorf("codex state for '%s' already exists at %s", newName, to[i])
			}
			codexMoves = append(codexMoves, [2]string{from[i], to[i]})
		}
	}

	// Files: the first directory becomes the new one; extra copies go.
	newDir := filepath.Join(sessionsDir, newKey)
	if len(oldDirs) > 0 {
		if err := os.Rename(oldDirs[0], newDir); err != nil {
			return fmt.Errorf("rename session directory: %w", err)
		}
		for _, d := range oldDirs[1:] {
			if err := os.RemoveAll(d); err != nil {
				return fmt.Errorf("remove old session directory: %w", err)
			}
		}
	} else if err := os.MkdirAll(newDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(newDir, ".name"), []byte(newName), 0o644); err != nil {
		return err
	}
	activeFile := filepath.Join(ProjectResumeDir(workspaceName, agentName, projectDir), ".active-session")
	if raw, err := os.ReadFile(activeFile); err == nil && strings.TrimSpace(string(raw)) == oldName {
		if err := os.WriteFile(activeFile, []byte(newName), 0o644); err != nil {
			return err
		}
	}
	for _, m := range codexMoves {
		if err := os.MkdirAll(filepath.Dir(m[1]), 0o755); err != nil {
			return err
		}
		if err := os.Rename(m[0], m[1]); err != nil {
			return fmt.Errorf("move codex session state: %w", err)
		}
	}

	if store == nil {
		return nil
	}
	if err := SaveSessionName(store, agentName, projectKey, newKey, newName); err != nil {
		return err
	}
	if oldKVKey != "" {
		token, err := store.Get([]byte(kvResumeTokenKey(agentName, projectKey, oldKVKey)))
		switch {
		case err == nil:
			if err := SaveResumeToken(store, agentName, projectKey, newKey, string(token)); err != nil {
				return err
			}
		case !errors.Is(err, kvstore.ErrNotFound):
			return err
		}
		if oldKVKey != newKey {
			for _, k := range []string{kvNameKey(agentName, projectKey, oldKVKey), kvResumeTokenKey(agentName, projectKey, oldKVKey)} {
				if err := store.Delete([]byte(k)); err != nil && !errors.Is(err, kvstore.ErrNotFound) {
					return err
				}
			}
		}
	}
	if active, err := store.Get([]byte(kvActiveSessionKey(agentName, projectKey))); err == nil && string(active) == oldName {
		if err := SetActiveSession(store, agentName, projectKey, newName); err != nil {
			return err
		}
	}
	return nil
}
