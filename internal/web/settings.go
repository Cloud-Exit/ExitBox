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
	"errors"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/profile"
)

// settingsDoc is the part of config.yaml and allowlist.yaml the Settings
// page edits. Fields marked read-only are shown but not saved.
type settingsDoc struct {
	AutoUpdate       bool   `json:"autoUpdate"`
	AutoUpdateTools  bool   `json:"autoUpdateTools"`
	StatusBar        bool   `json:"statusBar"`
	RTK              bool   `json:"rtk"`
	DefaultWorkspace string `json:"defaultWorkspace"`

	NoFirewall bool   `json:"noFirewall"`
	ReadOnly   bool   `json:"readOnly"`
	NoEnv      bool   `json:"noEnv"`
	AutoResume bool   `json:"autoResume"`
	FullGit    bool   `json:"fullGit"`
	Memory     string `json:"memory"`
	CPUs       string `json:"cpus"`

	WorkspaceMenu string `json:"workspaceMenu"`
	SessionMenu   string `json:"sessionMenu"`

	Agents     []agentDoc      `json:"agents"`
	Workspaces []workspaceEdit `json:"workspaces"`
	Allowlist  allowlistDoc    `json:"allowlist"`
	Web        *webSettingsDoc `json:"web,omitempty"` // read-only here
	Defaults   *keybindingsDoc `json:"keybindingDefaults,omitempty"`
}

type keybindingsDoc struct {
	WorkspaceMenu string `json:"workspaceMenu"`
	SessionMenu   string `json:"sessionMenu"`
}

// workspaceEdit holds the per-workspace fields the page can change.
type workspaceEdit struct {
	Name      string   `json:"name"`
	Directory string   `json:"directory"`
	GPU       bool     `json:"gpu"`
	Packages  []string `json:"packages"`
	// Read-only.
	Vault       string   `json:"vault,omitempty"`
	Development []string `json:"development,omitempty"`
}

type allowlistDoc struct {
	// Read-only categories.
	AIProviders    []string `json:"aiProviders,omitempty"`
	Development    []string `json:"development,omitempty"`
	CloudServices  []string `json:"cloudServices,omitempty"`
	CommonServices []string `json:"commonServices,omitempty"`
	// Editable.
	Custom    []string `json:"custom"`
	HostPorts []int    `json:"hostPorts"`
}

type webSettingsDoc struct {
	Listen       string `json:"listen"`
	ListenActive string `json:"listenActive"`
	Username     string `json:"username"`
	AuthEnabled  bool   `json:"authEnabled"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	cfg := s.store.loadConfig()
	al := s.store.loadAllowlist()
	st := cfg.Settings
	df := st.DefaultFlags
	kbDef := config.DefaultKeybindings()
	doc := settingsDoc{
		AutoUpdate: st.AutoUpdate, AutoUpdateTools: st.AutoUpdateTools, StatusBar: st.StatusBar, RTK: st.RTK,
		// The configured value, "" when unset: sending the resolved one back
		// would pin it over Workspaces.Active.
		DefaultWorkspace: st.DefaultWorkspace,
		NoFirewall:       df.NoFirewall, ReadOnly: df.ReadOnly, NoEnv: df.NoEnv, AutoResume: df.AutoResume, FullGit: df.FullGitSupport,
		Memory: df.Memory, CPUs: df.CPUs,
		WorkspaceMenu: orDefault(st.Keybindings.WorkspaceMenu, kbDef.WorkspaceMenu),
		SessionMenu:   orDefault(st.Keybindings.SessionMenu, kbDef.SessionMenu),
		Agents:        agentDocs(cfg),
		Allowlist: allowlistDoc{
			AIProviders: al.AIProviders, Development: al.Development, CloudServices: al.CloudServices, CommonServices: al.CommonServices,
			Custom: nonNil(al.Custom), HostPorts: nonNil(al.HostPorts),
		},
		Web: &webSettingsDoc{
			Listen: cfg.Web.ListenOrDefault(), ListenActive: s.opts.Listen,
			Username: cfg.Web.Username, AuthEnabled: s.auth.enabled(),
		},
		Defaults: &keybindingsDoc{WorkspaceMenu: kbDef.WorkspaceMenu, SessionMenu: kbDef.SessionMenu},
	}
	for _, wd := range workspaceDocs(cfg) {
		doc.Workspaces = append(doc.Workspaces, workspaceEdit{
			Name: wd.Name, Directory: wd.Directory, GPU: wd.GPU, Packages: wd.Packages,
			Vault: wd.Vault, Development: wd.Development,
		})
	}
	doc.Workspaces = nonNil(doc.Workspaces)
	writeJSON(w, http.StatusOK, doc)
}

// packageRe is an Alpine package name; never something that reads as a flag.
var packageRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]{0,127}$`)

