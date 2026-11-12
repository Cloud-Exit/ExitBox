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
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"
)

// errorSharingViolation is ERROR_SHARING_VIOLATION.
const errorSharingViolation = syscall.Errno(32)

// lockFile opens path without sharing: a second open fails until the
// first handle is closed or its process ends.
func lockFile(path string) (io.Closer, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		if errors.Is(err, errorSharingViolation) {
			return nil, ErrServerRunning
		}
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return os.NewFile(uintptr(h), path), nil
}
