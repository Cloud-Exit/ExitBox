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
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/websocket"
)

// What a tmux client asks its terminal when it starts, and what xterm.js
// answers.
const (
	tmuxQueries  = "\x1b[c\x1b[>c\x1b[>q\x1b[?2026$p"
	xtermAnswers = "\x1b[?1;2c\x1b[>0;276;0c\x1bP>|xterm.js(6.0.0)\x1b\\\x1b[?2026;2$y"
)

func TestQueryTrackerOneAnswerPerQuery(t *testing.T) {
	var q queryTracker
	q.sawOutput([]byte("hello" + tmuxQueries + "world"))
	if got := string(q.filterInput([]byte(xtermAnswers))); got != xtermAnswers {
		t.Fatalf("first answers %q, want them all", got)
	}
	// A second browser answers the same queries: nothing gets through.
	if got := string(q.filterInput([]byte(xtermAnswers))); got != "" {
		t.Fatalf("second answers %q, want none", got)
	}
}

func TestQueryTrackerKeepsKeys(t *testing.T) {
	var q queryTracker
	cases := []string{
		"plain text\r",
		"\x1b[A\x1b[B",               // arrows
		"\x1b[1;2R",                  // shift+F3 looks like a cursor report
		"\x1b[200~paste\x1b[201~",    // bracketed paste
		"\x1b",                       // a lone Escape
		"\x1b[",                      // Alt+[
		"\x1bOP",                     // F1
		"a\x1b[?1;2cb",               // an unasked answer between keys
		"\x1b]11;rgb:0000/0000/0000", // unfinished OSC
	}
	want := []string{"plain text\r", "\x1b[A\x1b[B", "\x1b[1;2R", "\x1b[200~paste\x1b[201~", "\x1b", "\x1b[", "\x1bOP", "ab", "\x1b]11;rgb:0000/0000/0000"}
	for i, in := range cases {
		if got := string(q.filterInput([]byte(in))); got != want[i] {
			t.Errorf("input %q: got %q, want %q", in, got, want[i])
		}
	}
}

func TestQueryTrackerSplitQueryAndModes(t *testing.T) {
	var q queryTracker
	// A query split across two output chunks still counts.
	q.sawOutput([]byte("x\x1b[?20"))
	q.sawOutput([]byte("26$py"))
	if got := string(q.filterInput([]byte("\x1b[?2004;1$y"))); got != "" {
		t.Fatalf("answer for another mode passed: %q", got)
	}
	if got := string(q.filterInput([]byte("\x1b[?2026;2$y"))); got != "\x1b[?2026;2$y" {
		t.Fatalf("answer for the asked mode dropped: %q", got)
	}
	// OSC colour queries, ended by BEL or ST.
	q.sawOutput([]byte("\x1b]11;?\x07"))
	if got := string(q.filterInput([]byte("\x1b]11;rgb:1111/2222/3333\x1b\\"))); got == "" {
		t.Fatal("OSC 11 answer dropped")
	}
	if got := string(q.filterInput([]byte("\x1b]11;rgb:1111/2222/3333\x07"))); got != "" {
		t.Fatalf("second OSC 11 answer passed: %q", got)
	}
}

func TestQueryTrackerExpires(t *testing.T) {
	now := time.Now()
	q := queryTracker{now: func() time.Time { return now }}
	q.sawOutput([]byte("\x1b[c"))
	now = now.Add(answerWindow + time.Second)
	if got := string(q.filterInput([]byte("\x1b[?1;2c"))); got != "" {
		t.Fatalf("answer after the window passed: %q", got)
	}
}

// Two browsers on one session both answer tmux's start-up queries; the
// program must see one set of answers, not text in its prompt.
func TestTwoViewersAnswerOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	// Ask like tmux does, then echo every byte read, made visible.
	script := `stty raw -echo; printf 'ready\033[c\033[>c\033[>q\033[?2026$p\n'; while :; do c=$(dd bs=1 count=1 2>/dev/null | od -An -c | tr -d ' '); printf '[%s]' "$c"; done`
	_, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, script)})
	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude"}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	var conns []*websocket.Conn
	for range 2 {
		ws, err := dialSession(t, ts, id, ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ws.Close() }()
		conns = append(conns, ws)
	}
	// Wait until the queries went out (live for both or in the replay).
	for _, ws := range conns {
		readUntil(t, ws, "ready")
	}
	for _, ws := range conns {
		if err := websocket.Message.Send(ws, `{"t":"i","d":"`+jsonEscape(xtermAnswers)+`"}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := websocket.Message.Send(conns[0], `{"t":"i","d":"Z"}`); err != nil {
		t.Fatal(err)
	}
	got, _ := readUntil(t, conns[0], "[Z]")
	if n := strings.Count(got, "[c]"); n != 2 {
		t.Fatalf("the program read %d 'c' bytes (two per answer set), want 2: %q", n, got)
	}
}

func jsonEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteString(`\` + string(r))
		case r < 0x20:
			b.WriteString(`\u00`)
			b.WriteByte("0123456789abcdef"[r>>4])
			b.WriteByte("0123456789abcdef"[r&15])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// After a restart of exitbox web the host replays old output, queries
// included. Browsers answer those again; the program must not read them.
func TestReplayedQueriesAfterServerRestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	script := `stty raw -echo; printf 'ready\033[c\n'; while :; do c=$(dd bs=1 count=1 2>/dev/null | od -An -c | tr -d ' '); printf '[%s]' "$c"; done`
	s1, st, ts1 := newTestServer(t, Options{Command: shellCommand(&recorder{}, script), StateDir: dir})
	resp, out := do(t, ts1, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude"}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	ws, err := dialSession(t, ts1, id, ts1.URL)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, "ready")
	if err := websocket.Message.Send(ws, `{"t":"i","d":"\u001b[?1;2c"}`); err != nil {
		t.Fatal(err)
	}
	readUntil(t, ws, "[c]")
	_ = ws.Close()
	s1.sessions.Disconnect()
	ts1.Close()

	s2, _, ts2 := newTestServer(t, Options{Command: shellCommand(&recorder{}, script), StateDir: dir})
	if n, err := s2.Restore(); err != nil || n != 1 {
		t.Fatalf("restored %d, %v", n, err)
	}
	ws2, err := dialSession(t, ts2, id, ts2.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws2.Close() }()
	readUntil(t, ws2, "[c]") // the replay came along
	if err := websocket.Message.Send(ws2, `{"t":"i","d":"\u001b[?1;2cZ"}`); err != nil {
		t.Fatal(err)
	}
	if got, _ := readUntil(t, ws2, "[Z]"); strings.Contains(got, "[c]") {
		t.Fatalf("the program read an answer to a replayed query: %q", got)
	}
}
