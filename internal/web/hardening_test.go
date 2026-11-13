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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
	"golang.org/x/net/websocket"
)

// loginServer is a test server with a login, plus a signed-in cookie.
func loginServer(t *testing.T) (*Server, *fakeStore, *httptest.Server, string) {
	t.Helper()
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	s, st, ts := newTestServer(t, Options{Username: "me", PasswordHash: hash, Command: shellCommand(&recorder{}, "sleep 30")})
	return s, st, ts, loginCookie(t, ts, "me", "correct horse")
}

// openStream starts a session and attaches a WebSocket to it.
func openStream(t *testing.T, ts *httptest.Server, dir, cookie string) *websocket.Conn {
	t.Helper()
	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": dir, "agent": "claude", "cols": 80, "rows": 24}, cookie)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	cfg, err := websocket.NewConfig(strings.Replace(ts.URL, "http", "ws", 1)+"/api/sessions/"+out["id"].(string)+"/attach", ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != "" {
		cfg.Header = http.Header{"Cookie": {cookie}}
	}
	ws, err := websocket.DialConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

// streamEnds reports whether the server closes the stream within d.
func streamEnds(t *testing.T, ws *websocket.Conn, d time.Duration) bool {
	t.Helper()
	if err := ws.SetReadDeadline(time.Now().Add(d)); err != nil {
		t.Fatal(err)
	}
	for {
		var frame []byte
		err := websocket.Message.Receive(ws, &frame)
		if err == nil {
			continue
		}
		var ne net.Error
		return !(errors.As(err, &ne) && ne.Timeout())
	}
}

func TestLogoutEndsWebSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	_, st, ts, cookie := loginServer(t)
	other := loginCookie(t, ts, "me", "correct horse")
	ws := openStream(t, ts, st.dirs[0], cookie)
	kept := openStream(t, ts, st.dirs[0], other)
	if resp, _ := do(t, ts, "POST", "/api/logout", map[string]string{}, cookie); resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	if !streamEnds(t, ws, 5*time.Second) {
		t.Fatal("the stream outlived its logout")
	}
	if streamEnds(t, kept, 300*time.Millisecond) {
		t.Fatal("another browser's stream ended too")
	}
}

func TestCredentialChangeEndsWebSockets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts, cookie := loginServer(t)
	ws := openStream(t, ts, st.dirs[0], cookie)
	user, hash := s.auth.credentials()
	s.auth.set(user, hash)
	if streamEnds(t, ws, 300*time.Millisecond) {
		t.Fatal("the same credentials ended the stream")
	}
	s.auth.set("someone", hash)
	if !streamEnds(t, ws, 5*time.Second) {
		t.Fatal("the stream outlived a credential change")
	}
}

func TestExpiredLoginEndsWebSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	old := streamAuthCheck
	streamAuthCheck = 50 * time.Millisecond
	t.Cleanup(func() { streamAuthCheck = old })
	s, st, ts, cookie := loginServer(t)
	ws := openStream(t, ts, st.dirs[0], cookie)
	s.auth.mu.Lock()
	s.auth.now = func() time.Time { return time.Now().Add(cookieTTL + time.Hour) }
	s.auth.mu.Unlock()
	if !streamEnds(t, ws, 5*time.Second) {
		t.Fatal("the stream outlived its login")
	}
}

func TestNoLoginStreamsUntouched(t *testing.T) {
	a := newAuth("", "")
	ended, untrack := a.track("")
	defer untrack()
	a.logout("whatever")
	if !a.live("") {
		t.Fatal("no-login stream not live")
	}
	select {
	case <-ended:
		t.Fatal("no-login stream ended")
	default:
	}
	// Turning a login on ends streams opened without one.
	a.set("me", "hash")
	select {
	case <-ended:
	default:
		t.Fatal("stream without a login kept after a login was set")
	}
}

