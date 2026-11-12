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
	"errors"
	"io"
	"path/filepath"
)

// ErrServerRunning is returned by LockStateDir while another exitbox web
// serves the same sessions.
var ErrServerRunning = errors.New("another exitbox web is already running for this user (stop it first, or exitbox web --remove-service if it runs as a service)")

func lockPath(dir string) string { return filepath.Join(dir, "server.lock") }

// LockStateDir takes the session state directory for one server: two
// servers would take each other's sessions. Close the result to let go;
// the lock also ends with the process.
func LockStateDir(dir string) (io.Closer, error) { return lockFile(lockPath(dir)) }

// Lock takes the server's state directory; see LockStateDir.
func (s *Server) Lock() (io.Closer, error) { return LockStateDir(s.opts.StateDir) }
