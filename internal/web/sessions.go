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
	"bytes"
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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloud-exit/exitbox/internal/webprompt"
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
	KindSSH    = "ssh"    // the system ssh client to one of the user's machines
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
	// Ended is when the process exited; kept across a restore.
	Ended   *time.Time `json:"ended,omitempty"`
	Viewers int        `json:"viewers"`
	// Container is the agent container, once it is known.
	Container string `json:"container,omitempty"`
	// RenameTo is a name the session takes when it ends.
	RenameTo    string `json:"renameTo,omitempty"`
	RenameError string `json:"renameError,omitempty"`
	// Request is how a run session was started here; nil for attached
	// sessions and those saved before it was kept.
	Request *RunOptions `json:"request,omitempty"`
	// SSH is how an SSH session connects; Agent is empty for those.
	SSH *SSHOptions `json:"ssh,omitempty"`
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
	// queries lets one browser answer each terminal query.
	queries queryTracker
	// exitAt is when the host saw the process exit, once it said so.
	exitAt *time.Time

	// gone: this server let go of the session (Disconnect, close); no
	// more reconnects or info writes.
	gone bool

	wmu      sync.Mutex // serialises frames to the host
	done     chan struct{}
	doneOnce sync.Once
	settled  chan struct{} // closed when the manager's exit hook has run

	saveMu    sync.Mutex // serialises info writes
	lastSaved []byte
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
	// Non-nil, so the page gets [] rather than null.
	info.Args = append([]string{}, s.info.Args...)
	if s.info.Request != nil {
		r := *s.info.Request
		r.AllowURLs = append([]string{}, r.AllowURLs...)
		info.Request = &r
	}
	if s.info.SSH != nil {
		o := *s.info.SSH
		info.SSH = &o
	}
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

// hostWriteTimeout bounds one frame to a host; a var for tests.
var hostWriteTimeout = 10 * time.Second

func (s *Session) frame(kind byte, payload []byte) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return errors.New("session host is gone")
	}
	s.wmu.Lock()
	err := conn.SetWriteDeadline(time.Now().Add(hostWriteTimeout))
	if err == nil {
		err = writeFrame(conn, kind, payload)
	}
	s.wmu.Unlock()
	if err != nil {
		// Part of the frame may have gone out: the host would read the rest
		// of the stream from the middle of a payload.
		s.lost(conn)
	}
	return err
}

// lost drops a connection that can no longer carry frames and dials the
// host again, as a restarted server would. A host that does not answer is
// gone, and so is the session.
func (s *Session) lost(conn net.Conn) {
	s.mu.Lock()
	if s.conn != conn || s.gone {
		s.mu.Unlock()
		return
	}
	s.conn = nil
	// The host sends its replay on the new connection; viewers reattach to it.
	s.replay = nil
	for sub := range s.subs {
		delete(s.subs, sub)
		sub.drop()
	}
	id := s.info.ID
	s.mu.Unlock()
	_ = conn.Close()
	nc, err := net.DialTimeout("unix", sockPath(s.m.dir, id), 2*time.Second)
	if err != nil {
		s.m.logf("web: session %s: host unreachable after a failed write: %v", id, err)
		s.ended(-1)
		return
	}
	s.mu.Lock()
	if s.gone || s.conn != nil {
		s.mu.Unlock()
		_ = nc.Close()
		return
	}
	s.conn = nc
	s.mu.Unlock()
	go s.read(nc)
}

// Write sends keyboard input to the process.
func (s *Session) Write(p []byte) error {
	if s.Info().Exited {
		return errors.New("session has ended")
	}
	return s.frame(frameInput, p)
}

// Input sends what a browser typed. Answers to terminal queries pass only
// when the output asked and no other browser answered first.
func (s *Session) Input(p []byte) error {
	p = s.queries.filterInput(p)
	if len(p) == 0 {
		return nil
	}
	return s.Write(p)
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
	s.mu.Unlock()
	s.save()
}