func TestBrokenConfigIsNotOverwritten(t *testing.T) {
	s, st, ts := newTestServer(t, Options{})
	s.vault = &memVault{pw: map[string]string{"work": "vault pass"}, data: map[string]map[string]string{}}
	st.loadErr = errors.New("reading config.yaml: yaml: line 3: bad indentation")
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/state", nil},
		{"GET", "/api/settings", nil},
		{"GET", "/api/vault", nil},
		{"GET", "/api/env-profiles?workspace=work", nil},
		{"PUT", "/api/settings", map[string]any{"statusBar": true}},
		{"PUT", "/api/settings/web", map[string]any{"listen": "127.0.0.1:5000"}},
		{"POST", "/api/vault/work/config", map[string]any{"enabled": false, "readOnly": false}},
		{"POST", "/api/vault/work/destroy", map[string]any{"confirm": "work"}},
	} {
		resp, out := do(t, ts, c.method, c.path, c.body, "")
		if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(fmt.Sprint(out["error"]), "bad indentation") {
			t.Errorf("%s %s: %d %v, want 500 with the load error", c.method, c.path, resp.StatusCode, out)
		}
	}
	if st.saves != 0 {
		t.Fatalf("%d saves over a config that failed to load", st.saves)
	}
	if err := s.saveVaultConfig("work", true, false); err == nil {
		t.Fatal("saveVaultConfig saved over a broken config")
	}
}

