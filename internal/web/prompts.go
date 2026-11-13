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

// promptNoViewerGrace lets a prompt nobody sees survive a page reload or reconnect.
const promptNoViewerGrace = 2 * time.Second

// maxAcceptBackoff caps the pause between failed accepts.
const maxAcceptBackoff = time.Second

// promptBroker passes the host prompts of web sessions (exitbox-allow,
// vault approvals, the vault password) to the browsers viewing them. A
// requester is an exitbox run; it connects to the broker's socket, sends one
// prompt and waits for the first answer. It names its web session, or, when
// started in a terminal, its container.
type promptBroker struct {
	known func(session string) bool
	// byContainer lists the sessions whose window shows a container.
	byContainer func(container string) []string
	logf        func(format string, args ...any)
	// noViewerGrace: see promptNoViewerGrace; tests shorten it.
	noViewerGrace time.Duration

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
	id string
	// sessions may answer it: one, or every window on its container.
	sessions []string
	kind     string
	seq      uint64
	msg      string
	reply    chan webprompt.Reply
	// seen holds the viewers that received msg; only they count as viewers.
	seen  map[*promptViewer]struct{}
	grace *time.Timer
}

func (p *pendingPrompt) shownIn(session string) bool {
	for _, s := range p.sessions {
		if s == session {
			return true
		}
	}
	return false
}

func newPromptBroker(known func(string) bool, byContainer func(string) []string, logf func(string, ...any)) *promptBroker {
	return &promptBroker{
		known:         known,
		byContainer:   byContainer,
		logf:          logf,
		noViewerGrace: promptNoViewerGrace,
		viewers:       map[string]map[*promptViewer]struct{}{},
		pending:       map[string]*pendingPrompt{},
		conns:         map[net.Conn]struct{}{},
		closed:        make(chan struct{}),
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
	b.path = path
	b.mu.Unlock()
	b.start(ln)
	return nil
}

// start serves requesters on ln until close.
func (b *promptBroker) start(ln net.Listener) {
	b.mu.Lock()
	b.ln = ln
	b.mu.Unlock()
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		backoff := 10 * time.Millisecond
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
				b.logf("web: prompt socket: %v", err)
				if errors.Is(err, net.ErrClosed) {
					return
				}
				// A transient failure (out of file descriptors) must not end prompts for good.
				select {
				case <-b.closed:
					return
				case <-time.After(backoff):
				}
				backoff = min(backoff*2, maxAcceptBackoff)
				continue
			}
			backoff = 10 * time.Millisecond
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
		if p.shownIn(session) {
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
			p.seen[v] = struct{}{}
		default:
		}
	}
	return v
}

// unwatch removes a viewer; a prompt left unseen returns to its requester after a grace.
func (b *promptBroker) unwatch(session string, v *promptViewer) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.viewers[session], v)
	if len(b.viewers[session]) == 0 {
		delete(b.viewers, session)
	}
	for _, p := range b.pending {
		if _, ok := p.seen[v]; !ok {
			continue
		}
		delete(p.seen, v)
		if len(p.seen) == 0 {
			if p.grace != nil {
				p.grace.Stop()
			}
			p.grace = time.AfterFunc(b.noViewerGrace, func() { b.expireUnseen(p) })
		}
	}
}

// expireUnseen returns a still unseen prompt to its requester, which shows the popup.
func (b *promptBroker) expireUnseen(p *pendingPrompt) {
	b.mu.Lock()
	if b.pending[p.id] != p || len(p.seen) > 0 {
		b.mu.Unlock()
		return
	}
	delete(b.pending, p.id)
	b.mu.Unlock()
	p.reply <- webprompt.Reply{Status: webprompt.StatusNoViewer}
}

