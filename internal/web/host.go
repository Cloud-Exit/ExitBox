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
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// hostSpec is what a session host runs, written by exitbox web.
type hostSpec struct {
	Path string   `json:"path"`
	Args []string `json:"args"`
	Env  []string `json:"env"`
	Dir  string   `json:"dir"`
	Cols int      `json:"cols"`
	Rows int      `json:"rows"`
}

// hostIdleExit is how long a host keeps an ended session that nobody
// looks at before it gives up and removes itself.
const hostIdleExit = 24 * time.Hour

func specPath(dir, id string) string { return filepath.Join(dir, id+".spec.json") }
func infoPath(dir, id string) string { return filepath.Join(dir, id+".info.json") }
func sockPath(dir, id string) string { return filepath.Join(dir, id+".sock") }
func logPath(dir, id string) string  { return filepath.Join(dir, id+".log") }

// RunHost runs one session: the process on its pseudo terminal, and a Unix
// socket exitbox web connects to. It outlives exitbox web, so restarting
// the server leaves sessions running. It returns when the window is closed.
func RunHost(dir, id string) error {
	data, err := os.ReadFile(specPath(dir, id))
	if err != nil {
		return err
	}
	// The spec holds the server's environment: keep it only in memory.
	if err := os.Remove(specPath(dir, id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var spec hostSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return err
	}
	// Catch hang-ups ourselves. A handled signal is reset for the child on
	// exec, while an ignored one (nohup, some service managers) would stay
	// ignored, and closing the window could then only kill the session.
	// The host itself ends only when its window is closed.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigs)
	cmd := exec.Command(spec.Path)
	cmd.Args = spec.Args
	cmd.Env = spec.Env
	cmd.Dir = spec.Dir
	h := &host{dir: dir, id: id, cmd: cmd, quit: make(chan struct{}), exited: make(chan struct{}), closed: make(chan struct{}), input: make(chan []byte, hostInputQueue)}
	// On failure the log stays: the error is printed into it after we
	// return, and exitbox web shows it.
	if h.pty, err = pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(spec.Cols), Rows: uint16(spec.Rows)}); err != nil {
		h.cleanup(true)
		return err
	}
	_ = os.Remove(sockPath(dir, id))
	ln, err := net.Listen("unix", sockPath(dir, id))
	if err != nil {
		kill(cmd)
		h.cleanup(true)
		return err
	}
	// Closing the listener would unlink the socket at once; cleanup removes
	// it last instead, so its presence means the session's files exist.
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	if err := os.Chmod(sockPath(dir, id), 0o600); err != nil {
		_ = ln.Close()
		kill(cmd)
		h.cleanup(true)
		return err
	}
	go h.pump()
	go h.writeInput()
	go h.wait()
	go h.accept(ln)

	idle := time.NewTimer(hostIdleExit)
	defer idle.Stop()
	for {
		select {
		case <-h.quit:
			_ = ln.Close()
			h.stop(5 * time.Second)
			h.cleanup(false)
			return nil
		case <-idle.C:
			h.mu.Lock()
			alone := h.client == nil
			h.mu.Unlock()
			select {
			case <-h.exited:
				if alone {
					_ = ln.Close()
					h.cleanup(false)
					return nil
				}
			default:
			}
			idle.Reset(hostIdleExit)
		}
	}
}

type host struct {
	dir, id string
	cmd     *exec.Cmd
	pty     *os.File

	mu       sync.Mutex // guards replay, client, code, exitAt
	replay   []byte
	client   net.Conn
	code     int
	exitAt   time.Time
	served   bool // a server has connected before
	exited   chan struct{}
	quit     chan struct{}
	quitOnce sync.Once

	// input queues keys for the terminal, so a process that does not read
	// them holds up only the writer, not resize, stop and quit.
	input     chan []byte
	closed    chan struct{} // closed by cleanup
	closeOnce sync.Once
}

// hostInputQueue is how many input frames may wait for the terminal before
// the frame reader waits too.
const hostInputQueue = 64

func (h *host) writeInput() {
	for {
		select {
		case p := <-h.input:
			if _, err := h.pty.Write(p); err != nil && errors.Is(err, os.ErrClosed) {
				return
			}
		case <-h.closed:
			return
		}
	}
}

