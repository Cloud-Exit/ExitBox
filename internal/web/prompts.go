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
	"encoding/json"
	"errors"
	"net"
	"os"
	"sync"
	"time"
	"unicode"

	"github.com/cloud-exit/exitbox/internal/webprompt"
)

// promptViewerBuffer is how many prompt messages a viewer may have queued.
const promptViewerBuffer = 32

// promptRequestTimeout bounds reading a requester's prompt line.
const promptRequestTimeout = 5 * time.Second

// promptBroker passes the host prompts of web sessions (exitbox-allow,
// vault approvals, the vault password) to the browsers viewing them. A
// requester is the exitbox run of a session; it connects to the broker's
// socket, sends one prompt and waits for the first answer.
type promptBroker struct {
	known func(session string) bool
	logf  func(format string, args ...any)

	mu      sync.Mutex
	viewers map[string]map[*promptViewer]struct{}
	pending map[string]*pendingPrompt
	seq     uint64
	ln      net.Listener
	path    string
	conns   map[net.Conn]struct{}
	closed  chan struct{}
	wg      sync.WaitGroup
}

// promptViewer is one browser of a session; out carries JSON messages.
type promptViewer struct {
	out chan string
}

type pendingPrompt struct {
	id      string
	session string
	kind    string
	seq     uint64
	msg     string
	reply   chan webprompt.Reply
}

func newPromptBroker(known func(string) bool, logf func(string, ...any)) *promptBroker {
	return &promptBroker{
		known:   known,
		logf:    logf,
		viewers: map[string]map[*promptViewer]struct{}{},
		pending: map[string]*pendingPrompt{},
		conns:   map[net.Conn]struct{}{},
		closed:  make(chan struct{}),
	}
}

// listen serves requesters on a socket only this user can reach.
func (b *promptBroker) listen(path string) error {
	// The server lock is held: a socket left here is stale.
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	b.mu.Lock()
	b.ln, b.path = ln, path
	b.mu.Unlock()
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-b.closed:
					return
				default:
				}
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue
				}
				return
			}
			b.mu.Lock()
			b.conns[conn] = struct{}{}
			b.mu.Unlock()
			b.wg.Add(1)
			go func() {
				defer b.wg.Done()
				b.serve(conn)
				b.mu.Lock()
				delete(b.conns, conn)
				b.mu.Unlock()
			}()
		}
	}()
	return nil
}

// close stops listening, cancels every prompt and removes the socket. The
// requesters fall back to their popups.
func (b *promptBroker) close() {
	b.mu.Lock()
	select {
	case <-b.closed:
		b.mu.Unlock()
		return
	default:
	}
	close(b.closed)
	ln, path := b.ln, b.path
	for c := range b.conns {
		_ = c.Close()
	}
	b.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	b.wg.Wait()
	if path != "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			b.logf("web: %v", err)
		}
	}
}

// watch registers a viewer of session and queues the prompts it waits on.
func (b *promptBroker) watch(session string) *promptViewer {
	v := &promptViewer{out: make(chan string, promptViewerBuffer)}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.viewers[session] == nil {
		b.viewers[session] = map[*promptViewer]struct{}{}
	}
	b.viewers[session][v] = struct{}{}
	var list []*pendingPrompt
	for _, p := range b.pending {
		if p.session == session {
			list = append(list, p)
		}
	}
	// Oldest first, as they were asked.
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].seq < list[j-1].seq; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	for _, p := range list {
		select {
		case v.out <- p.msg:
		default:
		}
	}
	return v
}

// unwatch removes a viewer. Its prompts stay pending for the next one.
func (b *promptBroker) unwatch(session string, v *promptViewer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.viewers[session], v)
	if len(b.viewers[session]) == 0 {
		delete(b.viewers, session)
	}
}

func (b *promptBroker) broadcastLocked(session, msg string) {
	for v := range b.viewers[session] {
		select {
		case v.out <- msg:
		default:
		}
	}
}

func doneMsg(id string) string {
	data, err := json.Marshal(struct {
		T  string `json:"t"`
		ID string `json:"id"`
	}{"prompt-done", id})
	if err != nil {
		return ""
	}
	return string(data)
}

