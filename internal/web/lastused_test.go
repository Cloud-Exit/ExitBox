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
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// sessionDir writes a saved session dir with .name and .resume-token, all
// at mtime base, and returns it.
func sessionDir(t *testing.T, name string, base time.Time) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sessions", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{".name", ".resume-token"} {
		p := filepath.Join(dir, f)
		if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, base, base); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(dir, base, base); err != nil {
		t.Fatal(err)
	}
	return dir
}

func touch(t *testing.T, p string, at time.Time) {
	t.Helper()
	if err := os.Chtimes(p, at, at); err != nil {
		t.Fatal(err)
	}
}

func savedByName(t *testing.T, out map[string]any) map[string]map[string]any {
	t.Helper()
	byName := map[string]map[string]any{}
	for _, v := range out["saved"].([]any) {
		d := v.(map[string]any)
		byName[d["name"].(string)] = d
	}
	return byName
}

func TestStateSavedLastUsed(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	dir := sessionDir(t, "feature-x", base)
	newest := base.Add(3 * time.Hour)
	touch(t, filepath.Join(dir, ".resume-token"), newest)
	// An older copy under another dir with the same name.
	old := sessionDir(t, "feature-x", base.Add(-time.Hour))
	k := "work/claude/" + st.dirs[0]
	st.saved[k] = []string{"feature-x", "gone"}
	st.savedDirs = map[string][]string{
		k + "/feature-x": {old, dir},
		k + "/gone":      {filepath.Join(t.TempDir(), "missing")},
	}

	_, out := do(t, ts, "GET", "/api/state", nil, "")
	byName := savedByName(t, out)
	if got := byName["feature-x"]["lastUsed"]; got != newest.Format(time.RFC3339) {
		t.Fatalf("lastUsed = %v, want %s", got, newest.Format(time.RFC3339))
	}
	if _, ok := byName["gone"]["lastUsed"]; ok {
		t.Fatalf("lastUsed for a missing dir: %v", byName["gone"])
	}
}

func TestLastUsedCacheInvalidate(t *testing.T) {
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	dir := sessionDir(t, "a", base)
	now := base.Add(24 * time.Hour)
	c := newLastUsedCache()
	c.now = func() time.Time { return now }

	if got := c.get([]string{dir}); !got.Equal(base) {
		t.Fatalf("first get = %v, want %v", got, base)
	}
	later := base.Add(time.Hour)
	touch(t, filepath.Join(dir, ".name"), later)
	if got := c.get([]string{dir}); !got.Equal(base) {
		t.Fatalf("cached get = %v, want %v", got, base)
	}
	c.invalidate()
	if got := c.get([]string{dir}); !got.Equal(later) {
		t.Fatalf("after invalidate = %v, want %v", got, later)
	}
	evenLater := base.Add(2 * time.Hour)
	touch(t, filepath.Join(dir, ".resume-token"), evenLater)
	now = now.Add(lastUsedTTL)
	if got := c.get([]string{dir}); !got.Equal(evenLater) {
		t.Fatalf("after expiry = %v, want %v", got, evenLater)
	}
	if got := c.get([]string{filepath.Join(dir, "missing")}); !got.IsZero() {
		t.Fatalf("missing dir = %v", got)
	}
}

// A web session's exit drops the cache, so the next poll sees fresh files.
func TestLastUsedFreshAfterSessionExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{})
	base := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	dir := sessionDir(t, "feature-x", base)
	k := "work/claude/" + st.dirs[0]
	st.savedDirs = map[string][]string{k + "/feature-x": {dir}}
	if _, out := do(t, ts, "GET", "/api/state", nil, ""); savedByName(t, out)["feature-x"]["lastUsed"] != base.Format(time.RFC3339) {
		t.Fatalf("first poll: %v", out["saved"])
	}

	resp, out := do(t, ts, "POST", "/api/sessions", map[string]any{"dir": st.dirs[0], "agent": "claude"}, "")
	if resp.StatusCode != 201 {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	sess := s.sessions.Get(out["id"].(string))
	// The entrypoint rewrites the session's files as it exits.
	later := base.Add(time.Hour)
	touch(t, filepath.Join(dir, ".resume-token"), later)
	select {
	case <-sess.settled:
	case <-time.After(5 * time.Second):
		t.Fatal("session did not end")
	}
	if _, out := do(t, ts, "GET", "/api/state", nil, ""); savedByName(t, out)["feature-x"]["lastUsed"] != later.Format(time.RFC3339) {
		t.Fatalf("after exit: %v", out["saved"])
	}
}

func TestEndedSetOnExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	dir := shortTempDir(t)
	m := NewManager(shellCommand(&recorder{}, "sleep 60"), inProcessHosts, dir)
	defer m.CloseAll()
	sess, err := m.Start(SessionInfo{Kind: KindRun, Agent: "claude", Dir: t.TempDir()}, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(sess.Info())
	if err != nil {
		t.Fatal(err)
	}
	var running map[string]any
	if err := json.Unmarshal(data, &running); err != nil {
		t.Fatal(err)
	}
	if _, ok := running["ended"]; ok {
		t.Fatalf("ended set while running: %s", data)
	}

	before := time.Now().Add(-time.Second)
	if err := sess.stop(); err != nil {
		t.Fatal(err)
	}
	info := sess.Info()
	if info.Ended == nil || info.Ended.Before(before) || info.Ended.After(time.Now()) {
		t.Fatalf("ended = %v", info.Ended)
	}
	raw, err := os.ReadFile(infoPath(dir, info.ID))
	if err != nil {
		t.Fatal(err)
	}
	var saved SessionInfo
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Ended == nil || !saved.Ended.Equal(*info.Ended) {
		t.Fatalf("saved ended = %v, want %v", saved.Ended, info.Ended)
	}
}
