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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func withConfigHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	orig := config.Home
	config.Home = home
	t.Cleanup(func() { config.Home = orig })
	return home
}

func TestPrepareConfigMount_OnlyActiveWorkspace(t *testing.T) {
	home := withConfigHome(t)
	cfg := &config.Config{}
	cfg.Workspaces.Items = []config.Workspace{
		{Name: "work", Directory: "/src/work", Vault: config.VaultConfig{Enabled: true}},
		{Name: "personal"},
	}
	cfg.Settings.DefaultFlags.NoFirewall = true
	cfg.Settings.DefaultFlags.FullGitSupport = true

	m, err := prepareConfigMount(cfg, "work")
	if err != nil {
		t.Fatalf("prepareConfigMount: %v", err)
	}
	t.Cleanup(func() { _ = m.Cleanup() })

	joined := strings.Join(m.Args, " ")
	if strings.Contains(joined, home+":") {
		t.Errorf("host config root mounted: %s", joined)
	}
	wantRO := m.tempDir + ":" + containerConfigRoot + ":ro"
	wantWS := filepath.Join(home, "profiles", "global", "work") + ":" + containerConfigRoot + "/profiles/global/work"
	want := []string{"-v", wantRO, "-v", wantWS}
	if strings.Join(m.Args, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q, want %q", m.Args, want)
	}

	if _, err := os.Stat(filepath.Join(home, "profiles", "global", "work")); err != nil {
		t.Errorf("host workspace dir not created: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.tempDir, "profiles", "global", "work")); err != nil {
		t.Errorf("nested mountpoint not pre-created: %v", err)
	}
	info, err := os.Stat(m.tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("runtime dir mode = %o, want 755", info.Mode().Perm())
	}
}

func TestPrepareConfigMount_RuntimeConfigContents(t *testing.T) {
	withConfigHome(t)
	cfg := &config.Config{}
	cfg.Workspaces.Items = []config.Workspace{
		{Name: "work", Directory: "/src/work", Packages: []string{"secretpkg"}, Vault: config.VaultConfig{Enabled: true}},
		{Name: "personal"},
	}
	cfg.Settings.DefaultFlags.NoFirewall = true
	cfg.Settings.DefaultFlags.FullGitSupport = true

	m, err := prepareConfigMount(cfg, "personal")
	if err != nil {
		t.Fatalf("prepareConfigMount: %v", err)
	}
	t.Cleanup(func() { _ = m.Cleanup() })

	data, err := os.ReadFile(filepath.Join(m.tempDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{"active: personal", "default_workspace: personal", "name: work", "directory: /src/work", "name: personal"} {
		if !strings.Contains(got, want) {
			t.Errorf("runtime config missing %q:\n%s", want, got)
		}
	}
	for _, leak := range []string{"no_firewall", "full_git_support", "default_flags", "vault", "packages", "secretpkg", "plugins"} {
		if strings.Contains(got, leak) {
			t.Errorf("runtime config leaks %q:\n%s", leak, got)
		}
	}
}

func TestPrepareConfigMount_DefaultWorkspace(t *testing.T) {
	home := withConfigHome(t)
	m, err := prepareConfigMount(&config.Config{}, "")
	if err != nil {
		t.Fatalf("prepareConfigMount: %v", err)
	}
	t.Cleanup(func() { _ = m.Cleanup() })

	if !strings.HasSuffix(m.Args[3], containerConfigRoot+"/profiles/global/default") ||
		!strings.HasPrefix(m.Args[3], filepath.Join(home, "profiles", "global", "default")+":") {
		t.Errorf("default workspace mount = %q", m.Args[3])
	}
	data, err := os.ReadFile(filepath.Join(m.tempDir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "active: default") || !strings.Contains(string(data), "name: default") {
		t.Errorf("runtime config:\n%s", data)
	}
}

func TestPrepareConfigMount_RejectsBadNames(t *testing.T) {
	home := withConfigHome(t)
	for _, name := range []string{"..", ".", "a/b", "../escape", "a:b"} {
		if m, err := prepareConfigMount(&config.Config{}, name); err == nil {
			_ = m.Cleanup()
			t.Errorf("name %q accepted", name)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(home), "escape")); err == nil {
		t.Error("path traversal created a directory outside config home")
	}
}

func TestConfigMount_Cleanup(t *testing.T) {
	withConfigHome(t)
	m, err := prepareConfigMount(&config.Config{}, "work")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Cleanup(); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	if _, err := os.Stat(m.tempDir); !os.IsNotExist(err) {
		t.Errorf("temp dir still exists: %v", err)
	}
	var nilMount *configMount
	if err := nilMount.Cleanup(); err != nil {
		t.Errorf("nil Cleanup: %v", err)
	}
}
