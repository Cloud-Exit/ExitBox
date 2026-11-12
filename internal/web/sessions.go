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
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// replayBytes is how much recent output a session keeps for a browser that
// attaches later. tmux redraws the screen on resize, so this only has to
// cover the gap until then.
const replayBytes = 512 << 10

// subscriberBuffer is how many output chunks a browser may fall behind
// before it is dropped. A dropped browser reconnects and gets the replay.
const subscriberBuffer = 256

// WebSessionEnv names the container variable holding the web session id.
const WebSessionEnv = "EXITBOX_WEB_SESSION"

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

// Session is one session, run by its host process (RunHost) and reached
// over the host's socket. The host outlives exitbox web, so a restart of
// the server reconnects instead of ending the session.
type Session struct {
	m *Manager

	mu     sync.Mutex
	info   SessionInfo
	conn   net.Conn
	replay []byte
	subs   map[*subscriber]struct{}

	wmu      sync.Mutex // serialises frames to the host
	done     chan struct{}
	doneOnce sync.Once
	settled  chan struct{} // closed when the manager's exit hook has run
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

func (s *Session) frame(kind byte, payload []byte) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return errors.New("session host is gone")
	}
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	return writeFrame(conn, kind, payload)
}

// Write sends keyboard input to the process.
func (s *Session) Write(p []byte) error {
	if s.Info().Exited {
		return errors.New("session has ended")
	}
	return s.frame(frameInput, p)
}

// Resize sets the terminal size. The process gets SIGWINCH.
func (s *Session) Resize(cols, rows int) error {
	if cols < 2 || rows < 2 || cols > 1000 || rows > 1000 {
		return errors.New("terminal size out of range")
	}
	if s.Info().Exited {
		return nil
	}
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[:4], uint32(cols))
	binary.BigEndian.PutUint32(b[4:], uint32(rows))
	return s.frame(frameResize, b)
}

// update changes the session's info under its lock and saves it, so a
// restarted server knows the session.
func (s *Session) update(fn func(*SessionInfo)) {
	s.mu.Lock()
	fn(&s.info)
	info := s.info
	s.mu.Unlock()
	s.m.saveInfo(info)
}

// read follows the host: output to the viewers, then the exit.
func (s *Session) read(conn net.Conn) {
	for {
		kind, payload, err := readFrame(conn)
		if err != nil {
			s.mu.Lock()
			ours := s.conn == conn
			s.mu.Unlock()
			// Not ours any more: exitbox web let go on purpose (Disconnect,
			// close). Otherwise the host went away without reporting an
			// exit: it crashed or was killed, and the session is over.
			if ours {
				s.ended(-1)
			}
			return
		}
		switch kind {
		case frameOutput:
			s.mu.Lock()
			s.replay = append(s.replay, payload...)
			if over := len(s.replay) - replayBytes; over > 0 {
				s.replay = append([]byte(nil), s.replay[over:]...)
			}
			for sub := range s.subs {
				select {
				case sub.out <- payload:
				default:
					// Too far behind: drop it; it reattaches with the replay.
					delete(s.subs, sub)
					sub.drop()
				}
			}
			s.mu.Unlock()
		case frameExit:
			code := -1
			if len(payload) == 4 {
				code = int(int32(binary.BigEndian.Uint32(payload)))
			}
			s.ended(code)
		}
	}
}

// ended records the exit once and runs the manager's exit hook.
func (s *Session) ended(code int) {
	s.doneOnce.Do(func() {
		s.update(func(i *SessionInfo) {
			if !i.Exited {
				i.Exited, i.ExitCode = true, code
			}
		})
		close(s.done)
		go func() {
			defer close(s.settled)
			s.m.onExit(s)
		}()
	})
}

// CommandFunc builds the process of a session: `exitbox <Args>` in Dir for
// KindRun, or an exec into Container's tmux for KindAttach.
type CommandFunc func(info SessionInfo) *exec.Cmd

// SpawnFunc starts the host of session id in dir, detached from exitbox
// web (RunHost in another process). Tests run it in-process.
type SpawnFunc func(dir, id string) error

// Manager owns the sessions: their hosts, files and connections.
type Manager struct {
	command CommandFunc
	spawn   SpawnFunc
	dir     string
	// onExit runs after a session's process has ended.
	onExit func(*Session)

	mu       sync.Mutex
	sessions map[string]*Session
}

// NewManager returns a manager keeping its session files in dir.
func NewManager(command CommandFunc, spawn SpawnFunc, dir string) *Manager {
	return &Manager{command: command, spawn: spawn, dir: dir, sessions: make(map[string]*Session), onExit: func(*Session) {}}
}