func TestConfigStoreDefaultsOnlyWhenMissing(t *testing.T) {
	old := config.Home
	config.Home = t.TempDir()
	t.Cleanup(func() { config.Home = old })
	var cs configStore
	if cfg, err := cs.loadConfig(); err != nil || cfg == nil {
		t.Fatalf("missing config.yaml: %v %v, want defaults", cfg, err)
	}
	if al, err := cs.loadAllowlist(); err != nil || al == nil {
		t.Fatalf("missing allowlist.yaml: %v %v, want defaults", al, err)
	}
	for _, name := range []string{"config.yaml", "allowlist.yaml"} {
		if err := os.WriteFile(filepath.Join(config.Home, name), []byte("version: [unclosed\n\t:"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cs.loadConfig(); err == nil {
		t.Fatal("a broken config.yaml loaded as defaults")
	}
	if _, err := cs.loadAllowlist(); err == nil {
		t.Fatal("a broken allowlist.yaml loaded as defaults")
	}
}

func TestConcurrentSettingsKeepBothChanges(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	_, doc := do(t, ts, "GET", "/api/settings", nil, "")
	delete(doc, "web")
	delete(doc, "keybindingDefaults")
	doc["statusBar"] = !st.cfg.Settings.StatusBar
	want := doc["statusBar"]
	// Every load waits, so unserialized handlers would both read the old file.
	st.loadHook = func() { time.Sleep(100 * time.Millisecond) }
	var wg sync.WaitGroup
	codes := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		resp, _ := do(t, ts, "PUT", "/api/settings", doc, "")
		codes[0] = resp.StatusCode
	}()
	go func() {
		defer wg.Done()
		resp, _ := do(t, ts, "PUT", "/api/settings/web", map[string]any{"listen": "127.0.0.1:5000"}, "")
		codes[1] = resp.StatusCode
	}()
	wg.Wait()
	if codes[0] != 200 || codes[1] != 200 {
		t.Fatalf("status %v", codes)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cfg.Settings.StatusBar != want || st.cfg.Web.Listen != "127.0.0.1:5000" {
		t.Fatalf("statusBar %v listen %q: a concurrent save lost a change", st.cfg.Settings.StatusBar, st.cfg.Web.Listen)
	}
}

func TestVaultDestroyRefusedWhileRunning(t *testing.T) {
	s, st, ts := newTestServer(t, Options{})
	mv := &memVault{pw: map[string]string{"work": "vault pass"}, data: map[string]map[string]string{"work": {}}}
	s.vault = mv
	st.ctrs = []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: st.dirs[0], Workspace: "work", Session: "s", Started: time.Now()}}
	resp, out := do(t, ts, "POST", "/api/vault/work/destroy", map[string]any{"confirm": "work"}, "")
	if resp.StatusCode != http.StatusConflict || !mv.initialized("work") {
		t.Fatalf("destroy while running: %d %v", resp.StatusCode, out)
	}
	st.mu.Lock()
	st.ctrs = nil
	st.mu.Unlock()
	if resp, out := do(t, ts, "POST", "/api/vault/work/destroy", map[string]any{"confirm": "work"}, ""); resp.StatusCode != http.StatusOK || mv.initialized("work") {
		t.Fatalf("destroy without a password once stopped: %d %v", resp.StatusCode, out)
	}
}

// Without a fresh container list destroy cannot rule out a running session.
func TestVaultDestroyRefusedWhenListingFails(t *testing.T) {
	s, st, ts := newTestServer(t, Options{})
	mv := &memVault{pw: map[string]string{"work": "vault pass"}, data: map[string]map[string]string{"work": {}}}
	s.vault = mv
	// The cache says nothing runs; the runtime cannot be asked.
	s.ctrMu.Lock()
	s.ctrList, s.ctrAt = nil, time.Now()
	s.ctrMu.Unlock()
	st.mu.Lock()
	st.ctrErr = errors.New("timed out")
	st.mu.Unlock()
	resp, out := do(t, ts, "POST", "/api/vault/work/destroy", map[string]any{"confirm": "work"}, "")
	if resp.StatusCode != http.StatusServiceUnavailable || out["error"] != "cannot check for running sessions; try again" || !mv.initialized("work") {
		t.Fatalf("destroy without a container list: %d %v, vault kept %v", resp.StatusCode, out, mv.initialized("work"))
	}
	st.mu.Lock()
	st.ctrErr = nil
	st.mu.Unlock()
	if resp, out := do(t, ts, "POST", "/api/vault/work/destroy", map[string]any{"confirm": "work"}, ""); resp.StatusCode != http.StatusOK || mv.initialized("work") {
		t.Fatalf("destroy once the runtime answers: %d %v", resp.StatusCode, out)
	}
}

func TestQueryTrackerBounded(t *testing.T) {
	now := time.Now()
	q := &queryTracker{now: func() time.Time { return now }}
	q.sawOutput(bytes.Repeat([]byte("\x1b[c"), 1000))
	if n := len(q.pending["da1"]); n > maxQueriesPerKind {
		t.Fatalf("%d da1 queries kept, cap %d", n, maxQueriesPerKind)
	}
	var many bytes.Buffer
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&many, "\x1b[?%d$p", i)
	}
	many.WriteString("\x1b[?" + strings.Repeat("1", 100) + "$p")
	q.sawOutput(many.Bytes())
	if len(q.pending) > maxQueryKinds {
		t.Fatalf("%d kinds kept, cap %d", len(q.pending), maxQueryKinds)
	}
	for k := range q.pending {
		if len(k) > maxQueryKindLen {
			t.Fatalf("kind %q over the length cap", k)
		}
	}
	// Unanswered queries go once the answer window passes.
	now = now.Add(answerWindow + time.Second)
	q.sawOutput([]byte("plain output"))
	if len(q.pending) != 0 {
		t.Fatalf("%d kinds kept past the answer window", len(q.pending))
	}
}

func TestNewLeavesSessionsFileAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.json")
	orig := []byte(`{"credential":"someone else","sessions":{"k":"2999-01-01T00:00:00Z"}}`)
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	s, err := New(Options{Listen: "0.0.0.0:0", Username: "me", PasswordHash: "$2a$x", Command: shellCommand(&recorder{}, "true"), Spawn: inProcessHosts, StateDir: shortTempDir(t), SessionsFile: path, Logger: log.New(&logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, orig) {
		t.Fatalf("New rewrote the sessions file: %s %v", data, err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, ln) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && !bytes.Equal(data, orig) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Serve did not take over the sessions file")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(logs.String(), "plain HTTP"); n != 1 {
		t.Fatalf("%d plain HTTP warnings, want 1:\n%s", n, logs.String())
	}
}

func TestLoopbackServeHasNoPlainHTTPWarning(t *testing.T) {
	var logs bytes.Buffer
	s, err := New(Options{Listen: "127.0.0.1:0", Command: shellCommand(&recorder{}, "true"), Spawn: inProcessHosts, StateDir: shortTempDir(t), SessionsFile: filepath.Join(t.TempDir(), "sessions.json"), Logger: log.New(&logs, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Serve(ctx, ln); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "plain HTTP") {
		t.Fatalf("loopback server warned:\n%s", logs.String())
	}
}

func TestRuntimeCallTimesOut(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no sleep binary on Windows")
	}
	old := runtimeTimeout
	runtimeTimeout = 100 * time.Millisecond
	t.Cleanup(func() { runtimeTimeout = old })
	start := time.Now()
	if _, err := runtimeOutput("sleep", "5"); err == nil {
		t.Fatal("a hung runtime call returned no error")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the runtime call took %v", d)
	}
	// A leftover child holding stdout must not outlast the timeout either.
	start = time.Now()
	if _, err := runtimeOutput("/bin/sh", "-c", "sleep 10 & wait"); err == nil {
		t.Fatal("a runtime call whose child holds stdout returned no error")
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("the runtime call with a child holding stdout took %v", d)
	}
}

func TestContainerListKeptOnError(t *testing.T) {
	s, st, _ := newTestServer(t, Options{})
	prev := []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude"}}
	s.ctrMu.Lock()
	s.ctrList, s.ctrAt = prev, time.Time{}
	s.ctrMu.Unlock()
	st.mu.Lock()
	st.ctrErr = errors.New("timed out")
	st.mu.Unlock()
	if got := s.runningContainers(); len(got) != 1 || got[0].Container != prev[0].Container {
		t.Fatalf("containers after a failed listing: %v", got)
	}
}

func TestConcurrentRestartRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	// The replacement's start waits until the second request has its answer.
	var mu sync.Mutex
	starts := 0
	entered, release := make(chan struct{}), make(chan struct{})
	command := func(info SessionInfo) *exec.Cmd {
		mu.Lock()
		starts++
		n := starts
		mu.Unlock()
		if n == 2 {
			close(entered)
			<-release
		}
		return exec.Command("/bin/sh", "-c", "exit 0")
	}
	s, st, ts := newTestServer(t, Options{Command: command})
	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude", "name": "x"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	waitDone(t, s.sessions.Get(id))
	type result struct {
		status int
		out    map[string]any
		err    error
	}
	first := make(chan result, 1)
	go func() {
		// No t.Fatal off the test goroutine.
		req, err := http.NewRequest("POST", ts.URL+"/api/sessions/"+id+"/restart", strings.NewReader(`{"cols":80,"rows":24}`))
		if err != nil {
			first <- result{err: err}
			return
		}
		req.Header.Set("X-ExitBox", "1")
		resp, err := ts.Client().Do(req)
		if err != nil {
			first <- result{err: err}
			return
		}
		defer func() { _ = resp.Body.Close() }()
		var out map[string]any
		err = json.NewDecoder(resp.Body).Decode(&out)
		first <- result{resp.StatusCode, out, err}
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("the first restart did not start a replacement")
	}
	resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]int{"cols": 80, "rows": 24}, "")
	close(release)
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("restart during a restart: %d %v", resp.StatusCode, out)
	}
	r := <-first
	if r.err != nil || r.status != http.StatusCreated {
		t.Fatalf("first restart: %d %v %v", r.status, r.out, r.err)
	}
	mu.Lock()
	n := starts
	mu.Unlock()
	list := s.sessions.List()
	if n != 2 || len(list) != 1 || list[0].ID != r.out["id"] {
		t.Fatalf("%d starts, sessions %+v; want one replacement %v", n, list, r.out["id"])
	}
	if resp, _ := do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]int{"cols": 80, "rows": 24}, ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("restart of the replaced session: %d, want 404", resp.StatusCode)
	}
}

