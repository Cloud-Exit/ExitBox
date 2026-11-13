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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLockStateDirIsExclusive(t *testing.T) {
	dir := shortTempDir(t)
	first, err := LockStateDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := LockStateDir(dir); !errors.Is(err, ErrServerRunning) {
		t.Fatalf("second lock: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := LockStateDir(dir)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
}

// The host's error reaches the browser, and Start stops waiting once the
// host has exited.
func TestStartReportsHostError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	// As spawnHost does: the log is the host's stderr, written after
	// RunHost returns.
	spawn := func(dir, id string) (<-chan struct{}, error) {
		logFile, err := os.OpenFile(logPath(dir, id), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		exited := make(chan struct{})
		go func() {
			defer close(exited)
			defer func() { _ = logFile.Close() }()
			if err := RunHost(dir, id); err != nil {
				_, _ = fmt.Fprintf(logFile, "Error: %v\n", err)
			}
		}()
		return exited, nil
	}
	m := NewManager(func(SessionInfo) *exec.Cmd { return exec.Command("/nonexistent/exitbox-test") }, spawn, dir)
	start := time.Now()
	_, err := m.Start(SessionInfo{Agent: "claude", Dir: t.TempDir()}, 80, 24)
	if err == nil {
		t.Fatal("a host that cannot start its process started")
	}
	if !strings.Contains(err.Error(), "/nonexistent/exitbox-test") {
		t.Fatalf("error lost the host's reason: %v", err)
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Start waited %v for a host that had exited", d)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("files left behind: %v", left)
	}
}

// A host that never listens is killed, not left running its agent.
func TestStartKillsHostThatNeverListens(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	var exited chan struct{}
	spawn := func(dir, id string) (<-chan struct{}, error) {
		c := exec.Command("sleep", "60")
		DetachProcess(c)
		if err := c.Start(); err != nil {
			return nil, err
		}
		exited = make(chan struct{})
		go func() {
			defer close(exited)
			_ = c.Wait()
		}()
		return exited, WriteHostPID(dir, id, c.Process.Pid)
	}
	m := NewManager(shellCommand(&recorder{}, "true"), spawn, dir)
	m.startTimeout = 200 * time.Millisecond
	if _, err := m.Start(SessionInfo{Agent: "claude", Dir: t.TempDir()}, 80, 24); err == nil {
		t.Fatal("a host that never listened started")
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("the host still runs")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("files left behind: %v", left)
	}
}

// The spec holds the server's environment: it must not stay on disk.
func TestHostRemovesSpec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	m := NewManager(shellCommand(&recorder{}, "sleep 60"), inProcessHosts, dir)
	sess, err := m.Start(SessionInfo{Agent: "claude", Dir: t.TempDir()}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll()
	if _, err := os.Stat(specPath(dir, sess.Info().ID)); !os.IsNotExist(err) {
		t.Fatalf("spec still on disk: %v", err)
	}
}

// fakeHost listens where a host would and hands over each connection.
func fakeHost(t *testing.T, dir, id string) (*net.UnixListener, chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("unix", sockPath(dir, id))
	if err != nil {
		t.Fatal(err)
	}
	ul := ln.(*net.UnixListener)
	ul.SetUnlinkOnClose(false)
	conns := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns <- c
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ul, conns
}

func connectFake(t *testing.T, m *Manager, id string, conns chan net.Conn) (*Session, net.Conn) {
	t.Helper()
	s := m.newSession(SessionInfo{ID: id, Kind: KindRun, Agent: "claude"})
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	conn, err := net.Dial("unix", sockPath(m.dir, id))
	if err != nil {
		t.Fatal(err)
	}
	s.connect(conn)
	select {
	case hc := <-conns:
		// Referenced until the end: a collected conn is closed by its finalizer.
		t.Cleanup(func() { _ = hc.Close() })
		return s, hc
	case <-time.After(5 * time.Second):
		t.Fatal("no connection")
	}
	return nil, nil
}

// A write that times out mid-frame must not leave the host parsing the
// rest of the stream from the middle of a payload.
func TestFailedWriteReconnects(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix sockets for hosts on Windows")
	}
	old := hostWriteTimeout
	hostWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { hostWriteTimeout = old })
	dir := shortTempDir(t)
	const id = "0123456789abcdef"
	_, conns := fakeHost(t, dir, id)
	m := NewManager(shellCommand(&recorder{}, "true"), inProcessHosts, dir)
	s, first := connectFake(t, m, id, conns)

	// The host does not read: the big write times out part way.
	if err := s.Write(bytes.Repeat([]byte("q"), 3<<20)); err == nil {
		t.Fatal("write to a host that does not read succeeded")
	}
	var second net.Conn
	select {
	case second = <-conns:
	case <-time.After(5 * time.Second):
		t.Fatal("no reconnect after a failed write")
	}
	t.Cleanup(func() { _ = second.Close() })
	// The cut connection is closed, so the host drops the partial frame.
	_ = first.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.Copy(io.Discard, first); err != nil {
		t.Fatalf("cut connection not closed: %v", err)
	}
	if s.Info().Exited {
		t.Fatal("a failed write ended a running session")
	}
	if err := s.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	_ = second.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, payload, err := readFrame(second)
	if err != nil || kind != frameInput || string(payload) != "hi" {
		t.Fatalf("next frame %q %q %v", kind, payload, err)
	}
	// A collected host conn would be closed by its finalizer and end the session.
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	if s.Info().Exited {
		t.Fatal("closing the cut connection ended the session")
	}
}

