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

// Package plugin defines the description of an agent plugin: what it is,
// which agents it extends, how it is installed into an ExitBox image, and
// which settings its configuration menu offers.
//
// Plugins are catalog entries, not code that runs on the host. A plugin
// contributes a Dockerfile snippet (baked into the per-agent plugins image
// layer) and a set of settings that are passed into the container as
// environment variables, where docker-entrypoint applies them.
package plugin

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// SettingType selects the editor used by the plugin configuration menu and
// the validation applied to stored values.
type SettingType string

const (
	// TypeString is free-form text.
	TypeString SettingType = "string"
	// TypeBool is a true/false toggle.
	TypeBool SettingType = "bool"
	// TypeInt is a whole number (e.g. a port).
	TypeInt SettingType = "int"
	// TypeEnum is exactly one value from Options.
	TypeEnum SettingType = "enum"
	// TypeMulti is zero or more values from Options, stored comma-separated.
	TypeMulti SettingType = "multi"
	// TypeSecret stores a vault key reference ("vault:KEY"), never the
	// secret value itself. The entrypoint resolves it with exitbox-vault.
	TypeSecret SettingType = "secret"
)

// VaultPrefix marks a setting value as a vault key reference.
const VaultPrefix = "vault:"

// Setting describes one field in a plugin's configuration menu.
type Setting struct {
	Key     string // config key, e.g. "providers"
	Label   string // menu label, e.g. "Providers"
	Help    string // one-line explanation shown under the label
	Type    SettingType
	Default string   // value used when unset ("" = empty)
	Options []string // choices for TypeEnum and TypeMulti
	// VaultKey is the vault key suggested for a TypeSecret setting when the
	// user has not chosen one.
	VaultKey string
	// DependsOn names another setting that must select DependsValue before
	// this one is offered (e.g. a provider's API key only appears once that
	// provider is selected). Empty means always visible.
	DependsOn    string
	DependsValue string
	// Check is an extra rule for a value that already passed the type
	// check, for values with a syntax of their own (e.g. a route list).
	Check func(value string) error
}

// Visible reports whether a setting applies given the current values of the
// plugin's other settings.
func (s *Setting) Visible(settings map[string]string) bool {
	if s.DependsOn == "" {
		return true
	}
	value := settings[s.DependsOn]
	if s.DependsValue == "" {
		return value != "" && value != "false"
	}
	if value == s.DependsValue {
		return true
	}
	for _, item := range SplitMulti(value) {
		if item == s.DependsValue {
			return true
		}
	}
	return false
}

// BuildInput is passed to a plugin's Dockerfile function when the plugins
// image layer is generated.
type BuildInput struct {
	Agent   string // agent the layer is built for
	Version string // resolved version/git ref
	// Libc is the C library of the image flavor, "musl" (Alpine) or "gnu"
	// (the glibc GPU flavor). Empty means musl.
	Libc string
	// Install renders a package install step for the image's distribution.
	// Nil falls back to Alpine's apk.
	Install func(pkgs []string) string
}

// IsMusl reports whether the image being built is musl based.
func (in BuildInput) IsMusl() bool { return in.Libc == "" || in.Libc == "musl" }

// InstallStep renders a package install step for the image's distribution.
// Package names are given as Alpine names and translated by the flavor.
func (in BuildInput) InstallStep(pkgs ...string) string {
	if in.Install != nil {
		return in.Install(pkgs)
	}
	return "RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache " + strings.Join(pkgs, " ")
}

// Plugin is one catalog entry.
type Plugin struct {
	Name        string   // config key and CLI name, e.g. "codex-router"
	Display     string   // human-friendly name
	Description string   // one-line summary shown by plugins list
	Agents      []string // agents this plugin can extend
	Repo        string   // upstream repository URL
	Version     string   // default version (git ref or tag)
	Domains     []string // domains the firewall must allow when enabled
	Settings    []Setting
	// Dockerfile returns the install snippet for the plugins image layer.
	// It runs as root and must be self-contained.
	Dockerfile func(in BuildInput) string
	// SettingDomains returns extra firewall domains implied by the plugin's
	// configuration (e.g. the API hosts of the selected model providers).
	SettingDomains func(settings map[string]string) []string
	// Conflicts names plugins that cannot be enabled for the same agent at
	// the same time (e.g. two routers that both own the agent's base URL).
	Conflicts []string
	// Files returns files the Dockerfile snippet copies into the image,
	// keyed by their name in the build context. arch is the Go name of the
	// image architecture ("amd64", "arm64").
	Files func(arch string) map[string][]byte
	// BuildID identifies the content of Files. It is part of the image
	// hash, so a new ExitBox with changed files rebuilds the layer.
	BuildID func(arch string) string
	// HostPorts returns ports on the host's loopback that the plugin's
	// configuration needs reachable from the container.
	HostPorts func(settings map[string]string) []int
	// VaultKeys returns vault keys the configuration refers to outside of
	// TypeSecret settings (e.g. inside a route list).
	VaultKeys func(settings map[string]string) []string
	// CheckRun reports why a session cannot start with this plugin as it is
	// configured. Nil means there is nothing to check.
	CheckRun func(in RunInput) error
}

// RunInput describes the session a plugin is about to be started in.
type RunInput struct {
	Agent    string
	Settings map[string]string // effective settings, defaults applied
	Firewall bool              // false with --no-firewall
	Ollama   bool              // --ollama
	// BaseURLFrom says where the agent's API base URL is set for this
	// session ("env profile 'x'", "settings.json", ...). Empty when it is
	// not set at all.
	BaseURLFrom string
}