// send writes a frame to the connected server, dropping it on error.
// Callers hold h.mu.
func (h *host) send(kind byte, payload []byte) {
	if h.client == nil {
		return
	}
	_ = h.client.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if err := writeFrame(h.client, kind, payload); err != nil {
		_ = h.client.Close()
		h.client = nil
	}
}

func (h *host) pump() {
	buf := make([]byte, 32<<10)
	for {
		n, err := h.pty.Read(buf)
		if n > 0 {
			h.mu.Lock()
			h.replay = append(h.replay, buf[:n]...)
			if over := len(h.replay) - replayBytes; over > 0 {
				h.replay = append([]byte(nil), h.replay[over:]...)
			}
			h.send(frameOutput, buf[:n])
			h.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (h *host) wait() {
	err := h.cmd.Wait()
	at := time.Now()
	code := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}
	// Let the last output out first; a leftover child may hold the terminal.
	time.Sleep(200 * time.Millisecond)
	h.mu.Lock()
	h.code, h.exitAt = code, at
	close(h.exited)
	h.sendExit()
	h.mu.Unlock()
}

// sendExit reports the exit and its time. Callers hold h.mu.
func (h *host) sendExit() {
	h.send(frameExitAt, exitAtPayload(h.exitAt))
	h.send(frameExit, exitPayload(h.code))
}

func exitPayload(code int) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, uint32(int32(code)))
	return b
}

func exitAtPayload(t time.Time) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(t.Unix()))
	return b
}

// accept serves exitbox web. A new connection replaces the old one: after
// a restart the new server takes over.
func (h *host) accept(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		h.mu.Lock()
		if h.client != nil {
			_ = h.client.Close()
		}
		h.client = conn
		h.send(frameHello, nil)
		if len(h.replay) > 0 {
			// The first server gets it as fresh output: nobody has
			// answered its queries yet. A later one gets a replay.
			kind := byte(frameOutput)
			if h.served {
				kind = frameReplay
			}
			h.send(kind, h.replay)
		}
		h.served = true
		select {
		case <-h.exited:
			h.sendExit()
		default:
		}
		h.mu.Unlock()
		go h.serve(conn)
	}
}

func (h *host) serve(conn net.Conn) {
	for {
		kind, payload, err := readFrame(conn)
		if err != nil {
			h.mu.Lock()
			if h.client == conn {
				h.client = nil
			}
			h.mu.Unlock()
			_ = conn.Close()
			return
		}
		switch kind {
		case frameInput:
			select {
			case h.input <- payload:
			case <-h.closed:
				_ = conn.Close()
				return
			}
		case frameResize:
			if len(payload) == 8 {
				c, r := binary.BigEndian.Uint32(payload[:4]), binary.BigEndian.Uint32(payload[4:])
				if c >= 2 && r >= 2 && c <= 1000 && r <= 1000 {
					_ = pty.Setsize(h.pty, &pty.Winsize{Cols: uint16(c), Rows: uint16(r)})
				}
			}
		case frameStop:
			go h.stop(5 * time.Second)
		case frameQuit:
			h.quitOnce.Do(func() { close(h.quit) })
		}
	}
}

// stop hangs up the process group, as closing a terminal does, and kills
// it after grace.
func (h *host) stop(grace time.Duration) {
	select {
	case <-h.exited:
		return
	default:
	}
	hangup(h.cmd)
	select {
	case <-h.exited:
	case <-time.After(grace):
		kill(h.cmd)
		select {
		case <-h.exited:
		case <-time.After(5 * time.Second):
		}
	}
}

// cleanup closes the terminal and removes the session's files; keepLog
// leaves the log for exitbox web to read why the host failed.
func (h *host) cleanup(keepLog bool) {
	h.closeOnce.Do(func() { close(h.closed) })
	if h.pty != nil {
		_ = h.pty.Close()
	}
	files := []string{specPath(h.dir, h.id), infoPath(h.dir, h.id)}
	if !keepLog {
		// A failed start leaves the pid for exitbox web to kill the host by.
		files = append(files, logPath(h.dir, h.id), pidPath(h.dir, h.id))
	}
	// The socket goes last: while it exists, the session's files do too.
	files = append(files, sockPath(h.dir, h.id))
	for _, p := range files {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "exitbox web host: %v\n", err)
		}
	}
}
