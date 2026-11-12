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
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/session"
	"golang.org/x/net/websocket"
)

// fakeStore keeps the config in memory.
type fakeStore struct {
	mu    sync.Mutex
	cfg   *config.Config
	al    *config.Allowlist
	dirs  []string
	saved map[string][]string // workspace/agent/dir -> names
	homeD string
	ctrs  []containerDoc
	// renames records renameSaved calls as "old->new".
	renames []string
	stopped []string
	// renameErr fails renameSaved; renameCalls counts its calls.
	renameErr   error
	renameCalls int
}

func (f *fakeStore) renameSaved(ws, agent, dir, oldName, newName string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renameCalls++
	if f.renameErr != nil {
		return f.renameErr
	}
	k := ws + "/" + agent + "/" + dir
	for _, n := range f.saved[k] {
		if n == newName {
			return &session.RefusedError{Msg: "a session named '" + newName + "' already exists"}
		}
	}
	for i, n := range f.saved[k] {
		if n == oldName {
			f.saved[k][i] = newName
			f.renames = append(f.renames, oldName+"->"+newName)
			return nil
		}
	}
	return &session.RefusedError{Msg: "no saved session named '" + oldName + "'"}
}

func (f *fakeStore) stopContainer(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = append(f.stopped, name)
	kept := f.ctrs[:0]
	for _, c := range f.ctrs {
		if c.Container != name {
			kept = append(kept, c)
		}
	}
	f.ctrs = kept
	return nil
}

func (f *fakeStore) containers() ([]containerDoc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]containerDoc(nil), f.ctrs...), nil
}

func (f *fakeStore) loadConfig() *config.Config {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, err := json.Marshal(f.cfg)
	if err != nil {
		panic(err)
	}
	var c config.Config
	if err := json.Unmarshal(data, &c); err != nil {
		panic(err)
	}
	return &c
}
func (f *fakeStore) saveConfig(c *config.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cfg = c
	return nil
}
func (f *fakeStore) loadAllowlist() *config.Allowlist {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := *f.al
	c.Custom = append([]string(nil), f.al.Custom...)
	return &c
}
func (f *fakeStore) saveAllowlist(a *config.Allowlist) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.al = a
	return nil
}
func (f *fakeStore) projects() []string { return f.dirs }
func (f *fakeStore) savedSessions(ws, agent, dir string) ([]string, error) {
	return f.saved[ws+"/"+agent+"/"+dir], nil
}
func (f *fakeStore) removeSaved(ws, agent, dir, name string) (bool, error) {
	k := ws + "/" + agent + "/" + dir
	for i, n := range f.saved[k] {
		if n == name {
			f.saved[k] = append(f.saved[k][:i], f.saved[k][i+1:]...)
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeStore) envProfiles(string) ([]string, string, error) {
	return []string{"openrouter"}, "", nil
}
func (f *fakeStore) runtime() string { return "podman" }
func (f *fakeStore) home() string    { return f.homeD }

type recorder struct {
	mu   sync.Mutex
	runs [][]string
	dirs []string
}

func (r *recorder) last() ([]string, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.runs) == 0 {
		return nil, ""
	}
	return r.runs[len(r.runs)-1], r.dirs[len(r.dirs)-1]
}

// shellCommand runs script instead of exitbox and records the arguments.
func shellCommand(rec *recorder, script string) CommandFunc {
	return func(info SessionInfo) *exec.Cmd {
		rec.mu.Lock()
		if info.Kind == KindAttach {
			rec.runs = append(rec.runs, []string{"attach", info.Container})
		} else {
			rec.runs = append(rec.runs, append([]string(nil), info.Args...))
		}
		rec.dirs = append(rec.dirs, info.Dir)
		rec.mu.Unlock()
		c := exec.Command("/bin/sh", "-c", script)
		if info.Dir != "" {
			c.Dir = info.Dir
		}
		return c
	}
}

func newTestServer(t *testing.T, opts Options) (*Server, *fakeStore, *httptest.Server) {
	t.Helper()
	home := t.TempDir()
	proj := filepath.Join(home, "code", "app")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.SetAgentEnabled("claude", true)
	cfg.SetAgentEnabled("codex", true)
	cfg.Workspaces.Items = append(cfg.Workspaces.Items, config.Workspace{Name: "work", Directory: proj})
	st := &fakeStore{cfg: cfg, al: config.DefaultAllowlist(), dirs: []string{proj}, saved: map[string][]string{"work/claude/" + proj: {"feature-x"}}, homeD: home}
	if opts.Listen == "" {
		opts.Listen = "127.0.0.1:4848"
	}
	if opts.Command == nil {
		opts.Command = shellCommand(&recorder{}, "true")
	}
	if opts.Spawn == nil {
		opts.Spawn = inProcessHosts
	}
	if opts.StateDir == "" {
		opts.StateDir = shortTempDir(t)
	}
	if opts.SessionsFile == "" {
		opts.SessionsFile = filepath.Join(t.TempDir(), "sessions.json")
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	s.store = st
	s.folders = &folderStore{path: filepath.Join(t.TempDir(), "folders.json")}
	s.layout = &layoutStore{path: filepath.Join(t.TempDir(), "layout.json")}
	s.renames = &renameQueue{path: filepath.Join(t.TempDir(), "renames.json")}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		ts.Close()
		s.sessions.CloseAll()
	})
	return s, st, ts
}

