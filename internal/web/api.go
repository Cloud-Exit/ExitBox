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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/env"
	"github.com/cloud-exit/exitbox/internal/kvstore"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/project"
	"github.com/cloud-exit/exitbox/internal/session"
)

// store is how the server reaches ExitBox's files; tests replace it.
type store interface {
	loadConfig() *config.Config
	saveConfig(*config.Config) error
	loadAllowlist() *config.Allowlist
	saveAllowlist(*config.Allowlist) error
	projects() []string
	savedSessions(workspace, agent, dir string) ([]string, error)
	removeSaved(workspace, agent, dir, name string) (bool, error)
	renameSaved(workspace, agent, dir, oldName, newName string) error
	envProfiles(workspace string) ([]string, string, error)
	runtime() string
	// containers lists the agent containers that run now.
	containers() ([]containerDoc, error)
	// stopContainer stops an agent container.
	stopContainer(name string) error
	home() string
	// sshPath finds the system ssh client.
	sshPath() (string, error)
}

type configStore struct{}

func (configStore) loadConfig() *config.Config              { return config.LoadOrDefault() }
func (configStore) saveConfig(c *config.Config) error       { return config.SaveConfig(c) }
func (configStore) loadAllowlist() *config.Allowlist        { return config.LoadAllowlistOrDefault() }
func (configStore) saveAllowlist(a *config.Allowlist) error { return config.SaveAllowlist(a) }

func (configStore) projects() []string {
	entries, err := os.ReadDir(config.ProjectsDir())
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(config.ProjectsDir(), e.Name(), ".project_path"))
		if err != nil {
			continue
		}
		if p := strings.TrimSpace(string(data)); p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func (configStore) savedSessions(workspace, agent, dir string) ([]string, error) {
	return session.ListNames(workspace, agent, dir)
}

// openKV opens a workspace's KV store. Sessions open it for one request
// at a time, so a busy store frees up within moments.
func openKV(workspace string) (*kvstore.Store, error) {
	var err error
	for i := 0; i < 10; i++ {
		var st *kvstore.Store
		if st, err = kvstore.Open(kvstore.Options{Dir: config.KVDir(workspace)}); err == nil {
			return st, nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return nil, fmt.Errorf("the workspace store is busy: %w", err)
}

func (configStore) removeSaved(workspace, agent, dir, name string) (bool, error) {
	kv, err := openKV(workspace)
	if err != nil {
		return false, err
	}
	defer func() { _ = kv.Close() }()
	return session.RemoveByNameKV(kv, agent, project.GenerateFolderName(dir), workspace, dir, name)
}

func (configStore) renameSaved(workspace, agent, dir, oldName, newName string) error {
	kv, err := openKV(workspace)
	if err != nil {
		return err
	}
	defer func() { _ = kv.Close() }()
	return session.Rename(kv, workspace, agent, dir, oldName, newName)
}

func (configStore) stopContainer(name string) error {
	rt := container.Detect()
	if rt == nil {
		return errors.New("no container runtime found")
	}
	return rt.Stop(name)
}

func (configStore) containers() ([]containerDoc, error) {
	rt := container.Detect()
	if rt == nil {
		return nil, nil
	}
	names, err := rt.PS("name=exitbox-", "{{.Names}}")
	if err != nil {
		return nil, err
	}
	var keep []string
	for _, n := range names {
		if containerNameRe.MatchString(n) {
			keep = append(keep, n)
		}
	}
	if len(keep) == 0 {
		return nil, nil
	}
	out, err := exec.Command(rt.Name(), append([]string{"inspect"}, keep...)...).Output()
	if err != nil {
		// A container that stopped between ps and inspect fails the call.
		return nil, err
	}
	return parseInspect(out)
}

func (configStore) envProfiles(workspace string) ([]string, string, error) {
	names, err := env.List(workspace)
	if err != nil {
		return nil, "", err
	}
	def, err := env.GetDefault(workspace)
	if err != nil {
		return names, "", err
	}
	return names, def, nil
}

func (configStore) runtime() string {
	if rt := container.Detect(); rt != nil {
		return rt.Name()
	}
	return ""
}

func (configStore) sshPath() (string, error) { return exec.LookPath("ssh") }

func (configStore) home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/"
	}
	return h
}

