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
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloud-exit/exitbox/internal/webprompt"
	"golang.org/x/net/websocket"
)

// safeBuf is a log sink for concurrent writers.
type safeBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// testBroker listens on a short socket path; known sessions are a and b.
func testBroker(t *testing.T) (*promptBroker, string, *safeBuf) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("prompts stay in the terminal on Windows")
	}
	logs := &safeBuf{}
	lg := log.New(logs, "", 0)
	b := newPromptBroker(func(id string) bool { return id == "a" || id == "b" }, lg.Printf)
	path := filepath.Join(shortTempDir(t), "prompts.sock")
	if err := b.listen(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.close)
	return b, path, logs
}

type requester struct {
	conn  net.Conn
	reply chan webprompt.Reply
	ended chan struct{}
}

// ask sends one prompt as exitbox run would and collects the reply.
func ask(t *testing.T, path string, req webprompt.Request) *requester {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	r := &requester{conn: conn, reply: make(chan webprompt.Reply, 1), ended: make(chan struct{})}
	go func() {
		defer close(r.ended)
		sc := bufio.NewScanner(conn)
		if !sc.Scan() {
			return
		}
		var rep webprompt.Reply
		if err := json.Unmarshal(sc.Bytes(), &rep); err == nil {
			r.reply <- rep
		}
	}()
	return r
}

func (r *requester) wait(t *testing.T) webprompt.Reply {
	t.Helper()
	select {
	case rep := <-r.reply:
		return rep
	case <-time.After(5 * time.Second):
		t.Fatal("no reply")
	}
	return webprompt.Reply{}
}

type promptMsg struct {
	T       string `json:"t"`
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Agent   string `json:"agent"`
	Subject string `json:"subject"`
}

func next(t *testing.T, v *promptViewer) promptMsg {
	t.Helper()
	select {
	case raw := <-v.out:
		var m promptMsg
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatal(err)
		}
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("no prompt message")
	}
	return promptMsg{}
}

func quiet(t *testing.T, v *promptViewer) {
	t.Helper()
	select {
	case m := <-v.out:
		t.Fatalf("unexpected message %s", m)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPromptSocketIsPrivate(t *testing.T) {
	_, path, _ := testBroker(t)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("socket mode %v", st.Mode().Perm())
	}
}

func TestPromptNoViewerRepliesAtOnce(t *testing.T) {
	_, path, _ := testBroker(t)
	r := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindAllow, Agent: "codex", Subject: "example.com"})
	if rep := r.wait(t); rep.Status != webprompt.StatusNoViewer {
		t.Fatalf("reply %+v", rep)
	}
}

func TestPromptRejectsBadRequests(t *testing.T) {
	_, path, _ := testBroker(t)
	for name, req := range map[string]webprompt.Request{
		"unknown session": {Session: "zzz", Kind: webprompt.KindAllow, Subject: "x.com"},
		"unknown kind":    {Session: "a", Kind: "shell", Subject: "x.com"},
		"no subject":      {Session: "a", Kind: webprompt.KindVaultRead},
		"long subject":    {Session: "a", Kind: webprompt.KindAllow, Subject: strings.Repeat("a", webprompt.MaxSubject+1)},
		"control chars":   {Session: "a", Kind: webprompt.KindAllow, Subject: "x.com\x1b[2J"},
	} {
		r := ask(t, path, req)
		if rep := r.wait(t); rep.Status != webprompt.StatusInvalid {
			t.Fatalf("%s: reply %+v", name, rep)
		}
	}
}

func TestPromptRoutesToItsSessionAndFirstAnswerWins(t *testing.T) {
	b, path, _ := testBroker(t)
	va1, va2, vb := b.watch("a"), b.watch("a"), b.watch("b")
	r := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindAllow, Agent: "codex", Subject: "registry.npmjs.org:443"})
	m1, m2 := next(t, va1), next(t, va2)
	if m1.T != "prompt" || m1.ID == "" || m1.ID != m2.ID || m1.Kind != webprompt.KindAllow || m1.Agent != "codex" || m1.Subject != "registry.npmjs.org:443" {
		t.Fatalf("prompt messages %+v %+v", m1, m2)
	}
	quiet(t, vb)
	// Another session's viewer cannot answer it.
	if b.answer("b", m1.ID, true, "") {
		t.Fatal("answer from another session taken")
	}
	if !b.answer("a", m1.ID, true, "") {
		t.Fatal("first answer refused")
	}
	if b.answer("a", m1.ID, false, "") {
		t.Fatal("second answer taken")
	}
	if rep := r.wait(t); rep.Status != webprompt.StatusAnswer || !rep.Allow {
		t.Fatalf("reply %+v", rep)
	}
	for _, v := range []*promptViewer{va1, va2} {
		if d := next(t, v); d.T != "prompt-done" || d.ID != m1.ID {
			t.Fatalf("done %+v", d)
		}
	}
	if b.answer("a", "nope", true, "") {
		t.Fatal("answer for an unknown id taken")
	}
}

func TestPromptDeny(t *testing.T) {
	b, path, _ := testBroker(t)
	v := b.watch("a")
	r := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindVaultWrite, Subject: "API_KEY"})
	m := next(t, v)
	if !b.answer("a", m.ID, false, "ignored") {
		t.Fatal("deny refused")
	}
	if rep := r.wait(t); rep.Status != webprompt.StatusAnswer || rep.Allow || rep.Password != "" {
		t.Fatalf("reply %+v", rep)
	}
}

