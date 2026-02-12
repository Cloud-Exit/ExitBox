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

package config

import (
	"os"
	"path/filepath"
	"runtime"
)

// XDG-compliant paths for exitbox configuration, cache, and data.
var (
	// Home is the configuration directory (~/.config/exitbox).
	Home string
	// Cache is the cache directory (~/.cache/exitbox).
	Cache string
	// Data is the data directory (~/.local/share/exitbox).
	Data string
)

func init() {
	Home = filepath.Join(xdgConfig(), "exitbox")
	Cache = filepath.Join(xdgCache(), "exitbox")
	Data = filepath.Join(xdgData(), "exitbox")
}

func xdgConfig() string {
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("APPDATA"); v != "" {
			return v
		}
	}
	return filepath.Join(homeDir(), ".config")
}

func xdgCache() string {
	if v := os.Getenv("XDG_CACHE_HOME"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return filepath.Join(v, "cache")
		}
	}
	return filepath.Join(homeDir(), ".cache")
}

func xdgData() string {
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		if v := os.Getenv("LOCALAPPDATA"); v != "" {
			return v
		}
	}
	return filepath.Join(homeDir(), ".local", "share")
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// ConfigFile returns the path to config.yaml.
func ConfigFile() string {
	return filepath.Join(Home, "config.yaml")
}

// AllowlistFile returns the path to allowlist.yaml.
func AllowlistFile() string {
	return filepath.Join(Home, "allowlist.yaml")
}

// ProjectsDir returns the path to the projects directory.
func ProjectsDir() string {
	return filepath.Join(Home, "projects")
}

// AgentDir returns the config directory for a specific agent.
func AgentDir(agent string) string {
	return filepath.Join(Home, agent)
}

// VaultDir returns the vault directory for a workspace.
func VaultDir(workspace string) string {
	return filepath.Join(Data, "vaults", workspace)
}

// VaultFile returns the path to the encrypted vault file for a workspace.
func VaultFile(workspace string) string {
	return filepath.Join(VaultDir(workspace), "vault.enc")
}