// A host that is gone leaves its files; closing its window removes them.
func TestCloseRemovesFilesOfDeadHost(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix sockets for hosts on Windows")
	}
	dir := shortTempDir(t)
	const id = "0123456789abcdef"
	ln, conns := fakeHost(t, dir, id)
	for _, p := range []string{specPath(dir, id), logPath(dir, id), infoPath(dir, id)} {
		if err := os.WriteFile(p, []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m := NewManager(shellCommand(&recorder{}, "true"), inProcessHosts, dir)
	s, hc := connectFake(t, m, id, conns)
	// The host is killed: its socket file stays, nobody listens.
	_ = hc.Close()
	_ = ln.Close()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("a lost host did not end its session")
	}
	if err := m.Close(id); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("files left behind: %v", left)
	}
}

// Concurrent updates leave the latest info on disk, through temp files of
// their own; an unchanged info is not written again.
func TestSaveInfoConcurrent(t *testing.T) {
	dir := shortTempDir(t)
	const id = "0123456789abcdef"
	m := NewManager(shellCommand(&recorder{}, "true"), inProcessHosts, dir)
	s := m.newSession(SessionInfo{ID: id, Kind: KindRun, Agent: "claude"})
	var wg sync.WaitGroup
	for n := 0; n < 64; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			s.update(func(i *SessionInfo) { i.Name = fmt.Sprint("name-", n) })
		}(n)
	}
	wg.Wait()
	read := func() SessionInfo {
		t.Helper()
		data, err := os.ReadFile(infoPath(dir, id))
		if err != nil {
			t.Fatal(err)
		}
		var info SessionInfo
		if err := json.Unmarshal(data, &info); err != nil {
			t.Fatalf("torn info file: %v", err)
		}
		return info
	}
	if got, want := read().Name, s.Info().Name; got != want {
		t.Fatalf("info file has %q, session has %q", got, want)
	}
	if tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp")); len(tmps) != 0 {
		t.Fatalf("temp files left: %v", tmps)
	}

	past := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(infoPath(dir, id), past, past); err != nil {
		t.Fatal(err)
	}
	s.update(func(*SessionInfo) {})
	if st, err := os.Stat(infoPath(dir, id)); err != nil || !st.ModTime().Equal(past) {
		t.Fatalf("unchanged info written again: %v", err)
	}
	s.update(func(i *SessionInfo) { i.Container = "exitbox-claude-p-0a1b2c3d" })
	if read().Container != "exitbox-claude-p-0a1b2c3d" {
		t.Fatal("changed info not written")
	}
}

