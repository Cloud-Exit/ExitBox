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
	"net/http"
	"sort"
	"strings"
	"testing"
)

// memVault is an in-memory vault backend.
type memVault struct {
	pw   map[string]string
	data map[string]map[string]string
	busy bool
}

func (m *memVault) initialized(ws string) bool { _, ok := m.pw[ws]; return ok }
func (m *memVault) create(ws, pw string) error {
	m.pw[ws] = pw
	m.data[ws] = map[string]string{}
	return nil
}
func (m *memVault) open(ws, pw string) (map[string]string, error) {
	if m.busy {
		return nil, errVaultBusy
	}
	if m.pw[ws] != pw {
		return nil, errors.New("wrong password or corrupted vault")
	}
	return m.data[ws], nil
}
func (m *memVault) list(ws, pw string) ([]string, error) {
	d, err := m.open(ws, pw)
	if err != nil {
		return nil, err
	}
	var keys []string
	for k := range d {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}
func (m *memVault) set(ws, pw, k, v string) error {
	d, err := m.open(ws, pw)
	if err != nil {
		return err
	}
	d[k] = v
	return nil
}
func (m *memVault) remove(ws, pw, k string) error {
	d, err := m.open(ws, pw)
	if err != nil {
		return err
	}
	delete(d, k)
	return nil
}
func (m *memVault) destroy(ws string) error {
	delete(m.pw, ws)
	delete(m.data, ws)
	return nil
}

func TestVaultManagement(t *testing.T) {
	s, st, ts := newTestServer(t, Options{})
	mv := &memVault{pw: map[string]string{}, data: map[string]map[string]string{}}
	s.vault = mv
	post := func(action string, body map[string]any) (*http.Response, map[string]any) {
		return do(t, ts, "POST", "/api/vault/work/"+action, body, "")
	}

	resp, err := ts.Client().Get(ts.URL + "/api/vault")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("vault list: %d", resp.StatusCode)
	}
	if resp, _ := post("keys", map[string]any{"password": "x"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("keys without a vault: %d", resp.StatusCode)
	}
	if resp, _ := post("create", map[string]any{"password": "short"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("short password: %d", resp.StatusCode)
	}
	if resp, out := post("create", map[string]any{"password": "vault pass", "readOnly": true}); resp.StatusCode != 200 {
		t.Fatalf("create: %d %v", resp.StatusCode, out)
	}
	if ws := st.cfg.Workspace("work"); !ws.Vault.Enabled || !ws.Vault.ReadOnly {
		t.Fatalf("vault config after create: %+v", ws.Vault)
	}
	if resp, _ := post("create", map[string]any{"password": "vault pass"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second create: %d", resp.StatusCode)
	}

	if resp, out := post("set", map[string]any{"password": "vault pass", "key": "API_TOKEN", "value": "s3cret"}); resp.StatusCode != 200 {
		t.Fatalf("set: %d %v", resp.StatusCode, out)
	}
	for _, bad := range []map[string]any{
		{"password": "vault pass", "key": "BAD-KEY", "value": "x"},
		{"password": "vault pass", "key": "OK", "value": ""},
		{"password": "vault pass", "key": "OK", "value": strings.Repeat("x", 48*1024+1)},
	} {
		if resp, _ := post("set", bad); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("bad set %v: %d", bad["key"], resp.StatusCode)
		}
	}
	resp, out := post("keys", map[string]any{"password": "vault pass"})
	if resp.StatusCode != 200 || stringify(out["keys"]) != "[API_TOKEN]" {
		t.Fatalf("keys: %d %v", resp.StatusCode, out)
	}
	if strings.Contains(stringify(out), "s3cret") {
		t.Fatal("a secret value left the server")
	}
	if resp, _ := post("keys", map[string]any{"password": "wrong pass"}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong password: %d", resp.StatusCode)
	}

	mv.busy = true
	if resp, _ := post("keys", map[string]any{"password": "vault pass"}); resp.StatusCode != http.StatusConflict {
		t.Fatalf("busy vault: %d", resp.StatusCode)
	}
	mv.busy = false

	if resp, _ := post("delete", map[string]any{"password": "vault pass", "key": "API_TOKEN"}); resp.StatusCode != 200 {
		t.Fatalf("delete: %d", resp.StatusCode)
	}
	if resp, _ := post("config", map[string]any{"enabled": false, "readOnly": false}); resp.StatusCode != 200 || st.cfg.Workspace("work").Vault.Enabled {
		t.Fatalf("disable: %d %+v", resp.StatusCode, st.cfg.Workspace("work").Vault)
	}
	if resp, _ := post("destroy", map[string]any{"confirm": "nope"}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("destroy without confirm: %d", resp.StatusCode)
	}
	if resp, _ := post("destroy", map[string]any{"confirm": "work"}); resp.StatusCode != 200 || mv.initialized("work") {
		t.Fatalf("destroy: %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts, "POST", "/api/vault/nope/keys", map[string]any{"password": "x"}, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown workspace: %d", resp.StatusCode)
	}
}

func TestVaultPasswordLimit(t *testing.T) {
	s, _, ts := newTestServer(t, Options{})
	mv := &memVault{pw: map[string]string{"work": "vault pass"}, data: map[string]map[string]string{"work": {}}}
	s.vault = mv
	for i := 0; i < maxFailures; i++ {
		do(t, ts, "POST", "/api/vault/work/keys", map[string]any{"password": "wrong"}, "")
	}
	if resp, _ := do(t, ts, "POST", "/api/vault/work/keys", map[string]any{"password": "vault pass"}, ""); resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("after %d wrong passwords: %d", maxFailures, resp.StatusCode)
	}
}

func stringify(v any) string {
	switch x := v.(type) {
	case []any:
		parts := make([]string, len(x))
		for i, p := range x {
			parts[i] = stringify(p)
		}
		return "[" + strings.Join(parts, " ") + "]"
	case string:
		return x
	case map[string]any:
		var b strings.Builder
		for k, val := range x {
			b.WriteString(k + "=" + stringify(val) + " ")
		}
		return b.String()
	}
	return ""
}
