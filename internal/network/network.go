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

package network

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/ui"
)

const (
	InternalNetwork = "exitbox-int"
	EgressNetwork   = "exitbox-egress"
	SquidContainer  = "exitbox-squid"
)

const (
	squidLockTimeout    = 60 * time.Second
	squidLockStaleAfter = 2 * time.Minute
)

func withSquidLock(fn func() error) error {
	unlock, err := acquireSquidLock()
	if err != nil {
		return err
	}
	defer unlock()
	return fn()
}

func acquireSquidLock() (func(), error) {
	lockDir := filepath.Join(config.Cache, "squid.lock")
	deadline := time.Now().Add(squidLockTimeout)

	for {
		if err := os.MkdirAll(filepath.Dir(lockDir), 0755); err != nil {
			return nil, fmt.Errorf("creating squid lock parent: %w", err)
		}
		err := os.Mkdir(lockDir, 0755)
		if err == nil {
			_ = os.WriteFile(filepath.Join(lockDir, "owner"), []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644)
			return func() { _ = os.RemoveAll(lockDir) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("acquiring squid lock: %w", err)
		}

		if info, statErr := os.Stat(lockDir); statErr == nil && time.Since(info.ModTime()) > squidLockStaleAfter {
			_ = os.RemoveAll(lockDir)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for Squid proxy lock")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// reconfigureSquid sends a HUP to the running squid process. If that fails
// (stale PID, process crashed), it restarts squid inside the container.
func reconfigureSquid(cmd string) {
	recCmd := exec.Command(cmd, "exec", SquidContainer, "squid", "-k", "reconfigure")
	if out, err := recCmd.CombinedOutput(); err != nil {
		detail := strings.TrimSpace(string(out))
		// Config is written before this call, so a parse failure means bad config.
		parseCmd := exec.Command(cmd, "exec", SquidContainer, "squid", "-k", "parse")
		if parseOut, parseErr := parseCmd.CombinedOutput(); parseErr != nil {
			ui.Warnf("Squid config error: %s", strings.TrimSpace(string(parseOut)))
			return
		}
		// Config is valid but reconfigure failed (stale PID / process died).
		restartCmd := exec.Command(cmd, "exec", SquidContainer, "sh", "-c", "squid -k shutdown 2>/dev/null; sleep 1; squid")
		if _, restartErr := restartCmd.CombinedOutput(); restartErr != nil {
			ui.Warnf("Failed to restart squid (reconfigure failed: %s): %v", detail, restartErr)
		}
	}
}

// EnsureNetworks creates the shared networks if they don't exist.
func EnsureNetworks(rt container.Runtime) {
	if !rt.NetworkExists(InternalNetwork) {
		ui.Infof("Creating internal network %s...", InternalNetwork)
		if err := rt.NetworkCreate(InternalNetwork, true); err != nil {
			ui.Warnf("Failed to create internal network: %v", err)
		}
	}
	if !rt.NetworkExists(EgressNetwork) {
		ui.Infof("Creating egress network %s...", EgressNetwork)
		if err := rt.NetworkCreate(EgressNetwork, false); err != nil {
			ui.Warnf("Failed to create egress network: %v", err)
		}
	}
}

// GetNetworkSubnet returns the subnet for a network.
func GetNetworkSubnet(rt container.Runtime, networkName string) (string, error) {
	EnsureNetworks(rt)

	out, err := rt.NetworkInspect(networkName, "")
	if err != nil {
		return "", err
	}

	// Try JSON parsing
	var networks []struct {
		IPAM struct {
			Config []struct {
				Subnet string `json:"Subnet"`
			} `json:"Config"`
		} `json:"IPAM"`
		Subnets []struct {
			Subnet string `json:"subnet"`
		} `json:"subnets"`
	}
	if err := json.Unmarshal([]byte(out), &networks); err == nil && len(networks) > 0 {
		n := networks[0]
		for _, c := range n.IPAM.Config {
			if c.Subnet != "" {
				return c.Subnet, nil
			}
		}
		for _, s := range n.Subnets {
			if s.Subnet != "" {
				return s.Subnet, nil
			}
		}
	}

	return "", fmt.Errorf("could not detect subnet for %s", networkName)
}

// sessionDir returns the directory for per-container session URL files.
func sessionDir() string {
	return filepath.Join(config.Cache, "squid-sessions")
}

// RegisterSessionURLs writes a session file for a container's extra URLs.
func RegisterSessionURLs(containerName string, urls []string) error {
	return RegisterSessionAccess(containerName, urls, nil)
}

// RegisterSessionAccess writes a session file for a container's firewall extras.
func RegisterSessionAccess(containerName string, urls []string, hostPorts []int) error {
	dir := sessionDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var b strings.Builder
	for _, url := range urls {
		if strings.TrimSpace(url) == "" {
			continue
		}
		b.WriteString(url)
		b.WriteByte('\n')
	}
	for _, port := range NormalizeHostPorts(hostPorts) {
		fmt.Fprintf(&b, "host-port:%d\n", port)
	}
	return os.WriteFile(filepath.Join(dir, containerName+".urls"), []byte(b.String()), 0644)
}

func sessionFileCount() int {
	entries, err := os.ReadDir(sessionDir())
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".urls") {
			count++
		}
	}
	return count
}

// RemoveSessionURLs removes a container's session file and regenerates squid config.
func RemoveSessionURLs(rt container.Runtime, containerName string) {
	if err := withSquidLock(func() error {
		dir := sessionDir()
		_ = os.Remove(filepath.Join(dir, containerName+".urls"))

		// Collect remaining URLs from all sessions and regenerate config
		remaining := collectAllSessionURLs()
		remainingHostPorts := collectAllSessionHostPorts()
		changed, writeErr := writeSquidConfig(rt, remaining, remainingHostPorts)
		if writeErr != nil {
			ui.Warnf("Failed to regenerate squid config: %v", writeErr)
			return nil
		}

		// Reconfigure squid if running and config changed
		if !changed {
			return nil
		}
		cmd := container.Cmd(rt)
		names, err := rt.PS("", "{{.Names}}")
		if err != nil {
			ui.Warnf("Failed to list containers: %v", err)
			return nil
		}
		for _, n := range names {
			if n == SquidContainer {
				reconfigureSquid(cmd)
				break
			}
		}
		return nil
	}); err != nil {
		ui.Warnf("Failed to update Squid session state: %v", err)
	}
}

// collectAllSessionURLs reads all session files and returns deduplicated URLs.
func collectAllSessionURLs() []string {
	dir := sessionDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var urls []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".urls") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "host-port:") {
				continue
			}
			if !seen[line] {
				seen[line] = true
				urls = append(urls, line)
			}
		}
	}
	return urls
}