// applySettings copies the editable fields onto the config and allowlist.
func applySettings(cfg *config.Config, al *config.Allowlist, doc settingsDoc, home string) error {
	if m := strings.TrimSpace(doc.Memory); m != "" && !memoryRe.MatchString(m) {
		return fmt.Errorf("invalid memory limit %q (e.g. 8g)", m)
	}
	if c := strings.TrimSpace(doc.CPUs); c != "" && !cpusRe.MatchString(c) {
		return fmt.Errorf("invalid CPU limit %q (e.g. 4)", c)
	}
	for _, k := range []string{doc.WorkspaceMenu, doc.SessionMenu} {
		if k != "" && !keyRe.MatchString(k) {
			return fmt.Errorf("invalid key %q (tmux notation, e.g. C-M-p)", k)
		}
	}
	if doc.DefaultWorkspace != "" && profile.FindWorkspace(cfg, doc.DefaultWorkspace) == nil {
		return fmt.Errorf("unknown workspace %q", doc.DefaultWorkspace)
	}
	for _, d := range doc.Allowlist.Custom {
		if !validDomain(strings.TrimSpace(d)) {
			return fmt.Errorf("invalid domain %q", d)
		}
	}
	for _, p := range doc.Allowlist.HostPorts {
		if p < 1 || p > 65535 {
			return fmt.Errorf("invalid port %d", p)
		}
	}
	for _, a := range doc.Agents {
		if agents.Get(a.Name) == nil {
			return fmt.Errorf("unknown agent %q", a.Name)
		}
		if a.Version != "" && !validName(a.Version) {
			return fmt.Errorf("invalid version %q", a.Version)
		}
	}

	st := &cfg.Settings
	st.AutoUpdate, st.AutoUpdateTools, st.StatusBar, st.RTK = doc.AutoUpdate, doc.AutoUpdateTools, doc.StatusBar, doc.RTK
	st.DefaultWorkspace = doc.DefaultWorkspace
	df := &st.DefaultFlags
	df.NoFirewall, df.ReadOnly, df.NoEnv, df.AutoResume, df.FullGitSupport = doc.NoFirewall, doc.ReadOnly, doc.NoEnv, doc.AutoResume, doc.FullGit
	df.Memory, df.CPUs = strings.TrimSpace(doc.Memory), strings.TrimSpace(doc.CPUs)

	// Store only keys that differ from the defaults, as exitbox setup does.
	kbDef := config.DefaultKeybindings()
	st.Keybindings = config.KeybindingsConfig{}
	if doc.WorkspaceMenu != "" && doc.WorkspaceMenu != kbDef.WorkspaceMenu {
		st.Keybindings.WorkspaceMenu = doc.WorkspaceMenu
	}
	if doc.SessionMenu != "" && doc.SessionMenu != kbDef.SessionMenu {
		st.Keybindings.SessionMenu = doc.SessionMenu
	}

	for _, a := range doc.Agents {
		cfg.SetAgentEnabled(a.Name, a.Enabled)
		setAgentVersion(cfg, a.Name, strings.TrimSpace(a.Version))
	}

	for _, we := range doc.Workspaces {
		ws := cfg.Workspace(we.Name)
		if ws == nil {
			return fmt.Errorf("unknown workspace %q", we.Name)
		}
		dir := strings.TrimSpace(we.Directory)
		if dir != "" {
			if strings.HasPrefix(dir, "~/") {
				dir = filepath.Join(home, dir[2:])
			}
			if !filepath.IsAbs(dir) {
				return fmt.Errorf("workspace %s: directory %q is not an absolute path", we.Name, we.Directory)
			}
			dir = filepath.Clean(dir)
		}
		var pkgs []string
		for _, p := range we.Packages {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if !packageRe.MatchString(p) {
				return fmt.Errorf("workspace %s: invalid package %q", we.Name, p)
			}
			pkgs = append(pkgs, p)
		}
		ws.Directory, ws.GPU, ws.Packages = dir, we.GPU, pkgs
	}

	var custom []string
	seen := map[string]bool{}
	for _, d := range doc.Allowlist.Custom {
		d = strings.ToLower(strings.TrimSpace(d))
		if d != "" && !seen[d] {
			seen[d] = true
			custom = append(custom, d)
		}
	}
	al.Custom = custom
	al.HostPorts = doc.Allowlist.HostPorts
	return nil
}