// save writes the session's current info. Writes are serialised and each
// takes the latest state, so an older snapshot never lands last.
func (s *Session) save() {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	gone := s.gone
	info := s.info
	s.mu.Unlock()
	if gone {
		return
	}
	info.Viewers = 0
	data, err := json.Marshal(info)
	if err != nil {
		s.m.logf("web: session %s: %v", info.ID, err)
		return
	}
	if bytes.Equal(data, s.lastSaved) {
		return
	}
	if err := s.m.writeInfo(info.ID, data); err != nil {
		s.m.logf("web: saving session %s: %v", info.ID, err)
		return
	}
	s.lastSaved = data
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
		case frameOutput, frameReplay:
			// Count the queries before any browser sees them. The host's
			// replay (after a restart of exitbox web) holds queries that
			// were answered long ago: answers to those are dropped.
			if kind == frameOutput {
				s.queries.sawOutput(payload)
			}
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
		case frameExitAt:
			if len(payload) == 8 {
				t := time.Unix(int64(binary.BigEndian.Uint64(payload)), 0).UTC()
				s.mu.Lock()
				s.exitAt = &t
				s.mu.Unlock()
			}
		case frameExit:
			code := -1
			if len(payload) == 4 {
				code = int(int32(binary.BigEndian.Uint32(payload)))
			}
			s.ended(code)
		}
	}
}

