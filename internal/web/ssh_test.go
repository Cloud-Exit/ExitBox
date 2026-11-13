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
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cloud-exit/exitbox/internal/webprompt"
)

func TestSSHOptionsCleanedAndArgs(t *testing.T) {
	home := t.TempDir()
	key := filepath.Join(home, ".ssh", "id_test")
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("not a real key"), 0o600); err != nil {
		t.Fatal(err)
	}
	ok := []struct {
		in   SSHOptions
		name string
		args []string
	}{
		{SSHOptions{Host: " box "}, "box", []string{"-t", "--", "box"}},
		{SSHOptions{Host: "box.example.com", User: "me", Port: 2222, Identity: "~/.ssh/id_test"}, "me@box.example.com",
			[]string{"-t", "-p", "2222", "-l", "me", "-i", key, "--", "box.example.com"}},
		{SSHOptions{Host: "10.0.0.5", User: "deploy_user.1", Name: " prod "}, "prod", []string{"-t", "-l", "deploy_user.1", "--", "10.0.0.5"}},
		{SSHOptions{Host: "[::1]", Port: 22, Identity: key}, "[::1]", []string{"-t", "-p", "22", "-i", key, "--", "[::1]"}},
		{SSHOptions{Host: "fe80::1%eth0"}, "fe80::1%eth0", []string{"-t", "--", "fe80::1%eth0"}},
		{SSHOptions{Host: "my_alias"}, "my_alias", []string{"-t", "--", "my_alias"}},
	}
	for _, c := range ok {
		got, err := c.in.cleaned(home)
		if err != nil {
			t.Fatalf("%+v: %v", c.in, err)
		}
		if got.Name != c.name {
			t.Errorf("%+v: name %q, want %q", c.in, got.Name, c.name)
		}
		if args := sshArgs(got); !reflect.DeepEqual(args, c.args) {
			t.Errorf("%+v: args %q, want %q", c.in, args, c.args)
		}
	}

	bad := []SSHOptions{
		{},
		{Host: "-oProxyCommand=x"},
		{Host: "box", User: "-oProxyCommand=x"},
		{Host: "-p"},
		{Host: "box", User: "me@other"},
		{Host: "me@box"},
		{Host: "box name"},
		{Host: "box;id"},
		{Host: "box$(id)"},
		{Host: "box\nother"},
		{Host: "box", User: "a b"},
		{Host: "box", Port: -1},
		{Host: "box", Port: 65536},
		{Host: "box", Identity: "~/.ssh/missing"},
		{Host: "box", Identity: "relative/key"},
		{Host: "box", Identity: filepath.Join(home, ".ssh")},
		{Host: strings.Repeat("a", 256)},
		{Host: "box", Name: "-x"},
	}
	for _, o := range bad {
		if got, err := o.cleaned(home); err == nil {
			t.Errorf("%+v accepted as %+v", o, got)
		}
	}
}

func writeSSHFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSSHConfigHosts(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	writeSSHFile(t, filepath.Join(ssh, "config"), `# global
User globaluser
Include conf.d/*.conf
Include missing/*.conf

Host web db
    HostName %h.internal.example.com
    Port 2200

Host "quoted"
  hostname=10.1.2.3

Host *.wild ?ingle !neg
    User wild

Host -oProxyCommand=x
    HostName evil

Match host web
    User matched

Host *
    Port 22
    User fallback
`)
	writeSSHFile(t, filepath.Join(ssh, "conf.d", "a.conf"), `Host alpha
  HostName alpha.example.com
  User ann
Include nested.inc
`)
	writeSSHFile(t, filepath.Join(ssh, "nested.inc"), `Host nested
  Port 2022
`)
	writeSSHFile(t, filepath.Join(ssh, "conf.d", "b.conf"), "Host web\n  User late\n")
	// A directory matched by an Include glob is skipped.
	if err := os.MkdirAll(filepath.Join(ssh, "conf.d", "dir.conf"), 0o700); err != nil {
		t.Fatal(err)
	}

	got := sshConfigHosts(home)
	want := []sshHostDoc{
		{Alias: "alpha", HostName: "alpha.example.com", User: "globaluser", Port: 22},
		{Alias: "nested", User: "globaluser", Port: 2022},
		{Alias: "web", HostName: "web.internal.example.com", User: "globaluser", Port: 2200},
		{Alias: "db", HostName: "db.internal.example.com", User: "globaluser", Port: 2200},
		{Alias: "quoted", HostName: "10.1.2.3", User: "globaluser", Port: 22},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts\n got %+v\nwant %+v", got, want)
	}
}