// --- state ---

type agentDoc struct {
	Name    string `json:"name"`
	Display string `json:"display"`
	Enabled bool   `json:"enabled"`
	Version string `json:"version,omitempty"`
}

type workspaceDoc struct {
	Name        string   `json:"name"`
	IsDefault   bool     `json:"isDefault"`
	Directory   string   `json:"directory,omitempty"`
	Vault       string   `json:"vault"` // rw, ro or off
	GPU         bool     `json:"gpu"`
	Packages    []string `json:"packages"`
	Development []string `json:"development"`
}

type projectDoc struct {
	Path      string `json:"path"`
	Workspace string `json:"workspace"` // the workspace a run there resolves to
	Scoped    bool   `json:"scoped"`    // a workspace is bound to this directory
}

type savedDoc struct {
	Workspace string `json:"workspace"`
	Agent     string `json:"agent"`
	Dir       string `json:"dir"`
	Name      string `json:"name"`
}

type defaultsDoc struct {
	NoFirewall bool   `json:"noFirewall"`
	ReadOnly   bool   `json:"readOnly"`
	NoEnv      bool   `json:"noEnv"`
	AutoResume bool   `json:"autoResume"`
	FullGit    bool   `json:"fullGit"`
	Memory     string `json:"memory"`
	CPUs       string `json:"cpus"`
}