// ended records the exit once and runs the manager's exit hook. The exit
// time is the host's when it sent one (a host from an older binary does
// not), else now.
func (s *Session) ended(code int) {
	s.doneOnce.Do(func() {
		s.update(func(i *SessionInfo) {
			if !i.Exited {
				i.Exited, i.ExitCode = true, code
			}
			if i.Ended == nil {
				t := time.Now().UTC().Truncate(time.Second)
				if s.exitAt != nil {
					t = *s.exitAt
				}
				i.Ended = &t
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
// KindRun, an exec into Container's tmux for KindAttach, or `ssh <Args>`
// for KindSSH.
type CommandFunc func(info SessionInfo) *exec.Cmd

// SpawnFunc starts the host of session id in dir, detached from exitbox
// web (RunHost in another process). Tests run it in-process. The channel,
// which may be nil, closes when the host has exited. A host process should
// be recorded with WriteHostPID so Start can stop one that never listens.
type SpawnFunc func(dir, id string) (<-chan struct{}, error)

func pidPath(dir, id string) string { return filepath.Join(dir, id+".pid") }

// WriteHostPID records the process of session id's host, the leader of its
// process group on Unix.
func WriteHostPID(dir, id string, pid int) error {
	return os.WriteFile(pidPath(dir, id), []byte(strconv.Itoa(pid)), 0o600)
}

// Manager owns the sessions: their hosts, files and connections.
type Manager struct {
	command CommandFunc
	spawn   SpawnFunc
	dir     string
	// startTimeout is how long a new host has to listen.
	startTimeout time.Duration
	// onExit runs after a session's process has ended.
	onExit func(*Session)
	logf   func(format string, args ...any)

	mu       sync.Mutex
	sessions map[string]*Session
	// promptSock is where run sessions send their host prompts; empty
	// keeps them in the terminal.
	promptSock string
}

func (m *Manager) setPromptSocket(path string) {
	m.mu.Lock()
	m.promptSock = path
	m.mu.Unlock()
}

// NewManager returns a manager keeping its session files in dir.
func NewManager(command CommandFunc, spawn SpawnFunc, dir string) *Manager {
	return &Manager{command: command, spawn: spawn, dir: dir, startTimeout: hostStartTimeout, sessions: make(map[string]*Session), onExit: func(*Session) {}, logf: func(string, ...any) {}}
}

// writeInfo replaces a session's info file through a temp file of its own,
// so concurrent writers never share one.
func (m *Manager) writeInfo(id string, data []byte) error {
	f, err := os.CreateTemp(m.dir, id+".info.*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, infoPath(m.dir, id))
	}
	if err != nil {
		if rerr := os.Remove(tmp); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			m.logf("web: %v", rerr)
		}
	}
	return err
}

// Start launches a session in a new host. args are the exitbox arguments,
// e.g. ["run", "claude", "--name", "x"]. extra are exitbox arguments of a
// run session for this start only: they are not kept in its info.
func (m *Manager) Start(info SessionInfo, cols, rows int, extra ...string) (*Session, error) {
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
		run.Args = append(append(append([]string(nil), info.Args...), extra...), "--env", WebSessionEnv+"="+id)
	}
	cmd := m.command(run)
	if cmd.Err != nil {
		// The program was not found: say so instead of a failed host.
		return nil, cmd.Err
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	env = withPromptEnv(env, info.Kind, m.promptSocket(), id)
	spec, err := json.Marshal(hostSpec{Path: cmd.Path, Args: cmd.Args, Env: env, Dir: cmd.Dir, Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(specPath(m.dir, id), spec, 0o600); err != nil {
		return nil, err
	}
	s := m.newSession(info)
	s.save()
	exited, err := m.spawn(m.dir, id)
	if err != nil {
		m.forget(id)
		return nil, fmt.Errorf("starting the session host: %w", err)
	}
	conn, err := m.dialNew(id, exited)
	if err != nil {
		// Read the host's log before forget removes it.
		err = m.hostError(id, err)
		// A host still starting would run its agent with nobody attached.
		m.killHost(id, exited)
		m.forget(id)
		return nil, fmt.Errorf("the session did not start: %w", err)
	}
	// The pid only serves to kill a host that never listens.
	m.removePID(id)
	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()
	s.connect(conn)
	return s, nil
}

func (m *Manager) removePID(id string) {
	if err := os.Remove(pidPath(m.dir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.logf("web: %v", err)
	}
}

func (m *Manager) promptSocket() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.promptSock
}

// withPromptEnv points a run session's exitbox run at the prompt socket.
// The variables stay on the host: exitbox run passes the container only
// what it names.
func withPromptEnv(env []string, kind, sock, id string) []string {
	out := make([]string, 0, len(env)+2)
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		if k == webprompt.EnvSocket || k == webprompt.EnvSession {
			continue
		}
		out = append(out, kv)
	}
	if kind == KindRun && sock != "" {
		out = append(out, webprompt.EnvSocket+"="+sock, webprompt.EnvSession+"="+id)
	}
	return out
}

// hostStartTimeout is how long a new host has to listen.
const hostStartTimeout = 5 * time.Second

// killHost stops the host recorded for id unless it has already exited.
func (m *Manager) killHost(id string, exited <-chan struct{}) {
	select {
	case <-exited:
		return
	default:
	}
	data, err := os.ReadFile(pidPath(m.dir, id))
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			m.logf("web: %v", err)
		}
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		m.logf("web: bad host pid file for session %s", id)
		return
	}
	if err := killHostProcess(pid); err != nil {
		m.logf("web: stopping the host of session %s: %v", id, err)
	}
}

// dialNew connects to a new host once it listens, and gives up as soon as
// its process has exited.
func (m *Manager) dialNew(id string, exited <-chan struct{}) (net.Conn, error) {
	deadline := time.Now().Add(m.startTimeout)
	for {
		conn, err := net.Dial("unix", sockPath(m.dir, id))
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-exited:
			return nil, errors.New("the session host exited")
		case <-time.After(25 * time.Millisecond):
		}
	}
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
	// Left by a server that died mid-write; no other server writes now.
	tmps, err := filepath.Glob(filepath.Join(m.dir, "*.info.*.tmp"))
	if err != nil {
		return 0, err
	}
	for _, p := range tmps {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			m.logf("web: %v", err)
		}
	}
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
	for _, p := range []string{sockPath(m.dir, id), specPath(m.dir, id), infoPath(m.dir, id), logPath(m.dir, id), pidPath(m.dir, id)} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			m.logf("web: %v", err)
		}
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

// stop hangs up the process and waits for it to end, keeping the session.
func (s *Session) stop() error {
	if err := s.frame(frameStop, nil); err != nil {
		select {
		case <-s.done:
			// It had ended already.
		default:
			return err
		}
	}
	select {
	case <-s.done:
	case <-time.After(15 * time.Second):
		return errors.New("the session did not end")
	}
	// A rename asked for while the session ran happens in the exit hook.
	select {
	case <-s.settled:
	case <-time.After(10 * time.Second):
	}
	return nil
}

func (s *Session) close() {
	id := s.Info().ID
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
	quitErr := s.frame(frameQuit, nil)
	if quitErr != nil {
		// A failed write reconnects if the host still listens: try once more.
		quitErr = s.frame(frameQuit, nil)
	}
	s.saveMu.Lock()
	s.mu.Lock()
	s.gone = true
	s.mu.Unlock()
	if quitErr != nil {
		// The host is gone (killed, crashed): nobody else removes its files.
		s.m.forget(id)
	} else {
		if err := os.Remove(infoPath(s.m.dir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
			// The server writes the info file, so it removes it too.
			s.m.logf("web: %v", err)
		}
		// Left by an older server, which kept it for the session's life.
		s.m.removePID(id)
	}
	s.saveMu.Unlock()
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
		s.gone = true
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
