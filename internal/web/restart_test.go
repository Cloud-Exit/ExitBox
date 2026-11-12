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
	"net/http"
	"os"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func startOpts(dir string) map[string]any {
	return map[string]any{
		"dir": dir, "agent": "claude", "name": " keep ", "envProfile": "openrouter",
		"noFirewall": true, "memory": "16g", "allowUrls": []string{"api.example.com", " "},
	}
}

func wantKept(dir string) RunOptions {
	return RunOptions{Dir: dir, Agent: "claude", Name: "keep", EnvProfile: "openrouter", NoFirewall: true, Memory: "16g", AllowURLs: []string{"api.example.com"}}
}

// The options a session started with are kept, on disk and in the state.
func TestStartKeepsRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "sleep 60"), StateDir: dir})
	resp, out := do(t, ts, "POST", "/api/sessions", startOpts(st.dirs[0]), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	if got := s.sessions.Get(id).Info().Request; got == nil || !reflect.DeepEqual(*got, wantKept(st.dirs[0])) {
		t.Fatalf("kept request %+v", got)
	}
	data, err := os.ReadFile(infoPath(dir, id))
	if err != nil {
		t.Fatal(err)
	}
	var saved SessionInfo
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Request == nil || !reflect.DeepEqual(*saved.Request, wantKept(st.dirs[0])) {
		t.Fatalf("info file request %+v", saved.Request)
	}
	_, state := do(t, ts, "GET", "/api/state", nil, "")
	sessions := state["sessions"].([]any)
	req, ok := sessions[0].(map[string]any)["request"].(map[string]any)
	if !ok || req["memory"] != "16g" || req["name"] != "keep" || req["envProfile"] != "openrouter" {
		t.Fatalf("state request %v", sessions[0])
	}
}

func TestRequestSurvivesRestore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	s1, st, ts1 := newTestServer(t, Options{Command: shellCommand(&recorder{}, "sleep 60"), StateDir: dir})
	resp, out := do(t, ts1, "POST", "/api/sessions", startOpts(st.dirs[0]), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	s1.sessions.Disconnect()
	ts1.Close()

	s2, _, _ := newTestServer(t, Options{Command: shellCommand(&recorder{}, "sleep 60"), StateDir: dir})
	if n, err := s2.Restore(); err != nil || n != 1 {
		t.Fatalf("restored %d, %v", n, err)
	}
	if got := s2.sessions.Get(id).Info().Request; got == nil || !reflect.DeepEqual(*got, wantKept(st.dirs[0])) {
		t.Fatalf("restored request %+v", got)
	}
}

// Info files written before the request was kept still restore, without one.
func TestRestoreOldInfoWithoutRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix sockets for hosts on Windows")
	}
	dir := shortTempDir(t)
	const id = "0123456789abcdef"
	fakeHost(t, dir, id)
	m := NewManager(shellCommand(&recorder{}, "true"), inProcessHosts, dir)
	defer m.Disconnect()
	old := `{"id":"` + id + `","kind":"run","agent":"claude","dir":"/p","name":"x","args":["run","claude","--name","x"]}`
	if err := m.writeInfo(id, []byte(old)); err != nil {
		t.Fatal(err)
	}
	if n, err := m.Restore(); err != nil || n != 1 {
		t.Fatalf("restored %d, %v", n, err)
	}
	info := m.Get(id).Info()
	if info.Request != nil || !reflect.DeepEqual(info.Args, []string{"run", "claude", "--name", "x"}) {
		t.Fatalf("restored %+v", info)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["request"]; ok {
		t.Fatal("an unknown request is sent as a value")
	}
}

func waitDone(t *testing.T, sess *Session) {
	t.Helper()
	select {
	case <-sess.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("session did not end")
	}
}

