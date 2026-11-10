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
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"gopkg.in/yaml.v3"
)

const (
	containerConfigRoot = "/home/user/.exitbox-config"
	defaultWorkspace    = "default"
)

// runtimeConfig is the only config the entrypoint sees: workspace names for
// the switch menu and the active one. Settings, flags, plugins and the
// allowlist stay on the host so the agent cannot alter its next run.
type runtimeConfig struct {
	Version    int                 `yaml:"version"`
	Workspaces runtimeWorkspaces   `yaml:"workspaces"`
	Settings   runtimeSettingsView `yaml:"settings"`
}

type runtimeWorkspaces struct {
	Active string             `yaml:"active"`
	Items  []runtimeWorkspace `yaml:"items"`
}

type runtimeWorkspace struct {
	Name      string `yaml:"name"`
	Directory string `yaml:"directory,omitempty"`
}

type runtimeSettingsView struct {
	DefaultWorkspace string `yaml:"default_workspace"`
}

// configMount holds the volume args that expose ExitBox state to the
// container: a read-only runtime config plus the active workspace's
// profile directory, never the whole host config tree.
type configMount struct {
	Args    []string
	tempDir string
}

// Cleanup removes the generated runtime config directory.
func (m *configMount) Cleanup() error {
	if m == nil || m.tempDir == "" {
		return nil
	}
	return os.RemoveAll(m.tempDir)
}

// prepareConfigMount builds the runtime config in a temp dir and returns the
// mounts for it (read-only), the active workspace's profile dir (read-write),
// and that workspace's subagent store (read-only).
// An empty wsName falls back to "default", matching the entrypoint.
func prepareConfigMount(cfg *config.Config, wsName string) (*configMount, error) {
	if wsName == "" {
		wsName = defaultWorkspace
	}
	if !validWorkspaceDirName(wsName) {
		return nil, fmt.Errorf("invalid workspace name %q", wsName)
	}

	hostWsDir := filepath.Join(config.Home, "profiles", "global", wsName)
	if err := os.MkdirAll(hostWsDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating workspace dir: %w", err)
	}
	// The subagent store doubles as the read-only mountpoint inside the
	// workspace mount, so it must exist before the container starts.
	hostSubagentsDir := filepath.Join(hostWsDir, "subagents")
	if err := os.MkdirAll(hostSubagentsDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating subagent store: %w", err)
	}

	tmp, err := os.MkdirTemp("", "exitbox-cfg-*")
	if err != nil {
		return nil, fmt.Errorf("creating runtime config dir: %w", err)
	}
	m := &configMount{tempDir: tmp}
	fail := func(e error) (*configMount, error) {
		_ = m.Cleanup()
		return nil, e
	}

	// Container user must be able to traverse the read-only mount.
	if err := os.Chmod(tmp, 0o755); err != nil {
		return fail(fmt.Errorf("chmod runtime config dir: %w", err))
	}

	rc := runtimeConfig{Version: 1}
	rc.Workspaces.Active = wsName
	rc.Settings.DefaultWorkspace = wsName
	if cfg != nil {
		for _, w := range cfg.Workspaces.Items {
			rc.Workspaces.Items = append(rc.Workspaces.Items, runtimeWorkspace{Name: w.Name, Directory: w.Directory})
		}
	}
	if len(rc.Workspaces.Items) == 0 {
		rc.Workspaces.Items = []runtimeWorkspace{{Name: wsName}}
	}

	data, err := yaml.Marshal(rc)
	if err != nil {
		return fail(fmt.Errorf("encoding runtime config: %w", err))
	}
	if err := os.WriteFile(filepath.Join(tmp, "config.yaml"), data, 0o644); err != nil {
		return fail(fmt.Errorf("writing runtime config: %w", err))
	}
	// Pre-create the nested mountpoint so the runtime never has to write
	// into the read-only parent.
	if err := os.MkdirAll(filepath.Join(tmp, "profiles", "global", wsName), 0o755); err != nil {
		return fail(fmt.Errorf("creating workspace mountpoint: %w", err))
	}

	containerWsDir := containerConfigRoot + "/profiles/global/" + wsName
	m.Args = []string{
		"-v", tmp + ":" + containerConfigRoot + ":ro",
		"-v", hostWsDir + ":" + containerWsDir,
		// Subagent definitions and the master skill are prompts agents trust
		// on every start; a writable store would let one session plant
		// instructions for the next.
		"-v", hostSubagentsDir + ":" + containerWsDir + "/subagents:ro",
	}
	return m, nil
}

// validWorkspaceDirName rejects names that would escape profiles/global or
// break the -v mount syntax.
func validWorkspaceDirName(name string) bool {
	return name != "." && name != ".." && filepath.Base(name) == name && !strings.Contains(name, ":")
}
