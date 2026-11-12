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

package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cloud-exit/exitbox/internal/web"
)

func TestWebURL(t *testing.T) {
	for addr, want := range map[string]string{
		"127.0.0.1:4848": "http://127.0.0.1:4848",
		"0.0.0.0:9000":   "http://127.0.0.1:9000",
		"[::]:9000":      "http://127.0.0.1:9000",
		"[::1]:9000":     "http://[::1]:9000",
	} {
		if got := webURL(addr); got != want {
			t.Errorf("webURL(%q) = %q, want %q", addr, got, want)
		}
	}
}

func TestWebSessionCommandEnv(t *testing.T) {
	t.Setenv("TERM", "xterm-kitty")
	t.Setenv("TMUX", "/tmp/tmux-1/default,1,0")
	t.Setenv("KITTY_WINDOW_ID", "3")
	t.Setenv("EXITBOX_WEB_TEST_KEEP", "yes")
	c := sessionCommand("/usr/bin/exitbox")(web.SessionInfo{Kind: web.KindRun, Dir: "/tmp", Args: []string{"run", "claude"}})
	if c.Dir != "/tmp" || strings.Join(c.Args, " ") != "/usr/bin/exitbox run claude" {
		t.Fatalf("command %q in %s", c.Args, c.Dir)
	}
	env := "\n" + strings.Join(c.Env, "\n")
	for _, gone := range []string{"TERM=xterm-kitty", "TMUX=", "KITTY_WINDOW_ID="} {
		if strings.Contains(env, "\n"+gone) {
			t.Errorf("env keeps %s", gone)
		}
	}
	for _, kept := range []string{"TERM=xterm-256color", "COLORTERM=truecolor", "EXITBOX_WEB_TEST_KEEP=yes"} {
		if !strings.Contains(env, "\n"+kept) {
			t.Errorf("env lacks %s", kept)
		}
	}
}

func TestWebAttachCommand(t *testing.T) {
	c := sessionCommand("/usr/bin/exitbox")(web.SessionInfo{Kind: web.KindAttach, Agent: "claude", Container: "exitbox-claude-x_1-0a1b2c3d"})
	got := strings.Join(c.Args[1:], " ")
	if got != "exec -it -e TERM=xterm-256color exitbox-claude-x_1-0a1b2c3d tmux attach-session -t exitbox-claude" {
		t.Fatalf("attach command %q", got)
	}
}

func TestWebSSHCommand(t *testing.T) {
	bin := t.TempDir()
	name := "ssh"
	if runtime.GOOS == "windows" {
		name = "ssh.exe"
	}
	fake := filepath.Join(bin, name)
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	c := sessionCommand("/usr/bin/exitbox")(web.SessionInfo{Kind: web.KindSSH, Dir: "/home/me", Args: []string{"-t", "-p", "2222", "--", "box"}})
	if c.Err != nil || c.Path != fake || c.Dir != "/home/me" || strings.Join(c.Args[1:], " ") != "-t -p 2222 -- box" {
		t.Fatalf("ssh command %q (%s) in %s: %v", c.Args, c.Path, c.Dir, c.Err)
	}
	if !strings.Contains("\n"+strings.Join(c.Env, "\n"), "\nTERM=xterm-256color") {
		t.Fatal("ssh env lacks TERM=xterm-256color")
	}
	t.Setenv("PATH", t.TempDir())
	if c := sessionCommand("/usr/bin/exitbox")(web.SessionInfo{Kind: web.KindSSH, Args: []string{"--", "box"}}); c.Err == nil {
		t.Fatal("a missing ssh is not reported")
	}
}

// A host that exits is reaped, and what it printed stays in its log.
func TestSpawnHostReapsAndKeepsLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script host")
	}
	dir := t.TempDir()
	exe := filepath.Join(t.TempDir(), "fake-exitbox")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho \"host failed: $*\" >&2\nexit 3\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	exited, err := spawnHost(exe)(dir, "abc")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		t.Fatal("host never reaped")
	}
	data, err := os.ReadFile(filepath.Join(dir, "abc.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "host failed: web-session-host --state "+dir+" --id abc") {
		t.Fatalf("log %q", data)
	}
}