// inProcessHosts runs session hosts as goroutines of the test.
func inProcessHosts(dir, id string) (<-chan struct{}, error) {
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		_ = RunHost(dir, id)
	}()
	return exited, nil
}

// shortTempDir is a temp dir with a path short enough for Unix sockets.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "xw")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func do(t *testing.T, ts *httptest.Server, method, path string, body any, cookie string) (*http.Response, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, ts.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ExitBox", "1")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &out)
	return resp, out
}

func TestNewRefusesPublicListenWithoutLogin(t *testing.T) {
	cmd := shellCommand(&recorder{}, "true")
	if _, err := New(Options{Listen: "0.0.0.0:4848", Command: cmd, Spawn: inProcessHosts, StateDir: shortTempDir(t), SessionsFile: filepath.Join(t.TempDir(), "sessions.json")}); err == nil {
		t.Fatal("public address without a login was accepted")
	}
	if _, err := New(Options{Listen: "0.0.0.0:4848", Username: "me", PasswordHash: "$2a$x", Command: cmd, Spawn: inProcessHosts, StateDir: shortTempDir(t), SessionsFile: filepath.Join(t.TempDir(), "sessions.json")}); err != nil {
		t.Fatalf("public address with a login refused: %v", err)
	}
	if _, err := New(Options{Listen: "127.0.0.1:4848", Command: cmd, Spawn: inProcessHosts, StateDir: shortTempDir(t), SessionsFile: filepath.Join(t.TempDir(), "sessions.json")}); err != nil {
		t.Fatalf("loopback without a login refused: %v", err)
	}
}

func TestGuard(t *testing.T) {
	_, _, ts := newTestServer(t, Options{})

	// DNS rebinding: a loopback server answers only to local names.
	req, err := http.NewRequest("GET", ts.URL+"/api/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "attacker.example"
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("foreign Host: status %d, want 421", resp.StatusCode)
	}

	// Writes need the custom header and no foreign Origin.
	req, err = http.NewRequest("POST", ts.URL+"/api/sessions", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST without X-ExitBox: status %d, want 403", resp.StatusCode)
	}
	req, err = http.NewRequest("POST", ts.URL+"/api/sessions", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ExitBox", "1")
	req.Header.Set("Origin", "http://evil.example")
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("POST from a foreign origin: status %d, want 403", resp.StatusCode)
	}

	resp, _ = do(t, ts, "GET", "/", nil, "")
	if resp.Header.Get("X-Frame-Options") != "DENY" || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("security headers missing")
	}
}

func TestMeReportsLoginAndLoopback(t *testing.T) {
	_, _, ts := newTestServer(t, Options{})
	resp, me := do(t, ts, "GET", "/api/me", nil, "")
	if resp.StatusCode != 200 || me["authEnabled"] != false || me["signedIn"] != true || me["loopback"] != true {
		t.Fatalf("me without login: %d %v", resp.StatusCode, me)
	}

	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	_, _, ts = newTestServer(t, Options{Listen: "0.0.0.0:4848", Username: "me", PasswordHash: hash})
	resp, me = do(t, ts, "GET", "/api/me", nil, "")
	if resp.StatusCode != 200 || me["authEnabled"] != true || me["loopback"] != false {
		t.Fatalf("me on a public address: %d %v", resp.StatusCode, me)
	}
}

