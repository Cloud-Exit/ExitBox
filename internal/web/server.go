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

// Package web serves the ExitBox browser UI: agent sessions in terminal
// windows, plus the settings of exitbox setup.
package web

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/websocket"
)

// assets is the SvelteKit build of web/ (make web). "all:" keeps _app/.
//
//go:embed all:assets
var assetFS embed.FS

// inlineScriptRe finds the inline scripts of index.html; SvelteKit starts
// the app from one.
var inlineScriptRe = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// contentSecurityPolicy allows the page's own files plus the inline scripts
// of index.html by hash.
func contentSecurityPolicy(assets fs.FS) string {
	scripts := "'self'"
	if index, err := fs.ReadFile(assets, "index.html"); err == nil {
		for _, m := range inlineScriptRe.FindAllSubmatch(index, -1) {
			sum := sha256.Sum256(m[1])
			scripts += " 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		}
	}
	return "default-src 'self'; script-src " + scripts + "; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'"
}

// Options configure a Server.
type Options struct {
	// Listen is the address to serve on, e.g. 127.0.0.1:4848.
	Listen string
	// Username and PasswordHash turn on the login. Both empty: no login.
	Username     string
	PasswordHash string
	// Version is shown in the UI.
	Version string
	// Command builds the exitbox process of a session.
	Command CommandFunc
	// Logger receives one line per notable event. Nil logs nowhere.
	Logger *log.Logger
	// Spawn starts a session host (RunHost) detached from this process.
	Spawn SpawnFunc
	// StateDir holds the session hosts' sockets and files; empty uses
	// DefaultStateDir.
	StateDir string
}

// Server is the exitbox web HTTP server.
type Server struct {
	opts     Options
	auth     *auth
	sessions *Manager
	store    store
	handler  http.Handler
	csp      string
	logf     func(format string, args ...any)

	ctrMu   sync.Mutex
	ctrList []containerDoc
	ctrAt   time.Time

	folders *folderStore
	layout  *layoutStore

	vault      vaultBackend
	vaultGuard vaultGuard
}

// New builds a server. It refuses to listen beyond this machine without a
// login, because a session can mount any directory of the host.
func New(opts Options) (*Server, error) {
	if opts.Listen == "" {
		return nil, errors.New("no listen address")
	}
	if _, _, err := net.SplitHostPort(opts.Listen); err != nil {
		return nil, err
	}
	if !isLoopbackListen(opts.Listen) && (opts.Username == "" || opts.PasswordHash == "") {
		return nil, errors.New("listening on " + opts.Listen + " reaches beyond this machine; set a username and password first (exitbox setup, or --username and --password)")
	}
	if opts.Command == nil || opts.Spawn == nil {
		return nil, errors.New("no session command")
	}
	if opts.StateDir == "" {
		dir, err := DefaultStateDir()
		if err != nil {
			return nil, err
		}
		opts.StateDir = dir
	}
	s := &Server{
		opts:     opts,
		auth:     newAuth(opts.Username, opts.PasswordHash),
		sessions: NewManager(opts.Command, opts.Spawn, opts.StateDir),
		store:    configStore{},
		folders:  &folderStore{path: foldersFile()},
		layout:   &layoutStore{path: layoutFile()},
		vault:    diskVault{},
		logf:     func(string, ...any) {},
	}
	if opts.Logger != nil {
		s.logf = opts.Logger.Printf
	}
	s.sessions.onExit = s.renameOnExit
	s.handler = s.routes()
	return s, nil
}

// Restore takes over the sessions an earlier exitbox web left running.
func (s *Server) Restore() (int, error) { return s.sessions.Restore() }

// Handler returns the HTTP handler, for tests.
func (s *Server) Handler() http.Handler { return s.handler }

// Serve runs until ctx ends. Sessions keep running in their hosts; the
// next exitbox web takes them over.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		s.sessions.Disconnect()
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Hijacked WebSocket connections are not tracked by Shutdown; letting
	// go of the sessions ends them.
	s.sessions.Disconnect()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	assets, err := fs.Sub(assetFS, "assets")
	if err != nil {
		panic(err)
	}
	s.csp = contentSecurityPolicy(assets)
	files := http.FileServer(http.FS(assets))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		switch {
		case strings.HasPrefix(r.URL.Path, "/_app/immutable/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		default:
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	}))

	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.handleLogout)
	mux.HandleFunc("GET /api/me", s.handleMe)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/state", s.handleState)
	api.HandleFunc("GET /api/fs", s.handleFS)
	api.HandleFunc("GET /api/env-profiles", s.handleEnvProfiles)
	api.HandleFunc("POST /api/sessions", s.handleStartSession)
	api.HandleFunc("DELETE /api/sessions/{id}", s.handleCloseSession)
	api.HandleFunc("POST /api/sessions/{id}/restart", s.handleRestartSession)
	api.HandleFunc("POST /api/sessions/{id}/rename", s.handleRenameSession)
	api.HandleFunc("POST /api/attach", s.handleAttach)
	api.HandleFunc("POST /api/saved/rename", s.handleRenameSaved)
	api.HandleFunc("PUT /api/folders", s.handlePutFolders)
	api.HandleFunc("GET /api/layout", s.handleGetLayout)
	api.HandleFunc("PUT /api/layout", s.handlePutLayout)
	api.HandleFunc("GET /api/vault", s.handleVaultList)
	api.HandleFunc("POST /api/vault/{workspace}/{action}", s.handleVault)
	api.Handle("GET /api/sessions/{id}/attach", s.attachHandler())
	api.HandleFunc("POST /api/saved/remove", s.handleRemoveSaved)
	api.HandleFunc("GET /api/settings", s.handleGetSettings)
	api.HandleFunc("PUT /api/settings", s.handlePutSettings)
	api.HandleFunc("PUT /api/settings/web", s.handlePutWebSettings)
	mux.Handle("/api/", s.requireAuth(api))

	return s.guard(mux)
}