// Restarting with changed options runs them, also over a running session.
func TestRestartWithNewOptions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	for _, script := range []string{"exit 0", "sleep 60"} {
		t.Run(script, func(t *testing.T) {
			rec := &recorder{}
			s, st, ts := newTestServer(t, Options{Command: shellCommand(rec, script)})
			resp, out := do(t, ts, "POST", "/api/sessions", startOpts(st.dirs[0]), "")
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("start: %d %v", resp.StatusCode, out)
			}
			id := out["id"].(string)
			old := s.sessions.Get(id)
			if script == "exit 0" {
				waitDone(t, old)
			}
			next := startOpts(st.dirs[0])
			next["name"] = "other"
			next["memory"] = "4g"
			next["readOnly"] = true
			resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]any{"cols": 80, "rows": 24, "request": next}, "")
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("restart: %d %v", resp.StatusCode, out)
			}
			waitDone(t, old)
			if s.sessions.Get(id) != nil {
				t.Fatal("restart kept the old session")
			}
			nid := out["id"].(string)
			want := []string{"run", "claude", "--no-firewall", "--read-only", "--name", "other", "--profile", "openrouter", "--memory", "4g", "--allow-urls", "api.example.com"}
			if args, _ := rec.last(); !reflect.DeepEqual(args, append(append([]string(nil), want...), "--env", WebSessionEnv+"="+nid)) {
				t.Fatalf("restart ran %q", args)
			}
			info := s.sessions.Get(nid).Info()
			if !reflect.DeepEqual(info.Args, want) || info.Name != "other" || info.Request == nil || info.Request.Memory != "4g" || !info.Request.ReadOnly {
				t.Fatalf("new session %+v", info)
			}
		})
	}
}

// Without a request, a restart keeps the options it had.
func TestRestartKeepsRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "exit 0")})
	resp, out := do(t, ts, "POST", "/api/sessions", startOpts(st.dirs[0]), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	waitDone(t, s.sessions.Get(out["id"].(string)))
	resp, out = do(t, ts, "POST", "/api/sessions/"+out["id"].(string)+"/restart", map[string]int{}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("restart: %d %v", resp.StatusCode, out)
	}
	if got := s.sessions.Get(out["id"].(string)).Info().Request; got == nil || !reflect.DeepEqual(*got, wantKept(st.dirs[0])) {
		t.Fatalf("request after restart %+v", got)
	}
}

func TestRestartRefusesInvalidRequest(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	rec := &recorder{}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(rec, "sleep 60")})
	resp, out := do(t, ts, "POST", "/api/sessions", startOpts(st.dirs[0]), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	bad := []map[string]any{
		{"dir": st.dirs[0], "agent": "claude", "memory": "8g; rm -rf /"},
		{"dir": st.dirs[0], "agent": "codex", "name": "--no-firewall"},
		{"dir": st.dirs[0], "agent": "claude", "noTmux": true},
		{"dir": "relative", "agent": "claude"},
	}
	for _, req := range bad {
		resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]any{"request": req}, "")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("request %v: %d %v", req, resp.StatusCode, out)
		}
	}
	rec.mu.Lock()
	runs := len(rec.runs)
	rec.mu.Unlock()
	if info := s.sessions.Get(id).Info(); info.Exited || runs != 1 {
		t.Fatalf("a refused restart touched the session: exited %v, %d runs", info.Exited, runs)
	}
	// A running session without a request still waits for its end.
	if resp, _ = do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]any{}, ""); resp.StatusCode != http.StatusConflict {
		t.Fatalf("plain restart of a running session: %d", resp.StatusCode)
	}
}

// A rename keeps the request in step, so its settings resume the new name.
func TestRenameKeepsRequestName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "exit 0")})
	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude", "name": "feature-x"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	waitDone(t, s.sessions.Get(id))
	if resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/rename", map[string]string{"name": "renamed"}, ""); resp.StatusCode != 200 {
		t.Fatalf("rename: %d %v", resp.StatusCode, out)
	}
	if r := s.sessions.Get(id).Info().Request; r == nil || r.Name != "renamed" {
		t.Fatalf("request after rename %+v", r)
	}
}

func TestRestartRequestRefusedForAttach(t *testing.T) {
	s, st, ts := newTestServer(t, Options{})
	sess := s.sessions.newSession(SessionInfo{ID: "0123456789abcdef", Kind: KindAttach, Agent: "claude", Exited: true})
	s.sessions.mu.Lock()
	s.sessions.sessions[sess.info.ID] = sess
	s.sessions.mu.Unlock()
	t.Cleanup(func() {
		s.sessions.mu.Lock()
		delete(s.sessions.sessions, sess.info.ID)
		s.sessions.mu.Unlock()
	})
	resp, out := do(t, ts, "POST", "/api/sessions/"+sess.info.ID+"/restart", map[string]any{"request": map[string]any{"dir": st.dirs[0], "agent": "claude"}}, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("attach restart with a request: %d %v", resp.StatusCode, out)
	}
}
