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

package run

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/network"
	"github.com/cloud-exit/exitbox/internal/project"
	"github.com/cloud-exit/exitbox/internal/statusbar"
	"github.com/cloud-exit/exitbox/internal/ui"
	"golang.org/x/term"
)

// Options holds all the flags for running a container.
type Options struct {
	Agent       string
	ProjectDir  string
	NoFirewall  bool
	ReadOnly    bool
	NoEnv       bool
	EnvVars     []string
	IncludeDirs []string
	AllowURLs   []string
	Passthrough []string
	Verbose     bool
	StatusBar   bool
	Version     string
}

// AgentContainer runs an agent container interactively.
func AgentContainer(rt container.Runtime, opts Options) (int, error) {
	cmd := container.Cmd(rt)
	imageName := project.ImageName(opts.Agent, opts.ProjectDir)
	containerName := project.ContainerName(opts.Agent, opts.ProjectDir)

	var args []string

	// Interactive mode
	if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		args = append(args, "-it")
	}
	args = append(args, "--rm", "--name", containerName, "--init")

	// Podman-specific
	if cmd == "podman" {
		args = append(args, "--userns=keep-id", "--security-opt=no-new-privileges")
	}

	// Network setup
	network.EnsureNetworks(rt)
	agentNetwork := network.InternalNetwork
	if opts.NoFirewall {
		agentNetwork = network.EgressNetwork
	}
	args = append(args, "--network", agentNetwork)

	// Firewall / proxy
	if !opts.NoFirewall {
		if err := network.StartSquidProxy(rt, opts.AllowURLs); err != nil {
			return 1, fmt.Errorf("failed to start firewall (Squid proxy): %w", err)
		}
		proxyArgs := network.GetProxyEnvVars(rt)
		args = append(args, proxyArgs...)
	}

	// Resource limits
	args = append(args, "--memory=8g", "--cpus=4")

	// Mount workspace
	mountMode := ""
	if opts.ReadOnly {
		mountMode = ":ro"
	}
	args = append(args, "-w", "/workspace", "-v", opts.ProjectDir+":/workspace"+mountMode)

	// Non-root
	args = append(args, "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()))

	// Include dirs
	for _, dir := range opts.IncludeDirs {
		dir = expandPath(dir, opts.ProjectDir)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			ui.Warnf("Include dir not found: %s", dir)
			continue
		}
		dir = strings.TrimSuffix(dir, "/")
		rel := strings.TrimPrefix(dir, "/")
		args = append(args, "-v", dir+":/workspace/"+rel)
	}

	// Agent-specific config mounts
	agentCfgDir := config.AgentDir(opts.Agent)
	os.MkdirAll(agentCfgDir, 0755)

	mounts := resolveAgentMounts(opts.Agent, agentCfgDir)
	args = append(args, mounts...)

	// Environment variables
	projectName := filepath.Base(opts.ProjectDir)
	if !opts.NoEnv {
		args = append(args,
			"-e", "NODE_ENV="+getEnvOr("NODE_ENV", "production"),
			"-e", "TERM="+getEnvOr("TERM", "xterm-256color"),
			"-e", "VERBOSE="+fmt.Sprint(opts.Verbose),
		)
	}

	// User env vars
	for _, ev := range opts.EnvVars {
		if !strings.Contains(ev, "=") {
			return 1, fmt.Errorf("invalid -e value '%s'. Expected KEY=VALUE", ev)
		}
		key := ev[:strings.Index(ev, "=")]
		if isReservedEnvVar(key) {
			return 1, fmt.Errorf("environment variable '%s' is reserved", key)
		}
		args = append(args, "-e", ev)
	}

	// Internal vars
	args = append(args,
		"-e", "EXITBOX_AGENT="+opts.Agent,
		"-e", "EXITBOX_PROJECT_NAME="+projectName,
	)

	// Security options
	args = append(args,
		"--security-opt=no-new-privileges:true",
		"--cap-drop=ALL",
	)

	// Image
	args = append(args, imageName)

	// Passthrough args
	args = append(args, opts.Passthrough...)

	if opts.Verbose {
		ui.Debugf("Container run: %s run %s", cmd, strings.Join(args, " "))
	}

	// Status bar
	if opts.StatusBar {
		statusbar.Show(opts.Version, opts.Agent)
	}

	// Run with inherited stdio
	c := exec.Command(cmd, append([]string{"run"}, args...)...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	err := c.Run()

	statusbar.Hide()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	network.CleanupSquidIfUnused(rt)
	return exitCode, nil
}