func TestSystemdEnvironmentQuoting(t *testing.T) {
	unit := SystemdUnit(ServiceSpec{Exe: "/x/exitbox", Args: []string{"web"}, Env: map[string]string{"DOCKER_HOST": `tcp://h$1:2375 %i "q" \b`}})
	if want := `Environment="DOCKER_HOST=tcp://h$1:2375 %%i \"q\" \\b"` + "\n"; !strings.Contains(unit, want) {
		t.Fatalf("unit lacks %q:\n%s", want, unit)
	}
	found := false
	for _, k := range ServiceEnvKeys {
		found = found || k == "TMPDIR"
	}
	if !found {
		t.Fatal("TMPDIR is not carried into the service")
	}
}

func TestHTTPServerLimits(t *testing.T) {
	s, _, _ := newTestServer(t, Options{})
	srv := s.httpServer()
	if srv.IdleTimeout <= 0 || srv.MaxHeaderBytes <= 0 || srv.ReadHeaderTimeout <= 0 {
		t.Fatalf("server limits %+v", srv)
	}
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Fatal("a global read or write timeout would end WebSockets")
	}
}

func TestLoginBodyDeadline(t *testing.T) {
	old := loginBodyTimeout
	loginBodyTimeout = 200 * time.Millisecond
	t.Cleanup(func() { loginBodyTimeout = old })
	_, _, ts, _ := loginServer(t)
	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	req := "POST /api/login HTTP/1.1\r\nHost: " + ts.Listener.Addr().String() + "\r\nX-ExitBox: 1\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{\"username\":"
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatal(err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("no answer to a stalled login body: %v", err)
	}
	if !strings.Contains(line, " 400 ") {
		t.Fatalf("status line %q, want 400", line)
	}
}

func TestLoginGivesUpOnCancel(t *testing.T) {
	a := newAuth("me", "$2a$12$abcdefghijklmnopqrstuuK4dhkKyv1sSxV0mmw2B5/2ZQ8ziNCcO")
	for i := 0; i < cap(bcryptSlots); i++ {
		bcryptSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < cap(bcryptSlots); i++ {
			<-bcryptSlots
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := a.login(ctx, "1.2.3.4", "me", "pw")
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("login error %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("login waited for a bcrypt slot after its request ended")
	}
	if n := len(a.failures[clientKey("1.2.3.4")]); n != 0 {
		t.Fatalf("a cancelled login counted %d failures", n)
	}
}

func TestAgentVersionRestricted(t *testing.T) {
	for _, c := range []struct {
		version string
		ok      bool
	}{
		{"", true}, {"latest", true}, {"1.2.3", true}, {"2.0.0-beta.1+build", true}, {" 1.0 ", true},
		{"1.0 && rm -rf /", false}, {`a"b`, false}, {"$(id)", false}, {"-x", false}, {".1", false},
		{"a\nb", false}, {strings.Repeat("1", 65), false},
	} {
		cfg := config.DefaultConfig()
		err := applySettings(cfg, config.DefaultAllowlist(), settingsDoc{Agents: []agentDoc{{Name: "claude", Enabled: true, Version: c.version}}}, "/home/u")
		if (err == nil) != c.ok {
			t.Errorf("version %q: err %v, want ok=%v", c.version, err, c.ok)
		}
	}
}

func TestTabRefusedInNamesAndDirs(t *testing.T) {
	if validName("a\tb") {
		t.Fatal("a tab passed validName")
	}
	home := t.TempDir()
	tabDir := filepath.Join(home, "a\tb")
	if err := os.MkdirAll(tabDir, 0o755); err != nil {
		t.Skipf("no tab in file names here: %v", err)
	}
	if _, err := expandDir(tabDir, home); err == nil {
		t.Fatal("a folder with a tab passed expandDir")
	}
	cfg := config.DefaultConfig()
	cfg.SetAgentEnabled("claude", true)
	if _, _, err := buildArgs(cfg, RunOptions{Dir: home, Agent: "claude", Name: "x\ty"}, home); err == nil {
		t.Fatal("a session name with a tab passed buildArgs")
	}
	_, _, ts := newTestServer(t, Options{})
	if resp, _ := do(t, ts, "POST", "/api/running/rename", map[string]string{"container": "c", "name": "x\ty"}, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("rename to a name with a tab: %d", resp.StatusCode)
	}
}

func TestRenameRunningChecksWorkspace(t *testing.T) {
	s, st, ts := newTestServer(t, Options{})
	st.ctrs = []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: st.dirs[0], Workspace: "gone", Session: "s", Started: time.Now()}}
	resp, out := do(t, ts, "POST", "/api/running/rename", map[string]string{"container": "exitbox-claude-p-0a1b2c3d", "name": "renamed"}, "")
	if resp.StatusCode != http.StatusBadRequest || len(s.renames.list()) != 0 {
		t.Fatalf("rename for an unknown workspace: %d %v, queue %v", resp.StatusCode, out, s.renames.list())
	}
}

