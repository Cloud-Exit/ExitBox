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

// Package router is the model router behind the claude-router plugin. It
// sits between Claude Code and the Anthropic API: requests for Claude models
// pass through untouched, and requests for a configured model ID go to a
// local backend that serves the Messages API (llama.cpp, vLLM).
package router

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// VaultPrefix marks a route field as a vault key reference.
const VaultPrefix = "vault:"

// DefaultPort is the port the router listens on inside the container.
const DefaultPort = 4203

// HostGateway is the name a container reaches the host's loopback by.
const HostGateway = "host.docker.internal"

// claudeAliases are the model values Claude Code resolves to a Claude model
// itself. A route with such an ID would never be asked for.
var claudeAliases = []string{"sonnet", "opus", "haiku", "fable", "inherit"}

// Route sends requests for one model ID to a backend.
type Route struct {
	ID       string // model ID Claude Code asks for
	URL      string // backend base URL, without a trailing slash or /v1
	Served   string // model name the backend serves; "" sends ID
	VaultKey string // vault key holding the backend's bearer token; "" for none
}

// ParseRoutes reads the routes setting: comma-separated entries of the form
//
//	model-id=backend-url[=served-name][=vault:KEY]
//
// for example "qwen3.6-27b=http://10.0.0.21:8080=vault:LOCAL_LLM_KEY".
func ParseRoutes(value string) ([]Route, error) {
	var routes []Route
	seen := map[string]bool{}
	for _, entry := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' }) {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		r, err := parseRoute(entry)
		if err != nil {
			return nil, err
		}
		if seen[r.ID] {
			return nil, fmt.Errorf("route %q is listed twice", r.ID)
		}
		seen[r.ID] = true
		routes = append(routes, r)
	}
	return routes, nil
}

func parseRoute(entry string) (Route, error) {
	fields := strings.Split(entry, "=")
	if len(fields) < 2 {
		return Route{}, fmt.Errorf("route %q must be model-id=backend-url[=served-name][=%sKEY]", entry, VaultPrefix)
	}
	r := Route{ID: strings.TrimSpace(fields[0])}
	if err := checkRouteID(r.ID); err != nil {
		return Route{}, err
	}
	base, err := normalizeBackendURL(strings.TrimSpace(fields[1]))
	if err != nil {
		return Route{}, fmt.Errorf("route %q: %w", r.ID, err)
	}
	r.URL = base
	for _, f := range fields[2:] {
		f = strings.TrimSpace(f)
		switch {
		case strings.HasPrefix(f, VaultPrefix):
			key := strings.TrimPrefix(f, VaultPrefix)
			if r.VaultKey != "" {
				return Route{}, fmt.Errorf("route %q has more than one vault key", r.ID)
			}
			if !validVaultKey(key) {
				return Route{}, fmt.Errorf("route %q: %q is not a vault key name (letters, digits, _ . -)", r.ID, key)
			}
			r.VaultKey = key
		case f == "" || strings.ContainsAny(f, " \t"):
			return Route{}, fmt.Errorf("route %q: served name %q must be one word", r.ID, f)
		case r.Served != "":
			return Route{}, fmt.Errorf("route %q has more than one served name", r.ID)
		default:
			r.Served = f
		}
	}
	return r, nil
}

// checkRouteID rejects IDs that are empty, malformed, or would take over
// traffic meant for Claude models.
func checkRouteID(id string) error {
	if id == "" {
		return fmt.Errorf("a route needs a model ID before the first =")
	}
	if strings.ContainsAny(id, " \t\"") {
		return fmt.Errorf("route ID %q must be one word", id)
	}
	lower := strings.ToLower(id)
	if strings.HasPrefix(lower, "claude") {
		return fmt.Errorf("route ID %q starts with \"claude\": it would take over requests for Claude models", id)
	}
	for _, a := range claudeAliases {
		if lower == a {
			return fmt.Errorf("route ID %q is a Claude Code model alias: Claude Code resolves it to a Claude model itself", id)
		}
	}
	return nil
}

// normalizeBackendURL checks a backend base URL and drops a trailing slash
// or /v1, because the router appends /v1/messages itself.
func normalizeBackendURL(raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("missing backend URL")
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		return "", fmt.Errorf("backend URL %q must start with http:// or https://", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("backend URL %q does not parse", raw)
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("backend URL %q has no host", raw)
	}
	if u.User != nil {
		return "", fmt.Errorf("backend URL %q must not contain credentials; use =%sKEY for a token", raw, VaultPrefix)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("backend URL %q must not have a query or fragment", raw)
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("backend URL %q has an invalid port", raw)
		}
	}
	u.Path = strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/v1")
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func validVaultKey(key string) bool {
	if key == "" {
		return false
	}
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '.', r == '-':
		default:
			return false
		}
	}
	return true
}

// Host returns the backend host name or IP, without the port.
func (r Route) Host() string {
	u, err := url.Parse(r.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Port returns the backend port, falling back to the scheme's default.
func (r Route) Port() int {
	u, err := url.Parse(r.URL)
	if err != nil {
		return 0
	}
	if p := u.Port(); p != "" {
		n, _ := strconv.Atoi(p)
		return n
	}
	if u.Scheme == "https" {
		return 443
	}
	return 80
}

// OnHostLoopback reports whether the backend listens on the loopback of the
// machine ExitBox runs on. Inside the container that address is reached
// through the host port relay, not directly.
func (r Route) OnHostLoopback() bool {
	host := strings.ToLower(r.Host())
	if host == "localhost" || host == HostGateway {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ModelName returns the model name sent to the backend.
func (r Route) ModelName() string {
	if r.Served != "" {
		return r.Served
	}
	return r.ID
}

// String renders the route the way ParseRoutes reads it.
func (r Route) String() string {
	s := r.ID + "=" + r.URL
	if r.Served != "" {
		s += "=" + r.Served
	}
	if r.VaultKey != "" {
		s += "=" + VaultPrefix + r.VaultKey
	}
	return s
}

// VaultKeys returns the vault keys the routes refer to, each once, in the
// order they first appear. The container entrypoint and the router agree on
// that order to hand tokens over without naming them.
func VaultKeys(routes []Route) []string {
	seen := map[string]bool{}
	var keys []string
	for _, r := range routes {
		if r.VaultKey != "" && !seen[r.VaultKey] {
			seen[r.VaultKey] = true
			keys = append(keys, r.VaultKey)
		}
	}
	return keys
}

// IDs returns the model IDs of the routes, in order.
func IDs(routes []Route) []string {
	ids := make([]string, 0, len(routes))
	for _, r := range routes {
		ids = append(ids, r.ID)
	}
	return ids
}

// Find returns the route for a model ID.
func Find(routes []Route, model string) (Route, bool) {
	for _, r := range routes {
		if r.ID == model {
			return r, true
		}
	}
	return Route{}, false
}