func (m *Manager) saveInfo(info SessionInfo) {
	info.Viewers = 0
	data, err := json.Marshal(info)
	if err != nil {
		return
	}
	tmp := infoPath(m.dir, info.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, infoPath(m.dir, info.ID))
}

// Start launches a session in a new host. args are the exitbox arguments,
// e.g. ["run", "claude", "--name", "x"].
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
	run := info
	if info.Kind == KindRun {
		// The container carries the session id, so the server can tell
		// which container is this session's, also after a relaunch.
		run.Args = append(append([]string(nil), info.Args...), "--env", WebSessionEnv+"="+id)
	}
	cmd := m.command(run)
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	spec, err := json.Marshal(hostSpec{Path: cmd.Path, Args: cmd.Args, Env: env, Dir: cmd.Dir, Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(specPath(m.dir, id), spec, 0o600); err != nil {
		return nil, err
	}
	s := m.newSession(info)
	m.saveInfo(info)
	if err := m.spawn(m.dir, id); err != nil {
		m.forget(id)
		return nil, fmt.Errorf("starting the session host: %w", err)
	}
	// The host listens once its process runs.
	var conn net.Conn
	for i := 0; i < 200; i++ {
		if conn, err = net.Dial("unix", sockPath(m.dir, id)); err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		m.forget(id)
		return nil, fmt.Errorf("the session did not start: %w", m.hostError(id, err))
	}
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	s.connect(conn)
	return s, nil
}

// hostError prefers what the host wrote to its log.
func (m *Manager) hostError(id string, err error) error {
	if data, rerr := os.ReadFile(logPath(m.dir, id)); rerr == nil && len(strings.TrimSpace(string(data))) > 0 {
		return errors.New(strings.TrimSpace(string(data)))
	}
	return err
}

func (m *Manager) newSession(info SessionInfo) *Session {
	return &Session{m: m, info: info, subs: map[*subscriber]struct{}{}, done: make(chan struct{}), settled: make(chan struct{})}
}

func (s *Session) connect(conn net.Conn) {
	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()
	go s.read(conn)
}

// Restore reconnects to the hosts left by an earlier exitbox web and
// removes the files of hosts that are gone.
func (m *Manager) Restore() (int, error) {
	paths, err := filepath.Glob(filepath.Join(m.dir, "*.info.json"))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, p := range paths {
		id := strings.TrimSuffix(filepath.Base(p), ".info.json")
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var info SessionInfo
		if err := json.Unmarshal(data, &info); err != nil || info.ID != id {
			continue
		}
		// The host reports an exit itself right after hello.
		info.Exited, info.ExitCode = false, 0
		conn, err := net.Dial("unix", sockPath(m.dir, id))
		if err != nil {
			m.forget(id)
			continue
		}
		s := m.newSession(info)
		m.mu.Lock()
		m.sessions[id] = s
		m.mu.Unlock()
		s.connect(conn)
		n++
	}
	return n, nil
}

// forget removes a session's files when its host never came up or is gone.
func (m *Manager) forget(id string) {
	for _, p := range []string{sockPath(m.dir, id), specPath(m.dir, id), infoPath(m.dir, id), logPath(m.dir, id)} {
		_ = os.Remove(p)
	}
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

// Close ends a session for good: its window was closed. The process is
// hung up if it still runs, and the host removes itself.
func (m *Manager) Close(id string) error {
	m.mu.Lock()
	s := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if s == nil {
		return errors.New("no such session")
	}
	s.close()
	return nil
}

func (s *Session) close() {
	if err := s.frame(frameStop, nil); err == nil {
		select {
		case <-s.done:
		case <-time.After(15 * time.Second):
		}
	}
	// A rename asked for while the session ran happens in the exit hook.
	select {
	case <-s.settled:
	case <-time.After(10 * time.Second):
	}
	_ = s.frame(frameQuit, nil)
	// The server writes the info file, so it removes it too.
	_ = os.Remove(infoPath(s.m.dir, s.Info().ID))
	s.mu.Lock()
	for sub := range s.subs {
		delete(s.subs, sub)
		sub.drop()
	}
	conn := s.conn
	s.conn = nil
	s.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

// Disconnect lets go of every host without stopping it: exitbox web is
// going away and the next one takes the sessions over.
func (m *Manager) Disconnect() {
	m.mu.Lock()
	list := make([]*Session, 0, len(m.sessions))
	for id, s := range m.sessions {
		list = append(list, s)
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	for _, s := range list {
		s.mu.Lock()
		conn := s.conn
		s.conn = nil
		for sub := range s.subs {
			delete(s.subs, sub)
			sub.drop()
		}
		s.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
	}
}

// CloseAll ends every session, as closing every window would.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			_ = m.Close(id)
		}(id)
	}
	wg.Wait()
}

func newID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
