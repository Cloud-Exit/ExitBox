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

package plugins

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/distro"
	"github.com/cloud-exit/exitbox/internal/plugin"
	"github.com/cloud-exit/exitbox/internal/plugins/clauderouter"
	"github.com/cloud-exit/exitbox/internal/plugins/codexrouter"
)

// catalog holds every known plugin, keyed by name. Adding an entry here
// makes the plugin configurable through `exitbox plugins` and the setup
// wizard, and installable into the per-agent plugins image layer.
var catalog = map[string]*plugin.Plugin{}

// Register adds a plugin to the catalog.
func Register(p *plugin.Plugin) {
	catalog[p.Name] = p
}

func init() {
	Register(codexrouter.New())
	Register(clauderouter.New())
}

// Get returns the catalog plugin with the given name, or nil.
func Get(name string) *plugin.Plugin {
	return catalog[name]
}

// Catalog returns every plugin, sorted by name.
func Catalog() []*plugin.Plugin {
	out := make([]*plugin.Plugin, 0, len(catalog))
	for _, p := range catalog {
		out = append(out, p)
	}
	sortPlugins(out)
	return out
}

// For returns the plugins that can extend an agent, sorted by name.
func For(agent string) []*plugin.Plugin {
	var out []*plugin.Plugin
	for _, p := range catalog {
		if p.SupportsAgent(agent) {
			out = append(out, p)
		}
	}
	sortPlugins(out)
	return out
}

// Lookup returns a plugin that supports the given agent. It reports a
// helpful error when the plugin is unknown or not available for the agent.
func Lookup(agent, name string) (*plugin.Plugin, error) {
	p := Get(name)
	if p == nil {
		return nil, fmt.Errorf("unknown plugin %q. Available plugins: %s", name, strings.Join(Names(), ", "))
	}
	if !p.SupportsAgent(agent) {
		return nil, fmt.Errorf("plugin %q does not support agent %q (supported: %s)", name, agent, strings.Join(p.Agents, ", "))
	}
	return p, nil
}