// collectAllSessionHostPorts reads all session files and returns deduplicated host ports.
func collectAllSessionHostPorts() []int {
	dir := sessionDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var ports []int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".urls") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "host-port:") {
				continue
			}
			p, err := strconv.Atoi(strings.TrimPrefix(line, "host-port:"))
			if err == nil {
				ports = append(ports, p)
			}
		}
	}
	return NormalizeHostPorts(ports)
}

// StartSquidProxy starts the Squid proxy container.
func StartSquidProxy(rt container.Runtime, containerName string, extraURLs []string, hostPorts []int) error {
	err := withSquidLock(func() error {
		return startSquidProxyLocked(rt, containerName, extraURLs, hostPorts)
	})
	if err != nil {
		_ = os.Remove(filepath.Join(sessionDir(), containerName+".urls"))
	}
	return err
}

func startSquidProxyLocked(rt container.Runtime, containerName string, extraURLs []string, hostPorts []int) error {
	cmd := container.Cmd(rt)

	// Register session URLs
	if err := RegisterSessionAccess(containerName, extraURLs, hostPorts); err != nil {
		ui.Warnf("Failed to register session firewall access: %v", err)
	}

	// Collect all session URLs for config generation
	allExtraURLs := collectAllSessionURLs()
	allHostPorts := collectAllSessionHostPorts()

	// Check if already running
	names, err := rt.PS("", "{{.Names}}")
	if err != nil {
		ui.Warnf("Failed to list containers: %v", err)
	}
	for _, n := range names {
		if n == SquidContainer {
			// Regenerate config and reload only if it changed.
			changed, writeErr := writeSquidConfig(rt, allExtraURLs, allHostPorts)
			if writeErr != nil {
				return writeErr
			}
			if changed {
				reconfigureSquid(cmd)
			}
			return nil
		}
	}

	// Remove if stopped
	_ = rt.Remove(SquidContainer)

	// Ensure networks
	EnsureNetworks(rt)

	// Generate config
	if _, err := writeSquidConfig(rt, allExtraURLs, allHostPorts); err != nil {
		return err
	}

	configFile := filepath.Join(config.Cache, "squid.conf")

	runArgs := []string{
		"run", "-d",
		"--name", SquidContainer,
		"--network", EgressNetwork,
		"-v", configFile + ":/etc/squid/squid.conf",
		"--restart=unless-stopped",
		"--add-host=host.docker.internal:host-gateway",
	}

	// DNS flags
	dnsServers := getSquidDNSServers()
	for _, dns := range dnsServers {
		runArgs = append(runArgs, "--dns", dns)
	}

	runArgs = append(runArgs, "exitbox-squid")

	ui.Info("Starting Squid proxy...")
	c := exec.Command(cmd, runArgs...)
	if out, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start Squid proxy: %w: %s", err, string(out))
	}

	// Connect to internal network
	if err := rt.NetworkConnect(InternalNetwork, SquidContainer); err != nil {
		_ = rt.Remove(SquidContainer)
		return fmt.Errorf("failed to connect Squid to internal network: %w", err)
	}

	return nil
}

