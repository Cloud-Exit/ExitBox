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

package container

import "os/exec"

// Detect finds and returns a container runtime (podman preferred, docker fallback).
// Returns nil if none found.
func Detect() Runtime {
	// Prefer Podman
	if _, err := exec.LookPath("podman"); err == nil {
		return &shellRuntime{cmd: "podman"}
	}
	// Fall back to Docker
	if _, err := exec.LookPath("docker"); err == nil {
		return &shellRuntime{cmd: "docker"}
	}
	return nil
}

// MustDetect returns a runtime or panics if none is found.
func MustDetect() Runtime {
	rt := Detect()
	if rt == nil {
		panic("no container runtime found: install podman or docker")
	}
	return rt
}

// IsAvailable checks if any container runtime is available and operational.
func IsAvailable(rt Runtime) bool {
	if rt == nil {
		return false
	}
	cmd := Cmd(rt)
	return exec.Command(cmd, "info").Run() == nil
}
