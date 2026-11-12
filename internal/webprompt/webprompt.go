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

// Package webprompt carries host approval prompts of an exitbox run started
// by exitbox web to the browser, over a Unix socket of exitbox web. Requests
// name what is asked for (a domain, a vault key), never a secret value.
package webprompt

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"time"
)

// Host-side environment of a web session's exitbox run; never forwarded
// into the container.
const (
	EnvSocket  = "EXITBOX_WEB_PROMPT_SOCK"
	EnvSession = "EXITBOX_WEB_PROMPT_SESSION"
)

// Prompt kinds.
const (
	KindAllow         = "allow"
	KindVaultRead     = "vault-read"
	KindVaultWrite    = "vault-write"
	KindVaultList     = "vault-list"
	KindVaultPassword = "vault-password"
)

// Reply statuses. Anything but StatusAnswer means: ask in the terminal.
const (
	StatusAnswer   = "answer"
	StatusNoViewer = "no-viewer"
	StatusInvalid  = "invalid"
)

// Size limits, checked by both ends.
const (
	MaxLine     = 8 << 10
	MaxSubject  = 256
	MaxName     = 128
	MaxPassword = 4 << 10
)

// ValidKind reports whether k is a known prompt kind.
func ValidKind(k string) bool {
	switch k {
	case KindAllow, KindVaultRead, KindVaultWrite, KindVaultList, KindVaultPassword:
		return true
	}
	return false
}

// Request is one prompt, sent as a single JSON line.
type Request struct {
	Session   string `json:"session"`
	Kind      string `json:"kind"`
	Agent     string `json:"agent"`
	Workspace string `json:"workspace"`
	// Subject is the domain or vault key name.
	Subject string `json:"subject"`
}

// Reply answers a Request, as a single JSON line.
type Reply struct {
	Status string `json:"status"`
	Allow  bool   `json:"allow,omitempty"`
	// Password is set only for KindVaultPassword.
	Password string `json:"password,omitempty"`
}

// ErrUnavailable means nobody can answer in the browser.
var ErrUnavailable = errors.New("no browser to ask")

// Client asks exitbox web for one session.
type Client struct {
	Socket    string
	Session   string
	Agent     string
	Workspace string
	// Timeout bounds the wait for an answer.
	Timeout time.Duration
}

// FromEnv returns a client when this process runs a web session, or nil.
// It removes the variables so child processes do not inherit them.
func FromEnv(agent, workspace string, timeout time.Duration) *Client {
	sock, id := os.Getenv(EnvSocket), os.Getenv(EnvSession)
	_ = os.Unsetenv(EnvSocket)
	_ = os.Unsetenv(EnvSession)
	if sock == "" || id == "" || runtime.GOOS == "windows" {
		return nil
	}
	return &Client{Socket: sock, Session: id, Agent: agent, Workspace: workspace, Timeout: timeout}
}

// dialTimeout bounds reaching exitbox web; a var for tests.
var dialTimeout = 2 * time.Second

// Ask sends one prompt and waits for its answer. ErrUnavailable means the
// caller should prompt another way; a timeout is a refusal.
func (c *Client) Ask(kind, subject string) (Reply, error) {
	conn, err := net.DialTimeout("unix", c.Socket, dialTimeout)
	if err != nil {
		return Reply{}, ErrUnavailable
	}
	defer func() { _ = conn.Close() }()
	line, err := json.Marshal(Request{Session: c.Session, Kind: kind, Agent: c.Agent, Workspace: c.Workspace, Subject: subject})
	if err != nil {
		return Reply{}, err
	}
	if err := conn.SetDeadline(time.Now().Add(c.Timeout)); err != nil {
		return Reply{}, ErrUnavailable
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return Reply{}, ErrUnavailable
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 512), MaxLine)
	if !sc.Scan() {
		var ne net.Error
		if errors.As(sc.Err(), &ne) && ne.Timeout() {
			return Reply{}, fmt.Errorf("prompt timed out after %v", c.Timeout)
		}
		// exitbox web went away mid-prompt.
		return Reply{}, ErrUnavailable
	}
	var r Reply
	if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
		return Reply{}, ErrUnavailable
	}
	if r.Status != StatusAnswer {
		return Reply{}, ErrUnavailable
	}
	return r, nil
}
