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

package webprompt

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fakeWeb answers each prompt with reply, or hangs up when reply is nil.
func fakeWeb(t *testing.T, reply *Reply) (string, chan Request) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no web prompts on Windows")
	}
	dir, err := os.MkdirTemp("", "wp")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "p.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan Request, 4)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				sc := bufio.NewScanner(conn)
				if !sc.Scan() {
					return
				}
				var req Request
				if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
					return
				}
				got <- req
				if reply == nil {
					return
				}
				if reply.Status == "" {
					// Never answer.
					time.Sleep(time.Second)
					return
				}
				data, err := json.Marshal(reply)
				if err != nil {
					return
				}
				_, _ = conn.Write(append(data, '\n'))
			}()
		}
	}()
	return path, got
}

func TestAskAnswer(t *testing.T) {
	path, got := fakeWeb(t, &Reply{Status: StatusAnswer, Allow: true})
	c := &Client{Socket: path, Session: "s1", Agent: "codex", Workspace: "work", Timeout: 5 * time.Second}
	r, err := c.Ask(KindAllow, "example.com")
	if err != nil || !r.Allow {
		t.Fatalf("Ask = %+v, %v", r, err)
	}
	req := <-got
	if req != (Request{Session: "s1", Kind: KindAllow, Agent: "codex", Workspace: "work", Subject: "example.com"}) {
		t.Fatalf("request %+v", req)
	}
}

func TestAskUnavailable(t *testing.T) {
	for name, reply := range map[string]*Reply{
		"no viewer": {Status: StatusNoViewer},
		"invalid":   {Status: StatusInvalid},
		"hung up":   nil,
	} {
		path, _ := fakeWeb(t, reply)
		c := &Client{Socket: path, Session: "s1", Timeout: 5 * time.Second}
		if _, err := c.Ask(KindVaultList, ""); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s: err %v", name, err)
		}
	}
	c := &Client{Socket: filepath.Join(t.TempDir(), "missing.sock"), Session: "s1", Timeout: time.Second}
	if _, err := c.Ask(KindAllow, "x.com"); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing socket: err %v", err)
	}
}

func TestAskTimeoutIsNotAFallback(t *testing.T) {
	path, _ := fakeWeb(t, &Reply{})
	c := &Client{Socket: path, Session: "s1", Timeout: 200 * time.Millisecond}
	_, err := c.Ask(KindAllow, "x.com")
	if err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v, want a timeout", err)
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv(EnvSocket, "/tmp/x.sock")
	t.Setenv(EnvSession, "abc")
	c := FromEnv("claude", "work", time.Second)
	if runtime.GOOS == "windows" {
		if c != nil {
			t.Fatal("client on Windows")
		}
		return
	}
	if c == nil || c.Socket != "/tmp/x.sock" || c.Session != "abc" || c.Agent != "claude" || c.Workspace != "work" {
		t.Fatalf("client %+v", c)
	}
	if os.Getenv(EnvSocket) != "" || os.Getenv(EnvSession) != "" {
		t.Fatal("variables left for child processes")
	}
	if FromEnv("claude", "work", time.Second) != nil {
		t.Fatal("client without variables")
	}
}
