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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"sort"
	"sync"
	"time"

	"github.com/creack/pty"
)

// replayBytes is how much recent output a session keeps for a browser that
// attaches later. tmux redraws the screen on resize, so this only has to
// cover the gap until then.
const replayBytes = 512 << 10

// subscriberBuffer is how many output chunks a browser may fall behind
// before it is dropped. A dropped browser reconnects and gets the replay.
const subscriberBuffer = 256

// Session kinds.
const (
	KindRun    = "run"    // exitbox run started by the web UI
	KindAttach = "attach" // a terminal on the tmux of a session started elsewhere
)

// SessionInfo is what the UI shows for a running session.
type SessionInfo struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Agent     string    `json:"agent"`
	Dir       string    `json:"dir"`
	Workspace string    `json:"workspace"`
	Name      string    `json:"name"`
	Args      []string  `json:"args"`
	Started   time.Time `json:"started"`
	Exited    bool      `json:"exited"`
	ExitCode  int       `json:"exitCode"`
	Viewers   int       `json:"viewers"`
	// Container is the agent container, once it is known.
	Container string `json:"container,omitempty"`
	// RenameTo is a name the session takes when it ends.
	RenameTo    string `json:"renameTo,omitempty"`
	RenameError string `json:"renameError,omitempty"`
}

// Session is one `exitbox run` process on a pseudo terminal.
type Session struct {
	info SessionInfo

	mu     sync.Mutex
	pty    *os.File
	cmd    *exec.Cmd
	replay []byte
	subs   map[*subscriber]struct{}
	pumped chan struct{} // closed when all output has been read
	done   chan struct{} // closed when the process has exited
}

type subscriber struct {
	out  chan []byte
	gone chan struct{}
	once sync.Once
}

func (s *subscriber) drop() { s.once.Do(func() { close(s.gone) }) }

// Info returns a snapshot of the session's state.
func (s *Session) Info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := s.info
	info.Args = append([]string(nil), s.info.Args...)
	info.Viewers = len(s.subs)
	return info
}

// Done is closed when the process has exited.
func (s *Session) Done() <-chan struct{} { return s.done }

// Attach registers a viewer. It returns the recent output and the viewer;
// call Detach when the viewer leaves.
func (s *Session) Attach() ([]byte, *subscriber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sub := &subscriber{out: make(chan []byte, subscriberBuffer), gone: make(chan struct{})}
	s.subs[sub] = struct{}{}
	return append([]byte(nil), s.replay...), sub
}

// Detach removes a viewer.
func (s *Session) Detach(sub *subscriber) {
	s.mu.Lock()
	delete(s.subs, sub)
	s.mu.Unlock()
	sub.drop()
}

// Write sends keyboard input to the process.
func (s *Session) Write(p []byte) error {
	s.mu.Lock()
	f := s.pty
	exited := s.info.Exited
	s.mu.Unlock()
	if exited || f == nil {
		return errors.New("session has ended")
	}
	_, err := f.Write(p)
	return err
}

// Resize sets the terminal size. The process gets SIGWINCH.
func (s *Session) Resize(cols, rows int) error {
	if cols < 2 || rows < 2 || cols > 1000 || rows > 1000 {
		return errors.New("terminal size out of range")
	}
	s.mu.Lock()
	f := s.pty
	exited := s.info.Exited
	s.mu.Unlock()
	if exited || f == nil {
		return nil
	}
	return pty.Setsize(f, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}

// pump copies process output to the replay buffer and every viewer.
func (s *Session) pump() {
	buf := make([]byte, 32<<10)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			chunk := append([]byte(nil), buf[:n]...)
			s.mu.Lock()
			s.replay = append(s.replay, chunk...)
			if over := len(s.replay) - replayBytes; over > 0 {
				s.replay = append([]byte(nil), s.replay[over:]...)
			}
			for sub := range s.subs {
				select {
				case sub.out <- chunk:
				default:
					// Too far behind: drop it; it reattaches with the replay.
					delete(s.subs, sub)
					sub.drop()
				}
			}
			s.mu.Unlock()
		}
		if err != nil {
			// io.EOF, or EIO on Linux once the process closed the terminal.
			close(s.pumped)
			return
		}
	}
}

func (s *Session) wait() {
	err := s.cmd.Wait()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	// Let the last output reach the viewers before reporting the exit. A
	// leftover child can keep the terminal open, so do not wait forever.
	select {
	case <-s.pumped:
	case <-time.After(2 * time.Second):
	}
	s.mu.Lock()
	s.info.Exited = true
	s.info.ExitCode = code
	s.mu.Unlock()
	close(s.done)
}

// update changes the session's info under its lock.
func (s *Session) update(fn func(*SessionInfo)) {
	s.mu.Lock()
	fn(&s.info)
	s.mu.Unlock()
}

// CommandFunc builds the process of a session: `exitbox <Args>` in Dir for
// KindRun, or an exec into Container's tmux for KindAttach.
type CommandFunc func(info SessionInfo) *exec.Cmd

// Manager owns the running sessions.
type Manager struct {
	command CommandFunc
	// onExit runs after a session's process has ended.
	onExit func(*Session)

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager returns a manager that starts processes with command.
func NewManager(command CommandFunc) *Manager {
	return &Manager{command: command, sessions: make(map[string]*Session), onExit: func(*Session) {}}
}

// Start launches a session. args are the exitbox arguments, e.g.
// ["run", "claude", "--name", "x"].
func (m *Manager) Start(info SessionInfo, cols, rows int) (*Session, error) {
	if cols < 2 || rows < 2 || cols > 1000 || rows > 1000 {
		cols, rows = 120, 36
	}
	id, err := newID()
	if err != nil {
		return nil, err
	}
	info.ID = id
	info.Started = time.Now()
	if info.Kind == "" {
		info.Kind = KindRun
	}
	cmd := m.command(info)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	s := &Session{info: info, pty: f, cmd: cmd, subs: make(map[*subscriber]struct{}), pumped: make(chan struct{}), done: make(chan struct{})}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	go s.pump()
	go func() {
		s.wait()
		m.onExit(s)
	}()
	return s, nil
}

// Get returns a session by id.
func (m *Manager) Get(id string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sessions[id]
}

// List returns all sessions, oldest first.
func (m *Manager) List() []SessionInfo {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		list = append(list, s)
	}
	m.mu.Unlock()
	out := make([]SessionInfo, 0, len(list))
	for _, s := range list {
		out = append(out, s.Info())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}

// Close stops a session if it still runs and forgets it.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return errors.New("no such session")
	}
	s.stop(5 * time.Second)
	return nil
}

// Shutdown stops every session.
func (m *Manager) Shutdown(grace time.Duration) {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for id, s := range m.sessions {
		list = append(list, s)
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, s := range list {
		wg.Add(1)
		go func(s *Session) {
			defer wg.Done()
			s.stop(grace)
		}(s)
	}
	wg.Wait()
}

// stop hangs up the process group, then kills it after grace. exitbox run
// stops its container when its terminal hangs up.
func (s *Session) stop(grace time.Duration) {
	select {
	case <-s.done:
	default:
		hangup(s.cmd)
		select {
		case <-s.done:
		case <-time.After(grace):
			kill(s.cmd)
			<-s.done
		}
	}
	s.mu.Lock()
	for sub := range s.subs {
		delete(s.subs, sub)
		sub.drop()
	}
	f := s.pty
	s.mu.Unlock()
	_ = f.Close()
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
