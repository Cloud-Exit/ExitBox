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
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/ui"
)

const (
	InternalNetwork = "exitbox-int"
	EgressNetwork   = "exitbox-egress"
	SquidContainer  = "exitbox-squid"
)

// EnsureNetworks creates the shared networks if they don't exist.
func EnsureNetworks(rt container.Runtime) {
	if !rt.NetworkExists(InternalNetwork) {
		ui.Infof("Creating internal network %s...", InternalNetwork)
		_ = rt.NetworkCreate(InternalNetwork, true)
	}
	if !rt.NetworkExists(EgressNetwork) {
		ui.Infof("Creating egress network %s...", EgressNetwork)
		_ = rt.NetworkCreate(EgressNetwork, false)
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

// StartSquidProxy starts the Squid proxy container.
func StartSquidProxy(rt container.Runtime, extraURLs []string) error {
	cmd := container.Cmd(rt)

	// Check if already running
	names, _ := rt.PS(fmt.Sprintf("name=^/%s$", SquidContainer), "{{.Names}}")
	for _, n := range names {
		if n == SquidContainer {
			// If extra URLs, regenerate config and reload
			if len(extraURLs) > 0 {
				ui.Info("Updating Squid config with extra allowed domains...")
				if err := writeSquidConfig(rt, extraURLs); err != nil {
					return err
				}
				_ = exec.Command(cmd, "exec", SquidContainer, "squid", "-k", "reconfigure").Run()
			}
			return nil
		}
	}

	// Remove if stopped
	_ = rt.Remove(SquidContainer)

	// Ensure networks
	EnsureNetworks(rt)

	// Build squid image if needed
	// (image building is handled by the caller before this)

	// Generate config
	if err := writeSquidConfig(rt, extraURLs); err != nil {
		return err
	}

	configFile := filepath.Join(config.Cache, "squid.conf")

	runArgs := []string{
		"run", "-d",
		"--name", SquidContainer,
		"--network", EgressNetwork,
		"-v", configFile + ":/etc/squid/squid.conf",
		"--restart=unless-stopped",
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
	cmd := container.Cmd(rt)
	names, _ := rt.PS("", "{{.Names}}")
	running := 0
	for _, n := range names {
		if n == SquidContainer {
			continue
		}
		if strings.HasPrefix(n, "exitbox-") {
			running++
		}
	}
	if running == 0 {
		// Check if squid is running
		squidNames, _ := rt.PS(fmt.Sprintf("name=^/%s$", SquidContainer), "{{.Names}}")
		for _, n := range squidNames {
			if n == SquidContainer {
				ui.Info("Stopping Squid proxy (no running agents)...")
				_ = exec.Command(cmd, "rm", "-f", SquidContainer).Run()
				break
			}
		}
	}
}

func writeSquidConfig(rt container.Runtime, extraURLs []string) error {
	subnet, err := GetNetworkSubnet(rt, InternalNetwork)
	if err != nil {
		return fmt.Errorf("could not detect internal network subnet: %w", err)
	}

	al := config.LoadAllowlistOrDefault()
	domains := al.AllDomains()

	content := GenerateSquidConfig(subnet, domains, extraURLs)
	configFile := filepath.Join(config.Cache, "squid.conf")
	os.MkdirAll(filepath.Dir(configFile), 0755)
	return os.WriteFile(configFile, []byte(content), 0644)
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