func TestPendingRenameNeedsWorkspaceAndFreshList(t *testing.T) {
	s, st, _ := newTestServer(t, Options{})
	dir := st.dirs[0]
	st.saved["work/claude/"+dir] = []string{"busy"}
	if err := s.renames.add(pendingRename{Workspace: "gone", Agent: "claude", Dir: dir, Name: "old", To: "new"}); err != nil {
		t.Fatal(err)
	}
	if err := s.renames.add(pendingRename{Workspace: "work", Agent: "claude", Dir: dir, Name: "busy", To: "free"}); err != nil {
		t.Fatal(err)
	}
	// The cache, two seconds old, says nothing runs; the runtime knows "busy" still does.
	st.mu.Lock()
	st.ctrs = []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: dir, Workspace: "work", Session: "busy", Started: time.Now()}}
	st.mu.Unlock()
	s.ctrMu.Lock()
	s.ctrList, s.ctrAt = nil, time.Now().Add(-2*time.Second)
	s.ctrMu.Unlock()
	s.applyPendingRenames(nil, nil)
	st.mu.Lock()
	calls := st.renameCalls
	st.mu.Unlock()
	if calls != 0 {
		t.Fatalf("%d renames applied; one has no workspace, the other still runs", calls)
	}
	left := s.renames.list()
	if len(left) != 1 || left[0].Name != "busy" {
		t.Fatalf("queue %v, want only the running session's rename", left)
	}
}