// GetProxyEnvVars returns proxy environment variable flags for container run.
func GetProxyEnvVars(rt container.Runtime) []string {
	cmd := container.Cmd(rt)

	proxyHost := SquidContainer
	// Try to get IP
	out, err := exec.Command(cmd, "inspect", SquidContainer,
		"--format", fmt.Sprintf(`{{with index .NetworkSettings.Networks "%s"}}{{.IPAddress}}{{end}}`, InternalNetwork)).Output()
	if err == nil && strings.TrimSpace(string(out)) != "" {
		proxyHost = strings.TrimSpace(string(out))
	}

	proxyURL := fmt.Sprintf("http://%s:3128", proxyHost)
	return []string{
		"-e", "http_proxy=" + proxyURL,
		"-e", "https_proxy=" + proxyURL,
		"-e", "HTTP_PROXY=" + proxyURL,
		"-e", "HTTPS_PROXY=" + proxyURL,
		"-e", "no_proxy=localhost,127.0.0.1,.local",
		"-e", "NO_PROXY=localhost,127.0.0.1,.local",
	}
}

// CleanupSquidIfUnused stops squid if no agent containers are running.
func CleanupSquidIfUnused(rt container.Runtime) {
	if err := withSquidLock(func() error {
		cmd := container.Cmd(rt)
		names, err := rt.PS("", "{{.Names}}")
		if err != nil {
			ui.Warnf("Failed to list containers: %v", err)
			return nil
		}
		running := 0
		squidRunning := false
		for _, n := range names {
			if n == SquidContainer {
				squidRunning = true
				continue
			}
			if strings.HasPrefix(n, "exitbox-") {
				running++
			}
		}
		if running == 0 && sessionFileCount() == 0 && squidRunning {
			ui.Info("Stopping Squid proxy (no running agents)...")
			// Stop first (handles restart policy), then remove.
			_ = exec.Command(cmd, "stop", SquidContainer).Run()
			if rmErr := exec.Command(cmd, "rm", "-f", SquidContainer).Run(); rmErr != nil {
				ui.Warnf("Failed to remove Squid proxy: %v", rmErr)
			}
		}
		if running == 0 && sessionFileCount() == 0 {
			// Clean stale session files
			_ = os.RemoveAll(sessionDir())
		}
		return nil
	}); err != nil {
		ui.Warnf("Failed to clean up Squid proxy: %v", err)
	}
}

