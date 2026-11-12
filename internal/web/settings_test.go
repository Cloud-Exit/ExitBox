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

package web

import (
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestSettingsRoundTrip(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	resp, doc := do(t, ts, "GET", "/api/settings", nil, "")
	if resp.StatusCode != 200 {
		t.Fatalf("get: %d", resp.StatusCode)
	}
	if doc["workspaceMenu"] != config.DefaultKeybindings().WorkspaceMenu {
		t.Fatalf("workspace menu key %v", doc["workspaceMenu"])
	}

	doc["rtk"] = true
	doc["noFirewall"] = true
	doc["memory"] = "16g"
	doc["defaultWorkspace"] = "work"
	doc["sessionMenu"] = "C-M-x"
	for _, a := range doc["agents"].([]any) {
		m := a.(map[string]any)
		if m["name"] == "qwen" {
			m["enabled"] = true
			m["version"] = "1.2.3"
		}
	}
	for _, w := range doc["workspaces"].([]any) {
		m := w.(map[string]any)
		if m["name"] == "work" {
			m["directory"] = "~/elsewhere"
			m["packages"] = []any{"postgresql-client", "py3-pip"}
			m["gpu"] = true
		}
	}
	al := doc["allowlist"].(map[string]any)
	al["custom"] = []any{"Example.com", "example.com", "10.0.0.1"}
	al["hostPorts"] = []any{5432}

	resp, out := do(t, ts, "PUT", "/api/settings", doc, "")
	if resp.StatusCode != 200 {
		t.Fatalf("put: %d %v", resp.StatusCode, out)
	}
	c := st.cfg
	if !c.Settings.RTK || !c.Settings.DefaultFlags.NoFirewall || c.Settings.DefaultFlags.Memory != "16g" || c.Settings.DefaultWorkspace != "work" {
		t.Fatalf("settings not saved: %+v", c.Settings)
	}
	if c.Settings.Keybindings.SessionMenu != "C-M-x" || c.Settings.Keybindings.WorkspaceMenu != "" {
		t.Fatalf("keybindings %+v; only changed keys are stored", c.Settings.Keybindings)
	}
	if !c.IsAgentEnabled("qwen") || c.GetAgentVersion("qwen") != "1.2.3" {
		t.Fatal("qwen not enabled and pinned")
	}
	ws := c.Workspace("work")
	if ws.Directory != filepath.Join(st.homeD, "elsewhere") || !ws.GPU || !reflect.DeepEqual(ws.Packages, []string{"postgresql-client", "py3-pip"}) {
		t.Fatalf("workspace %+v", ws)
	}
	if !reflect.DeepEqual(st.al.Custom, []string{"example.com", "10.0.0.1"}) || !reflect.DeepEqual(st.al.HostPorts, []int{5432}) {
		t.Fatalf("allowlist custom %v ports %v", st.al.Custom, st.al.HostPorts)
	}
	if len(st.al.AIProviders) == 0 {
		t.Fatal("read-only categories were dropped")
	}
}

func TestSettingsRejectsBadInput(t *testing.T) {
	cases := map[string]func(*settingsDoc){
		"memory":    func(d *settingsDoc) { d.Memory = "lots" },
		"cpus":      func(d *settingsDoc) { d.CPUs = "x" },
		"key":       func(d *settingsDoc) { d.SessionMenu = "C-M-x; rm" },
		"workspace": func(d *settingsDoc) { d.DefaultWorkspace = "nope" },
		"domain":    func(d *settingsDoc) { d.Allowlist.Custom = []string{"bad domain"} },
		"port":      func(d *settingsDoc) { d.Allowlist.HostPorts = []int{70000} },
		"agent":     func(d *settingsDoc) { d.Agents = []agentDoc{{Name: "nope"}} },
		"dir":       func(d *settingsDoc) { d.Workspaces = []workspaceEdit{{Name: "default", Directory: "relative"}} },
		"package":   func(d *settingsDoc) { d.Workspaces = []workspaceEdit{{Name: "default", Packages: []string{"a b"}}} },
		"flag-pkg": func(d *settingsDoc) {
			d.Workspaces = []workspaceEdit{{Name: "default", Packages: []string{"--allow-untrusted"}}}
		},
		"unknown": func(d *settingsDoc) { d.Workspaces = []workspaceEdit{{Name: "nope"}} },
	}
	for name, mutate := range cases {
		cfg := config.DefaultConfig()
		doc := settingsDoc{}
		mutate(&doc)
		if err := applySettings(cfg, config.DefaultAllowlist(), doc, "/home/u"); err == nil {
			t.Errorf("%s: bad input accepted", name)
		}
	}
}

func TestWebSettings(t *testing.T) {
	cfg := config.DefaultConfig()
	if err := applyWebSettings(cfg, webAuthRequest{Username: "me"}, "127.0.0.1:4848"); err == nil {
		t.Fatal("login without a password accepted")
	}
	if err := applyWebSettings(cfg, webAuthRequest{Username: "me", Password: "correct horse"}, "127.0.0.1:4848"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Web.AuthEnabled() || cfg.Web.Listen != "" {
		t.Fatalf("web %+v", cfg.Web)
	}
	hash := cfg.Web.PasswordHash
	// An empty password keeps the current one.
	if err := applyWebSettings(cfg, webAuthRequest{Username: "me", Listen: "0.0.0.0:9000"}, "127.0.0.1:4848"); err != nil {
		t.Fatal(err)
	}
	if cfg.Web.PasswordHash != hash || cfg.Web.Listen != "0.0.0.0:9000" {
		t.Fatalf("web %+v", cfg.Web)
	}
	// No login while listening publicly.
	if err := applyWebSettings(cfg, webAuthRequest{Disable: true, Listen: "0.0.0.0:9000"}, "127.0.0.1:4848"); err == nil {
		t.Fatal("public listen without login accepted")
	}
	if err := applyWebSettings(cfg, webAuthRequest{Disable: true}, "0.0.0.0:9000"); err == nil {
		t.Fatal("turning off the login of a running public server accepted")
	}
	if err := applyWebSettings(cfg, webAuthRequest{Disable: true}, "127.0.0.1:4848"); err != nil {
		t.Fatal(err)
	}
	if cfg.Web.AuthEnabled() {
		t.Fatal("login still on")
	}
	if err := applyWebSettings(cfg, webAuthRequest{Username: "a:b", Password: "correct horse"}, "127.0.0.1:4848"); err == nil {
		t.Fatal("username with a colon accepted")
	}
}

func TestPutWebSettingsSignsOut(t *testing.T) {
	s, st, ts := newTestServer(t, Options{})
	resp, out := do(t, ts, "PUT", "/api/settings/web", map[string]any{"username": "me", "password": "correct horse"}, "")
	if resp.StatusCode != 200 || out["authEnabled"] != true {
		t.Fatalf("put web: %d %v", resp.StatusCode, out)
	}
	if !st.cfg.Web.AuthEnabled() || !s.auth.enabled() {
		t.Fatal("login not saved or not applied")
	}
	resp, _ = do(t, ts, "GET", "/api/state", nil, "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("state after turning on login: %d", resp.StatusCode)
	}
}

func TestServiceFiles(t *testing.T) {
	spec := ServiceSpec{Exe: "/opt/exit box/exitbox", Args: []string{"web"}, Path: "/usr/bin:/a b$%", Home: "/home/u"}
	unit := SystemdUnit(spec)
	for _, want := range []string{
		`ExecStart="/opt/exit box/exitbox" web`,
		`Environment="PATH=/usr/bin:/a b$$%%"`,
		"WantedBy=default.target",
		"Restart=on-failure",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	plist := LaunchdPlist(ServiceSpec{Exe: "/x/exitbox", Args: []string{"web"}, Path: "/a&b", Home: "/Users/u"}, "/Users/u/Library/Logs/exitbox-web.log")
	for _, want := range []string{"<string>/x/exitbox</string>", "<string>web</string>", "<string>/a&amp;b</string>", "<key>RunAtLoad</key>", "exitbox-web.log"} {
		if !strings.Contains(plist, want) {
			t.Errorf("plist lacks %q:\n%s", want, plist)
		}
	}
}

func TestSettingsKeepDefaultWorkspaceUnset(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	st.cfg.Settings.DefaultWorkspace = ""
	_, doc := do(t, ts, "GET", "/api/settings", nil, "")
	if doc["defaultWorkspace"] != "" {
		t.Fatalf("defaultWorkspace %v, want the unset value", doc["defaultWorkspace"])
	}
	doc["statusBar"] = false
	if resp, out := do(t, ts, "PUT", "/api/settings", doc, ""); resp.StatusCode != 200 {
		t.Fatalf("put: %d %v", resp.StatusCode, out)
	}
	if st.cfg.Settings.DefaultWorkspace != "" {
		t.Fatalf("saving another setting pinned the default workspace to %q", st.cfg.Settings.DefaultWorkspace)
	}
}

func TestWebSettingsListenOnly(t *testing.T) {
	cfg := config.DefaultConfig()
	if err := applyWebSettings(cfg, webAuthRequest{Listen: "127.0.0.1:9000"}, "127.0.0.1:4848"); err != nil {
		t.Fatalf("changing only the listen address: %v", err)
	}
	if cfg.Web.Listen != "127.0.0.1:9000" || cfg.Web.AuthEnabled() {
		t.Fatalf("web %+v", cfg.Web)
	}
	if err := applyWebSettings(cfg, webAuthRequest{Listen: "0.0.0.0:9000"}, "127.0.0.1:4848"); err == nil {
		t.Fatal("public listen without login accepted")
	}
}

func TestServiceFilesCarryEnv(t *testing.T) {
	spec := ServiceSpec{Exe: "/x/exitbox", Args: []string{"web"}, Env: map[string]string{"XDG_CONFIG_HOME": "/cfg", "DOCKER_HOST": "unix:///run/d.sock"}}
	unit := SystemdUnit(spec)
	if !strings.Contains(unit, "Environment=DOCKER_HOST=unix:///run/d.sock\n") || !strings.Contains(unit, "Environment=XDG_CONFIG_HOME=/cfg\n") {
		t.Fatalf("unit env:\n%s", unit)
	}
	plist := LaunchdPlist(spec, "/l")
	if !strings.Contains(plist, "<key>XDG_CONFIG_HOME</key>\n    <string>/cfg</string>") {
		t.Fatalf("plist env:\n%s", plist)
	}
}