// Names returns every catalog plugin name, sorted.
func Names() []string {
	out := make([]string, 0, len(catalog))
	for name := range catalog {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Agents returns the agents with at least one catalog plugin, sorted.
func Agents() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range catalog {
		for _, a := range p.Agents {
			if !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Enabled returns the catalog plugins enabled for an agent in a workspace,
// sorted by name. Enabled entries without a catalog plugin (e.g. a plugin
// removed from a newer ExitBox) are ignored.
func Enabled(ws *config.Workspace, agent string) []*plugin.Plugin {
	var out []*plugin.Plugin
	for _, name := range ws.EnabledPlugins(agent) {
		p := Get(name)
		if p == nil || !p.SupportsAgent(agent) {
			continue
		}
		out = append(out, p)
	}
	sortPlugins(out)
	return out
}

// Settings returns the stored settings of a plugin merged over its
// defaults, so callers always see the effective configuration.
func Settings(ws *config.Workspace, agent string, p *plugin.Plugin) map[string]string {
	effective := p.DefaultSettings()
	if effective == nil {
		effective = map[string]string{}
	}
	entry, _ := ws.PluginEntry(agent, p.Name)
	for k, v := range entry.Settings {
		effective[k] = v
	}
	return effective
}

// EnvVars returns the container environment describing every enabled plugin
// for an agent: the plugin list plus each plugin's settings.
func EnvVars(ws *config.Workspace, agent string) []string {
	enabled := Enabled(ws, agent)
	if len(enabled) == 0 {
		return nil
	}
	names := make([]string, 0, len(enabled))
	var vars []string
	for _, p := range enabled {
		names = append(names, p.Name)
		vars = append(vars, p.EnvVars(Settings(ws, agent, p))...)
	}
	return append([]string{"EXITBOX_PLUGINS=" + strings.Join(names, ",")}, vars...)
}

// Domains returns the firewall domains required by the enabled plugins,
// including the ones implied by their settings.
func Domains(ws *config.Workspace, agent string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(domains []string) {
		for _, d := range domains {
			if d != "" && !seen[d] {
				seen[d] = true
				out = append(out, d)
			}
		}
	}
	for _, p := range Enabled(ws, agent) {
		add(p.Domains)
		if p.SettingDomains != nil {
			add(p.SettingDomains(Settings(ws, agent, p)))
		}
	}
	sort.Strings(out)
	return out
}

// HostPorts returns the host loopback ports the enabled plugins need
// relayed into the container, sorted.
func HostPorts(ws *config.Workspace, agent string) []int {
	seen := map[int]bool{}
	var out []int
	for _, p := range Enabled(ws, agent) {
		if p.HostPorts == nil {
			continue
		}
		for _, port := range p.HostPorts(Settings(ws, agent, p)) {
			if !seen[port] {
				seen[port] = true
				out = append(out, port)
			}
		}
	}
	sort.Ints(out)
	return out
}

// Conflict reports why a plugin cannot be enabled for an agent: another
// plugin that is enabled for it does the same job. among lists the enabled
// plugin names to check against.
func Conflict(agent, name string, among []string) error {
	p := Get(name)
	if p == nil {
		return nil
	}
	for _, other := range among {
		o := Get(other)
		if o == nil || !o.SupportsAgent(agent) || !p.ConflictsWith(o) {
			continue
		}
		return fmt.Errorf("%s cannot be enabled together with %s for %s: both set the agent's API base URL. Disable one first: exitbox plugins disable %s %s",
			p.Name, o.Name, agent, agent, o.Name)
	}
	return nil
}

// CheckRun reports why a session cannot start with the plugins enabled for
// an agent: two of them conflict, or one cannot work in this session.
func CheckRun(ws *config.Workspace, agent string, in plugin.RunInput) error {
	enabled := Enabled(ws, agent)
	names := make([]string, 0, len(enabled))
	for _, p := range enabled {
		names = append(names, p.Name)
	}
	for _, p := range enabled {
		if err := Conflict(agent, p.Name, names); err != nil {
			return err
		}
		if p.CheckRun == nil {
			continue
		}
		in.Agent = agent
		in.Settings = Settings(ws, agent, p)
		if err := p.CheckRun(in); err != nil {
			return err
		}
	}
	return nil
}

// BuildFiles returns the files the enabled plugins' Dockerfile snippets
// copy from the build context, keyed by file name.
func BuildFiles(ws *config.Workspace, agent string) map[string][]byte {
	out := map[string][]byte{}
	for _, p := range Enabled(ws, agent) {
		if p.Files == nil {
			continue
		}
		for name, data := range p.Files(runtime.GOARCH) {
			out[name] = data
		}
	}
	return out
}

// DockerfileSnippets returns the install steps for the enabled plugins of
// an agent, in catalog order.
func DockerfileSnippets(ws *config.Workspace, agent string, d *distro.Distro) []string {
	if d == nil {
		d = distro.Default()
	}
	var out []string
	for _, p := range Enabled(ws, agent) {
		if p.Dockerfile == nil {
			continue
		}
		entry, _ := ws.PluginEntry(agent, p.Name)
		in := plugin.BuildInput{
			Agent:   agent,
			Version: p.VersionOrDefault(entry.Version),
			Libc:    d.LibcTag(),
		}
		if !d.IsDefault() {
			in.Install = d.InstallSnippet
		}
		snippet := p.Dockerfile(in)
		if strings.TrimSpace(snippet) == "" {
			continue
		}
		out = append(out, snippet)
	}
	return out
}

// BuildHash returns a short hash of everything that changes the plugins
// image layer for an agent: which plugins are enabled and at what version.
// Settings are applied at container start, so they are deliberately not
// part of the hash: editing an API key never forces a rebuild.
func BuildHash(ws *config.Workspace, agent string) string {
	var parts []string
	for _, p := range Enabled(ws, agent) {
		entry, _ := ws.PluginEntry(agent, p.Name)
		part := p.Name + "@" + p.VersionOrDefault(entry.Version)
		if p.BuildID != nil {
			// A plugin that ships its own files changes with ExitBox, not
			// with a version the user pins.
			part += "+" + p.BuildID(runtime.GOARCH)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	h := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return fmt.Sprintf("%x", h[:8])
}

func sortPlugins(list []*plugin.Plugin) {
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
}