func TestSSHConfigHostsMissingAndLoops(t *testing.T) {
	if got := sshConfigHosts(t.TempDir()); len(got) != 0 {
		t.Fatalf("no config: %+v", got)
	}
	home := t.TempDir()
	// Includes itself: the depth cap ends it.
	writeSSHFile(t, filepath.Join(home, ".ssh", "config"), "Include config\nHost loop\n")
	got := sshConfigHosts(home)
	if len(got) != 1 || got[0].Alias != "loop" {
		t.Fatalf("hosts %+v", got)
	}
}

func TestSSHConfigLine(t *testing.T) {
	cases := map[string][]string{
		"Host a b":                 {"host", "a", "b"},
		"  HostName=x.example.com": {"hostname", "x.example.com"},
		"Port = 22":                {"port", "22"},
		`Host "a b" c`:             {"host", "a b", "c"},
		"\tUSER\tme":               {"user", "me"},
	}
	for line, want := range cases {
		k, args := sshConfigLine(line)
		if got := append([]string{k}, args...); !reflect.DeepEqual(got, want) {
			t.Errorf("%q: %q, want %q", line, got, want)
		}
	}
	for _, line := range []string{"", "   ", "# Host x"} {
		if k, _ := sshConfigLine(line); k != "" {
			t.Errorf("%q: key %q", line, k)
		}
	}
}

func TestSSHHostMatches(t *testing.T) {
	cases := []struct {
		pats []string
		host string
		want bool
	}{
		{[]string{"*"}, "x", true},
		{[]string{"*.example.com"}, "a.EXAMPLE.com", true},
		{[]string{"*.example.com"}, "example.com", false},
		{[]string{"b?x"}, "box", true},
		{[]string{"*", "!box"}, "box", false},
		{[]string{"!box"}, "other", false},
	}
	for _, c := range cases {
		if got := sshHostMatches(c.pats, c.host); got != c.want {
			t.Errorf("%q vs %q: %v", c.pats, c.host, got)
		}
	}
}