// answer settles a prompt of session; the first answer wins. It reports
// whether the answer was taken.
func (b *promptBroker) answer(session, id string, allow bool, password string) bool {
	b.mu.Lock()
	p := b.pending[id]
	if p == nil || p.session != session {
		b.mu.Unlock()
		return false
	}
	isPassword := p.kind == webprompt.KindVaultPassword
	if isPassword && allow && (password == "" || len(password) > webprompt.MaxPassword) {
		b.mu.Unlock()
		return false
	}
	delete(b.pending, id)
	b.broadcastLocked(session, doneMsg(id))
	b.mu.Unlock()
	r := webprompt.Reply{Status: webprompt.StatusAnswer, Allow: allow}
	if isPassword && allow {
		r.Password = password
	}
	p.reply <- r
	return true
}

// cancel drops a prompt nobody waits for any more.
func (b *promptBroker) cancel(p *pendingPrompt) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending[p.id] != p {
		return
	}
	delete(b.pending, p.id)
	b.broadcastLocked(p.session, doneMsg(p.id))
}

// open records a prompt and shows it to the session's viewers; nil when
// nobody views the session.
func (b *promptBroker) open(req webprompt.Request) (*pendingPrompt, error) {
	id, err := newID()
	if err != nil {
		return nil, err
	}
	msg, err := json.Marshal(struct {
		T         string `json:"t"`
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		Agent     string `json:"agent"`
		Workspace string `json:"workspace"`
		Subject   string `json:"subject"`
	}{"prompt", id, req.Kind, req.Agent, req.Workspace, req.Subject})
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.viewers[req.Session]) == 0 {
		return nil, nil
	}
	b.seq++
	p := &pendingPrompt{id: id, session: req.Session, kind: req.Kind, seq: b.seq, msg: string(msg), reply: make(chan webprompt.Reply, 1)}
	b.pending[id] = p
	b.broadcastLocked(req.Session, p.msg)
	return p, nil
}

func (b *promptBroker) validRequest(req webprompt.Request) bool {
	if req.Session == "" || len(req.Session) > webprompt.MaxName || !webprompt.ValidKind(req.Kind) {
		return false
	}
	if len(req.Agent) > webprompt.MaxName || len(req.Workspace) > webprompt.MaxName || len(req.Subject) > webprompt.MaxSubject {
		return false
	}
	switch req.Kind {
	case webprompt.KindAllow, webprompt.KindVaultRead, webprompt.KindVaultWrite:
		if req.Subject == "" {
			return false
		}
	}
	for _, s := range []string{req.Agent, req.Workspace, req.Subject} {
		for _, r := range s {
			if unicode.IsControl(r) {
				return false
			}
		}
	}
	return b.known(req.Session)
}

// serve handles one requester: one prompt, one reply.
func (b *promptBroker) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reply := func(r webprompt.Reply) {
		data, err := json.Marshal(r)
		if err != nil {
			return
		}
		if err := conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return
		}
		_, _ = conn.Write(append(data, '\n'))
	}
	if err := conn.SetReadDeadline(time.Now().Add(promptRequestTimeout)); err != nil {
		return
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 512), webprompt.MaxLine)
	if !sc.Scan() {
		return
	}
	var req webprompt.Request
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil || !b.validRequest(req) {
		reply(webprompt.Reply{Status: webprompt.StatusInvalid})
		return
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return
	}
	p, err := b.open(req)
	if err != nil {
		b.logf("web: prompt for session %s: %v", req.Session, err)
		reply(webprompt.Reply{Status: webprompt.StatusInvalid})
		return
	}
	if p == nil {
		reply(webprompt.Reply{Status: webprompt.StatusNoViewer})
		return
	}
	// The requester sends nothing more: a read returns when it hangs up
	// (timed out, or its session ended).
	gone := make(chan struct{})
	go func() {
		defer close(gone)
		buf := make([]byte, 1)
		for {
			if _, err := conn.Read(buf); err != nil {
				return
			}
		}
	}()
	select {
	case r := <-p.reply:
		reply(r)
	case <-gone:
		b.cancel(p)
	case <-b.closed:
		b.cancel(p)
	}
}
