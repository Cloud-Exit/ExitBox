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

package image

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/distro"
	"github.com/cloud-exit/exitbox/internal/plugins"
)

func workspaceWithPlugin() *config.Workspace {
	ws := &config.Workspace{Name: "default"}
	ws.SetPluginEntry("codex", "codex-router", config.PluginEntry{Enabled: true})
	return ws
}

func TestPluginsImageName(t *testing.T) {
	if got := PluginsImageName("codex", "abc123", distro.Default()); got != "exitbox-codex-plugins-abc123" {
		t.Errorf("PluginsImageName() = %q", got)
	}
	if got := ToolsImageName("codex", distro.Default()); got != "exitbox-codex-tools" {
		t.Errorf("ToolsImageName() = %q", got)
	}
}

func TestPluginsDockerfile(t *testing.T) {
	ws := workspaceWithPlugin()
	hash := plugins.BuildHash(ws, "codex")
	df := PluginsDockerfile(ws, "codex", "exitbox-codex-tools", hash, distro.Default())

	for _, want := range []string{
		"FROM exitbox-codex-tools",
		"USER root",
		"codex-router",
		`LABEL exitbox.plugins.hash="` + hash + `"`,
	} {
		if !strings.Contains(df, want) {
			t.Errorf("Dockerfile missing %q:\n%s", want, df)
		}
	}
}

func TestPluginsDockerfileEmptyWithoutPlugins(t *testing.T) {
	ws := &config.Workspace{Name: "default"}
	if df := PluginsDockerfile(ws, "codex", "exitbox-codex-tools", "", distro.Default()); df != "" {
		t.Errorf("Dockerfile = %q, want empty when no plugin is enabled", df)
	}
	// A plugin enabled for another agent must not leak into this layer.
	ws.SetPluginEntry("claude", "codex-router", config.PluginEntry{Enabled: true})
	if df := PluginsDockerfile(ws, "codex", "exitbox-codex-tools", "", distro.Default()); df != "" {
		t.Errorf("Dockerfile = %q, want empty for an agent without plugins", df)
	}
}

func TestWorkspaceHashIncludesPlugins(t *testing.T) {
	dir := t.TempDir()
	base := config.DefaultConfig()
	withPlugin := config.DefaultConfig()
	ws := withPlugin.Workspace("default")
	if ws == nil {
		t.Fatal("default workspace missing")
	}
	ws.SetPluginEntry("codex", "codex-router", config.PluginEntry{Enabled: true})

	if WorkspaceHash(base, "codex", dir, "") == WorkspaceHash(withPlugin, "codex", dir, "") {
		t.Error("WorkspaceHash should change when a plugin is enabled")
	}
	// Another agent's project image is unaffected.
	if WorkspaceHash(base, "qwen", dir, "") != WorkspaceHash(withPlugin, "qwen", dir, "") {
		t.Error("WorkspaceHash for an agent without the plugin should not change")
	}
}

func TestWorkspaceHashIncludesGPU(t *testing.T) {
	dir := t.TempDir()
	base := config.DefaultConfig()
	withGPU := config.DefaultConfig()
	withGPU.Workspace("default").GPU = true

	if WorkspaceHash(base, "codex", dir, "") == WorkspaceHash(withGPU, "codex", dir, "") {
		t.Error("WorkspaceHash should change when GPU passthrough is enabled")
	}
}

func TestPluginsDockerfileForABuiltInPlugin(t *testing.T) {
	ws := &config.Workspace{Name: "default"}
	ws.SetPluginEntry("claude", "claude-router", config.PluginEntry{Enabled: true})
	df := PluginsDockerfile(ws, "claude", "exitbox-claude-tools", plugins.BuildHash(ws, "claude"), distro.Default())
	for _, want := range []string{"FROM exitbox-claude-tools", "COPY exitbox-router /usr/local/bin/exitbox-router"} {
		if !strings.Contains(df, want) {
			t.Errorf("Dockerfile missing %q:\n%s", want, df)
		}
	}
}

func TestWritePluginFiles(t *testing.T) {
	dir := t.TempDir()
	if err := writePluginFiles(dir, map[string][]byte{"exitbox-router": []byte("binary")}); err != nil {
		t.Fatalf("writePluginFiles: %v", err)
	}
	path := filepath.Join(dir, "exitbox-router")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "binary" {
		t.Fatalf("file = %q, %v", data, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm()&0111 == 0 {
			t.Errorf("mode = %v, the copied binary must be executable", info.Mode())
		}
	}
	// A plugin cannot write outside the build context or replace the
	// Dockerfile it is copied by.
	for _, name := range []string{"../escape", "sub/file", "", "Dockerfile", ".."} {
		if err := writePluginFiles(dir, map[string][]byte{name: []byte("x")}); err == nil {
			t.Errorf("file name %q was accepted", name)
		}
	}
	if err := writePluginFiles(dir, nil); err != nil {
		t.Errorf("no files: %v", err)
	}
}
