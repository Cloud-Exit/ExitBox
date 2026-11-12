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

package run

import (
	"bufio"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloud-exit/exitbox/internal/ipc"
	"github.com/cloud-exit/exitbox/internal/webprompt"
)

// fakePromptSocket answers every prompt with answer(req).
func fakePromptSocket(t *testing.T, answer func(webprompt.Request) webprompt.Reply) (string, chan webprompt.Request) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("no web prompts on Windows")
	}
	dir, err := os.MkdirTemp("", "rp")
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
	got := make(chan webprompt.Request, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			sc := bufio.NewScanner(conn)
			if sc.Scan() {
				var req webprompt.Request
				if err := json.Unmarshal(sc.Bytes(), &req); err == nil {
					got <- req
					if data, err := json.Marshal(answer(req)); err == nil {
						_, _ = conn.Write(append(data, '\n'))
					}
				}
			}
			_ = conn.Close()
		}
	}()
	return path, got
}

func client(path string) *webprompt.Client {
	return &webprompt.Client{Socket: path, Session: "s1", Agent: "codex", Workspace: "work", Timeout: 5 * time.Second}
}

func noPopup(t *testing.T) func(string) (bool, error) {
	return func(string) (bool, error) {
		t.Error("popup shown although the browser answered")
		return false, nil
	}
}

func TestWebApproveAllowAndDeny(t *testing.T) {
	var allow atomic.Bool
	allow.Store(true)
	path, got := fakePromptSocket(t, func(webprompt.Request) webprompt.Reply {
		return webprompt.Reply{Status: webprompt.StatusAnswer, Allow: allow.Load()}
	})
	fn := webApprove(client(path), webprompt.KindAllow, noPopup(t))
	ok, err := fn("registry.npmjs.org:443")
	if err != nil || !ok {
		t.Fatalf("allow: %v %v", ok, err)
	}
	if req := <-got; req.Kind != webprompt.KindAllow || req.Subject != "registry.npmjs.org:443" || req.Agent != "codex" || req.Session != "s1" {
		t.Fatalf("request %+v", req)
	}
	allow.Store(false)
	if ok, err := fn("example.com"); err != nil || ok {
		t.Fatalf("deny: %v %v", ok, err)
	}
}

func TestWebApproveFallsBackToPopup(t *testing.T) {
	popups := 0
	popup := func(s string) (bool, error) {
		popups++
		return s == "x.com", nil
	}
	// No socket at all.
	fn := webApprove(client(filepath.Join(t.TempDir(), "gone.sock")), webprompt.KindAllow, popup)
	if ok, err := fn("x.com"); err != nil || !ok || popups != 1 {
		t.Fatalf("missing socket: %v %v popups=%d", ok, err, popups)
	}
	// Nobody views the session.
	path, _ := fakePromptSocket(t, func(webprompt.Request) webprompt.Reply {
		return webprompt.Reply{Status: webprompt.StatusNoViewer}
	})
	fn = webApprove(client(path), webprompt.KindAllow, popup)
	if ok, err := fn("x.com"); err != nil || !ok || popups != 2 {
		t.Fatalf("no viewer: %v %v popups=%d", ok, err, popups)
	}
	// Not a web session.
	if fn := webApprove(nil, webprompt.KindAllow, popup); fn == nil {
		t.Fatal("nil wrapper")
	} else if _, err := fn("x.com"); err != nil || popups != 3 {
		t.Fatalf("plain run: %v popups=%d", err, popups)
	}
}

func TestWebVaultConfig(t *testing.T) {
	const pw = "correct horse"
	path, got := fakePromptSocket(t, func(req webprompt.Request) webprompt.Reply {
		r := webprompt.Reply{Status: webprompt.StatusAnswer, Allow: true}
		if req.Kind == webprompt.KindVaultPassword {
			r.Password = pw
		}
		return r
	})
	cfg := webVaultConfig(ipc.VaultHandlerConfig{WorkspaceName: "work"}, client(path))
	if ok, err := cfg.PromptApproveFunc("API_KEY"); err != nil || !ok {
		t.Fatalf("read: %v %v", ok, err)
	}
	if ok, err := cfg.PromptApproveSetFunc("API_KEY"); err != nil || !ok {
		t.Fatalf("write: %v %v", ok, err)
	}
	if ok, err := cfg.PromptApproveListFunc(); err != nil || !ok {
		t.Fatalf("list: %v %v", ok, err)
	}
	got1, err := cfg.PromptPasswordFunc()
	if err != nil || got1 != pw {
		t.Fatal("password not passed through")
	}
	var kinds []string
	for i := 0; i < 4; i++ {
		req := <-got
		kinds = append(kinds, req.Kind+":"+req.Subject)
	}
	if strings.Join(kinds, ",") != "vault-read:API_KEY,vault-write:API_KEY,vault-list:,vault-password:" {
		t.Fatalf("kinds %v", kinds)
	}
	if cfg := webVaultConfig(ipc.VaultHandlerConfig{}, nil); cfg.PromptApproveFunc != nil || cfg.PromptPasswordFunc != nil {
		t.Fatal("plain run got web prompts")
	}
}

func TestWebPasswordCancel(t *testing.T) {
	path, _ := fakePromptSocket(t, func(webprompt.Request) webprompt.Reply {
		return webprompt.Reply{Status: webprompt.StatusAnswer}
	})
	fn := webPassword(client(path), func() (string, error) {
		t.Error("popup shown after a cancel in the browser")
		return "", nil
	})
	if _, err := fn(); err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("err %v", err)
	}
}