func resolveAgentMounts(agent, cfgDir string) []string {
	home := os.Getenv("HOME")
	var args []string

	switch agent {
	case "claude":
		claudeDir := ensureDir(cfgDir, ".claude")
		seedDirOnce(filepath.Join(home, ".claude"), claudeDir)
		args = append(args, "-v", claudeDir+":/home/user/.claude")

		claudeJSON := ensureFile(cfgDir, ".claude.json")
		seedFileOnce(filepath.Join(home, ".claude.json"), claudeJSON)
		args = append(args, "-v", claudeJSON+":/home/user/.claude.json")

		configDir := ensureDir(cfgDir, ".config")
		args = append(args, "-v", configDir+":/home/user/.config")

	case "codex":
		codexDir := ensureDir(cfgDir, ".codex")
		seedDirOnce(filepath.Join(home, ".codex"), codexDir)
		args = append(args, "-v", codexDir+":/home/user/.codex")

		codexCfgDir := ensureDir(cfgDir, ".config", "codex")
		seedDirOnce(filepath.Join(home, ".config", "codex"), codexCfgDir)
		args = append(args, "-v", codexCfgDir+":/home/user/.config/codex")

	case "opencode":
		ocDir := ensureDir(cfgDir, ".opencode")
		seedDirOnce(filepath.Join(home, ".opencode"), ocDir)
		args = append(args, "-v", ocDir+":/home/user/.opencode")

		ocCfgDir := ensureDir(cfgDir, ".config", "opencode")
		seedDirOnce(filepath.Join(home, ".config", "opencode"), ocCfgDir)
		args = append(args, "-v", ocCfgDir+":/home/user/.config/opencode")

		ocShareDir := ensureDir(cfgDir, ".local", "share", "opencode")
		seedDirOnce(filepath.Join(home, ".local", "share", "opencode"), ocShareDir)
		args = append(args, "-v", ocShareDir+":/home/user/.local/share/opencode")

		ocStateDir := ensureDir(cfgDir, ".local", "state")
		args = append(args, "-v", ocStateDir+":/home/user/.local/state")

		ocCacheDir := ensureDir(cfgDir, ".cache", "opencode")
		args = append(args, "-v", ocCacheDir+":/home/user/.cache/opencode")
	}

	return args
}

func ensureDir(parts ...string) string {
	p := filepath.Join(parts...)
	os.MkdirAll(p, 0755)
	return p
}

func ensureFile(parts ...string) string {
	p := filepath.Join(parts...)
	os.MkdirAll(filepath.Dir(p), 0755)
	if _, err := os.Stat(p); os.IsNotExist(err) {
		_ = os.WriteFile(p, nil, 0644)
	}
	return p
}

func seedDirOnce(host, managed string) {
	if _, err := os.Stat(host); os.IsNotExist(err) {
		return
	}
	entries, err := os.ReadDir(managed)
	if err == nil && len(entries) > 0 {
		return
	}
	os.MkdirAll(managed, 0755)
	_ = exec.Command("cp", "-R", host+"/.", managed+"/").Run()
}

func seedFileOnce(host, managed string) {
	if _, err := os.Stat(host); os.IsNotExist(err) {
		return
	}
	if _, err := os.Stat(managed); err == nil {
		return
	}
	os.MkdirAll(filepath.Dir(managed), 0755)
	data, err := os.ReadFile(host)
	if err == nil {
		os.WriteFile(managed, data, 0644)
	}
}

func expandPath(dir, projectDir string) string {
	if strings.HasPrefix(dir, "~/") {
		dir = filepath.Join(os.Getenv("HOME"), dir[2:])
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(projectDir, dir)
	}
	return dir
}

func getEnvOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func isReservedEnvVar(key string) bool {
	reserved := map[string]bool{
		"EXITBOX_AGENT":        true,
		"EXITBOX_PROJECT_NAME": true,
		"http_proxy":            true,
		"https_proxy":           true,
		"HTTP_PROXY":            true,
		"HTTPS_PROXY":           true,
		"no_proxy":              true,
		"NO_PROXY":              true,
	}
	return reserved[key]
}