func TestAuthFlow(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	_, _, ts := newTestServer(t, Options{Username: "me", PasswordHash: hash})

	resp, _ := do(t, ts, "GET", "/api/state", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("state without login: %d, want 401", resp.StatusCode)
	}
	resp, me := do(t, ts, "GET", "/api/me", nil, "")
	if resp.StatusCode != 200 || me["authEnabled"] != true || me["signedIn"] != false {
		t.Fatalf("me before login: %d %v", resp.StatusCode, me)
	}
	resp, _ = do(t, ts, "POST", "/api/login", map[string]string{"username": "me", "password": "nope"}, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad login: %d, want 401", resp.StatusCode)
	}
	resp, _ = do(t, ts, "POST", "/api/login", map[string]string{"username": "me", "password": "correct horse"}, "")
	if resp.StatusCode != 200 {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	var cookie string
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("cookie flags: %+v", c)
			}
			cookie = c.Name + "=" + c.Value
		}
	}
	if cookie == "" {
		t.Fatal("no session cookie")
	}
	resp, _ = do(t, ts, "GET", "/api/state", nil, cookie)
	if resp.StatusCode != 200 {
		t.Fatalf("state with login: %d", resp.StatusCode)
	}
	do(t, ts, "POST", "/api/logout", map[string]string{}, cookie)
	resp, _ = do(t, ts, "GET", "/api/state", nil, cookie)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("state after logout: %d, want 401", resp.StatusCode)
	}
}

