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

// Package clauderouter describes the claude-router plugin: a router inside
// the container that keeps a Claude Code session on Claude models while its
// subagents run on local models served by llama.cpp or vLLM.
package clauderouter

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"

	"github.com/cloud-exit/exitbox/internal/plugin"
	"github.com/cloud-exit/exitbox/internal/router"
	"github.com/cloud-exit/exitbox/static"
)

// Name is the plugin's config key and CLI name.
const Name = "claude-router"

// Binary is the router's file name in the build context and in the image.
const Binary = "exitbox-router"

// codexRouter is the other plugin that sets Claude Code's base URL.
const codexRouter = "codex-router"

// New returns the plugin descriptor.
func New() *plugin.Plugin {
	return &plugin.Plugin{
		Name:        Name,
		Display:     "Claude Router",
		Description: "Experimental: run Claude Code subagents on local llama.cpp or vLLM models while the session stays on Claude",
		Agents:      []string{"claude"},
		Repo:        "built into ExitBox",
		Version:     "builtin",
		Conflicts:   []string{codexRouter},
		Settings:    settings(),
		Dockerfile:  dockerfile,
		Files:       files,
		BuildID:     buildID,
		SettingDomains: func(settings map[string]string) []string {
			return Domains(Routes(settings))
		},
		HostPorts: func(settings map[string]string) []int {
			return HostPorts(Routes(settings))
		},
		VaultKeys: func(settings map[string]string) []string {
			return router.VaultKeys(Routes(settings))
		},
		CheckRun: checkRun,
	}
}

func settings() []plugin.Setting {
	return []plugin.Setting{
		{
			Key:   "routes",
			Label: "Routes",
			Help:  "model-id=backend-url[=served-name][=vault:KEY], comma-separated",
			Type:  plugin.TypeString,
			Check: func(value string) error {
				_, err := router.ParseRoutes(value)
				return err
			},
		},
		{
			Key:     "port",
			Label:   "Router port",
			Help:    "Local port the router listens on inside the container",
			Type:    plugin.TypeInt,
			Default: fmt.Sprint(router.DefaultPort),
		},
		{
			Key:     "auto_start",
			Label:   "Start with the container",
			Help:    "Start the router and point Claude Code at it when the container starts",
			Type:    plugin.TypeBool,
			Default: "true",
		},
		{
			Key:     "sanitize",
			Label:   "Sanitize requests",
			Help:    "Drop request fields and server tools that local backends reject",
			Type:    plugin.TypeBool,
			Default: "true",
		},
	}
}

// Routes returns the routes a settings map configures. A value that does
// not parse yields none; `exitbox plugins` refuses to store one.
func Routes(settings map[string]string) []router.Route {
	routes, err := router.ParseRoutes(settings["routes"])
	if err != nil {
		return nil
	}
	return routes
}

// Active reports whether the router will be started with these settings.
func Active(settings map[string]string) bool {
	return settings["auto_start"] != "false" && len(Routes(settings)) > 0
}

// Domains returns the hosts the firewall must allow for the routes. A
// backend on the host's loopback is reached through the host gateway.
func Domains(routes []router.Route) []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range routes {
		host := r.Host()
		if r.OnHostLoopback() {
			host = router.HostGateway
		}
		if host != "" && !seen[host] {
			seen[host] = true
			out = append(out, host)
		}
	}
	return out
}

// HostPorts returns the host loopback ports the routes point at. ExitBox
// relays them into the container, the same way --ollama reaches Ollama.
func HostPorts(routes []router.Route) []int {
	seen := map[int]bool{}
	var out []int
	for _, r := range routes {
		if !r.OnHostLoopback() {
			continue
		}
		if p := r.Port(); p > 0 && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// checkRun refuses a session in which the router could not do its job:
// something else owns Claude Code's base URL, or the firewall cannot carry
// a route.
func checkRun(in plugin.RunInput) error {
	if !Active(in.Settings) {
		return nil
	}
	const disable = "exitbox plugins disable claude " + Name
	if in.Ollama {
		return fmt.Errorf("%s cannot be combined with --ollama: both set Claude Code's base URL. Drop --ollama, or run: %s", Name, disable)
	}
	if in.BaseURLFrom != "" {
		return fmt.Errorf("%s needs to set ANTHROPIC_BASE_URL itself, but %s already sets it. Remove it there, or run: %s", Name, in.BaseURLFrom, disable)
	}
	if !in.Firewall {
		return nil
	}
	for _, r := range Routes(in.Settings) {
		// The firewall proxy only tunnels TLS to port 443 and to relayed
		// host ports; plain http is not limited that way.
		if strings.HasPrefix(r.URL, "https://") && r.Port() != 443 && !r.OnHostLoopback() {
			return fmt.Errorf("%s: route %q uses https on port %d, which the firewall does not tunnel. Use http, port 443, or --no-firewall", Name, r.ID, r.Port())
		}
	}
	return nil
}

// dockerfile copies the router into the image. The binary comes from the
// ExitBox build itself, so the layer needs no network and no toolchain.
func dockerfile(_ plugin.BuildInput) string {
	return fmt.Sprintf("# claude-router (built into ExitBox)\nCOPY %s /usr/local/bin/%s\nRUN chmod 0755 /usr/local/bin/%s\n", Binary, Binary, Binary)
}

func binary(arch string) []byte {
	if arch == "arm64" {
		return static.ExitboxRouterArm64
	}
	return static.ExitboxRouterAmd64
}

func files(arch string) map[string][]byte {
	return map[string][]byte{Binary: binary(arch)}
}

var (
	buildIDs  = map[string]string{}
	buildIDMu sync.Mutex
)

// buildID is a short digest of the router binary, so an ExitBox that ships
// a newer router rebuilds the plugins layer.
func buildID(arch string) string {
	buildIDMu.Lock()
	defer buildIDMu.Unlock()
	if id, ok := buildIDs[arch]; ok {
		return id
	}
	sum := sha256.Sum256(binary(arch))
	id := fmt.Sprintf("%x", sum[:6])
	buildIDs[arch] = id
	return id
}
