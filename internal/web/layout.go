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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/cloud-exit/exitbox/internal/config"
)

// maxLayout bounds the layout document the browser saves.
const maxLayout = 512 << 10

// layoutFile holds the desktop layout (tabs, tiling, window places), so it
// survives restarts and is the same in every browser. The server keeps it
// as an opaque JSON object; the page owns its shape.
func layoutFile() string {
	return filepath.Join(config.Home, "web", "layout.json")
}

type layoutStore struct {
	mu   sync.Mutex
	path string
}

func (l *layoutStore) load() (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	data, err := os.ReadFile(l.path)
	if errors.Is(err, os.ErrNotExist) {
		return json.RawMessage("{}"), nil
	}
	if err != nil {
		return nil, err
	}
	if !json.Valid(data) {
		return json.RawMessage("{}"), nil
	}
	return data, nil
}

func (l *layoutStore) save(doc []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, doc, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

func (s *Server) handleGetLayout(w http.ResponseWriter, _ *http.Request) {
	doc, err := s.layout.load()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(doc)
}

func (s *Server) handlePutLayout(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLayout))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, fmt.Errorf("the layout is larger than %d bytes", maxLayout))
		return
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("the layout must be a JSON object"))
		return
	}
	if err := s.layout.save(data); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
