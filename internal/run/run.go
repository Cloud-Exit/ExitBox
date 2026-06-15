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
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/generate"
	"github.com/cloud-exit/exitbox/internal/ipc"
	"github.com/cloud-exit/exitbox/internal/network"
	"github.com/cloud-exit/exitbox/internal/platform"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/project"
	"github.com/cloud-exit/exitbox/internal/redactor"
	"github.com/cloud-exit/exitbox/internal/ui"
	"golang.org/x/term"
)

// isEnvSampleFile returns true for .env.sample, .env-sample, .env.example, etc.
func isEnvSampleFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "sample") || strings.Contains(lower, "example")
}

// Options holds all the flags for running a container.
type Options struct {
	Agent             string
	ProjectDir        string
	WorkspaceHash     string
	WorkspaceOverride string
	EnvProfile        string
	NoFirewall        bool
	ReadOnly          bool
	NoEnv             bool
	Resume            bool
	ResumeToken       string
	SessionName       string
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
	Keybindings       string
	FullGitSupport    bool
	RTK               bool
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

	// Workspace resolution (needed early for config host detection).
	cfg := config.LoadOrDefault()
	activeWorkspace, err := profile.ResolveActiveWorkspace(cfg, opts.ProjectDir, opts.WorkspaceOverride)
	if err != nil {
		return 1, fmt.Errorf("failed to resolve active workspace: %w", err)
	}

	// Detect hosts in agent config and offer to permanently allow them through the firewall.
	if activeWorkspace != nil && !opts.NoFirewall {
		agentDir := profile.WorkspaceAgentDir(activeWorkspace.Workspace.Name, opts.Agent)
		configHosts := generate.ExtractConfigHosts(agentDir, opts.Agent)
		if len(configHosts) > 0 {
			// Filter out hosts already in the permanent allowlist.
			allowlist := config.LoadAllowlistOrDefault()
			allowed := make(map[string]struct{})
			for _, d := range allowlist.AllDomains() {
				allowed[d] = struct{}{}
			}
			var newHosts []string
			for _, h := range configHosts {
				if _, ok := allowed[h]; !ok {
					newHosts = append(newHosts, h)
				}
			}
			if len(newHosts) > 0 {
				shouldAllow := true
				if term.IsTerminal(int(os.Stdin.Fd())) {
					fmt.Println("Detected server in agent config:")
					for _, h := range newHosts {
						fmt.Printf("  - %s\n", h)
					}
					fmt.Print("Allow through firewall? [Y/n]: ")
					reader := bufio.NewReader(os.Stdin)
					answer, _ := reader.ReadString('\n')
					answer = strings.TrimSpace(strings.ToLower(answer))
					shouldAllow = answer == "" || answer == "y" || answer == "yes"
				}
				if shouldAllow {
					allowlist.Custom = append(allowlist.Custom, newHosts...)
					if err := config.SaveAllowlist(allowlist); err != nil {
						ui.Warnf("Failed to save allowlist: %v", err)
						// Fall back to session-level allow so this run still works.
						opts.AllowURLs = append(opts.AllowURLs, newHosts...)
					}
				}
			}
		}
	}

	// Ollama mode: route traffic through the firewall to host Ollama.
	if opts.Ollama {
		opts.AllowURLs = append(opts.AllowURLs, "host.docker.internal")
		ui.Infof("Ollama mode: routing traffic through firewall to host")
	}

	// Network setup
	if opts.NoFirewall {
		// Host networking gives unrestricted internet access and exposes
		// all container ports directly (e.g. OAuth callbacks).
		args = append(args, "--network", "host")
	} else {
		network.EnsureNetworks(rt)
		args = append(args, "--network", network.InternalNetwork)
		if err := network.StartSquidProxy(rt, containerName, opts.AllowURLs); err != nil {
			return 1, fmt.Errorf("failed to start firewall (Squid proxy): %w", err)
		}
		proxyArgs := network.GetProxyEnvVars(rt)
		args = append(args, proxyArgs...)

		// Publish OAuth callback ports so browser redirects to localhost
		// reach the agent inside the container. A socat relay in
		// docker-entrypoint bridges 0.0.0.0:RELAY → 127.0.0.1:APP.
		// Skip if the port is already in use (e.g. another exitbox session).
		switch opts.Agent {
		case "codex":
			if portAvailable("127.0.0.1", 1455) {
				args = append(args, "-p", "127.0.0.1:1455:2455")
			}
		case "opencode":
			if portAvailable("127.0.0.1", 8085) {
				args = append(args, "-p", "127.0.0.1:8085:2085")
			}
		}
	}

	// IPC server: always start (vault/kv depend on it).
	// Domain-allow handler only required when firewall is enabled.
	ipcServer, ipcErr := newIPCServer(!opts.NoFirewall, rt, containerName)
	if ipcErr != nil {
		if activeWorkspace != nil && activeWorkspace.Workspace.Vault.Enabled {
			return 1, fmt.Errorf("failed to start IPC server for vault-enabled workspace: %w", ipcErr)
		}
		ui.Warnf("Failed to start IPC server: %v (KV/vault/domain approval IPC disabled this run)", ipcErr)
		ipcServer = nil
	} else {
		ipcServer.Start()
		defer ipcServer.Stop()
	}

	// IDE relay: bridge the host IDE's WebSocket to a Unix socket in the
	// IPC directory so the containerised agent can reach it without host
	// network access.
	var ideRelay *IDERelay
	if idePort, ok := DetectIDE(opts.Agent); ok {
		if opts.NoFirewall {
			// Host network: 127.0.0.1 is the host loopback, no relay needed.
			args = append(args,
				"-e", "CLAUDE_CODE_SSE_PORT="+idePort,
				"-e", "ENABLE_IDE_INTEGRATION=true",
			)
			ideLockDir := filepath.Join(os.Getenv("HOME"), ".claude", "ide")
			if info, statErr := os.Stat(ideLockDir); statErr == nil && info.IsDir() {
				args = append(args, "-v", ideLockDir+":/home/user/.claude/ide:ro")
			}
		} else if ipcServer != nil {
			ideRelay = StartIDERelay(ipcServer.SocketDir(), idePort)
			if ideRelay != nil {
				ideRelay.LockDir = PrepareIDELockFile(idePort)
				args = append(args, ideRelay.ContainerArgs()...)
			}
		}
	}
	defer StopIDERelay(ideRelay)

	// Full git support: mount SSH_AUTH_SOCK socket and .gitconfig so that
	// git/ssh inside the container can request signatures (without exposing
	// private key material) and honour the user's git identity/aliases.
	if opts.FullGitSupport {
		if sock := os.Getenv("SSH_AUTH_SOCK"); sock != "" {
			if info, err := os.Stat(sock); err == nil && info.Mode()&os.ModeSocket != 0 {
				args = append(args,
					"-v", sock+":/run/exitbox/ssh-agent.sock",
					"-e", "SSH_AUTH_SOCK=/run/exitbox/ssh-agent.sock",
				)
			}
		}
		// Mount host .gitconfig read-only so user.name/email/aliases are available.
		gitconfig := filepath.Join(os.Getenv("HOME"), ".gitconfig")
		if info, err := os.Stat(gitconfig); err == nil && !info.IsDir() {
			args = append(args, "-v", gitconfig+":/home/user/.gitconfig:ro")
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

	// Non-root (Windows: os.Getuid/Getgid are -1)
	runUID, runGID := platform.HostUIDGID()
	args = append(args, "--user", fmt.Sprintf("%d:%d", runUID, runGID))

	// Include dirs
	for _, dir := range opts.IncludeDirs {
		dir = expandPath(dir, opts.ProjectDir)
		if _, err := os.Stat(dir); err != nil {
			ui.Warnf("Include dir not accessible: %s: %v", dir, err)
			continue
		}
		dir = strings.TrimSuffix(dir, "/")
		base := filepath.Base(dir)
		args = append(args, "-v", dir+":/workspace/"+base)
	}

	// Mount the entire config directory so the entrypoint can read/write
	// config.yaml (workspace switching) and access workspace profiles.
	// Ensure the config file exists before mounting (otherwise the runtime
	// creates a directory named config.yaml instead of a file).
	configFile := filepath.Join(config.Home, "config.yaml")
	if _, statErr := os.Stat(configFile); statErr != nil {
		if saveErr := config.SaveConfig(cfg); saveErr != nil {
			ui.Warnf("Failed to create config file for mount: %v", saveErr)
		}
	}
	args = append(args, "-v", config.Home+":/home/user/.exitbox-config")

	if activeWorkspace != nil {
		if err := profile.EnsureAgentConfig(activeWorkspace.Workspace.Name, opts.Agent); err != nil {
			ui.Warnf("Failed to prepare workspace config for %s/%s: %v", activeWorkspace.Scope, activeWorkspace.Workspace.Name, err)
		}
		args = append(args,
			"-e", "EXITBOX_WORKSPACE_SCOPE="+activeWorkspace.Scope,
			"-e", "EXITBOX_WORKSPACE_NAME="+activeWorkspace.Workspace.Name,
		)
	}

	// Register KV IPC handlers (always enabled when IPC server is running).
	if ipcServer != nil && activeWorkspace != nil {
		kvCfg := ipc.KVHandlerConfig{
			WorkspaceName: activeWorkspace.Workspace.Name,
		}
		ipcServer.Handle("kv_get", ipc.NewKVGetHandler(kvCfg))
		ipcServer.Handle("kv_set", ipc.NewKVSetHandler(kvCfg))
		ipcServer.Handle("kv_delete", ipc.NewKVDeleteHandler(kvCfg))
		ipcServer.Handle("kv_list", ipc.NewKVListHandler(kvCfg))
	}

	// Register vault IPC handlers when vault is enabled for the workspace.
	var vaultState *ipc.VaultState
	if activeWorkspace != nil && activeWorkspace.Workspace.Vault.Enabled && ipcServer != nil {
		vaultState = &ipc.VaultState{}
		vCfg := ipc.VaultHandlerConfig{
			Runtime:       rt,
			ContainerName: containerName,
			WorkspaceName: activeWorkspace.Workspace.Name,
		}
		ipcServer.Handle("vault_get", ipc.NewVaultGetHandler(vCfg, vaultState))
		ipcServer.Handle("vault_list", ipc.NewVaultListHandler(vCfg, vaultState))
		if !activeWorkspace.Workspace.Vault.ReadOnly {
			ipcServer.Handle("vault_set", ipc.NewVaultSetHandler(vCfg, vaultState))
		}
	}
	defer func() {
		if vaultState != nil {
			vaultState.Cleanup()
		}
	}()

	// Vault env var and .env masking
	if activeWorkspace != nil && activeWorkspace.Workspace.Vault.Enabled {
		args = append(args, "-e", "EXITBOX_VAULT_ENABLED=true")
		if activeWorkspace.Workspace.Vault.ReadOnly {
			args = append(args, "-e", "EXITBOX_VAULT_READONLY=true")
		}

		// Mask all .env* files (except sample/example files) by mounting /dev/null over them.
		matches, _ := filepath.Glob(filepath.Join(opts.ProjectDir, ".env*"))
		for _, f := range matches {
			info, statErr := os.Stat(f)
			if statErr != nil || info.IsDir() {
				continue
			}
			base := filepath.Base(f)
			if isEnvSampleFile(base) {
				continue
			}
			rel, relErr := filepath.Rel(opts.ProjectDir, f)
			if relErr != nil {
				continue
			}
			args = append(args, "-v", "/dev/null:/workspace/"+rel+":ro")
		}
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
		if opts.Ollama && isOllamaEnvVar(key) {
			return 1, fmt.Errorf("environment variable '%s' conflicts with --ollama", key)
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
		"-e", "EXITBOX_SESSION_NAME="+opts.SessionName,
	)
	if opts.ResumeToken != "" {
		args = append(args, "-e", "EXITBOX_RESUME_TOKEN="+opts.ResumeToken)
	}
	if opts.EnvProfile != "" {
		args = append(args, "-e", "EXITBOX_ENV_PROFILE="+opts.EnvProfile)
	}
	if opts.Keybindings != "" {
		args = append(args, "-e", "EXITBOX_KEYBINDINGS="+opts.Keybindings)
	}
	if opts.RTK {
		args = append(args, "-e", "EXITBOX_RTK=true")
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

	// Run with inherited stdio, filtering output through redactor if vault is enabled.
	c := exec.Command(cmd, append([]string{"run"}, args...)...)
	c.Stdin = os.Stdin

	if vaultState != nil {
		red := redactor.NewWithProvider(vaultState.GetRetrievedSecrets)
		c.Stdout = &redactorWriter{w: os.Stdout, r: red}
		c.Stderr = &redactorWriter{w: os.Stderr, r: red}
	} else {
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
	}

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
func ollamaEnvVars(agentName string) []string {
	const ollamaURL = "http://host.docker.internal:11434"

	a := agents.Get(agentName)
	if a == nil {
		return nil
	}

	envPairs := a.OllamaEnvVars(ollamaURL)
	if len(envPairs) == 0 {
		return nil
	}

	out := make([]string, 0, len(envPairs)*2)
	for _, ev := range envPairs {
		// ev is expected to be "KEY=VALUE"
		out = append(out, "-e", ev)
	}
	return out
}

// redactorWriter wraps an io.Writer and filters output through a redactor.
type redactorWriter struct {
	w  io.Writer
	r  *redactor.Redactor
	mu sync.Mutex
}

func (rw *redactorWriter) Write(p []byte) (int, error) {
	rw.mu.Lock()
	defer rw.mu.Unlock()
	filtered := rw.r.Filter(p)
	_, err := rw.w.Write(filtered)
	return len(p), err
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
		"EXITBOX_SESSION_NAME":    true,
		"EXITBOX_KEYBINDINGS":     true,
		"EXITBOX_VAULT_ENABLED":   true,
		"EXITBOX_VAULT_READONLY":  true,
		"EXITBOX_RTK":             true,
		"EXITBOX_IDE_PORT":        true,
		"CLAUDE_CODE_SSE_PORT":    true,
		"ENABLE_IDE_INTEGRATION":  true,
		"TERM":                    true,
		"http_proxy":              true,
		"https_proxy":             true,
		"HTTP_PROXY":              true,
		"HTTPS_PROXY":             true,
		"no_proxy":                true,
		"NO_PROXY":                true,
		"SSH_AUTH_SOCK":           true,
	}
	return reserved[key]
}

// isOllamaEnvVar returns true for env vars that are set by --ollama mode.
// These are only reserved when --ollama is active; otherwise the user is
// free to set them (e.g. to point an agent at a custom API endpoint).
func isOllamaEnvVar(key string) bool {
	switch key {
	case "OLLAMA_HOST", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_API_KEY", "OPENAI_BASE_URL":
		return true
	}
	return false
}

// portAvailable returns true if the given TCP port can be bound.
func portAvailable(host string, port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// newIPCServer creates and configures an IPC server.
// When firewallEnabled is true, it also registers the allow_domain handler.
func newIPCServer(firewallEnabled bool, rt container.Runtime, containerName string) (*ipc.Server, error) {
	s, err := ipc.NewServer()
	if err != nil {
		return nil, err
	}
	if firewallEnabled {
		s.Handle("allow_domain", ipc.NewAllowDomainHandler(ipc.AllowDomainHandlerConfig{
			Runtime:       rt,
			ContainerName: containerName,
		}))
	}
	return s, nil
}