func TestBuildArgs(t *testing.T) {
	home := t.TempDir()
	proj := filepath.Join(home, "p")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.SetAgentEnabled("claude", true)

	dir, args, err := buildArgs(cfg, startRequest{
		Dir: "~/p", Agent: "claude", Workspace: "default", Name: "feature x", EnvProfile: "openrouter",
		NoFirewall: true, ReadOnly: true, NoEnv: true, Ollama: true, Fresh: true,
		Memory: "16g", CPUs: "8", AllowURLs: []string{"api.example.com", " ", "10.0.0.1"},
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	if dir != proj {
		t.Fatalf("dir %q, want %q", dir, proj)
	}
	want := []string{"run", "claude", "--workspace", "default", "--no-firewall", "--read-only", "--no-env", "--ollama",
		"--name", "feature x", "--no-resume", "--profile", "openrouter", "--memory", "16g", "--cpus", "8",
		"--allow-urls", "api.example.com", "--allow-urls", "10.0.0.1"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args\n got %q\nwant %q", args, want)
	}

	_, args, err = buildArgs(cfg, startRequest{Dir: proj, Agent: "claude", Resume: true}, home)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(args, []string{"run", "claude", "--resume"}) {
		t.Fatalf("bare resume args %q", args)
	}

	bad := []startRequest{
		{Dir: proj, Agent: "codex"},                              // not enabled
		{Dir: proj, Agent: "nope"},                               // unknown
		{Dir: "relative", Agent: "claude"},                       // not absolute
		{Dir: filepath.Join(home, "missing"), Agent: "claude"},   // missing
		{Dir: proj, Agent: "claude", Workspace: "nope"},          // unknown workspace
		{Dir: proj, Agent: "claude", Memory: "8g; rm -rf /"},     // bad memory
		{Dir: proj, Agent: "claude", CPUs: "four"},               // bad cpus
		{Dir: proj, Agent: "claude", AllowURLs: []string{"a b"}}, // bad domain
		{Dir: proj, Agent: "claude", Name: "--no-firewall"},      // flag as name
		{Dir: proj, Agent: "claude", EnvProfile: "../x"},         // path as profile
		{Dir: proj, Agent: "claude", AllowURLs: []string{"-f"}},  // flag as domain
	}
	for _, req := range bad {
		if _, args, err := buildArgs(cfg, req, home); err == nil {
			t.Errorf("request %+v accepted as %q", req, args)
		}
	}
}

func TestStateLists(t *testing.T) {
	_, st, ts := newTestServer(t, Options{Version: "9.9.9"})
	resp, out := do(t, ts, "GET", "/api/state", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("state: %d", resp.StatusCode)
	}
	if out["version"] != "9.9.9" || out["runtime"] != "podman" {
		t.Fatalf("state header fields: %v", out)
	}
	projects := out["projects"].([]any)
	if len(projects) != 1 {
		t.Fatalf("projects: %v", projects)
	}
	p := projects[0].(map[string]any)
	if p["path"] != st.dirs[0] || p["workspace"] != "work" || p["scoped"] != true {
		t.Fatalf("project: %v", p)
	}
	saved := out["saved"].([]any)
	if len(saved) != 1 || saved[0].(map[string]any)["name"] != "feature-x" {
		t.Fatalf("saved: %v", saved)
	}

	resp, _ = do(t, ts, "POST", "/api/saved/remove", map[string]string{"workspace": "work", "agent": "claude", "dir": st.dirs[0], "name": "feature-x"}, "")
	if resp.StatusCode != 200 {
		t.Fatalf("remove saved: %d", resp.StatusCode)
	}
	if n := len(st.saved["work/claude/"+st.dirs[0]]); n != 0 {
		t.Fatalf("saved session still there (%d)", n)
	}
}

func TestFolderBrowser(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	if err := os.MkdirAll(filepath.Join(st.homeD, ".hidden"), 0o755); err != nil {
		t.Fatal(err)
	}
	resp, out := do(t, ts, "GET", "/api/fs", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("fs: %d %v", resp.StatusCode, out)
	}
	if out["path"] != st.homeD {
		t.Fatalf("fs path %v, want home", out["path"])
	}
	dirs := out["dirs"].([]any)
	if len(dirs) != 1 || dirs[0] != "code" {
		t.Fatalf("dirs %v, want [code] without hidden ones", dirs)
	}
	_, out = do(t, ts, "GET", "/api/fs?hidden=1", nil, "")
	if dirs := out["dirs"].([]any); len(dirs) != 2 || dirs[0] != ".hidden" {
		t.Fatalf("dirs with hidden %v, want [.hidden code]", dirs)
	}
	resp, _ = do(t, ts, "GET", "/api/fs?path=relative", nil, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("relative path: %d", resp.StatusCode)
	}
}

func dialSession(t *testing.T, ts *httptest.Server, id, origin string) (*websocket.Conn, error) {
	t.Helper()
	cfg, err := websocket.NewConfig(strings.Replace(ts.URL, "http", "ws", 1)+"/api/sessions/"+id+"/attach", origin)
	if err != nil {
		t.Fatal(err)
	}
	return websocket.DialConfig(cfg)
}

// readUntil reads output frames until want shows up.
// sawLive records that a stream sent its end-of-replay marker.
var sawLive bool

func readUntil(t *testing.T, ws *websocket.Conn, want string) (string, map[string]any) {
	t.Helper()
	var got strings.Builder
	if err := ws.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for {
		var frame []byte
		if err := websocket.Message.Receive(ws, &frame); err != nil {
			t.Fatalf("waiting for %q, got %q: %v", want, got.String(), err)
		}
		if bytes.HasPrefix(frame, []byte(`{"t":`)) {
			var msg map[string]any
			if err := json.Unmarshal(frame, &msg); err == nil {
				if msg["t"] == "live" {
					sawLive = true
					continue
				}
				if want == "" {
					return got.String(), msg
				}
				continue
			}
		}
		got.Write(frame)
		if want != "" && strings.Contains(got.String(), want) {
			return got.String(), nil
		}
	}
}

func TestSessionOverWebSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	rec := &recorder{}
	// Prints the terminal size, echoes one line, then exits with 3.
	script := `stty size; IFS= read -r line; echo "got:$line"; stty size; exit 3`
	s, st, ts := newTestServer(t, Options{Command: shellCommand(rec, script)})

	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude", "name": "x", "cols": 100, "rows": 30}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	if out["workspace"] != "work" {
		t.Fatalf("workspace %v, want the directory-scoped one", out["workspace"])
	}
	args, dir := rec.last()
	if !reflect.DeepEqual(args, []string{"run", "claude", "--name", "x", "--env", WebSessionEnv + "=" + id}) || dir != st.dirs[0] {
		t.Fatalf("ran %q in %s", args, dir)
	}
	if got := s.sessions.Get(id).Info().Args; !reflect.DeepEqual(got, []string{"run", "claude", "--name", "x"}) {
		t.Fatalf("session args %q keep the id out", got)
	}

	if _, err := dialSession(t, ts, id, "http://evil.example"); err == nil {
		t.Fatal("WebSocket from a foreign origin accepted")
	}
	ws, err := dialSession(t, ts, id, ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Close() }()

	sawLive = false
	readUntil(t, ws, "30 100")
	if err := websocket.Message.Send(ws, `{"t":"r","c":90,"r":20}`); err != nil {
		t.Fatal(err)
	}
	// Give the resize a moment to land before the shell reads the size again.
	time.Sleep(200 * time.Millisecond)
	if err := websocket.Message.Send(ws, `{"t":"i","d":"hello\r"}`); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, "got:hello")
	if !sawLive {
		t.Fatal("no end-of-replay marker")
	}
	readUntil(t, ws, "20 90")
	_, exit := readUntil(t, ws, "")
	if exit["t"] != "exit" || exit["code"] != float64(3) {
		t.Fatalf("exit message %v", exit)
	}

	info := s.sessions.Get(id).Info()
	if !info.Exited || info.ExitCode != 3 {
		t.Fatalf("session state %+v", info)
	}

	// A later viewer gets the replay and the exit.
	ws2, err := dialSession(t, ts, id, ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws2.Close() }()
	readUntil(t, ws2, "got:hello")

	// Start it again: same arguments, new id, old one forgotten.
	resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]int{"cols": 80, "rows": 24}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("restart: %d %v", resp.StatusCode, out)
	}
	if out["id"] == id || s.sessions.Get(id) != nil {
		t.Fatal("restart kept the old session")
	}
	if args, _ := rec.last(); !reflect.DeepEqual(args, []string{"run", "claude", "--name", "x", "--env", WebSessionEnv + "=" + out["id"].(string)}) {
		t.Fatalf("restart ran %q", args)
	}
}

func TestCloseStopsSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "sleep 60")})
	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	sess := s.sessions.Get(id)
	resp, _ = do(t, ts, "DELETE", "/api/sessions/"+id, nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("close: %d", resp.StatusCode)
	}
	select {
	case <-sess.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("process still running after close")
	}
	if len(s.sessions.List()) != 0 {
		t.Fatal("closed session still listed")
	}
}

func TestReplayIsBounded(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	m := NewManager(shellCommand(&recorder{}, `head -c 1200000 /dev/zero | tr '\0' x; echo; echo END`), inProcessHosts, shortTempDir(t))
	sess, err := m.Start(SessionInfo{Agent: "claude", Dir: t.TempDir()}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll()
	select {
	case <-sess.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("process did not finish")
	}
	replay, sub := sess.Attach()
	sess.Detach(sub)
	if len(replay) > replayBytes {
		t.Fatalf("replay holds %d bytes, limit %d", len(replay), replayBytes)
	}
	if !bytes.Contains(replay, []byte("END")) {
		t.Fatal("replay lost the newest output")
	}
}

func TestEmbeddedAppAndCSP(t *testing.T) {
	_, _, ts := newTestServer(t, Options{})
	resp, err := ts.Client().Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || !bytes.Contains(body, []byte("/_app/immutable/")) {
		t.Fatalf("index: %d, no SvelteKit bundle reference (run make web)", resp.StatusCode)
	}
	csp := resp.Header.Get("Content-Security-Policy")
	m := inlineScriptRe.FindSubmatch(body)
	if m == nil {
		t.Fatal("index.html has no inline start script")
	}
	sum := sha256.Sum256(m[1])
	if want := "'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"; !strings.Contains(csp, want) {
		t.Fatalf("CSP %q does not allow the start script %s", csp, want)
	}
	if strings.Contains(csp, "'unsafe-inline'") && strings.Contains(csp, "script-src 'unsafe-inline'") {
		t.Fatal("CSP allows any inline script")
	}
}

func TestEnvProfilesOnDemand(t *testing.T) {
	_, _, ts := newTestServer(t, Options{})
	_, state := do(t, ts, "GET", "/api/state", nil, "")
	for _, w := range state["workspaces"].([]any) {
		if _, ok := w.(map[string]any)["envProfiles"]; ok {
			t.Fatal("state polls read env profiles (workspace KV)")
		}
	}
	resp, out := do(t, ts, "GET", "/api/env-profiles?workspace=work", nil, "")
	if resp.StatusCode != 200 || len(out["profiles"].([]any)) != 1 {
		t.Fatalf("env profiles: %d %v", resp.StatusCode, out)
	}
	resp, _ = do(t, ts, "GET", "/api/env-profiles?workspace=nope", nil, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown workspace: %d", resp.StatusCode)
	}
}

func TestRestartUnnamedForgetsName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "exit 0")})
	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	sess := s.sessions.Get(out["id"].(string))
	<-sess.Done()
	// The name it learned from its container is not the next run's name.
	sess.update(func(i *SessionInfo) { i.Name = "2026-11-12 10:00:00" })
	resp, out = do(t, ts, "POST", "/api/sessions/"+sess.Info().ID+"/restart", map[string]int{}, "")
	if resp.StatusCode != http.StatusCreated || out["name"] != "" {
		t.Fatalf("restart: %d name %v", resp.StatusCode, out["name"])
	}
}

