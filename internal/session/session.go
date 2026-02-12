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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/project"
)

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

// ListNames returns all named sessions for a workspace/agent/project.
func ListNames(workspaceName, agentName, projectDir string) ([]string, error) {
	sessionsDir := ProjectSessionsDir(workspaceName, agentName, projectDir)
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}

	seen := make(map[string]struct{})
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		nameFile := filepath.Join(sessionsDir, e.Name(), ".name")
		raw, readErr := os.ReadFile(nameFile)
		if readErr != nil {
			continue
		}
		name := strings.TrimSpace(string(raw))
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
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
