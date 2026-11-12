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
	"net/http"
	"reflect"
	"runtime"
	"testing"
	"time"
)

const inspectSample = `[
 {"Name": "exitbox-claude-home_u_code_app_0a1b2c3d-1234abcd",
  "Config": {"Env": ["PATH=/usr/bin", "EXITBOX_AGENT=claude", "EXITBOX_SESSION_NAME=feature-x", "EXITBOX_WORKSPACE_NAME=work"]},
  "Mounts": [{"Source": "/home/u/.config/exitbox", "Destination": "/home/user/.config"}, {"Source": "/home/u/code/app", "Destination": "/workspace"}],
  "State": {"Running": true, "StartedAt": "2026-11-12T10:00:00.123456789Z"}},
 {"Name": "/exitbox-codex-home_u_code_api_99999999-deadbeef",
  "Config": {"Env": ["EXITBOX_AGENT=codex", "EXITBOX_SESSION_NAME=", "EXITBOX_NO_TMUX=1"]},
  "Mounts": [{"Source": "/home/u/code/api", "Destination": "/workspace"}],
  "State": {"Running": true, "StartedAt": "2026-11-12T09:00:00Z"}},
 {"Name": "exitbox-squid-abc", "Config": {"Env": []}, "State": {"Running": true}},
 {"Name": "exitbox-claude-x_1-0a1b2c3d", "Config": {"Env": ["EXITBOX_AGENT=claude"]}, "State": {"Running": false}}
]`

func TestParseInspectWebSession(t *testing.T) {
	got, err := parseInspect([]byte(`[{"Name": "exitbox-claude-p-0a1b2c3d", "Config": {"Env": ["EXITBOX_AGENT=claude", "EXITBOX_WEB_SESSION=abc"]}, "State": {"Running": true}}]`))
	if err != nil || len(got) != 1 || got[0].WebSession != "abc" {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestParseInspect(t *testing.T) {
	got, err := parseInspect([]byte(inspectSample))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d containers, want the 2 running agent ones: %+v", len(got), got)
	}
	// Sorted by start time; docker's leading slash dropped.
	if got[0].Container != "exitbox-codex-home_u_code_api_99999999-deadbeef" || !got[0].NoTmux || got[0].Dir != "/home/u/code/api" {
		t.Fatalf("first: %+v", got[0])
	}
	want := containerDoc{
		Container: "exitbox-claude-home_u_code_app_0a1b2c3d-1234abcd", Agent: "claude", Dir: "/home/u/code/app",
		Workspace: "work", Session: "feature-x", Started: time.Date(2026, 11, 12, 10, 0, 0, 123456789, time.UTC),
	}
	if !reflect.DeepEqual(got[1], want) {
		t.Fatalf("second\n got %+v\nwant %+v", got[1], want)
	}
}

func TestMatchContainers(t *testing.T) {
	t0 := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	ctrs := []containerDoc{
		// A terminal session in the same folder that started later.
		{Container: "c-term-later", Agent: "claude", Dir: "/p", Session: "x", Started: t0.Add(2 * time.Second)},
		// The web session's container, relaunched under another name.
		{Container: "c-web", Agent: "claude", Dir: "/p", Session: "switched", Started: t0.Add(5 * time.Second), WebSession: "web1"},
		{Container: "c-attached", Agent: "codex", Dir: "/q", Started: t0},
	}
	sessions := []SessionInfo{
		{ID: "web1", Kind: KindRun, Agent: "claude", Dir: "/p", Name: "first", Container: "c-old", Started: t0},
		{ID: "web2", Kind: KindRun, Agent: "claude", Dir: "/p", Started: t0},
		{ID: "attach", Kind: KindAttach, Agent: "codex", Dir: "/q", Container: "c-attached", Started: t0},
		{ID: "ended", Kind: KindRun, Agent: "claude", Dir: "/p", Started: t0, Exited: true},
	}
	got := matchContainers(sessions, ctrs)
	want := map[string]int{"web1": 1, "attach": 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches %v, want %v: pairing goes by session id, never by folder and start time", got, want)
	}
}

func TestStateListsTerminalSessionsAndAttach(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	rec := &recorder{}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(rec, "sleep 60")})
	st.ctrs = []containerDoc{
		{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: st.dirs[0], Workspace: "work", Session: "from-terminal", Started: time.Now().Add(-time.Hour)},
		{Container: "exitbox-codex-p-0a1b2c3e", Agent: "codex", Dir: st.dirs[0], Workspace: "work", NoTmux: true, Started: time.Now().Add(-time.Hour)},
	}
	_, out := do(t, ts, "GET", "/api/state", nil, "")
	if n := len(out["running"].([]any)); n != 2 {
		t.Fatalf("running: %v", out["running"])
	}

	resp, out := do(t, ts, "POST", "/api/attach", map[string]any{"container": "exitbox-codex-p-0a1b2c3e"}, "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("attach to a --no-tmux session: %d %v", resp.StatusCode, out)
	}
	resp, out = do(t, ts, "POST", "/api/attach", map[string]any{"container": "exitbox-claude-p-0a1b2c3d", "cols": 80, "rows": 24}, "")
	if resp.StatusCode != http.StatusCreated || out["kind"] != KindAttach || out["name"] != "from-terminal" {
		t.Fatalf("attach: %d %v", resp.StatusCode, out)
	}
	if args, _ := rec.last(); !reflect.DeepEqual(args, []string{"attach", "exitbox-claude-p-0a1b2c3d"}) {
		t.Fatalf("attach ran %q", args)
	}
	id := out["id"].(string)

	// The window owns the container now; it leaves the terminal list.
	s.ctrAt = time.Time{}
	_, out = do(t, ts, "GET", "/api/state", nil, "")
	if n := len(out["running"].([]any)); n != 1 {
		t.Fatalf("running after attach: %v", out["running"])
	}
	resp, _ = do(t, ts, "POST", "/api/attach", map[string]any{"container": "exitbox-claude-p-0a1b2c3d"}, "")
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("second attach to the same session: %d", resp.StatusCode)
	}
	resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/rename", map[string]string{"name": "x"}, "")
	if resp.StatusCode != 200 || out["renameTo"] != "x" {
		t.Fatalf("rename of an attached terminal session: %d %v", resp.StatusCode, out)
	}
}