func agentDocs(cfg *config.Config) []agentDoc {
	var out []agentDoc
	for _, a := range agents.All() {
		out = append(out, agentDoc{Name: a.Name(), Display: a.DisplayName(), Enabled: cfg.IsAgentEnabled(a.Name()), Version: cfg.GetAgentVersion(a.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func defaultWorkspaceName(cfg *config.Config) string {
	if ws, err := profile.ResolveActiveWorkspace(cfg, "", ""); err == nil && ws != nil {
		return ws.Workspace.Name
	}
	return ""
}

func workspaceDocs(cfg *config.Config) []workspaceDoc {
	def := defaultWorkspaceName(cfg)
	out := make([]workspaceDoc, 0, len(cfg.Workspaces.Items))
	for _, w := range cfg.Workspaces.Items {
		d := workspaceDoc{
			Name:        w.Name,
			IsDefault:   w.Name == def,
			Directory:   w.Directory,
			Vault:       "off",
			GPU:         w.GPU,
			Packages:    append([]string{}, w.Packages...),
			Development: append([]string{}, w.Development...),
		}
		if w.Vault.Enabled {
			d.Vault = "rw"
			if w.Vault.ReadOnly {
				d.Vault = "ro"
			}
		}
		out = append(out, d)
	}
	return out
}

func (s *Server) handleState(w http.ResponseWriter, _ *http.Request) {
	cfg := s.store.loadConfig()
	sessions, others := s.liveSessions()
	folders, err := s.folders.load()
	if err != nil {
		s.logf("web: %v", err)
		folders = []Folder{}
	}
	df := cfg.Settings.DefaultFlags

	var projects []projectDoc
	var saved []savedDoc
	names := agents.Names()
	sort.Strings(names)
	for _, dir := range s.store.projects() {
		p := projectDoc{Path: dir}
		if ws, err := profile.ResolveActiveWorkspace(cfg, dir, ""); err == nil && ws != nil {
			p.Workspace = ws.Workspace.Name
			p.Scoped = ws.Scope == profile.ScopeDirectory
		}
		projects = append(projects, p)
		for _, ws := range cfg.Workspaces.Items {
			for _, a := range names {
				list, err := s.store.savedSessions(ws.Name, a, dir)
				if err != nil {
					continue
				}
				for _, n := range list {
					saved = append(saved, savedDoc{Workspace: ws.Name, Agent: a, Dir: dir, Name: n})
				}
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":     s.opts.Version,
		"runtime":     s.store.runtime(),
		"authEnabled": s.auth.enabled(),
		"user":        s.auth.user(),
		"listen":      s.opts.Listen,
		"home":        s.store.home(),
		"agents":      agentDocs(cfg),
		// No env profiles here: they live in the workspace KV, and reading
		// it on every poll would contend with sessions for its lock.
		"workspaces": workspaceDocs(cfg),
		"defaults": defaultsDoc{
			NoFirewall: df.NoFirewall, ReadOnly: df.ReadOnly, NoEnv: df.NoEnv,
			AutoResume: df.AutoResume, FullGit: df.FullGitSupport,
			Memory: orDefault(df.Memory, "8g"), CPUs: orDefault(df.CPUs, "4"),
		},
		"projects": nonNil(projects),
		"saved":    nonNil(saved),
		"sessions": sessions,
		"running":  nonNil(others),
		"folders":  folders,
	})
}

// liveSessions pairs the web sessions with the running containers. Each run
// session learns its container and, when it had none, its session name.
// It returns the sessions and the containers no web session owns: agents
// started in a terminal.
func (s *Server) liveSessions() ([]SessionInfo, []containerDoc) {
	ctrs := s.runningContainers()
	sessions := s.sessions.List()
	owner := matchContainers(sessions, ctrs)
	var others []containerDoc
	owned := map[int]bool{}
	for id, i := range owner {
		owned[i] = true
		c := ctrs[i]
		if sess := s.sessions.Get(id); sess != nil {
			sess.update(func(info *SessionInfo) {
				info.Container = c.Container
				// The container is the truth: a switch from the tmux menu
				// relaunches it under another session or workspace.
				if c.Session != "" {
					info.Name = c.Session
				}
				if c.Workspace != "" {
					info.Workspace = c.Workspace
				}
			})
		}
	}
	for i, c := range ctrs {
		if !owned[i] {
			c.RenameTo = s.pendingFor(c.Workspace, c.Agent, c.Dir, c.Session)
			others = append(others, c)
		}
	}
	s.applyPendingRenames(ctrs, s.sessions.List())
	// Attached windows show a queued rename of the session they show.
	for _, info := range s.sessions.List() {
		if info.Kind != KindAttach || info.Exited {
			continue
		}
		to := s.pendingFor(info.Workspace, info.Agent, info.Dir, info.Name)
		if sess := s.sessions.Get(info.ID); sess != nil && info.RenameTo != to {
			sess.update(func(i *SessionInfo) { i.RenameTo = to })
		}
	}
	return s.sessions.List(), others
}

// runningContainers lists the agent containers, cached for a few seconds:
// every open browser polls the state.
func (s *Server) runningContainers() []containerDoc {
	s.ctrMu.Lock()
	defer s.ctrMu.Unlock()
	if time.Since(s.ctrAt) < 3*time.Second {
		return s.ctrList
	}
	list, err := s.store.containers()
	if err != nil {
		s.logf("web: listing containers: %v", err)
		// Keep the last list rather than hiding every session.
		return s.ctrList
	}
	s.ctrList, s.ctrAt = list, time.Now()
	return list
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// --- folder browser ---

func (s *Server) handleFS(w http.ResponseWriter, r *http.Request) {
	p, err := expandDir(r.URL.Query().Get("path"), s.store.home())
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	entries, err := os.ReadDir(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	showHidden := r.URL.Query().Get("hidden") == "1"
	dirs := []string{}
	for _, e := range entries {
		if !showHidden && isHidden(p, e.Name()) {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e.Name())
			continue
		}
		// Follow symlinks to directories.
		if e.Type()&os.ModeSymlink != 0 {
			if st, err := os.Stat(filepath.Join(p, e.Name())); err == nil && st.IsDir() {
				dirs = append(dirs, e.Name())
			}
		}
	}
	sort.Strings(dirs)
	parent := filepath.Dir(p)
	if parent == p {
		parent = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{"path": p, "parent": parent, "dirs": dirs})
}

// expandDir turns "~/x" or an absolute path into a clean absolute path of
// an existing directory. Empty means home.
func expandDir(p, home string) (string, error) {
	p = strings.TrimSpace(p)
	switch {
	case p == "" || p == "~":
		p = home
	case strings.HasPrefix(p, "~/"):
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%q is not an absolute path", p)
	}
	p = filepath.Clean(p)
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%s is not a directory", p)
	}
	return p, nil
}

// --- sessions ---

// RunOptions are the user-facing options of a session started here. Only
// these fields reach the command line, each through its own flag. There is
// no --no-tmux: web windows and attaching need the session's tmux. None
// holds a secret (an env profile is a name), so sessions keep them.
type RunOptions struct {
	Dir        string   `json:"dir"`
	Agent      string   `json:"agent"`
	Workspace  string   `json:"workspace"` // "" resolves as exitbox run does
	Name       string   `json:"name"`
	EnvProfile string   `json:"envProfile"`
	NoFirewall bool     `json:"noFirewall"`
	ReadOnly   bool     `json:"readOnly"`
	NoEnv      bool     `json:"noEnv"`
	Ollama     bool     `json:"ollama"`
	Fresh      bool     `json:"fresh"`
	Resume     bool     `json:"resume"`
	Memory     string   `json:"memory"`
	CPUs       string   `json:"cpus"`
	AllowURLs  []string `json:"allowUrls"`
}

// startRequest describes a new session: an exitbox run (Kind "run", the
// default) or an SSH session (Kind "ssh" with SSH).
type startRequest struct {
	RunOptions
	Kind string      `json:"kind"`
	SSH  *SSHOptions `json:"ssh"`
	Cols int         `json:"cols"`
	Rows int         `json:"rows"`
}

var (
	memoryRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?[bkmgtBKMGT]?$`)
	cpusRe   = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	domainRe = regexp.MustCompile(`^(\*\.)?[A-Za-z0-9]([A-Za-z0-9-]{0,62}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,62}[A-Za-z0-9])?)*$`)
	keyRe    = regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`)
)

func validDomain(d string) bool {
	if net.ParseIP(strings.Trim(d, "[]")) != nil {
		return true
	}
	return len(d) <= 253 && domainRe.MatchString(d)
}

func validName(n string) bool {
	return n != "" && len(n) <= 128 && !strings.ContainsAny(n, "\x00\n\r") && !strings.HasPrefix(n, "-")
}

// buildArgs turns a request into exitbox arguments.
func buildArgs(cfg *config.Config, req RunOptions, home string) (dir string, args []string, err error) {
	if agents.Get(req.Agent) == nil {
		return "", nil, fmt.Errorf("unknown agent %q", req.Agent)
	}
	if !cfg.IsAgentEnabled(req.Agent) {
		return "", nil, fmt.Errorf("agent %s is not enabled; enable it under Settings > Agents", req.Agent)
	}
	if dir, err = expandDir(req.Dir, home); err != nil {
		return "", nil, err
	}
	args = []string{"run", req.Agent}
	if req.Workspace != "" {
		if profile.FindWorkspace(cfg, req.Workspace) == nil {
			return "", nil, fmt.Errorf("unknown workspace %q", req.Workspace)
		}
		args = append(args, "--workspace", req.Workspace)
	}
	if req.NoFirewall {
		args = append(args, "--no-firewall")
	}
	if req.ReadOnly {
		args = append(args, "--read-only")
	}
	if req.NoEnv {
		args = append(args, "--no-env")
	}
	if req.Ollama {
		args = append(args, "--ollama")
	}
	if name := strings.TrimSpace(req.Name); name != "" {
		if !validName(name) {
			return "", nil, fmt.Errorf("invalid session name %q", name)
		}
		args = append(args, "--name", name)
	}
	switch {
	case req.Fresh:
		args = append(args, "--no-resume")
	case req.Resume && strings.TrimSpace(req.Name) == "":
		args = append(args, "--resume")
	}
	if p := strings.TrimSpace(req.EnvProfile); p != "" {
		if !validName(p) || strings.ContainsAny(p, `/\:`) || p == "." || p == ".." {
			return "", nil, fmt.Errorf("invalid env profile %q", p)
		}
		args = append(args, "--profile", p)
	}
	if m := strings.TrimSpace(req.Memory); m != "" {
		if !memoryRe.MatchString(m) {
			return "", nil, fmt.Errorf("invalid memory limit %q (e.g. 8g)", m)
		}
		args = append(args, "--memory", m)
	}
	if c := strings.TrimSpace(req.CPUs); c != "" {
		if !cpusRe.MatchString(c) {
			return "", nil, fmt.Errorf("invalid CPU limit %q (e.g. 4)", c)
		}
		args = append(args, "--cpus", c)
	}
	for _, d := range req.AllowURLs {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if !validDomain(d) {
			return "", nil, fmt.Errorf("invalid domain %q", d)
		}
		args = append(args, "--allow-urls", d)
	}
	return dir, args, nil
}

// runSession checks options as buildArgs does and returns the session they
// start, keeping the options with it.
func (s *Server) runSession(opts RunOptions) (SessionInfo, error) {
	cfg := s.store.loadConfig()
	dir, args, err := buildArgs(cfg, opts, s.store.home())
	if err != nil {
		return SessionInfo{}, err
	}
	ws := opts.Workspace
	if ws == "" {
		if res, err := profile.ResolveActiveWorkspace(cfg, dir, ""); err == nil && res != nil {
			ws = res.Workspace.Name
		}
	}
	kept := opts.cleaned(dir)
	return SessionInfo{Kind: KindRun, Agent: opts.Agent, Dir: dir, Workspace: ws, Name: kept.Name, Args: args, Request: &kept}, nil
}

// cleaned is the options as buildArgs read them, with dir as the folder.
func (o RunOptions) cleaned(dir string) RunOptions {
	o.Dir = dir
	o.Name = strings.TrimSpace(o.Name)
	o.EnvProfile = strings.TrimSpace(o.EnvProfile)
	o.Memory = strings.TrimSpace(o.Memory)
	o.CPUs = strings.TrimSpace(o.CPUs)
	urls := []string{}
	for _, d := range o.AllowURLs {
		if d = strings.TrimSpace(d); d != "" {
			urls = append(urls, d)
		}
	}
	o.AllowURLs = urls
	return o
}

func (s *Server) handleStartSession(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	var info SessionInfo
	var err error
	switch req.Kind {
	case "", KindRun:
		info, err = s.runSession(req.RunOptions)
	case KindSSH:
		if req.SSH == nil {
			err = errors.New("an SSH session needs ssh options")
		} else {
			info, err = s.sshSession(*req.SSH)
		}
	default:
		err = fmt.Errorf("unknown session kind %q", req.Kind)
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sess, err := s.sessions.Start(info, req.Cols, req.Rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("starting %s: %w", program(info), err))
		return
	}
	if info.Kind == KindSSH {
		s.logf("web: started ssh session %s: ssh %s", sess.Info().ID, strings.Join(info.Args, " "))
	} else {
		s.logf("web: started session %s: exitbox %s (in %s)", sess.Info().ID, strings.Join(info.Args, " "), info.Dir)
	}
	writeJSON(w, http.StatusCreated, sess.Info())
}

// program is what a session runs, for messages.
func program(info SessionInfo) string {
	if info.Kind == KindSSH {
		return "ssh"
	}
	return "exitbox"
}

func (s *Server) handleCloseSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.sessions.Close(id); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	s.logf("web: closed session %s", id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleTerminateSession ends a session for good, wherever it was started:
// its agent container is stopped (so the exitbox run in a terminal ends
// too), and its window's process goes. Closing a window without this only
// detaches.
func (s *Server) handleTerminateSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sess := s.sessions.Get(id)
	if sess == nil {
		writeError(w, http.StatusNotFound, errors.New("no such session"))
		return
	}
	if sess.Info().Kind == KindSSH {
		// No container: ending the ssh process is all there is.
		if err := s.sessions.Close(id); err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		s.logf("web: disconnected ssh session %s", id)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	// Learn the container if the last poll has not yet.
	s.liveSessions()
	info := sess.Info()
	if info.Container != "" && containerNameRe.MatchString(info.Container) {
		if err := s.store.stopContainer(info.Container); err != nil {
			s.logf("web: stop container %s: %v", info.Container, err)
			if info.Kind == KindAttach {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("stopping the session's container: %w", err))
				return
			}
		}
	} else if info.Kind == KindAttach {
		writeError(w, http.StatusConflict, errors.New("the session's container is not known; detach instead"))
		return
	}
	if err := s.sessions.Close(id); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	s.ctrMu.Lock()
	s.ctrAt = time.Time{}
	s.ctrMu.Unlock()
	s.logf("web: terminated session %s (%s)", id, info.Container)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRemoveSaved(w http.ResponseWriter, r *http.Request) {
	var req savedDoc
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := s.store.loadConfig()
	if profile.FindWorkspace(cfg, req.Workspace) == nil || agents.Get(req.Agent) == nil || !validName(req.Name) {
		writeError(w, http.StatusBadRequest, errors.New("unknown saved session"))
		return
	}
	switch s.savedInUse(req.Agent, req.Dir, req.Name) {
	case inUseWindow:
		writeError(w, http.StatusConflict, errors.New("this session is open in a window; close it first"))
		return
	case inUseRunning:
		writeError(w, http.StatusConflict, errors.New("this session is running; remove it once it has ended"))
		return
	}
	ok, err := s.store.removeSaved(req.Workspace, req.Agent, req.Dir, req.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, errors.New("saved session not found"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRestartSession starts a session again and forgets the old one. With
// a request (ssh for an SSH session), it starts with those options instead
// of its own, and one still running is stopped first.
func (s *Server) handleRestartSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cols    int         `json:"cols"`
		Rows    int         `json:"rows"`
		Request *RunOptions `json:"request"`
		SSH     *SSHOptions `json:"ssh"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	old := s.sessions.Get(r.PathValue("id"))
	if old == nil {
		writeError(w, http.StatusNotFound, errors.New("no such session"))
		return
	}
	info := old.Info()
	var next SessionInfo
	if req.SSH != nil {
		if info.Kind != KindSSH {
			writeError(w, http.StatusBadRequest, errors.New("only an SSH session takes ssh options"))
			return
		}
		var err error
		if next, err = s.sshSession(*req.SSH); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !info.Exited {
			if err := old.stop(); err != nil {
				writeError(w, http.StatusInternalServerError, fmt.Errorf("stopping the session: %w", err))
				return
			}
		}
	} else if req.Request != nil {
		if info.Kind != KindRun {
			writeError(w, http.StatusBadRequest, errors.New("only a session started here can change its settings"))
			return
		}
		var err error
		if next, err = s.runSession(*req.Request); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if !info.Exited {
			if err := s.stopForRestart(old); err != nil {
				writeError(w, http.StatusInternalServerError, err)
				return
			}
		}
	} else {
		if !info.Exited {
			writeError(w, http.StatusConflict, errors.New("the session is still running"))
			return
		}
		if info.Kind == KindSSH {
			// Reconnect with the same options, checked again.
			if info.SSH == nil {
				writeError(w, http.StatusConflict, errors.New("this SSH session lost its options; start a new one"))
				return
			}
			var err error
			if next, err = s.sshSession(*info.SSH); err != nil {
				writeError(w, http.StatusBadRequest, err)
				return
			}
			next.Name = info.Name
			next.SSH.Name = info.Name
		} else {
			next = SessionInfo{Kind: info.Kind, Agent: info.Agent, Dir: info.Dir, Workspace: info.Workspace, Name: info.Name, Args: info.Args, Request: info.Request}
			if info.Kind == KindRun && !hasFlag(info.Args, "--name") {
				// Without --name the new run picks a new timestamp name; it is
				// learned from the container again.
				next.Name = ""
			}
			if info.Kind == KindAttach {
				next.Container = info.Container
			}
		}
	}
	sess, err := s.sessions.Start(next, req.Cols, req.Rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("starting %s: %w", program(next), err))
		return
	}
	if err := s.sessions.Close(info.ID); err != nil {
		s.logf("web: forget session %s: %v", info.ID, err)
	}
	if next.Kind == KindSSH {
		s.logf("web: restarted ssh session %s as %s: ssh %s", info.ID, sess.Info().ID, strings.Join(next.Args, " "))
	} else {
		s.logf("web: restarted session %s as %s: exitbox %s (in %s)", info.ID, sess.Info().ID, strings.Join(next.Args, " "), next.Dir)
	}
	writeJSON(w, http.StatusCreated, sess.Info())
}

// stopForRestart ends a running run session as terminating it would, but
// keeps it until its replacement has started.
func (s *Server) stopForRestart(sess *Session) error {
	s.liveSessions()
	info := sess.Info()
	if info.Container != "" && containerNameRe.MatchString(info.Container) {
		if err := s.store.stopContainer(info.Container); err != nil {
			s.logf("web: stop container %s: %v", info.Container, err)
		}
	}
	if err := sess.stop(); err != nil {
		return fmt.Errorf("stopping the session: %w", err)
	}
	s.ctrMu.Lock()
	s.ctrAt = time.Time{}
	s.ctrMu.Unlock()
	return nil
}

// handleEnvProfiles lists a workspace's env profiles. The New session
// dialog asks for them when it needs them.
func (s *Server) handleEnvProfiles(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("workspace")
	if profile.FindWorkspace(s.store.loadConfig(), name) == nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("unknown workspace %q", name))
		return
	}
	out := map[string]any{"profiles": []string{}, "default": ""}
	names, def, err := s.store.envProfiles(name)
	if err != nil {
		out["error"] = "env profiles could not be read: " + err.Error()
	}
	if names != nil {
		out["profiles"] = names
	}
	out["default"] = def
	writeJSON(w, http.StatusOK, out)
}

// handleAttach opens a window on the tmux of a session started elsewhere.
func (s *Server) handleAttach(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Container string `json:"container"`
		Cols      int    `json:"cols"`
		Rows      int    `json:"rows"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	_, others := s.liveSessions()
	var target *containerDoc
	for i := range others {
		if others[i].Container == req.Container {
			target = &others[i]
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, errors.New("that session is not running, or a window already shows it"))
		return
	}
	if target.NoTmux {
		writeError(w, http.StatusConflict, errors.New("this session runs without tmux (--no-tmux), so it can only be used in the terminal that started it"))
		return
	}
	sess, err := s.sessions.Start(SessionInfo{
		Kind: KindAttach, Agent: target.Agent, Dir: target.Dir, Workspace: target.Workspace,
		Name: target.Session, Container: target.Container,
	}, req.Cols, req.Rows)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("attaching: %w", err))
		return
	}
	s.logf("web: attached session %s to %s", sess.Info().ID, target.Container)
	writeJSON(w, http.StatusCreated, sess.Info())
}

type renameRequest struct {
	Name string `json:"name"`
}

func cleanName(n string) (string, error) {
	n = strings.TrimSpace(n)
	if !validName(n) {
		return "", fmt.Errorf("invalid session name %q", n)
	}
	return n, nil
}

// handleRenameSession renames a window's session. A running session takes
// the new name when it ends, because the container saves its conversation
// under the name it started with.
func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	var req renameRequest
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	newName, err := cleanName(req.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	sess := s.sessions.Get(r.PathValue("id"))
	if sess == nil {
		writeError(w, http.StatusNotFound, errors.New("no such session"))
		return
	}
	info := sess.Info()
	switch {
	case info.Kind == KindSSH:
		// Nothing is saved under an SSH session's name: rename it now.
		sess.update(func(i *SessionInfo) {
			i.Name, i.RenameTo, i.RenameError = newName, "", ""
			if i.SSH != nil {
				o := *i.SSH
				o.Name = newName
				i.SSH = &o
			}
		})
	case info.Kind == KindAttach && !info.Exited:
		// Started in a terminal: the rename waits until the session ends.
		if info.Name == "" {
			writeError(w, http.StatusConflict, errors.New("this session has no name to rename yet"))
			return
		}
		if err := s.renames.add(pendingRename{Workspace: info.Workspace, Agent: info.Agent, Dir: info.Dir, Name: info.Name, To: newName}); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		sess.update(func(i *SessionInfo) {
			i.RenameTo = newName
			if newName == i.Name {
				i.RenameTo = ""
			}
		})
	case info.Kind == KindAttach:
		writeError(w, http.StatusConflict, errors.New("this window was detached; rename the session from the sidebar"))
		return
	case info.Name == "":
		writeError(w, http.StatusConflict, errors.New("the session is still starting; try again in a moment"))
		return
	case info.Exited:
		if err := s.store.renameSaved(info.Workspace, info.Agent, info.Dir, info.Name, newName); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		s.noteRename(info.Workspace, info.Agent, info.Dir, info.Name, newName)
		sess.update(func(i *SessionInfo) {
			i.Name, i.RenameTo, i.RenameError = newName, "", ""
			renameArgs(i, newName)
		})
	default:
		sess.update(func(i *SessionInfo) {
			i.RenameTo, i.RenameError = newName, ""
			if newName == i.Name {
				i.RenameTo = ""
			}
		})
	}
	writeJSON(w, http.StatusOK, sess.Info())
}

// renameOnExit applies a rename asked for while the session ran.
func (s *Server) renameOnExit(sess *Session) {
	info := sess.Info()
	if info.Kind == KindSSH || info.RenameTo == "" || info.Name == "" {
		return
	}
	if err := s.store.renameSaved(info.Workspace, info.Agent, info.Dir, info.Name, info.RenameTo); err != nil {
		s.logf("web: rename session %q: %v", info.Name, err)
		sess.update(func(i *SessionInfo) { i.RenameError = err.Error() })
		return
	}
	s.noteRename(info.Workspace, info.Agent, info.Dir, info.Name, info.RenameTo)
	sess.update(func(i *SessionInfo) {
		i.Name, i.RenameTo = i.RenameTo, ""
		renameArgs(i, i.Name)
	})
}

// renameArgs makes starting the session again resume it under name.
func renameArgs(i *SessionInfo, name string) {
	if !hasFlag(i.Args, "--name") {
		return
	}
	i.Args = withName(i.Args, name)
	if i.Request != nil {
		// A new copy: snapshots share the old one.
		r := *i.Request
		r.Name = name
		i.Request = &r
	}
}

// withName points a --name argument at name, so starting the session again
// resumes it under its new name.
func withName(args []string, name string) []string {
	out := append([]string(nil), args...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == "--name" {
			out[i+1] = name
			return out
		}
	}
	return out
}

func (s *Server) handleRenameSaved(w http.ResponseWriter, r *http.Request) {
	var req struct {
		savedDoc
		NewName string `json:"newName"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	newName, err := cleanName(req.NewName)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	cfg := s.store.loadConfig()
	if profile.FindWorkspace(cfg, req.Workspace) == nil || agents.Get(req.Agent) == nil || !validName(req.Name) {
		writeError(w, http.StatusBadRequest, errors.New("unknown saved session"))
		return
	}
	switch s.savedInUse(req.Agent, req.Dir, req.Name) {
	case inUseWindow:
		writeError(w, http.StatusConflict, errors.New("this session is open in a window; rename it there"))
		return
	case inUseRunning:
		writeError(w, http.StatusConflict, errors.New("this session is running; rename it once it has ended"))
		return
	}
	if err := s.store.renameSaved(req.Workspace, req.Agent, req.Dir, req.Name, newName); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	s.noteRename(req.Workspace, req.Agent, req.Dir, req.Name, newName)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// Why a saved session cannot be changed now.
const (
	inUseNone    = iota
	inUseWindow  // a running web session shows it
	inUseRunning // its container runs, started in a terminal
)

// savedInUse reports whether a saved session is running: its saved state
// is still being written, so it must not be renamed or removed.
func (s *Server) savedInUse(agent, dir, name string) int {
	// liveSessions names the web sessions after their containers first.
	sessions, others := s.liveSessions()
	for _, info := range sessions {
		if !info.Exited && info.Agent == agent && info.Dir == dir && info.Name == name {
			return inUseWindow
		}
	}
	for _, c := range others {
		if c.Agent == agent && c.Dir == dir && c.Session == name {
			return inUseRunning
		}
	}
	return inUseNone
}

func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}