func TestSSHConfigTrailingComments(t *testing.T) {
	home := t.TempDir()
	writeSSHFile(t, filepath.Join(home, ".ssh", "config"), "Host alpha # my box\n  HostName a.example.com #where\n  User ann#e\n")
	got := sshConfigHosts(home)
	want := []sshHostDoc{{Alias: "alpha", HostName: "a.example.com", User: "ann#e"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts\n got %+v\nwant %+v", got, want)
	}
}

// Options after an Include in a Host block stay with that block.
func TestSSHConfigIncludeInHostBlock(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	writeSSHFile(t, filepath.Join(ssh, "config"), "Host outer\n  Include inc.conf\n  User after\nHost other\n  Port 2200\n")
	writeSSHFile(t, filepath.Join(ssh, "inc.conf"), "  Port 2022\nHost inner\n  HostName inner.example.com\n")
	got := sshConfigHosts(home)
	want := []sshHostDoc{
		{Alias: "outer", User: "after", Port: 2022},
		{Alias: "inner", HostName: "inner.example.com"},
		{Alias: "other", Port: 2200},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("hosts\n got %+v\nwant %+v", got, want)
	}
}

func TestSSHGlob(t *testing.T) {
	cases := []struct {
		pat, s string
		want   bool
	}{
		{"", "", true},
		{"", "a", false},
		{"*", "", true},
		{"a*", "a", true},
		{"a*b", "axxb", true},
		{"a*b", "axxbc", false},
		{"a*b*c", "abxbc", true},
		{"?", "", false},
		{"??", "ab", true},
		{"a?c", "abc", true},
		{"*.example.com", "a.example.com", true},
		{"*.example.com", "example.com", false},
		{"**x", "abx", true},
		{"x*", "yx", false},
	}
	for _, c := range cases {
		if got := sshGlob(c.pat, c.s); got != c.want {
			t.Errorf("sshGlob(%q, %q) = %v", c.pat, c.s, got)
		}
	}
	// Exponential backtracking would take ages here.
	start := time.Now()
	if sshGlob(strings.Repeat("*a", 30)+"b", strings.Repeat("a", 200)) {
		t.Fatal("matched")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("took %v", d)
	}
}

func TestSSHHostsEndpoint(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	writeSSHFile(t, filepath.Join(st.homeD, ".ssh", "config"), "Host box\n  HostName box.example.com\n  User me\n  Port 2222\n")
	resp, out := do(t, ts, "GET", "/api/ssh-hosts", nil, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ssh-hosts: %d %v", resp.StatusCode, out)
	}
	hosts, _ := out["hosts"].([]any)
	if len(hosts) != 1 {
		t.Fatalf("hosts %v", out)
	}
	h := hosts[0].(map[string]any)
	if h["alias"] != "box" || h["hostname"] != "box.example.com" || h["user"] != "me" || h["port"] != float64(2222) {
		t.Fatalf("host %v", h)
	}
}

func sshStart(host string) map[string]any {
	return map[string]any{"kind": "ssh", "ssh": map[string]any{"host": host, "user": "me", "port": 2222}, "cols": 80, "rows": 24}
}

func TestStartSSHSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	rec := &recorder{}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(rec, "sleep 60")})
	resp, out := do(t, ts, "POST", "/api/sessions", sshStart("box"), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	args, dir := rec.last()
	if want := []string{"-t", "-p", "2222", "-l", "me", "--", "box"}; !reflect.DeepEqual(args, want) || dir != st.homeD {
		t.Fatalf("ran %q in %s", args, dir)
	}
	info := s.sessions.Get(id).Info()
	if info.Kind != KindSSH || info.Agent != "" || info.Name != "me@box" || info.Request != nil ||
		info.SSH == nil || *info.SSH != (SSHOptions{Name: "me@box", Host: "box", User: "me", Port: 2222}) {
		t.Fatalf("info %+v", info)
	}
	if out["kind"] != "ssh" || out["ssh"].(map[string]any)["host"] != "box" {
		t.Fatalf("reply %v", out)
	}
	// Kept on disk for a restarted server.
	data, err := os.ReadFile(infoPath(s.sessions.dir, id))
	if err != nil {
		t.Fatal(err)
	}
	var saved SessionInfo
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.SSH == nil || saved.SSH.Host != "box" {
		t.Fatalf("saved %+v", saved)
	}
	// Never paired with a container, even one that claims its id.
	st.ctrs = []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: st.dirs[0], WebSession: id, Started: time.Now()}}
	s.ctrAt = time.Time{}
	sessions, others := s.liveSessions()
	if len(others) != 1 || sessions[0].Container != "" {
		t.Fatalf("ssh session took a container: %+v / %+v", sessions, others)
	}
}

func TestStartSSHRefused(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	for _, body := range []map[string]any{
		{"kind": "ssh"},
		{"kind": "telnet", "ssh": map[string]any{"host": "box"}},
		{"kind": "ssh", "ssh": map[string]any{"host": "-oProxyCommand=touch /tmp/x"}},
		{"kind": "ssh", "ssh": map[string]any{"host": "box", "user": "-oProxyCommand=x"}},
		{"kind": "ssh", "ssh": map[string]any{"host": "box", "port": 70000}},
		{"kind": "ssh", "ssh": map[string]any{"host": "box", "identity": "~/nope"}},
	} {
		if resp, out := do(t, ts, "POST", "/api/sessions", body, ""); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%v: %d %v", body, resp.StatusCode, out)
		}
	}
	st.noSSH = true
	resp, out := do(t, ts, "POST", "/api/sessions", sshStart("box"), "")
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(out["error"].(string), "no ssh client") {
		t.Fatalf("without ssh: %d %v", resp.StatusCode, out)
	}
}

// A program that is not installed fails Start at once, leaving nothing.
func TestStartMissingProgram(t *testing.T) {
	dir := shortTempDir(t)
	m := NewManager(func(SessionInfo) *exec.Cmd { return exec.Command("exitbox-test-no-such-ssh") }, inProcessHosts, dir)
	if _, err := m.Start(SessionInfo{Kind: KindSSH}, 80, 24); err == nil || !strings.Contains(err.Error(), "exitbox-test-no-such-ssh") {
		t.Fatalf("start: %v", err)
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("files left behind: %v", left)
	}
}

