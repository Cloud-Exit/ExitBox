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

package router

import (
	"fmt"
	"strconv"
)

// Environment variables the exitbox-router binary reads. The container
// entrypoint sets them for the router process only.
const (
	EnvRoutes   = "EXITBOX_ROUTER_ROUTES"   // the routes setting
	EnvPort     = "EXITBOX_ROUTER_PORT"     // listen port
	EnvSanitize = "EXITBOX_ROUTER_SANITIZE" // "false" turns the sanitizer off
	EnvLog      = "EXITBOX_ROUTER_LOG"      // request log file
	EnvUpstream = "EXITBOX_ROUTER_UPSTREAM" // Anthropic API base URL
	// EnvSecretPrefix plus an index carries the token of the n-th vault key
	// in VaultKeys order. Tokens are passed by position so that no vault
	// key name has to be a valid variable name.
	EnvSecretPrefix = "EXITBOX_ROUTER_SECRET_"
)

// Settings is the router configuration read from the environment.
type Settings struct {
	Config  Config
	Port    int
	LogPath string
	// Missing lists the vault keys that routes refer to but that came
	// without a token.
	Missing []string
}

// FromEnv builds the router configuration from environment variables.
func FromEnv(getenv func(string) string) (Settings, error) {
	routes, err := ParseRoutes(getenv(EnvRoutes))
	if err != nil {
		return Settings{}, err
	}
	s := Settings{
		Config: Config{
			Routes:   routes,
			Tokens:   map[string]string{},
			Upstream: getenv(EnvUpstream),
			Sanitize: getenv(EnvSanitize) != "false",
		},
		Port:    DefaultPort,
		LogPath: getenv(EnvLog),
	}
	if p := getenv(EnvPort); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Settings{}, fmt.Errorf("%s=%q is not a port", EnvPort, p)
		}
		s.Port = n
	}
	for i, key := range VaultKeys(routes) {
		if token := getenv(EnvSecretPrefix + strconv.Itoa(i)); token != "" {
			s.Config.Tokens[key] = token
		} else {
			s.Missing = append(s.Missing, key)
		}
	}
	return s, nil
}