func TestRenameSessions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, `IFS= read -r line; exit 0`)})
	dir := st.dirs[0]
	key := "work/claude/" + dir
	st.saved[key] = []string{"feature-x", "taken"}

	// Saved session.
	resp, out := do(t, ts, "POST", "/api/saved/rename", map[string]string{"workspace": "work", "agent": "claude", "dir": dir, "name": "feature-x", "newName": "taken"}, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("rename onto an existing name: %d %v", resp.StatusCode, out)
	}
	resp, out = do(t, ts, "POST", "/api/saved/rename", map[string]string{"workspace": "work", "agent": "claude", "dir": dir, "name": "feature-x", "newName": "feature-y"}, "")
	if resp.StatusCode != 200 || st.saved[key][0] != "feature-y" {
		t.Fatalf("saved rename: %d %v %v", resp.StatusCode, out, st.saved[key])
	}

	// A running window takes the new name when it ends.
	resp, out = do(t, ts, "POST", "/api/sessions", map[string]any{"dir": dir, "agent": "claude", "name": "feature-y"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	resp, _ = do(t, ts, "POST", "/api/saved/rename", map[string]string{"workspace": "work", "agent": "claude", "dir": dir, "name": "feature-y", "newName": "z"}, "")
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("saved rename of an open session: %d", resp.StatusCode)
	}
	resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/rename", map[string]string{"name": "invoice export"}, "")
	if resp.StatusCode != 200 || out["renameTo"] != "invoice export" || out["name"] != "feature-y" {
		t.Fatalf("rename running: %d %v", resp.StatusCode, out)
	}
	sess := s.sessions.Get(id)
	if err := sess.Write([]byte("bye\r")); err != nil {
		t.Fatal(err)
	}
	<-sess.Done()
	deadline := time.Now().Add(5 * time.Second)
	for sess.Info().Name != "invoice export" && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	info := sess.Info()
	if info.Name != "invoice export" || info.RenameTo != "" || !reflect.DeepEqual(info.Args, []string{"run", "claude", "--name", "invoice export"}) {
		t.Fatalf("after exit: %+v", info)
	}
	if st.saved[key][0] != "invoice export" {
		t.Fatalf("saved names %v", st.saved[key])
	}

	// An ended window renames at once.
	resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/rename", map[string]string{"name": "final"}, "")
	if resp.StatusCode != 200 || out["name"] != "final" {
		t.Fatalf("rename ended: %d %v", resp.StatusCode, out)
	}
	resp, _ = do(t, ts, "POST", "/api/sessions/"+id+"/rename", map[string]string{"name": "--no-firewall"}, "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("flag as name: %d", resp.StatusCode)
	}
}