// AddSessionURLAndReload adds a domain to a container's session URLs and
// hot-reloads Squid so the change takes effect immediately.
func AddSessionURLAndReload(rt container.Runtime, containerName string, domain string) error {
	return withSquidLock(func() error {
		return addSessionURLAndReloadLocked(rt, containerName, domain)
	})
}

func addSessionURLAndReloadLocked(rt container.Runtime, containerName string, domain string) error {
	cmd := container.Cmd(rt)
	dir := sessionDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Read existing session URLs for this container.
	urlFile := filepath.Join(dir, containerName+".urls")
	var urls []string
	var hostPorts []int
	if data, err := os.ReadFile(urlFile); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "host-port:") {
				if p, err := strconv.Atoi(strings.TrimPrefix(line, "host-port:")); err == nil {
					hostPorts = append(hostPorts, p)
				}
				continue
			}
			urls = append(urls, line)
		}
	}

	// Deduplicate: skip if already present.
	for _, u := range urls {
		if u == domain {
			return nil // already allowed
		}
	}
	urls = append(urls, domain)

	// Write back and regenerate config.
	if err := RegisterSessionAccess(containerName, urls, hostPorts); err != nil {
		return err
	}

	allURLs := collectAllSessionURLs()
	allHostPorts := collectAllSessionHostPorts()
	changed, writeErr := writeSquidConfig(rt, allURLs, allHostPorts)
	if writeErr != nil {
		return writeErr
	}
	if !changed {
		return nil
	}

	// Hot-reload Squid.
	names, psErr := rt.PS("", "{{.Names}}")
	if psErr != nil {
		ui.Warnf("Failed to list containers for squid reload: %v", psErr)
		return nil
	}
	for _, n := range names {
		if n == SquidContainer {
			reconfigureSquid(cmd)
			break
		}
	}

	return nil
}

// writeSquidConfig generates and writes the squid config. Returns (changed, error)
// where changed indicates whether the config file content actually changed.
func writeSquidConfig(rt container.Runtime, extraURLs []string, sessionHostPorts []int) (bool, error) {
	subnet, err := GetNetworkSubnet(rt, InternalNetwork)
	if err != nil {
		return false, fmt.Errorf("could not detect internal network subnet: %w", err)
	}

	al := config.LoadAllowlistOrDefault()
	domains := al.AllDomains()
	hostPorts := NormalizeHostPorts(append(append([]int{}, al.HostPorts...), sessionHostPorts...))

	content := GenerateSquidConfigWithHostPorts(subnet, domains, extraURLs, hostPorts)
	configFile := filepath.Join(config.Cache, "squid.conf")
	if err := os.MkdirAll(filepath.Dir(configFile), 0755); err != nil {
		return false, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Skip write if content hasn't changed (avoids unnecessary squid reconfigure).
	if existing, readErr := os.ReadFile(configFile); readErr == nil && string(existing) == content {
		return false, nil
	}

	return true, os.WriteFile(configFile, []byte(content), 0644)
}

func getSquidDNSServers() []string {
	v := os.Getenv("EXITBOX_SQUID_DNS")
	if v == "" {
		return []string{"1.1.1.1", "8.8.8.8"}
	}
	v = strings.ReplaceAll(v, ",", " ")
	var servers []string
	for _, s := range strings.Fields(v) {
		if s != "" {
			servers = append(servers, s)
		}
	}
	return servers
}