// waitOutput waits until a session's output holds want.
func waitOutput(t *testing.T, sess *Session, want string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		sess.mu.Lock()
		out := string(sess.replay)
		sess.mu.Unlock()
		if strings.Contains(out, want) {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no %q in the output", want)
	return ""
}

// Web prompts are for exitbox run only: an SSH session gets no socket and
// the broker does not take its id.
func TestSSHSessionHasNoPromptEnv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "env; echo ENV-DONE; sleep 60")})
	s.sessions.setPromptSocket("/tmp/xw-prompts.sock")
	resp, out := do(t, ts, "POST", "/api/sessions", sshStart("box"), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start ssh: %d %v", resp.StatusCode, out)
	}
	sshID := out["id"].(string)
	if env := waitOutput(t, s.sessions.Get(sshID), "ENV-DONE"); strings.Contains(env, webprompt.EnvSocket) || strings.Contains(env, webprompt.EnvSession) {
		t.Fatalf("ssh session got prompt env:\n%s", env)
	}
	if s.prompts.known(sshID) {
		t.Fatal("the broker takes prompts for an ssh session")
	}
	// The same harness gives a run session the variables.
	resp, out = do(t, ts, "POST", "/api/sessions", startOpts(st.dirs[0]), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start run: %d %v", resp.StatusCode, out)
	}
	runID := out["id"].(string)
	if env := waitOutput(t, s.sessions.Get(runID), "ENV-DONE"); !strings.Contains(env, webprompt.EnvSession+"="+runID) {
		t.Fatalf("run session lacks prompt env:\n%s", env)
	}
	if !s.prompts.known(runID) {
		t.Fatal("the broker refuses a run session")
	}
}

func TestRestartSSHSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	rec := &recorder{}
	s, _, ts := newTestServer(t, Options{Command: shellCommand(rec, "exit 0")})
	resp, out := do(t, ts, "POST", "/api/sessions", sshStart("box"), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	waitDone(t, s.sessions.Get(id))
	if resp, out := do(t, ts, "POST", "/api/sessions/"+id+"/rename", map[string]any{"name": "build box"}, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("rename: %d %v", resp.StatusCode, out)
	}
	resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]any{"cols": 80, "rows": 24}, "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("restart: %d %v", resp.StatusCode, out)
	}
	if s.sessions.Get(id) != nil {
		t.Fatal("restart kept the old session")
	}
	info := s.sessions.Get(out["id"].(string)).Info()
	if args, _ := rec.last(); !reflect.DeepEqual(args, []string{"-t", "-p", "2222", "-l", "me", "--", "box"}) {
		t.Fatalf("reconnect ran %q", args)
	}
	if info.Kind != KindSSH || info.Name != "build box" || info.SSH == nil || info.SSH.Name != "build box" || info.SSH.Host != "box" {
		t.Fatalf("reconnected %+v", info)
	}
}

// The settings cog restarts an SSH session with new options, also over a
// running one.
func TestRestartSSHWithNewOptions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	for _, script := range []string{"exit 0", "sleep 60"} {
		t.Run(script, func(t *testing.T) {
			rec := &recorder{}
			s, st, ts := newTestServer(t, Options{Command: shellCommand(rec, script)})
			resp, out := do(t, ts, "POST", "/api/sessions", sshStart("box"), "")
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("start: %d %v", resp.StatusCode, out)
			}
			id := out["id"].(string)
			old := s.sessions.Get(id)
			if script == "exit 0" {
				waitDone(t, old)
			}
			// Bad options leave the running session alone.
			bad := map[string]any{"ssh": map[string]any{"host": "-oProxyCommand=x"}}
			if resp, out := do(t, ts, "POST", "/api/sessions/"+id+"/restart", bad, ""); resp.StatusCode != http.StatusBadRequest || s.sessions.Get(id) == nil {
				t.Fatalf("bad restart: %d %v", resp.StatusCode, out)
			}
			// Run options do not apply to an SSH session.
			if resp, _ := do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]any{"request": startOpts(st.dirs[0])}, ""); resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("run request on ssh: %d", resp.StatusCode)
			}
			next := map[string]any{"cols": 80, "rows": 24, "ssh": map[string]any{"host": "other.example.com", "port": 22, "name": "other"}}
			resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/restart", next, "")
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("restart: %d %v", resp.StatusCode, out)
			}
			waitDone(t, old)
			if s.sessions.Get(id) != nil {
				t.Fatal("restart kept the old session")
			}
			want := []string{"-t", "-p", "22", "--", "other.example.com"}
			if args, _ := rec.last(); !reflect.DeepEqual(args, want) {
				t.Fatalf("restart ran %q", args)
			}
			info := s.sessions.Get(out["id"].(string)).Info()
			if !reflect.DeepEqual(info.Args, want) || info.Name != "other" || info.SSH == nil || info.SSH.Host != "other.example.com" || info.SSH.User != "" {
				t.Fatalf("new session %+v", info)
			}
		})
	}
}