// A process that does not read its input must not stop the host from
// reading stop and quit.
func TestHostReadsFramesWhileTerminalIsFull(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	old := hostWriteTimeout
	hostWriteTimeout = 3 * time.Second
	t.Cleanup(func() { hostWriteTimeout = old })
	m := NewManager(shellCommand(&recorder{}, "stty raw -echo; echo ready; sleep 60"), inProcessHosts, shortTempDir(t))
	sess, err := m.Start(SessionInfo{Agent: "claude", Dir: t.TempDir()}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer m.CloseAll()
	// Raw mode: the terminal holds input until it is read, so writes block.
	deadline := time.Now().Add(5 * time.Second)
	for {
		replay, sub := sess.Attach()
		sess.Detach(sub)
		if bytes.Contains(replay, []byte("ready")) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the process did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	chunk := bytes.Repeat([]byte("x"), 64<<10)
	for i := 0; i < 32; i++ {
		if err := sess.Write(chunk); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	if err := sess.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- m.Close(sess.Info().ID) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("close hung behind unread input")
	}
	select {
	case <-sess.Done():
	default:
		t.Fatal("process still running after close")
	}
}

func TestPendingRenameRetriesUntilApplied(t *testing.T) {
	s, st, _ := newTestServer(t, Options{})
	dir := st.dirs[0]
	key := "work/claude/" + dir
	st.saved[key] = []string{"old", "taken"}
	if err := s.renames.add(pendingRename{Workspace: "work", Agent: "claude", Dir: dir, Name: "old", To: "new"}); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	s.renameNow = func() time.Time { return now }
	// The workspace store is busy: the rename stays queued.
	st.renameErr = errors.New("the workspace store is busy")
	s.applyPendingRenames(nil, nil)
	if len(s.renames.list()) != 1 {
		t.Fatal("a rename that failed for now was dropped")
	}

	// Polls at once, past the retry pause, apply it once.
	st.renameErr = nil
	now = now.Add(renameRetryMax)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.applyPendingRenames(nil, nil)
		}()
	}
	wg.Wait()
	s.applyPendingRenames(nil, nil)
	if !reflect.DeepEqual(st.renames, []string{"old->new"}) || st.renameCalls != 2 || len(s.renames.list()) != 0 {
		t.Fatalf("renames %v, calls %d, queue %v", st.renames, st.renameCalls, s.renames.list())
	}

	// A refused rename never succeeds: it is dropped.
	if err := s.renames.add(pendingRename{Workspace: "work", Agent: "claude", Dir: dir, Name: "taken", To: "new"}); err != nil {
		t.Fatal(err)
	}
	s.applyPendingRenames(nil, nil)
	if len(s.renames.list()) != 0 {
		t.Fatal("a refused rename stayed queued")
	}
}

func TestRemoveSavedRefusedWhileRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "sleep 60")})
	dir := st.dirs[0]
	key := "work/claude/" + dir
	body := map[string]string{"workspace": "work", "agent": "claude", "dir": dir, "name": "feature-x"}

	// Running in a terminal.
	st.mu.Lock()
	st.ctrs = []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: dir, Workspace: "work", Session: "feature-x", Started: time.Now()}}
	st.mu.Unlock()
	s.ctrAt = time.Time{}
	if resp, _ := do(t, ts, "POST", "/api/saved/remove", body, ""); resp.StatusCode != http.StatusConflict {
		t.Fatalf("remove while running in a terminal: %d", resp.StatusCode)
	}
	st.mu.Lock()
	st.ctrs = nil
	st.mu.Unlock()
	s.ctrAt = time.Time{}

	// Open in a window.
	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": dir, "agent": "claude", "name": "feature-x"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	if resp, _ := do(t, ts, "POST", "/api/saved/remove", body, ""); resp.StatusCode != http.StatusConflict {
		t.Fatalf("remove while open in a window: %d", resp.StatusCode)
	}
	if len(st.saved[key]) != 1 {
		t.Fatal("saved state of a running session removed")
	}

	if resp, _ := do(t, ts, "DELETE", "/api/sessions/"+out["id"].(string), nil, ""); resp.StatusCode != 200 {
		t.Fatalf("close: %d", resp.StatusCode)
	}
	if resp, _ := do(t, ts, "POST", "/api/saved/remove", body, ""); resp.StatusCode != 200 {
		t.Fatalf("remove after the end: %d", resp.StatusCode)
	}
}