// broadcastLocked queues msg for the viewers of sessions and returns those that took it.
func (b *promptBroker) broadcastLocked(sessions []string, msg string) map[*promptViewer]struct{} {
	got := map[*promptViewer]struct{}{}
	for _, s := range sessions {
		for v := range b.viewers[s] {
			select {
			case v.out <- msg:
				got[v] = struct{}{}
			default:
			}
		}
	}
	return got
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

// errNoPrompt: the prompt was answered already, cancelled or never shown here.
var errNoPrompt = errors.New("no such prompt")

// answerFrom settles a prompt for viewer v and tells v why an answer was refused.
func (b *promptBroker) answerFrom(v *promptViewer, session, id string, allow bool, password string) {
	err := b.settle(session, id, allow, password)
	if err == nil || errors.Is(err, errNoPrompt) {
		return
	}
	data, merr := json.Marshal(struct {
		T     string `json:"t"`
		ID    string `json:"id"`
		Error string `json:"error"`
	}{"prompt-error", id, err.Error()})
	if merr != nil {
		return
	}
	select {
	case v.out <- string(data):
	default:
	}
}

// settle answers a prompt of session; the first answer wins. It returns
// why an answer was not taken.
func (b *promptBroker) settle(session, id string, allow bool, password string) error {
	b.mu.Lock()
	p := b.pending[id]
	if p == nil || !p.shownIn(session) {
		b.mu.Unlock()
		return errNoPrompt
	}
	isPassword := p.kind == webprompt.KindVaultPassword
	if isPassword && allow {
		reason := ""
		switch {
		case password == "":
			reason = "enter the vault password"
		case len(password) > webprompt.MaxPassword:
			reason = "the password is too long"
		}
		if reason != "" {
			b.mu.Unlock()
			return errors.New(reason)
		}
	}
	delete(b.pending, id)
	b.broadcastLocked(p.sessions, doneMsg(id))
	b.mu.Unlock()
	r := webprompt.Reply{Status: webprompt.StatusAnswer, Allow: allow}
	if isPassword && allow {
		r.Password = password
	}
	p.reply <- r
	return nil
}

// cancel drops a prompt nobody waits for any more.
func (b *promptBroker) cancel(p *pendingPrompt) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pending[p.id] != p {
		return
	}
	delete(b.pending, p.id)
	b.broadcastLocked(p.sessions, doneMsg(p.id))
}

// open records a prompt and shows it to the viewers of sessions; nil when
// nobody views them.
func (b *promptBroker) open(req webprompt.Request, sessions []string) (*pendingPrompt, error) {
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
	// Only a viewer that took the message counts.
	seen := b.broadcastLocked(sessions, string(msg))
	if len(seen) == 0 {
		return nil, nil
	}
	b.seq++
	p := &pendingPrompt{id: id, sessions: sessions, kind: req.Kind, seq: b.seq, msg: string(msg), reply: make(chan webprompt.Reply, 1), seen: seen}
	b.pending[id] = p
	return p, nil
}

// route returns the sessions a request is for; false refuses it. A
// container nobody shows is valid, with no sessions.
func (b *promptBroker) route(req webprompt.Request) ([]string, bool) {
	if !b.validRequest(req) {
		return nil, false
	}
	if req.Session != "" {
		return []string{req.Session}, b.known(req.Session)
	}
	if b.byContainer == nil {
		return nil, true
	}
	return b.byContainer(req.Container), true
}

func (b *promptBroker) validRequest(req webprompt.Request) bool {
	if !webprompt.ValidKind(req.Kind) {
		return false
	}
	switch {
	case req.Session != "" && req.Container == "":
		if len(req.Session) > webprompt.MaxName {
			return false
		}
	case req.Session == "" && req.Container != "":
		if len(req.Container) > webprompt.MaxSubject || !containerNameRe.MatchString(req.Container) {
			return false
		}
	default:
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
	for _, s := range []string{req.Agent, req.Workspace, req.Subject, req.Container} {
		for _, r := range s {
			if unicode.IsControl(r) {
				return false
			}
		}
	}
	return true
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
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		reply(webprompt.Reply{Status: webprompt.StatusInvalid})
		return
	}
	sessions, ok := b.route(req)
	if !ok {
		reply(webprompt.Reply{Status: webprompt.StatusInvalid})
		return
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		return
	}
	p, err := b.open(req, sessions)
	if err != nil {
		who := req.Session
		if who == "" {
			who = req.Container
		}
		b.logf("web: prompt for %s: %v", who, err)
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