// SSH options do not apply to a run session.
func TestRestartRunRefusesSSHOptions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "sleep 60")})
	resp, out := do(t, ts, "POST", "/api/sessions", startOpts(st.dirs[0]), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	if resp, _ := do(t, ts, "POST", "/api/sessions/"+id+"/restart", map[string]any{"ssh": map[string]any{"host": "box"}}, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("ssh options on a run session: %d", resp.StatusCode)
	}
	if info := s.sessions.Get(id).Info(); info.Exited {
		t.Fatal("the run session was stopped")
	}
}

func TestTerminateAndRenameSSHSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no pty on Windows")
	}
	s, st, ts := newTestServer(t, Options{Command: shellCommand(&recorder{}, "sleep 60")})
	resp, out := do(t, ts, "POST", "/api/sessions", sshStart("box"), "")
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %v", resp.StatusCode, out)
	}
	id := out["id"].(string)
	sess := s.sessions.Get(id)

	// A running SSH session takes a new name at once; nothing saved changes.
	resp, out = do(t, ts, "POST", "/api/sessions/"+id+"/rename", map[string]any{"name": " prod shell "}, "")
	if resp.StatusCode != http.StatusOK || out["name"] != "prod shell" || out["renameTo"] != nil {
		t.Fatalf("rename: %d %v", resp.StatusCode, out)
	}
	if info := sess.Info(); info.SSH == nil || info.SSH.Name != "prod shell" || st.renameCalls != 0 {
		t.Fatalf("after rename %+v, store renames %d", info, st.renameCalls)
	}
	if resp, _ := do(t, ts, "POST", "/api/sessions/"+id+"/rename", map[string]any{"name": "-x"}, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("flag as name: %d", resp.StatusCode)
	}

	// Disconnect ends the ssh process; no container is touched.
	st.ctrs = []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", Agent: "claude", Dir: st.dirs[0], WebSession: id, Started: time.Now()}}
	s.ctrAt = time.Time{}
	if resp, out := do(t, ts, "POST", "/api/sessions/"+id+"/terminate", map[string]any{}, ""); resp.StatusCode != http.StatusOK {
		t.Fatalf("terminate: %d %v", resp.StatusCode, out)
	}
	waitDone(t, sess)
	if s.sessions.Get(id) != nil || len(st.stopped) != 0 {
		t.Fatalf("session kept: %v, stopped %v", s.sessions.Get(id) != nil, st.stopped)
	}
}

func TestMatchContainersSkipsSSH(t *testing.T) {
	sessions := []SessionInfo{{ID: "s1", Kind: KindSSH, Container: "exitbox-claude-p-0a1b2c3d"}}
	ctrs := []containerDoc{{Container: "exitbox-claude-p-0a1b2c3d", WebSession: "s1"}}
	if got := matchContainers(sessions, ctrs); len(got) != 0 {
		t.Fatalf("ssh session owns %v", got)
	}
	if got := withPromptEnv([]string{"PATH=/bin"}, KindSSH, "/s/prompts.sock", "s1"); !reflect.DeepEqual(got, []string{"PATH=/bin"}) {
		t.Fatalf("ssh env %q", got)
	}
}

// The UI files an SSH session under sessionKey("", "ssh", "", id).
func TestFoldersKeepSSHKeys(t *testing.T) {
	key := sessionKey("", "ssh", "", "0123456789abcdef")
	out, err := cleanFolders([]Folder{{ID: "f1", Name: "Servers", Items: []string{key}}})
	if err != nil || len(out) != 1 || !reflect.DeepEqual(out[0].Items, []string{key}) {
		t.Fatalf("folders %+v, %v", out, err)
	}
}