// The pid file only serves until the host listens; none is left behind.
func TestPIDFileRemovedAfterStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	// A stand-in pid: Start kills it only if the host never listens.
	stand := exec.Command("sleep", "60")
	DetachProcess(stand)
	if err := stand.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stand.Process.Kill()
		_ = stand.Wait()
	})
	spawn := func(dir, id string) (<-chan struct{}, error) {
		if err := WriteHostPID(dir, id, stand.Process.Pid); err != nil {
			return nil, err
		}
		return inProcessHosts(dir, id)
	}
	m := NewManager(shellCommand(&recorder{}, "sleep 60"), spawn, dir)
	sess, err := m.Start(SessionInfo{Agent: "claude", Dir: t.TempDir()}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	id := sess.Info().ID
	if _, err := os.Stat(pidPath(dir, id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid file kept after the start: %v", err)
	}
	// One left by an older server goes with the session.
	if err := WriteHostPID(dir, id, stand.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(id); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		left, err := filepath.Glob(filepath.Join(dir, "*"))
		if err != nil {
			t.Fatal(err)
		}
		if len(left) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("files left behind: %v", left)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A session that ended while exitbox web was down keeps the time it ended,
// not the time the next server learned of it.
func TestEndedIsTheHostsExitTime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	m1 := NewManager(shellCommand(&recorder{}, "sleep 1"), inProcessHosts, dir)
	sess, err := m1.Start(SessionInfo{Agent: "claude", Dir: t.TempDir()}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	id := sess.Info().ID
	m1.Disconnect()
	time.Sleep(3 * time.Second)
	m2 := NewManager(shellCommand(&recorder{}, "true"), inProcessHosts, dir)
	defer m2.CloseAll()
	restored := time.Now()
	if n, err := m2.Restore(); err != nil || n != 1 {
		t.Fatalf("restored %d, %v", n, err)
	}
	waitDone(t, m2.Get(id))
	info := m2.Get(id).Info()
	if info.Ended == nil || restored.Sub(*info.Ended) < time.Second {
		t.Fatalf("ended %v, restored at %v: not the host's exit time", info.Ended, restored)
	}
}

// A host from an older binary sends no exit time: now stands in. One that
// does is taken at its word.
func TestEndedFromExitFrames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no Unix sockets for hosts on Windows")
	}
	dir := shortTempDir(t)
	m := NewManager(shellCommand(&recorder{}, "true"), inProcessHosts, dir)
	defer m.Disconnect()
	at := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	for _, c := range []struct {
		id     string
		exitAt bool
	}{{"0123456789abcde1", false}, {"0123456789abcde2", true}} {
		_, conns := fakeHost(t, dir, c.id)
		s, hc := connectFake(t, m, c.id, conns)
		if c.exitAt {
			if err := writeFrame(hc, frameExitAt, exitAtPayload(at)); err != nil {
				t.Fatal(err)
			}
		}
		before := time.Now().Add(-time.Second)
		if err := writeFrame(hc, frameExit, exitPayload(3)); err != nil {
			t.Fatal(err)
		}
		waitDone(t, s)
		info := s.Info()
		if info.Ended == nil || info.ExitCode != 3 {
			t.Fatalf("%s: ended %v, code %d", c.id, info.Ended, info.ExitCode)
		}
		if c.exitAt && !info.Ended.Equal(at) {
			t.Fatalf("ended %v, the host said %v", info.Ended, at)
		}
		if !c.exitAt && (info.Ended.Before(before) || info.Ended.After(time.Now())) {
			t.Fatalf("ended %v without a host time, want now", info.Ended)
		}
	}
}

// Empty lists reach the page as [], as its types say, not null.
func TestInfoJSONEmptyLists(t *testing.T) {
	m := NewManager(shellCommand(&recorder{}, "true"), inProcessHosts, shortTempDir(t))
	for _, info := range []SessionInfo{
		{ID: "0123456789abcde1", Kind: KindAttach, Agent: "claude", Container: "exitbox-claude-p-0a1b2c3d"},
		{ID: "0123456789abcde2", Kind: KindRun, Agent: "claude", Request: &RunOptions{Agent: "claude"}},
	} {
		data, err := json.Marshal(m.newSession(info).Info())
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(data, []byte(`"args":[]`)) {
			t.Errorf("%s: no empty args in %s", info.Kind, data)
		}
		if info.Request != nil && !bytes.Contains(data, []byte(`"allowUrls":[]`)) {
			t.Errorf("%s: no empty allowUrls in %s", info.Kind, data)
		}
	}
}
