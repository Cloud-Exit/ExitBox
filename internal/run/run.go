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
	"github.com/cloud-exit/exitbox/internal/ipc"
	"github.com/cloud-exit/exitbox/internal/network"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/project"
	"github.com/cloud-exit/exitbox/internal/ui"
	"golang.org/x/term"
)

// Options holds all the flags for running a container.
type Options struct {
	Agent             string
	ProjectDir        string
	WorkspaceHash     string
	WorkspaceOverride string
	NoFirewall        bool
	ReadOnly          bool
	NoEnv             bool
	Resume            bool
	ResumeToken       string
	EnvVars           []string
	IncludeDirs       []string
	AllowURLs         []string
	Passthrough       []string
	Verbose           bool
	StatusBar         bool
	Version           string
	Ollama            bool
	Memory            string
	CPUs              string
}

// AgentContainer runs an agent container interactively.
func AgentContainer(rt container.Runtime, opts Options) (int, error) {
	cmd := container.Cmd(rt)
	imageName := project.ImageName(opts.Agent, opts.ProjectDir, opts.WorkspaceHash)
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

	// Ollama mode: route traffic through the firewall to host Ollama.
	if opts.Ollama {
		opts.AllowURLs = append(opts.AllowURLs, "host.docker.internal")
		ui.Infof("Ollama mode: routing traffic through firewall to host")
	}

	// Network setup
	if opts.NoFirewall {
		// Host networking gives unrestricted internet access and exposes
		// all container ports directly (e.g. Codex OAuth on 1455).
		args = append(args, "--network", "host")
	} else {
		network.EnsureNetworks(rt)
		args = append(args, "--network", network.InternalNetwork)
		if err := network.StartSquidProxy(rt, containerName, opts.AllowURLs); err != nil {
			return 1, fmt.Errorf("failed to start firewall (Squid proxy): %w", err)
		}
		proxyArgs := network.GetProxyEnvVars(rt)
		args = append(args, proxyArgs...)
	}

	// IPC server for runtime domain allow requests.
	var ipcServer *ipc.Server
	if !opts.NoFirewall {
		var ipcErr error
		ipcServer, ipcErr = ipc.NewServer()
		if ipcErr != nil {
			ui.Warnf("Failed to start IPC server: %v", ipcErr)
		} else {
			ipcServer.Handle("allow_domain", ipc.NewAllowDomainHandler(ipc.AllowDomainHandlerConfig{
				Runtime:       rt,
				ContainerName: containerName,
			}))
			ipcServer.Start()
			defer ipcServer.Stop()
		}
	}

	// Ensure squid cleanup runs on ALL return paths (including early errors).
	defer func() {
		if len(opts.AllowURLs) > 0 {
			network.RemoveSessionURLs(rt, containerName)
		}
		network.CleanupSquidIfUnused(rt)
	}()

	// Resource limits
	memory := "8g"
	if opts.Memory != "" {
		memory = opts.Memory
	}
	cpus := "4"
	if opts.CPUs != "" {
		cpus = opts.CPUs
	}
	args = append(args, "--memory="+memory, "--cpus="+cpus)

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

	// Workspace resolution and isolated config mounts.
	cfg := config.LoadOrDefault()
	activeWorkspace, err := profile.ResolveActiveWorkspace(cfg, opts.ProjectDir, opts.WorkspaceOverride)
	if err != nil {
		return 1, fmt.Errorf("failed to resolve active workspace: %w", err)
	}

	// Mount config.yaml individually (read-write for in-container workspace switching).
	configFile := filepath.Join(config.Home, "config.yaml")
	if _, statErr := os.Stat(configFile); os.IsNotExist(statErr) {
		_ = config.SaveConfig(cfg)
	}
	args = append(args, "-v", configFile+":/home/user/.exitbox-config/config.yaml")

	if activeWorkspace != nil {
		if err := profile.EnsureAgentConfig(activeWorkspace.Workspace.Name, opts.Agent); err != nil {
			ui.Warnf("Failed to prepare workspace config for %s/%s: %v", activeWorkspace.Scope, activeWorkspace.Workspace.Name, err)
		}
		// Mount only the active workspace's agent dir for credential isolation.
		// Other workspaces' credentials are not accessible from within the container.
		hostDir := profile.WorkspaceAgentDir(activeWorkspace.Workspace.Name, opts.Agent)
		containerDir := "/home/user/.exitbox-config/profiles/global/" + activeWorkspace.Workspace.Name + "/" + opts.Agent
		args = append(args, "-v", hostDir+":"+containerDir)
		args = append(args,
			"-e", "EXITBOX_WORKSPACE_SCOPE="+activeWorkspace.Scope,
			"-e", "EXITBOX_WORKSPACE_NAME="+activeWorkspace.Workspace.Name,
		)
	}

	// Environment variables
	projectName := filepath.Base(opts.ProjectDir)
	projectKey := project.GenerateFolderName(opts.ProjectDir)
	if !opts.NoEnv {
		args = append(args,
			"-e", "NODE_ENV="+getEnvOr("NODE_ENV", "production"),
			"-e", "TERM=xterm-256color",
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
		"-e", "EXITBOX_PROJECT_KEY="+projectKey,
		"-e", "EXITBOX_VERSION="+opts.Version,
		"-e", "EXITBOX_STATUS_BAR="+fmt.Sprint(opts.StatusBar),
		"-e", "EXITBOX_AUTO_RESUME="+fmt.Sprint(opts.Resume),
	)
	if opts.ResumeToken != "" {
		args = append(args, "-e", "EXITBOX_RESUME_TOKEN="+opts.ResumeToken)
	}
	if opts.Ollama {
		args = append(args, ollamaEnvVars(opts.Agent)...)
	}

	// Security options
	args = append(args,
		"--security-opt=no-new-privileges:true",
		"--cap-drop=ALL",
	)

	// IPC socket mount
	if ipcServer != nil {
		args = append(args, "-v", ipcServer.SocketDir()+":/run/exitbox")
		args = append(args, "-e", "EXITBOX_IPC_SOCKET=/run/exitbox/host.sock")
	}

	// Image
	args = append(args, imageName)

	// Passthrough args
	args = append(args, opts.Passthrough...)

	if opts.Verbose {
		ui.Debugf("Container run: %s run %s", cmd, strings.Join(args, " "))
	}

	// Run with inherited stdio
	c := exec.Command(cmd, append([]string{"run"}, args...)...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	err = c.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return exitCode, nil
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

// ollamaEnvVars returns the container env flags to point an agent at host Ollama.
func ollamaEnvVars(agent string) []string {
	ollamaURL := "http://host.docker.internal:11434"
	switch agent {
	case "claude":
		return []string{
			"-e", "ANTHROPIC_BASE_URL=" + ollamaURL,
			"-e", "ANTHROPIC_AUTH_TOKEN=ollama",
			"-e", "ANTHROPIC_API_KEY=",
		}
	case "codex":
		return []string{
			"-e", "OPENAI_BASE_URL=" + ollamaURL + "/v1",
		}
	case "opencode":
		return []string{
			"-e", "OLLAMA_HOST=" + ollamaURL,
		}
	}
	return nil
}

func isReservedEnvVar(key string) bool {
	reserved := map[string]bool{
		"EXITBOX_AGENT":           true,
		"EXITBOX_PROJECT_NAME":    true,
		"EXITBOX_PROJECT_KEY":     true,
		"EXITBOX_WORKSPACE_SCOPE": true,
		"EXITBOX_WORKSPACE_NAME":  true,
		"EXITBOX_VERSION":         true,
		"EXITBOX_STATUS_BAR":      true,
		"EXITBOX_AUTO_RESUME":     true,
		"EXITBOX_IPC_SOCKET":      true,
		"EXITBOX_RESUME_TOKEN":    true,
		"TERM":                    true,
		"http_proxy":              true,
		"https_proxy":             true,
		"HTTP_PROXY":              true,
		"HTTPS_PROXY":             true,
		"no_proxy":                true,
		"NO_PROXY":                true,
		"OLLAMA_HOST":             true,
		"ANTHROPIC_BASE_URL":      true,
		"ANTHROPIC_AUTH_TOKEN":    true,
		"ANTHROPIC_API_KEY":       true,
		"OPENAI_BASE_URL":        true,
	}
	return reserved[key]
}
