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

package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/cloud-exit/exitbox/internal/config"
)

// Folder groups sessions in the sidebar. Items are session keys
// (sessionKey), so a session keeps its folder across runs.
type Folder struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Items []string `json:"items"`
}

const (
	maxFolders     = 200
	maxFolderItems = 1000
)

var folderIDRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,40}$`)

// sessionKey identifies a named session across runs.
func sessionKey(workspace, agent, dir, name string) string {
	return workspace + "\t" + agent + "\t" + dir + "\t" + name
}

// foldersFile is where the folders live: UI state, not configuration.
func foldersFile() string {
	return filepath.Join(config.Home, "web", "folders.json")
}

// folderStore keeps the folders on disk.
type folderStore struct {
	mu   sync.Mutex
	path string
}

func (f *folderStore) load() ([]Folder, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.loadLocked()
}

func (f *folderStore) loadLocked() ([]Folder, error) {
	data, err := os.ReadFile(f.path)
	if errors.Is(err, os.ErrNotExist) {
		return []Folder{}, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Folder
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("read %s: %w", f.path, err)
	}
	return nonNil(out), nil
}

func (f *folderStore) saveLocked(folders []Folder) error {
	data, err := json.MarshalIndent(folders, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	tmp := f.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.path)
}

func (f *folderStore) save(folders []Folder) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saveLocked(folders)
}

// rename moves a renamed session's key in every folder.
func (f *folderStore) rename(oldKey, newKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	folders, err := f.loadLocked()
	if err != nil {
		return err
	}
	changed := false
	for i := range folders {
		for j, k := range folders[i].Items {
			if k == oldKey {
				folders[i].Items[j] = newKey
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	return f.saveLocked(folders)
}

// cleanFolders validates folders sent by the browser.
func cleanFolders(in []Folder) ([]Folder, error) {
	if len(in) > maxFolders {
		return nil, fmt.Errorf("at most %d folders", maxFolders)
	}
	ids := map[string]bool{}
	filed := map[string]bool{}
	out := make([]Folder, 0, len(in))
	for _, f := range in {
		name := strings.TrimSpace(f.Name)
		if name == "" || len(name) > 64 || strings.ContainsAny(name, "\x00\n\r") {
			return nil, fmt.Errorf("invalid folder name %q (1 to 64 characters)", f.Name)
		}
		if !folderIDRe.MatchString(f.ID) || ids[f.ID] {
			return nil, fmt.Errorf("invalid folder id %q", f.ID)
		}
		ids[f.ID] = true
		if len(f.Items) > maxFolderItems {
			return nil, fmt.Errorf("folder %s has too many sessions", name)
		}
		items := []string{}
		for _, k := range f.Items {
			// A session sits in one folder at most.
			if len(k) > 4096 || strings.Count(k, "\t") != 3 || filed[k] {
				continue
			}
			filed[k] = true
			items = append(items, k)
		}
		out = append(out, Folder{ID: f.ID, Name: name, Items: items})
	}
	return out, nil
}

func (s *Server) handlePutFolders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Folders []Folder `json:"folders"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	folders, err := cleanFolders(req.Folders)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.folders.save(folders); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("saving folders: %w", err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": folders})
}

// noteRename keeps a renamed session in its folder.
func (s *Server) noteRename(workspace, agent, dir, oldName, newName string) {
	if err := s.folders.rename(sessionKey(workspace, agent, dir, oldName), sessionKey(workspace, agent, dir, newName)); err != nil {
		s.logf("web: update folders after rename: %v", err)
	}
}