func TestMeHidesUserBeforeLogin(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	_, _, ts := newTestServer(t, Options{Username: "me", PasswordHash: hash})
	if _, me := do(t, ts, "GET", "/api/me", nil, ""); me["user"] != "" {
		t.Fatalf("username before login: %v", me["user"])
	}
}

func TestLoginCookieSecureBehindTLSProxy(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	_, _, ts := newTestServer(t, Options{Username: "me", PasswordHash: hash})
	req, err := http.NewRequest("POST", ts.URL+"/api/login", strings.NewReader(`{"username":"me","password":"correct horse"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-ExitBox", "1")
	req.Header.Set("X-Forwarded-Proto", "https")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for _, c := range resp.Cookies() {
		if c.Name == cookieName && !c.Secure {
			t.Fatal("cookie not Secure behind a TLS proxy")
		}
	}
}

// A restart of exitbox web leaves sessions running; the next server takes
// them over with their output, and only closing the window ends them.
func TestSessionsSurviveServerRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	script := `echo started; while IFS= read -r l; do echo "got:$l"; done`
	s1, st, ts1 := newTestServer(t, Options{Command: shellCommand(&recorder{}, script), StateDir: dir})
	resp, out := do(t, ts1, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude", "name": "keep"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	keep := out["id"].(string)
	resp, out = do(t, ts1, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude", "name": "ends"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	ends := out["id"].(string)
	ws, err := dialSession(t, ts1, keep, ts1.URL)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, "started")
	_ = ws.Close()

	// The server stops: hosts keep running.
	s1.sessions.Disconnect()
	ts1.Close()

	// While nobody is connected, one session ends.
	c, err := net.Dial("unix", sockPath(dir, ends))
	if err != nil {
		t.Fatalf("host of an unwatched session is gone: %v", err)
	}
	if err := writeFrame(c, frameStop, nil); err != nil {
		t.Fatal(err)
	}
	// Wait for the host to report the exit (after the hang-up, or the kill
	// that follows its grace period).
	_ = c.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		k, _, err := readFrame(c)
		if err != nil {
			t.Fatalf("no exit from the stopped session: %v", err)
		}
		if k == frameExit {
			break
		}
	}
	_ = c.Close()

	// A new server takes both over.
	s2, _, ts2 := newTestServer(t, Options{Command: shellCommand(&recorder{}, script), StateDir: dir})
	n, err := s2.Restore()
	if err != nil || n != 2 {
		t.Fatalf("restored %d, %v", n, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for !s2.sessions.Get(ends).Info().Exited && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if !s2.sessions.Get(ends).Info().Exited {
		t.Fatal("the session that ended meanwhile is not reported as ended")
	}
	if info := s2.sessions.Get(keep).Info(); info.Exited || info.Name != "keep" {
		t.Fatalf("restored session %+v", info)
	}
	ws2, err := dialSession(t, ts2, keep, ts2.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws2.Close() }()
	readUntil(t, ws2, "started") // the replay came along
	if err := websocket.Message.Send(ws2, `{"t":"i","d":"hello\r"}`); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws2, "got:hello")

	// Closing the window ends the session and its host cleans up.
	if resp, _ := do(t, ts2, "DELETE", "/api/sessions/"+keep, nil, ""); resp.StatusCode != 200 {
		t.Fatalf("close: %d", resp.StatusCode)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath(dir, keep)); os.IsNotExist(err) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, p := range []string{sockPath(dir, keep), specPath(dir, keep), infoPath(dir, keep)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s left behind", p)
		}
	}
}

func TestRestoreDropsDeadHosts(t *testing.T) {
	dir := shortTempDir(t)
	m := NewManager(shellCommand(&recorder{}, "true"), inProcessHosts, dir)
	if err := m.writeInfo("0123456789abcdef", []byte(`{"id":"0123456789abcdef","agent":"claude"}`)); err != nil {
		t.Fatal(err)
	}
	// A temp file of a server that died mid-write.
	if err := os.WriteFile(filepath.Join(dir, "0123456789abcdef.info.123.tmp"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := m.Restore()
	if err != nil || n != 0 {
		t.Fatalf("restored %d, %v", n, err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("files left behind: %v", left)
	}
}

func TestNoTmuxIsRefused(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	resp, _ := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude", "noTmux": true}, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("noTmux accepted: %d", resp.StatusCode)
	}
}