func TestPromptCancelledWhenRequesterHangsUp(t *testing.T) {
	b, path, _ := testBroker(t)
	v := b.watch("a")
	r := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindVaultList})
	m := next(t, v)
	if err := r.conn.Close(); err != nil {
		t.Fatal(err)
	}
	if d := next(t, v); d.T != "prompt-done" || d.ID != m.ID {
		t.Fatalf("done %+v", d)
	}
	if b.answer("a", m.ID, true, "") {
		t.Fatal("answer to a cancelled prompt taken")
	}
}

func TestPromptResentToLateViewer(t *testing.T) {
	b, path, _ := testBroker(t)
	v1 := b.watch("a")
	r1 := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindAllow, Subject: "one.example"})
	first := next(t, v1)
	r2 := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindVaultRead, Subject: "TOKEN"})
	second := next(t, v1)
	// The only viewer leaves (a reload); the prompts wait for the next one.
	b.unwatch("a", v1)
	v2 := b.watch("a")
	if m := next(t, v2); m.ID != first.ID {
		t.Fatalf("first resent %+v", m)
	}
	if m := next(t, v2); m.ID != second.ID || m.Subject != "TOKEN" {
		t.Fatalf("second resent %+v", m)
	}
	if !b.answer("a", second.ID, true, "") || !b.answer("a", first.ID, false, "") {
		t.Fatal("answers refused")
	}
	if rep := r1.wait(t); rep.Allow {
		t.Fatalf("first %+v", rep)
	}
	if rep := r2.wait(t); !rep.Allow {
		t.Fatalf("second %+v", rep)
	}
}

func TestPromptPasswordStaysOutOfLogs(t *testing.T) {
	b, path, logs := testBroker(t)
	const secret = "hunter2-very-secret"
	v := b.watch("a")
	r := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindVaultPassword, Agent: "claude", Workspace: "work"})
	m := next(t, v)
	// Unlock without a password is not an answer.
	if b.answer("a", m.ID, true, "") {
		t.Fatal("empty password taken")
	}
	if !b.answer("a", m.ID, true, secret) {
		t.Fatal("password refused")
	}
	if rep := r.wait(t); !rep.Allow || rep.Password != secret {
		t.Fatalf("reply allow=%v", rep.Allow)
	}
	if d := next(t, v); d.T != "prompt-done" || d.ID != m.ID {
		t.Fatalf("done %+v", d)
	}
	// A password never rides along on other kinds.
	r2 := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindVaultRead, Subject: "K"})
	id := next(t, v).ID
	if !b.answer("a", id, true, secret) {
		t.Fatal("read approval refused")
	}
	if rep := r2.wait(t); rep.Password != "" {
		t.Fatal("password sent with a read approval")
	}
	b.close()
	if strings.Contains(logs.String(), secret) {
		t.Fatal("password in the log")
	}
}

func TestPromptCloseCancelsWaiting(t *testing.T) {
	b, path, _ := testBroker(t)
	v := b.watch("a")
	r := ask(t, path, webprompt.Request{Session: "a", Kind: webprompt.KindAllow, Subject: "x.example"})
	next(t, v)
	b.close()
	select {
	case <-r.ended:
	case <-time.After(5 * time.Second):
		t.Fatal("requester still waiting after close")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("socket left behind: %v", err)
	}
}

func TestWithPromptEnv(t *testing.T) {
	base := []string{"PATH=/bin", webprompt.EnvSocket + "=/old", webprompt.EnvSession + "=old"}
	got := withPromptEnv(base, KindRun, "/s/prompts.sock", "id1")
	want := []string{"PATH=/bin", webprompt.EnvSocket + "=/s/prompts.sock", webprompt.EnvSession + "=id1"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("run env %q", got)
	}
	if got := withPromptEnv(base, KindAttach, "/s/prompts.sock", "id1"); fmt.Sprint(got) != "[PATH=/bin]" {
		t.Fatalf("attach env %q", got)
	}
	if got := withPromptEnv(base, KindRun, "", "id1"); fmt.Sprint(got) != "[PATH=/bin]" {
		t.Fatalf("no socket env %q", got)
	}
}

// TestPromptOverWebSocket: a browser viewing the session gets the prompt and
// its answer reaches exitbox run.
func TestPromptOverWebSocket(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "echo $"+webprompt.EnvSession+"; sleep 30")})
	if err := s.startPrompts(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.stopPrompts)

	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "codex", "cols": 100, "rows": 30}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	ws, err := dialSession(t, ts, id, ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Close() }()
	// The session's exitbox run sees its id in the environment.
	readUntil(t, ws, id)

	path := filepath.Join(s.opts.StateDir, "prompts.sock")
	// The viewer registers right after the live marker; wait for it.
	deadline := time.Now().Add(5 * time.Second)
	for {
		s.prompts.mu.Lock()
		n := len(s.prompts.viewers[id])
		s.prompts.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("viewer never registered")
		}
		time.Sleep(10 * time.Millisecond)
	}
	r := ask(t, path, webprompt.Request{Session: id, Kind: webprompt.KindAllow, Agent: "codex", Subject: "registry.npmjs.org:443"})
	_, msg := readUntil(t, ws, "")
	if msg["t"] != "prompt" || msg["subject"] != "registry.npmjs.org:443" || msg["kind"] != "allow" {
		t.Fatalf("prompt message %v", msg)
	}
	answer, err := json.Marshal(map[string]any{"t": "answer", "id": msg["id"], "allow": true})
	if err != nil {
		t.Fatal(err)
	}
	if err := websocket.Message.Send(ws, string(answer)); err != nil {
		t.Fatal(err)
	}
	if rep := r.wait(t); rep.Status != webprompt.StatusAnswer || !rep.Allow {
		t.Fatalf("reply %+v", rep)
	}
	_, done := readUntil(t, ws, "")
	if done["t"] != "prompt-done" || done["id"] != msg["id"] {
		t.Fatalf("done %v", done)
	}
}
