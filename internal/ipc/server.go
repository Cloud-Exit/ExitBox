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

package ipc

import (
	"bufio"
	"encoding/json"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
)

// maxRequestSize is the maximum allowed size of a single IPC request line.
const maxRequestSize = 64 * 1024 // 64 KB

// HandlerFunc processes an IPC request and returns a response payload.
type HandlerFunc func(req *Request) (interface{}, error)

// Server listens on a Unix domain socket and dispatches JSON-lines messages.
type Server struct {
	socketDir  string
	socketPath string
	listener   net.Listener
	handlers   map[string]HandlerFunc
	mu         sync.Mutex // serializes handler calls (one prompt at a time)
	done       chan struct{}
	wg         sync.WaitGroup
}

// NewServer creates a new IPC server with a temporary socket directory.
func NewServer() (*Server, error) {
	dir, err := os.MkdirTemp("", "exitbox-ipc-*")
	if err != nil {
		return nil, err
	}

	socketPath := filepath.Join(dir, "host.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	// Allow non-root container user to connect.
	if err := os.Chmod(socketPath, 0666); err != nil {
		listener.Close()
		os.RemoveAll(dir)
		return nil, err
	}

	return &Server{
		socketDir:  dir,
		socketPath: socketPath,
		listener:   listener,
		handlers:   make(map[string]HandlerFunc),
		done:       make(chan struct{}),
	}, nil
}

// Handle registers a handler for a message type.
// Must be called before Start.
func (s *Server) Handle(msgType string, h HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[msgType] = h
}

// Start begins accepting connections in a background goroutine.
func (s *Server) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				select {
				case <-s.done:
					return
				default:
					continue
				}
			}
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				s.handleConnection(conn)
			}()
		}
	}()
}

// Stop closes the listener, waits for goroutines, and removes the socket dir.
func (s *Server) Stop() {
	close(s.done)
	_ = s.listener.Close()
	s.wg.Wait()
	_ = os.RemoveAll(s.socketDir)
}

// SocketDir returns the directory containing the socket (for container mount).
func (s *Server) SocketDir() string {
	return s.socketDir
}

// ErrorResponse is a generic error payload for IPC responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, maxRequestSize), maxRequestSize)
	if !scanner.Scan() {
		return
	}

	var req Request
	if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
		resp := Response{Type: "error", Payload: ErrorResponse{Error: "invalid request"}}
		data, merr := json.Marshal(resp)
		if merr != nil {
			log.Printf("ipc: failed to marshal error response: %v", merr)
			return
		}
		_, _ = conn.Write(append(data, '\n'))
		return
	}

	s.mu.Lock()
	handler, ok := s.handlers[req.Type]
	if !ok {
		s.mu.Unlock()
		resp := Response{
			Type: req.Type,
			ID:   req.ID,
			Payload: ErrorResponse{
				Error: "unknown message type: " + req.Type,
			},
		}
		data, merr := json.Marshal(resp)
		if merr != nil {
			log.Printf("ipc: failed to marshal error response: %v", merr)
			return
		}
		_, _ = conn.Write(append(data, '\n'))
		return
	}

	// Serialize handler calls so only one TTY prompt runs at a time.
	payload, err := handler(&req)
	s.mu.Unlock()

	resp := Response{
		Type: req.Type,
		ID:   req.ID,
	}
	if err != nil {
		resp.Payload = ErrorResponse{Error: err.Error()}
	} else {
		resp.Payload = payload
	}

	data, merr := json.Marshal(resp)
	if merr != nil {
		log.Printf("ipc: failed to marshal response: %v", merr)
		return
	}
	_, _ = conn.Write(append(data, '\n'))
}
