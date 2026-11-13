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
	"sync"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/session"
)

// pendingRename is a rename asked for while the session runs. A running
// container saves its conversation under the name it started with, so the
// rename waits until the session has ended. Kept on disk, so it survives
// restarts of exitbox web.
type pendingRename struct {
	Workspace string `json:"workspace"`
	Agent     string `json:"agent"`
	Dir       string `json:"dir"`
	Name      string `json:"name"`
	To        string `json:"to"`
}

func (p pendingRename) key() string { return sessionKey(p.Workspace, p.Agent, p.Dir, p.Name) }

func renamesFile() string { return filepath.Join(config.Home, "web", "renames.json") }

type renameQueue struct {
	mu   sync.Mutex
	path string
}

func (q *renameQueue) loadLocked() []pendingRename {
	data, err := os.ReadFile(q.path)
	if err != nil {
		return nil
	}
	var out []pendingRename
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}

func (q *renameQueue) saveLocked(list []pendingRename) error {
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(q.path), 0o700); err != nil {
		return err
	}
	tmp := q.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, q.path)
}

func (q *renameQueue) list() []pendingRename {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.loadLocked()
}

// add queues a rename, replacing an earlier one of the same session.
func (q *renameQueue) add(p pendingRename) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []pendingRename
	for _, x := range q.loadLocked() {
		if x.key() != p.key() {
			out = append(out, x)
		}
	}
	if p.To != p.Name {
		out = append(out, p)
	}
	return q.saveLocked(out)
}

// done drops a rename once it is applied or refused. A rename asked for
// again meanwhile (another target) stays.
func (q *renameQueue) done(p pendingRename) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	var out []pendingRename
	for _, x := range q.loadLocked() {
		if x != p {
			out = append(out, x)
		}
	}
	return q.saveLocked(out)
}

// pendingFor returns the queued new name of a session, or "".
func (s *Server) pendingFor(workspace, agent, dir, name string) string {
	key := sessionKey(workspace, agent, dir, name)
	for _, p := range s.renames.list() {
		if p.key() == key {
			return p.To
		}
	}
	return ""
}

// applyPendingRenames renames the sessions that have ended since a rename
// was asked for.
func (s *Server) applyPendingRenames(ctrs []containerDoc, sessions []SessionInfo) {
	// Concurrent polls would try the same rename twice; the next poll
	// catches up.
	if !s.renameMu.TryLock() {
		return
	}
	defer s.renameMu.Unlock()
	running := func(p pendingRename, ctrs []containerDoc) bool {
		for _, c := range ctrs {
			if c.Agent == p.Agent && c.Dir == p.Dir && c.Session == p.Name {
				return true
			}
		}
		for _, si := range sessions {
			if !si.Exited && si.Kind == KindRun && si.Agent == p.Agent && si.Dir == p.Dir && si.Name == p.Name {
				return true
			}
		}
		return false
	}
	now := s.renameTime()
	queued := map[string]bool{}
	var due []pendingRename
	for _, p := range s.renames.list() {
		queued[p.retryKey()] = true
		if running(p, ctrs) {
			continue
		}
		if r := s.renameRetries[p.retryKey()]; r != nil && now.Before(r.next) {
			continue
		}
		due = append(due, p)
	}
	for k := range s.renameRetries {
		if !queued[k] {
			delete(s.renameRetries, k)
		}
	}
	if len(due) == 0 {
		return
	}
	// The cached list may predate a restart under the same name; renaming
	// a running session would lose what it saves.
	fresh, err := s.recentContainers(renameFreshAge)
	if err != nil {
		for _, p := range due {
			s.renameFailed(p, now, fmt.Errorf("listing containers: %w", err))
		}
		return
	}
	cfg, err := s.store.loadConfig()
	if err != nil {
		for _, p := range due {
			s.renameFailed(p, now, err)
		}
		return
	}
	for _, p := range due {
		if running(p, fresh) {
			continue
		}
		if profile.FindWorkspace(cfg, p.Workspace) == nil {
			s.logf("web: rename session %q to %q dropped: unknown workspace %q", p.Name, p.To, p.Workspace)
			if err := s.renames.done(p); err != nil {
				s.logf("web: rename queue: %v", err)
			}
			delete(s.renameRetries, p.retryKey())
			continue
		}
		err := s.store.renameSaved(p.Workspace, p.Agent, p.Dir, p.Name, p.To)
		var refused *session.RefusedError
		switch {
		case err == nil:
			s.noteRename(p.Workspace, p.Agent, p.Dir, p.Name, p.To)
			s.logf("web: renamed session %q to %q", p.Name, p.To)
		case errors.As(err, &refused):
			s.logf("web: rename session %q to %q refused: %v", p.Name, p.To, err)
		default:
			// Kept for a later poll: the workspace store may be busy.
			s.renameFailed(p, now, err)
			continue
		}
		delete(s.renameRetries, p.retryKey())
		if err := s.renames.done(p); err != nil {
			s.logf("web: rename queue: %v", err)
		}
	}
}

const (
	// renameFreshAge: a container list this young is fresh enough to rename by.
	renameFreshAge = time.Second
	renameRetryMin = 2 * time.Second
	renameRetryMax = time.Minute
)

// renameRetry spaces the attempts of a queued rename that keeps failing.
type renameRetry struct {
	next  time.Time
	delay time.Duration
	err   string // the last error logged, so a repeat is not logged again
}

// retryKey tells renames apart by target too: a new target retries at once.
func (p pendingRename) retryKey() string { return p.key() + "\x00" + p.To }

func (s *Server) renameTime() time.Time {
	if s.renameNow != nil {
		return s.renameNow()
	}
	return time.Now()
}

// renameFailed backs off a rename that failed for now. Callers hold renameMu.
func (s *Server) renameFailed(p pendingRename, now time.Time, err error) {
	if s.renameRetries == nil {
		s.renameRetries = map[string]*renameRetry{}
	}
	r := s.renameRetries[p.retryKey()]
	if r == nil {
		r = &renameRetry{}
		s.renameRetries[p.retryKey()] = r
	}
	r.delay = min(max(2*r.delay, renameRetryMin), renameRetryMax)
	r.next = now.Add(r.delay)
	if msg := err.Error(); msg != r.err {
		r.err = msg
		s.logf("web: rename session %q to %q: %v; retrying", p.Name, p.To, err)
	}
}

// checkWorkspace refuses a rename for a workspace the config does not
// have, as the saved-session handlers do.
func (s *Server) checkWorkspace(ws string) (int, error) {
	cfg, err := s.store.loadConfig()
	if err != nil {
		return http.StatusInternalServerError, err
	}
	if profile.FindWorkspace(cfg, ws) == nil {
		return http.StatusBadRequest, fmt.Errorf("unknown workspace %q", ws)
	}
	return http.StatusOK, nil
}

// handleRenameRunning queues a rename of a session started in a terminal,
// from its sidebar row.
func (s *Server) handleRenameRunning(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Container string `json:"container"`
		Name      string `json:"name"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	newName, err := cleanName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	for _, c := range s.runningContainers() {
		if c.Container != req.Container {
			continue
		}
		if c.Session == "" {
			writeError(w, http.StatusConflict, errors.New("this session has no name to rename yet"))
			return
		}
		if status, err := s.checkWorkspace(c.Workspace); err != nil {
			writeError(w, status, err)
			return
		}
		if err := s.renames.add(pendingRename{Workspace: c.Workspace, Agent: c.Agent, Dir: c.Dir, Name: c.Session, To: newName}); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "renameTo": newName})
		return
	}
	writeError(w, http.StatusNotFound, errors.New("that session is not running"))
}