// guard applies the checks every request needs: a Host this server
// answers to, no cross-site writes, and no framing.
func (s *Server) guard(next http.Handler) http.Handler {
	loopback := isLoopbackListen(s.opts.Listen)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if loopback && !loopbackHost(r.Host) {
			http.Error(w, "unknown host", http.StatusMisdirectedRequest)
			return
		}
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", s.csp)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get(csrfHeader) != "1" || !sameOrigin(r, false) {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth.enabled() && !s.auth.valid(r) {
			writeError(w, http.StatusUnauthorized, errors.New("sign in first"))
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"authEnabled": s.auth.enabled(),
		"signedIn":    !s.auth.enabled() || s.auth.valid(r),
		"user":        s.signedInUser(r),
		"version":     s.opts.Version,
	})
}

// signedInUser is the username, shown only to a signed-in browser.
func (s *Server) signedInUser(r *http.Request) string {
	if s.auth.enabled() && !s.auth.valid(r) {
		return ""
	}
	return s.auth.user()
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.auth.enabled() {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := readJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	token, err := s.auth.login(remoteHost(r), req.Username, req.Password)
	if err != nil {
		status := http.StatusUnauthorized
		if errors.Is(err, errTooManyFailures) {
			status = http.StatusTooManyRequests
		}
		s.logf("web: failed login from %s", remoteHost(r))
		writeError(w, status, err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(cookieTTL / time.Second),
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// secureRequest reports whether the browser reached us over TLS, directly
// or through a TLS reverse proxy. Trusting the header only ever makes the
// cookie stricter.
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieName); err == nil {
		s.auth.logout(c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// attachHandler streams a session over a WebSocket. Output goes out as
// binary frames. The browser sends JSON text frames: {"t":"i","d":"..."}
// for keys and {"t":"r","c":cols,"r":rows} for a resize. The server sends
// {"t":"exit","code":N} when the process ends.
func (s *Server) attachHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := s.sessions.Get(r.PathValue("id"))
		if sess == nil {
			writeError(w, http.StatusNotFound, errors.New("no such session"))
			return
		}
		ws := websocket.Server{
			Handshake: func(_ *websocket.Config, r *http.Request) error {
				if !sameOrigin(r, true) {
					return errors.New("cross-site WebSocket refused")
				}
				return nil
			},
			Handler: func(conn *websocket.Conn) { s.stream(conn, sess) },
		}
		ws.ServeHTTP(w, r)
	})
}

// writeTimeout bounds one WebSocket write to a browser.
const writeTimeout = 30 * time.Second

type clientMsg struct {
	T string `json:"t"`
	D string `json:"d,omitempty"`
	C int    `json:"c,omitempty"`
	R int    `json:"r,omitempty"`
}

func (s *Server) stream(conn *websocket.Conn, sess *Session) {
	defer func() { _ = conn.Close() }()
	conn.PayloadType = websocket.BinaryFrame

	replay, sub := sess.Attach()
	defer sess.Detach(sub)

	// A browser that stops reading (a laptop asleep) must not hold this
	// goroutine forever: every send has a deadline.
	send := func(v any) error {
		if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
			return err
		}
		return websocket.Message.Send(conn, v)
	}
	if len(replay) > 0 {
		if err := send(replay); err != nil {
			return
		}
	}

	go func() {
		defer sub.drop()
		for {
			var raw string
			if err := websocket.Message.Receive(conn, &raw); err != nil {
				return
			}
			var msg clientMsg
			if err := json.Unmarshal([]byte(raw), &msg); err != nil {
				continue
			}
			switch msg.T {
			case "i":
				if err := sess.Write([]byte(msg.D)); err != nil {
					return
				}
			case "r":
				if err := sess.Resize(msg.C, msg.R); err != nil {
					s.logf("web: resize session %s: %v", sess.Info().ID, err)
				}
			}
		}
	}()

	for {
		select {
		case chunk := <-sub.out:
			if err := send(chunk); err != nil {
				return
			}
		case <-sub.gone:
			return
		case <-sess.Done():
			for {
				select {
				case chunk := <-sub.out:
					if err := send(chunk); err != nil {
						return
					}
					continue
				default:
				}
				break
			}
			info := sess.Info()
			msg, err := json.Marshal(struct {
				T    string `json:"t"`
				Code int    `json:"code"`
			}{"exit", info.ExitCode})
			if err != nil {
				return
			}
			_ = send(string(msg))
			return
		}
	}
}

func readJSON(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errors.New("bad request body: " + err.Error())
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": strings.TrimSpace(err.Error())})
}
