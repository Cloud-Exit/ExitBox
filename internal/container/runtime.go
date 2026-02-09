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

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runtime is the container runtime interface.
type Runtime interface {
	Name() string
	Build(ctx context.Context, args []string) error
	Run(ctx context.Context, args []string) (int, error)
	Exec(ctx context.Context, ctr string, args []string) error
	ImageExists(image string) bool
	ImageInspect(image, format string) (string, error)
	ImageList(filter string) ([]string, error)
	ImageRemove(image string) error
	PS(filter, format string) ([]string, error)
	Stop(container string) error
	Remove(container string) error
	NetworkCreate(name string, internal bool) error
	NetworkExists(name string) bool
	NetworkConnect(network, container string) error
	NetworkInspect(name, format string) (string, error)
	IsRootless() bool
}

// shellRuntime implements Runtime by shelling out to podman/docker.
type shellRuntime struct {
	cmd string
}

func (r *shellRuntime) Name() string { return r.cmd }

func (r *shellRuntime) Build(ctx context.Context, args []string) error {
	cmdArgs := append([]string{"build"}, args...)
	c := exec.CommandContext(ctx, r.cmd, cmdArgs...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func (r *shellRuntime) Run(ctx context.Context, args []string) (int, error) {
	cmdArgs := append([]string{"run"}, args...)
	c := exec.CommandContext(ctx, r.cmd, cmdArgs...)
	c.Stdin = nil  // will be set by caller if interactive
	c.Stdout = nil // inherit
	c.Stderr = nil
	err := c.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func (r *shellRuntime) Exec(ctx context.Context, ctr string, args []string) error {
	cmdArgs := append([]string{"exec", ctr}, args...)
	return exec.CommandContext(ctx, r.cmd, cmdArgs...).Run()
}

func (r *shellRuntime) ImageExists(image string) bool {
	return exec.Command(r.cmd, "image", "inspect", image).Run() == nil
}

func (r *shellRuntime) ImageInspect(image, format string) (string, error) {
	args := []string{"inspect", image}
	if format != "" {
		args = []string{"inspect", "--format", format, image}
	}
	out, err := exec.Command(r.cmd, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *shellRuntime) ImageList(filter string) ([]string, error) {
	out, err := exec.Command(r.cmd, "images", "--filter", "reference="+filter, "--format", "{{.Repository}}:{{.Tag}}").Output()
	if err != nil {
		return nil, err
	}
	var images []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != "<none>:<none>" {
			images = append(images, line)
		}
	}
	return images, nil
}

func (r *shellRuntime) ImageRemove(image string) error {
	return exec.Command(r.cmd, "rmi", "-f", image).Run()
}

func (r *shellRuntime) PS(filter, format string) ([]string, error) {
	args := []string{"ps"}
	if filter != "" {
		args = append(args, "--filter", filter)
	}
	if format != "" {
		args = append(args, "--format", format)
	}
	out, err := exec.Command(r.cmd, args...).Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var result []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			result = append(result, l)
		}
	}
	return result, nil
}

func (r *shellRuntime) Stop(ctr string) error {
	return exec.Command(r.cmd, "stop", ctr).Run()
}

func (r *shellRuntime) Remove(ctr string) error {
	return exec.Command(r.cmd, "rm", "-f", ctr).Run()
}

func (r *shellRuntime) NetworkCreate(name string, internal bool) error {
	args := []string{"network", "create"}
	if internal {
		args = append(args, "--internal")
	}
	args = append(args, name)
	return exec.Command(r.cmd, args...).Run()
}

func (r *shellRuntime) NetworkExists(name string) bool {
	out, err := exec.Command(r.cmd, "network", "ls", "--format", "{{.Name}}").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.TrimSpace(line) == name {
			return true
		}
	}
	return false
}

func (r *shellRuntime) NetworkConnect(network, ctr string) error {
	return exec.Command(r.cmd, "network", "connect", network, ctr).Run()
}

func (r *shellRuntime) NetworkInspect(name, format string) (string, error) {
	args := []string{"network", "inspect", name}
	if format != "" {
		args = []string{"network", "inspect", "--format", format, name}
	}
	out, err := exec.Command(r.cmd, args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *shellRuntime) IsRootless() bool {
	if r.cmd == "podman" {
		out, err := exec.Command(r.cmd, "info", "--format", "{{.Host.Security.Rootless}}").Output()
		if err == nil && strings.TrimSpace(string(out)) == "true" {
			return true
		}
		return os.Getuid() != 0
	}
	return false
}

// ExecInteractive runs a command with inherited stdio (for interactive use).
func ExecInteractive(rt Runtime, args []string) (int, error) {
	sr, ok := rt.(*shellRuntime)
	if !ok {
		return 1, fmt.Errorf("unsupported runtime type")
	}
	c := exec.Command(sr.cmd, args...)
	c.Stdin = nil  // inherited from parent
	c.Stdout = nil // inherited from parent
	c.Stderr = nil // inherited from parent
	err := c.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

// BuildQuiet runs a build command capturing all output. Returns combined
// output and error. Use this for non-verbose builds with a spinner.
func BuildQuiet(rt Runtime, args []string) (string, error) {
	sr, ok := rt.(*shellRuntime)
	if !ok {
		return "", fmt.Errorf("unsupported runtime type")
	}
	cmdArgs := append([]string{"build"}, args...)
	c := exec.Command(sr.cmd, cmdArgs...)
	out, err := c.CombinedOutput()
	return string(out), err
}

// BuildInteractive runs a build command with inherited stdout/stderr.
func BuildInteractive(rt Runtime, args []string) error {
	sr, ok := rt.(*shellRuntime)
	if !ok {
		return fmt.Errorf("unsupported runtime type")
	}
	cmdArgs := append([]string{"build"}, args...)
	c := exec.Command(sr.cmd, cmdArgs...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// PullQuiet runs a pull command capturing all output. Returns combined
// output and error. Use this for non-verbose pulls with a spinner.
func PullQuiet(rt Runtime, image string) (string, error) {
	sr, ok := rt.(*shellRuntime)
	if !ok {
		return "", fmt.Errorf("unsupported runtime type")
	}
	c := exec.Command(sr.cmd, "pull", image)
	out, err := c.CombinedOutput()
	return string(out), err
}

// PullInteractive runs a pull command with inherited stdout/stderr.
func PullInteractive(rt Runtime, image string) error {
	sr, ok := rt.(*shellRuntime)
	if !ok {
		return fmt.Errorf("unsupported runtime type")
	}
	c := exec.Command(sr.cmd, "pull", image)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// TagImage tags an image with a new name.
func TagImage(rt Runtime, src, dst string) error {
	sr, ok := rt.(*shellRuntime)
	if !ok {
		return fmt.Errorf("unsupported runtime type")
	}
	return exec.Command(sr.cmd, "tag", src, dst).Run()
}

// Cmd returns the raw command name for the runtime.
func Cmd(rt Runtime) string {
	if sr, ok := rt.(*shellRuntime); ok {
		return sr.cmd
	}
	return "docker"
}
