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
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"time"
)

// A FIFO named by the config or an Include is skipped, not opened.
func TestSSHConfigSkipsFIFO(t *testing.T) {
	home := t.TempDir()
	ssh := filepath.Join(home, ".ssh")
	writeSSHFile(t, filepath.Join(ssh, "config"), "Include *.conf\nHost alpha\n")
	if err := syscall.Mkfifo(filepath.Join(ssh, "pipe.conf"), 0o600); err != nil {
		t.Fatal(err)
	}
	fifoHome := t.TempDir()
	writeSSHFile(t, filepath.Join(fifoHome, ".ssh", "keep"), "")
	if err := syscall.Mkfifo(filepath.Join(fifoHome, ".ssh", "config"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan []sshHostDoc, 2)
	go func() {
		done <- sshConfigHosts(home)
		done <- sshConfigHosts(fifoHome)
	}()
	for i, want := range [][]sshHostDoc{{{Alias: "alpha"}}, {}} {
		select {
		case got := <-done:
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%d: hosts %+v", i, got)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("parsing blocked on a FIFO")
		}
	}
}