// SupportsAgent reports whether the plugin can be enabled for an agent.
func (p *Plugin) SupportsAgent(agent string) bool {
	for _, a := range p.Agents {
		if a == agent {
			return true
		}
	}
	return false
}

// VersionOrDefault returns the configured version, falling back to the
// plugin's default.
func (p *Plugin) VersionOrDefault(version string) string {
	if version != "" {
		return version
	}
	return p.Version
}

// Setting returns the named setting, or nil when the plugin has no such
// field.
func (p *Plugin) Setting(key string) *Setting {
	for i := range p.Settings {
		if p.Settings[i].Key == key {
			return &p.Settings[i]
		}
	}
	return nil
}

// ConflictsWith reports whether two plugins cannot be enabled together.
// Either side may declare the conflict.
func (p *Plugin) ConflictsWith(other *Plugin) bool {
	if other == nil || p.Name == other.Name {
		return false
	}
	return containsOption(p.Conflicts, other.Name) || containsOption(other.Conflicts, p.Name)
}

// SecretKeys returns every vault key the settings refer to: the values of
// TypeSecret settings and whatever VaultKeys reports, each once, in order.
func (p *Plugin) SecretKeys(settings map[string]string) []string {
	seen := map[string]bool{}
	var keys []string
	add := func(key string) {
		if key != "" && !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	for _, s := range p.Settings {
		if s.Type != TypeSecret || !s.Visible(settings) {
			continue
		}
		if key, ok := VaultKey(settings[s.Key]); ok {
			add(key)
		}
	}
	if p.VaultKeys != nil {
		for _, key := range p.VaultKeys(settings) {
			add(key)
		}
	}
	return keys
}

// EnvPrefix returns the environment variable prefix for this plugin, e.g.
// "EXITBOX_PLUGIN_CODEX_ROUTER".
func (p *Plugin) EnvPrefix() string {
	return "EXITBOX_PLUGIN_" + envToken(p.Name)
}

// EnvName returns the environment variable carrying one setting, e.g.
// "EXITBOX_PLUGIN_CODEX_ROUTER_PROVIDERS".
func (p *Plugin) EnvName(key string) string {
	return p.EnvPrefix() + "_" + envToken(key)
}

// EnvVars renders the plugin's settings as KEY=VALUE environment entries.
// Stored values win over defaults, and settings that resolve to an empty
// value are omitted so the entrypoint can apply its own fallback.
func (p *Plugin) EnvVars(settings map[string]string) []string {
	vars := make([]string, 0, len(p.Settings))
	for _, s := range p.Settings {
		if !s.Visible(settings) {
			continue
		}
		value := settings[s.Key]
		if value == "" {
			value = s.Default
		}
		if value == "" {
			continue
		}
		vars = append(vars, p.EnvName(s.Key)+"="+value)
	}
	sort.Strings(vars)
	return vars
}

// DefaultSettings returns the settings map a freshly enabled plugin starts
// with. Only non-empty defaults are stored.
func (p *Plugin) DefaultSettings() map[string]string {
	out := map[string]string{}
	for _, s := range p.Settings {
		if s.Default != "" {
			out[s.Key] = s.Default
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Validate checks a value against a setting's type and options.
func (s *Setting) Validate(value string) error {
	if value == "" {
		return nil
	}
	switch s.Type {
	case TypeBool:
		if value != "true" && value != "false" {
			return fmt.Errorf("%s must be true or false", s.Key)
		}
	case TypeInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("%s must be a number", s.Key)
		}
	case TypeEnum:
		if !containsOption(s.Options, value) {
			return fmt.Errorf("%s must be one of: %s", s.Key, strings.Join(s.Options, ", "))
		}
	case TypeMulti:
		for _, item := range SplitMulti(value) {
			if !containsOption(s.Options, item) {
				return fmt.Errorf("%s: unknown value %q. Choose from: %s", s.Key, item, strings.Join(s.Options, ", "))
			}
		}
	case TypeSecret:
		if _, ok := VaultKey(value); !ok {
			return fmt.Errorf("%s must be a vault reference like %sMY_KEY", s.Key, VaultPrefix)
		}
	}
	if s.Check != nil {
		if err := s.Check(value); err != nil {
			return fmt.Errorf("%s: %w", s.Key, err)
		}
	}
	return nil
}

// Normalize canonicalizes a user-supplied value for storage. It trims
// whitespace, lowercases booleans, tidies multi-select lists, and adds the
// vault: prefix to a bare vault key name.
func (s *Setting) Normalize(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	switch s.Type {
	case TypeBool:
		return strings.ToLower(value)
	case TypeMulti:
		return strings.Join(SplitMulti(value), ",")
	case TypeSecret:
		if !strings.HasPrefix(value, VaultPrefix) {
			return VaultPrefix + value
		}
	}
	return value
}

// SplitMulti splits a stored multi-select value into its items, dropping
// empty entries and surrounding whitespace.
func SplitMulti(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// VaultKey returns the vault key a secret setting points at. It reports
// false when the value is not a vault reference.
func VaultKey(value string) (string, bool) {
	if !strings.HasPrefix(value, VaultPrefix) {
		return "", false
	}
	key := strings.TrimSpace(strings.TrimPrefix(value, VaultPrefix))
	if key == "" {
		return "", false
	}
	return key, true
}

// VaultRef renders a vault key as a stored secret setting value.
func VaultRef(key string) string { return VaultPrefix + key }

func containsOption(options []string, value string) bool {
	for _, o := range options {
		if o == value {
			return true
		}
	}
	return false
}

// envToken converts a plugin or setting name into the uppercase token used
// in environment variable names.
func envToken(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
