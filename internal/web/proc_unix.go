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

package web

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/cloud-exit/exitbox/internal/webprompt"
)

// hangup sends SIGHUP to the session's process group, as closing a
// terminal window does.
func hangup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGHUP)
}

func kill(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// killHostProcess kills a session host and its process group.
func killHostProcess(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// DetachProcess makes cmd a session leader with no terminal, so it is not
// stopped with exitbox web: not by Ctrl+C in its terminal, not by its
// process group going away.
func DetachProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// DefaultStateDir is where session hosts keep their sockets: a directory
// only this user can enter, under the temp dir (sockets need short paths).
// exitbox run finds the prompt socket there too.
func DefaultStateDir() (string, error) {
	dir, err := webprompt.StateDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return "", err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || !ok || int(sys.Uid) != os.Getuid() {
		return "", errors.New(dir + " is not a directory owned by you; remove it and start again")
	}
	if st.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(dir, 0o700); err != nil {
			return "", err
		}
	}
	return dir, nil
}

// isHidden reports whether a directory entry is hidden: a dotfile.
func isHidden(_, name string) bool {
	return len(name) > 0 && name[0] == '.'
}
