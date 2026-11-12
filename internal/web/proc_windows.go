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

//go:build windows

package web

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func hangup(cmd *exec.Cmd) { kill(cmd) }

func kill(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

// DetachProcess is a no-op on Windows, where exitbox web has no sessions.
func DetachProcess(*exec.Cmd) {}

// DefaultStateDir is where session hosts keep their files.
func DefaultStateDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "exitbox-web")
	return dir, os.MkdirAll(dir, 0o700)
}

// isHidden reports whether a directory entry is hidden: marked Hidden, or
// a dotfile as on Unix.
func isHidden(dir, name string) bool {
	if len(name) > 0 && name[0] == '.' {
		return true
	}
	p, err := syscall.UTF16PtrFromString(filepath.Join(dir, name))
	if err != nil {
		return false
	}
	attrs, err := syscall.GetFileAttributes(p)
	if err != nil {
		return false
	}
	return attrs&syscall.FILE_ATTRIBUTE_HIDDEN != 0
}
