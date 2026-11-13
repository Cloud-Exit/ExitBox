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

//go:build !windows

package webprompt

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// StateDir is where exitbox web keeps its sockets: per user, under the temp
// dir (TMPDIR), as sockets need short paths. It is not created here.
func StateDir() (string, error) {
	return filepath.Join(os.TempDir(), fmt.Sprintf("exitbox-web-%d", os.Getuid())), nil
}

// WellKnownSocket returns the prompt socket of a running exitbox web, if
// it is there and only this user can reach it.
func WellKnownSocket() (string, error) {
	dir, err := StateDir()
	if err != nil {
		return "", err
	}
	if err := checkPrivate(dir, true); err != nil {
		return "", err
	}
	path := filepath.Join(dir, SocketName)
	if err := checkPrivate(path, false); err != nil {
		return "", err
	}
	return path, nil
}

// checkPrivate accepts a directory (or a socket) that is not a symlink, is
// owned by this user and is closed to group and others.
func checkPrivate(path string, dir bool) error {
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if dir && !st.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	if !dir && st.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s is not a socket", path)
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || int(sys.Uid) != os.Getuid() {
		return fmt.Errorf("%s is not owned by you", path)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is open to other users", path)
	}
	return nil
}