func TestTerminateStopsTheContainer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "sleep 60")})
	st.ctrs = []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: st.dirs[0], Workspace: "work", Session: "from-terminal", Started: time.Now()}}

	// Detach (close) leaves the terminal session running.
	resp, out := do(t, ts, "POST", "/api/attach", map[string]any{"container": "exitbox-claude-p-0a1b2c3d"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("attach: %d %v", resp.StatusCode, out)
	}
	if resp, _ := do(t, ts, "DELETE", "/api/sessions/"+out["id"].(string), nil, ""); resp.StatusCode != 200 || len(st.stopped) != 0 {
		t.Fatalf("detach: %d, stopped %v", resp.StatusCode, st.stopped)
	}

	// Terminate stops the container.
	s.ctrAt = time.Time{}
	resp, out = do(t, ts, "POST", "/api/attach", map[string]any{"container": "exitbox-claude-p-0a1b2c3d"}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("attach again: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	if resp, out := do(t, ts, "POST", "/api/sessions/"+id+"/terminate", map[string]any{}, ""); resp.StatusCode != 200 {
		t.Fatalf("terminate: %d %v", resp.StatusCode, out)
	}
	if !reflect.DeepEqual(st.stopped, []string{"exitbox-claude-p-0a1b2c3d"}) || s.sessions.Get(id) != nil {
		t.Fatalf("stopped %v, session kept: %v", st.stopped, s.sessions.Get(id) != nil)
	}
}

func TestRenameTerminalSessionWhenItEnds(t *testing.T) {
	s, st, ts := newTestServer(t, Options{})
	dir := st.dirs[0]
	key := "work/claude/" + dir
	st.saved[key] = []string{"from-terminal"}
	st.ctrs = []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: dir, Workspace: "work", Session: "from-terminal", Started: time.Now()}}
	if resp, _ := do(t, ts, "PUT", "/api/folders", map[string]any{"folders": []map[string]any{{"id": "f1", "name": "F", "items": []string{sessionKey("work", "claude", dir, "from-terminal")}}}}, ""); resp.StatusCode != 200 {
		t.Fatal("folders")
	}

	resp, out := do(t, ts, "POST", "/api/running/rename", map[string]string{"container": "exitbox-claude-p-0a1b2c3d", "name": "renamed"}, "")
	if resp.StatusCode != 200 {
		t.Fatalf("rename running: %d %v", resp.StatusCode, out)
	}
	s.ctrAt = time.Time{}
	_, state := do(t, ts, "GET", "/api/state", nil, "")
	if r := state["running"].([]any)[0].(map[string]any); r["renameTo"] != "renamed" {
		t.Fatalf("running row %v does not show the queued name", r)
	}
	if len(st.renames) != 0 {
		t.Fatal("renamed while the session still runs")
	}

	// The session ends in its terminal: the next poll renames it.
	st.mu.Lock()
	st.ctrs = nil
	st.mu.Unlock()
	s.ctrAt = time.Time{}
	do(t, ts, "GET", "/api/state", nil, "")
	if !reflect.DeepEqual(st.renames, []string{"from-terminal->renamed"}) || st.saved[key][0] != "renamed" {
		t.Fatalf("renames %v, saved %v", st.renames, st.saved[key])
	}
	if len(s.renames.list()) != 0 {
		t.Fatal("the queued rename stayed queued")
	}
	_, state = do(t, ts, "GET", "/api/state", nil, "")
	items := state["folders"].([]any)[0].(map[string]any)["items"].([]any)
	if items[0] != sessionKey("work", "claude", dir, "renamed") {
		t.Fatalf("folder item %v did not follow the rename", items[0])
	}
}