func setAgentVersion(cfg *config.Config, name, version string) {
	switch name {
	case "claude":
		cfg.Agents.Claude.Version = version
	case "codex":
		cfg.Agents.Codex.Version = version
	case "copilot":
		cfg.Agents.Copilot.Version = version
	case "cursor":
		cfg.Agents.Cursor.Version = version
	case "kimi":
		cfg.Agents.Kimi.Version = version
	case "opencode":
		cfg.Agents.OpenCode.Version = version
	case "pi":
		cfg.Agents.Pi.Version = version
	case "qwen":
		cfg.Agents.Qwen.Version = version
	}
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var doc settingsDoc
	if err := readJSON(r, &doc); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := s.store.loadConfig()
	al := s.store.loadAllowlist()
	if err := applySettings(cfg, al, doc, s.store.home()); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.saveConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("saving config.yaml: %w", err))
		return
	}
	if err := s.store.saveAllowlist(al); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("saving allowlist.yaml: %w", err))
		return
	}
	s.logf("web: settings saved")
	s.handleGetSettings(w, r)
}

// webAuthRequest changes the login and the listen address.
type webAuthRequest struct {
	Listen   string `json:"listen"`
	Username string `json:"username"`
	// Password empty keeps the current one.
	Password string `json:"password"`
	// Disable removes the login.
	Disable bool `json:"disable"`
}

// applyWebSettings updates cfg.Web. active is the address this server
// listens on now.
func applyWebSettings(cfg *config.Config, req webAuthRequest, active string) error {
	listen := strings.TrimSpace(req.Listen)
	if listen == "" {
		listen = config.DefaultWebListen
	}
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return fmt.Errorf("invalid listen address %q (e.g. 127.0.0.1:4848)", listen)
	}
	next := cfg.Web
	next.Listen = listen
	if listen == config.DefaultWebListen {
		next.Listen = ""
	}
	switch {
	case req.Disable:
		next.Username, next.PasswordHash = "", ""
	case strings.TrimSpace(req.Username) == "" && req.Password == "" && !next.AuthEnabled():
		// Only the listen address changes; there was no login and is none.
	default:
		user := strings.TrimSpace(req.Username)
		if user == "" || len(user) > 64 || strings.ContainsAny(user, ":\x00\n\r") {
			return errors.New("a username is required (up to 64 characters, no colon)")
		}
		if req.Password != "" {
			hash, err := HashPassword(req.Password)
			if err != nil {
				return err
			}
			next.PasswordHash = hash
		}
		if next.PasswordHash == "" {
			return errors.New("a password is required")
		}
		next.Username = user
	}
	if !next.AuthEnabled() && (!isLoopbackListen(listen) || !isLoopbackListen(active)) {
		return errors.New("a login is required while exitbox web listens beyond this machine")
	}
	cfg.Web = next
	return nil
}

func (s *Server) handlePutWebSettings(w http.ResponseWriter, r *http.Request) {
	var req webAuthRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := s.store.loadConfig()
	if err := applyWebSettings(cfg, req, s.opts.Listen); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := s.store.saveConfig(cfg); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("saving config.yaml: %w", err))
		return
	}
	// New credentials sign everyone out, this browser included.
	s.auth.set(cfg.Web.Username, cfg.Web.PasswordHash)
	s.logf("web: login settings changed (login %v)", cfg.Web.AuthEnabled())
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":            true,
		"authEnabled":   cfg.Web.AuthEnabled(),
		"restartNeeded": cfg.Web.ListenOrDefault() != s.opts.Listen,
		"listen":        cfg.Web.ListenOrDefault(),
	})
}