// A rename that keeps failing is retried with growing pauses and logged
// once per error, and a young container list is reused.
func TestPendingRenameBacksOff(t *testing.T) {
	logs := &safeBuf{}
	s, st, _ := newTestServer(t, Options{Logger: log.New(logs, "", 0)})
	dir := st.dirs[0]
	st.saved["work/claude/"+dir] = []string{"old"}
	if err := s.renames.add(pendingRename{Workspace: "work", Agent: "claude", Dir: dir, Name: "old", To: "new"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.renameNow = func() time.Time { return now }
	st.mu.Lock()
	st.renameErr = errors.New("the workspace store is busy")
	st.mu.Unlock()
	counts := func() (renames, lists int) {
		st.mu.Lock()
		defer st.mu.Unlock()
		return st.renameCalls, st.ctrCalls
	}
	s.ctrMu.Lock()
	s.ctrList, s.ctrAt = nil, time.Now()
	s.ctrMu.Unlock()
	for i := 0; i < 5; i++ {
		s.applyPendingRenames(nil, nil)
	}
	if r, l := counts(); r != 1 || l != 0 {
		t.Fatalf("%d rename tries and %d listings for polls in one moment, want 1 and 0", r, l)
	}
	now = now.Add(renameRetryMin)
	s.applyPendingRenames(nil, nil)
	if r, _ := counts(); r != 2 {
		t.Fatalf("%d rename tries after the first pause, want 2", r)
	}
	// The pause doubles.
	now = now.Add(renameRetryMin)
	s.applyPendingRenames(nil, nil)
	if r, _ := counts(); r != 2 {
		t.Fatalf("%d rename tries within the doubled pause, want 2", r)
	}
	now = now.Add(renameRetryMin)
	s.ctrMu.Lock()
	s.ctrAt = time.Now().Add(-2 * time.Second)
	s.ctrMu.Unlock()
	s.applyPendingRenames(nil, nil)
	if r, l := counts(); r != 3 || l != 1 {
		t.Fatalf("%d rename tries and %d listings after the doubled pause, want 3 and 1", r, l)
	}
	if n := strings.Count(logs.String(), "the workspace store is busy"); n != 1 {
		t.Fatalf("the same failure logged %d times:\n%s", n, logs.String())
	}
	// It is capped at a minute, and applied once the store answers.
	st.mu.Lock()
	st.renameErr = nil
	st.mu.Unlock()
	now = now.Add(renameRetryMax)
	s.applyPendingRenames(nil, nil)
	if len(s.renames.list()) != 0 || len(s.renameRetries) != 0 {
		t.Fatalf("queue %v, retries %v after success", s.renames.list(), s.renameRetries)
	}
}

// The body deadline must not cancel a login waiting for its bcrypt turn.
func TestLoginDeadlineClearedAfterBody(t *testing.T) {
	old := loginBodyTimeout
	loginBodyTimeout = 100 * time.Millisecond
	t.Cleanup(func() { loginBodyTimeout = old })
	_, _, ts, _ := loginServer(t)
	for i := 0; i < cap(bcryptSlots); i++ {
		bcryptSlots <- struct{}{}
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		for i := 0; i < cap(bcryptSlots); i++ {
			<-bcryptSlots
		}
	}()
	resp, out := do(t, ts, "POST", "/api/login", map[string]string{"username": "me", "password": "correct horse"}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login that waited past the body deadline: %d %v", resp.StatusCode, out)
	}
}

// A login whose request ended while it waited is no failed login.
func TestLoginCancelledIsBusyNotFailed(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	logs := &safeBuf{}
	s, _, _ := newTestServer(t, Options{Username: "me", PasswordHash: hash, Logger: log.New(logs, "", 0)})
	for i := 0; i < cap(bcryptSlots); i++ {
		bcryptSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < cap(bcryptSlots); i++ {
			<-bcryptSlots
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"me","password":"correct horse"}`)).WithContext(ctx)
	req.Host = "127.0.0.1:4848"
	req.Header.Set("X-ExitBox", "1")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("cancelled login: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(logs.String(), "failed login") {
		t.Fatalf("a cancelled login was logged as failed:\n%s", logs.String())
	}
	if n := len(s.auth.failures[clientKey(remoteHost(req))]); n != 0 {
		t.Fatalf("a cancelled login counted %d failures", n)
	}
}

func TestServiceEnvTMPDIR(t *testing.T) {
	// Not t.TempDir: on macOS that is the OS default under /var/folders.
	custom, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		tmpdir string
		keep   bool
	}{
		{"", false},
		{"/tmp", false},
		{"/tmp/", false},
		{filepath.Join(t.TempDir(), "gone"), false},
		{"relative/tmp", false},
		{custom, true},
	} {
		env := ServiceEnv(func(k string) string {
			switch k {
			case "TMPDIR":
				return c.tmpdir
			case "DOCKER_HOST":
				return "unix:///run/docker.sock"
			}
			return ""
		})
		if _, ok := env["TMPDIR"]; ok != c.keep {
			t.Errorf("TMPDIR %q carried %v, want %v", c.tmpdir, ok, c.keep)
		}
		if env["DOCKER_HOST"] != "unix:///run/docker.sock" || len(env) > 2 {
			t.Errorf("TMPDIR %q: env %v", c.tmpdir, env)
		}
	}
}
