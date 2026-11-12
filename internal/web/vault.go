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
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/vault"
)

// vaultBackend is how the server reaches the encrypted vaults; tests
// replace it. The password comes with every call and is never kept.
type vaultBackend interface {
	initialized(workspace string) bool
	create(workspace, password string) error
	list(workspace, password string) ([]string, error)
	get(workspace, password, key string) (string, error)
	set(workspace, password, key, value string) error
	remove(workspace, password, key string) error
	destroy(workspace string) error
}

type diskVault struct{}

// errVaultBusy means a running session of the workspace holds the vault.
var errVaultBusy = errors.New("a running session of this workspace has the vault open; stop it, or manage secrets with exitbox-vault inside it")

// open opens a vault, waiting a moment when another process holds it.
func (diskVault) open(workspace, password string) (*vault.Store, error) {
	var err error
	for i := 0; i < 5; i++ {
		var st *vault.Store
		if st, err = vault.Open(workspace, password); err == nil {
			return st, nil
		}
		if !strings.Contains(err.Error(), "directory lock") {
			return nil, err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return nil, errVaultBusy
}

func (diskVault) initialized(ws string) bool       { return vault.IsInitialized(ws) }
func (diskVault) create(ws, password string) error { return vault.Init(ws, password) }
func (diskVault) destroy(ws string) error          { return vault.Destroy(ws) }

func (d diskVault) list(ws, password string) ([]string, error) {
	st, err := d.open(ws, password)
	if err != nil {
		return nil, err
	}
	defer func() { _ = st.Close() }()
	return st.List()
}

func (d diskVault) get(ws, password, key string) (string, error) {
	st, err := d.open(ws, password)
	if err != nil {
		return "", err
	}
	defer func() { _ = st.Close() }()
	return st.Get(key)
}

func (d diskVault) set(ws, password, key, value string) error {
	st, err := d.open(ws, password)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	return st.Set(key, value)
}

func (d diskVault) remove(ws, password, key string) error {
	st, err := d.open(ws, password)
	if err != nil {
		return err
	}
	defer func() { _ = st.Close() }()
	return st.Delete(key)
}

// validVaultKey matches the keys exitbox-vault accepts in a session.
func validVaultKey(k string) bool {
	if k == "" || len(k) > 128 {
		return false
	}
	for _, c := range k {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

// vaultGuard limits wrong vault passwords per workspace, like logins.
type vaultGuard struct {
	mu       sync.Mutex
	failures map[string][]time.Time
}

func (g *vaultGuard) check(ws string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.failures == nil {
		g.failures = map[string][]time.Time{}
	}
	now := time.Now()
	recent := g.failures[ws][:0]
	for _, t := range g.failures[ws] {
		if now.Sub(t) < failureWindow {
			recent = append(recent, t)
		}
	}
	g.failures[ws] = recent
	if len(recent) >= maxFailures {
		return errors.New("too many wrong vault passwords; wait a few minutes")
	}
	return nil
}

func (g *vaultGuard) failed(ws string) {
	g.mu.Lock()
	g.failures[ws] = append(g.failures[ws], time.Now())
	g.mu.Unlock()
}

type vaultDoc struct {
	Workspace   string `json:"workspace"`
	Initialized bool   `json:"initialized"`
	Enabled     bool   `json:"enabled"`
	ReadOnly    bool   `json:"readOnly"`
}

func (s *Server) handleVaultList(w http.ResponseWriter, _ *http.Request) {
	cfg := s.store.loadConfig()
	out := []vaultDoc{}
	for _, ws := range cfg.Workspaces.Items {
		out = append(out, vaultDoc{Workspace: ws.Name, Initialized: s.vault.initialized(ws.Name), Enabled: ws.Vault.Enabled, ReadOnly: ws.Vault.ReadOnly})
	}
	writeJSON(w, http.StatusOK, out)
}

type vaultRequest struct {
	Password string `json:"password"`
	Key      string `json:"key"`
	Value    string `json:"value"`
	Enabled  *bool  `json:"enabled"`
	ReadOnly *bool  `json:"readOnly"`
	Confirm  string `json:"confirm"`
}

// handleVault serves POST /api/vault/{workspace}/{action}.
func (s *Server) handleVault(w http.ResponseWriter, r *http.Request) {
	ws, action := r.PathValue("workspace"), r.PathValue("action")
	cfg := s.store.loadConfig()
	if profile.FindWorkspace(cfg, ws) == nil {
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown workspace %q", ws))
		return
	}
	var req vaultRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}

	// Actions that need the vault password, and count wrong ones.
	withPassword := func(fn func() error) {
		if err := s.vaultGuard.check(ws); err != nil {
			writeError(w, http.StatusTooManyRequests, err)
			return
		}
		if !s.vault.initialized(ws) {
			writeError(w, http.StatusConflict, errors.New("this workspace has no vault yet"))
			return
		}
		if err := fn(); err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "wrong password") {
				s.vaultGuard.failed(ws)
				// Not 401: that means "sign in to exitbox web" to the page.
				status = http.StatusForbidden
				err = errors.New("wrong vault password")
			} else if errors.Is(err, errVaultBusy) {
				status = http.StatusConflict
			}
			writeError(w, status, err)
			return
		}
	}

	switch action {
	case "create":
		if s.vault.initialized(ws) {
			writeError(w, http.StatusConflict, errors.New("this workspace already has a vault"))
			return
		}
		if len(req.Password) < 8 {
			writeError(w, http.StatusBadRequest, errors.New("the vault password needs at least 8 characters"))
			return
		}
		if err := s.vault.create(ws, req.Password); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		readOnly := req.ReadOnly != nil && *req.ReadOnly
		if err := s.saveVaultConfig(ws, true, readOnly); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.logf("web: vault created for workspace %s", ws)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "config":
		if req.Enabled == nil || req.ReadOnly == nil {
			writeError(w, http.StatusBadRequest, errors.New("enabled and readOnly are required"))
			return
		}
		if *req.Enabled && !s.vault.initialized(ws) {
			writeError(w, http.StatusConflict, errors.New("create the vault first"))
			return
		}
		if err := s.saveVaultConfig(ws, *req.Enabled, *req.ReadOnly); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	case "keys":
		withPassword(func() error {
			keys, err := s.vault.list(ws, req.Password)
			if err != nil {
				return err
			}
			writeJSON(w, http.StatusOK, map[string]any{"keys": nonNil(keys)})
			return nil
		})
	case "reveal":
		// The edit box shows the current value, on request. Only the key
		// name is logged.
		if !validVaultKey(req.Key) {
			writeError(w, http.StatusBadRequest, errors.New("invalid key"))
			return
		}
		withPassword(func() error {
			value, err := s.vault.get(ws, req.Password, req.Key)
			if err != nil {
				return err
			}
			s.logf("web: vault %s: revealed %s", ws, req.Key)
			writeJSON(w, http.StatusOK, map[string]any{"value": value})
			return nil
		})
	case "set":
		if !validVaultKey(req.Key) {
			writeError(w, http.StatusBadRequest, errors.New("a key has letters, digits and underscores only"))
			return
		}
		if req.Value == "" || len(req.Value) > vault.MaxFileValueSize {
			writeError(w, http.StatusBadRequest, fmt.Errorf("a value has 1 to %d bytes", vault.MaxFileValueSize))
			return
		}
		withPassword(func() error {
			if err := s.vault.set(ws, req.Password, req.Key, req.Value); err != nil {
				return err
			}
			s.logf("web: vault %s: set %s", ws, req.Key)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return nil
		})
	case "delete":
		if !validVaultKey(req.Key) {
			writeError(w, http.StatusBadRequest, errors.New("invalid key"))
			return
		}
		withPassword(func() error {
			if err := s.vault.remove(ws, req.Password, req.Key); err != nil {
				return err
			}
			s.logf("web: vault %s: deleted %s", ws, req.Key)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return nil
		})
	case "destroy":
		if req.Confirm != ws {
			writeError(w, http.StatusBadRequest, errors.New("type the workspace name to confirm"))
			return
		}
		if !s.vault.initialized(ws) {
			writeError(w, http.StatusConflict, errors.New("this workspace has no vault"))
			return
		}
		if err := s.vault.destroy(ws); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if err := s.saveVaultConfig(ws, false, false); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		s.logf("web: vault destroyed for workspace %s", ws)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusNotFound, fmt.Errorf("unknown vault action %q", action))
	}
}

func (s *Server) saveVaultConfig(ws string, enabled, readOnly bool) error {
	cfg := s.store.loadConfig()
	w := cfg.Workspace(ws)
	if w == nil {
		return fmt.Errorf("unknown workspace %q", ws)
	}
	w.Vault = config.VaultConfig{Enabled: enabled, ReadOnly: readOnly}
	return s.store.saveConfig(cfg)
}
