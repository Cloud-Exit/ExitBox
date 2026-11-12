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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/internal/web"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type webOptions struct {
	listen        string
	username      string
	password      string
	service       bool
	removeService bool
	open          bool
}

func newWebCmd() *cobra.Command {
	var o webOptions
	cmd := &cobra.Command{
		Use:   "web",
		Short: "Run agent sessions in the browser",
		Long: `Serve the ExitBox web UI: start agents in project folders and work with
each session in a terminal window you can move and resize. Settings edits the
same config as exitbox setup.

It listens on 127.0.0.1:4848 unless --listen or the config says otherwise.
Listening beyond this machine needs a login.

Login:
  Set it in exitbox setup (Web access), or pass --username and --password.
  Without --service the flags apply to this run only; with --service they
  are saved so the service uses them. --username without --password asks
  for the password.

Service:
  --service installs a systemd user unit (Linux) or a launchd agent (macOS)
  that runs exitbox web at login, and starts it. --remove-service removes it.`,
		Example: `  exitbox web
  exitbox web --listen 127.0.0.1:9000 --open
  exitbox web --username me
  exitbox web --service
  exitbox web --service --listen 0.0.0.0:4848 --username me`,
		Args: cobra.NoArgs,
		// Runtime errors (a server already running, a busy port) are not usage errors.
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWeb(cmd, o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.listen, "listen", "", "Address to listen on (default from config, else "+config.DefaultWebListen+")")
	f.StringVar(&o.username, "username", "", "Require a login with this username")
	f.StringVar(&o.password, "password", "", "Password for --username (asked for when omitted)")
	f.BoolVar(&o.service, "service", false, "Install and start exitbox web as a login service")
	f.BoolVar(&o.removeService, "remove-service", false, "Stop and remove the login service")
	f.BoolVar(&o.open, "open", false, "Open the UI in the browser")
	return cmd
}

func runWeb(cmd *cobra.Command, o webOptions) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if o.removeService {
		lines, err := web.RemoveService(home)
		if err != nil {
			return err
		}
		for _, l := range lines {
			ui.Info(l)
		}
		return nil
	}

	cfg := config.LoadOrDefault()
	listen := cfg.Web.ListenOrDefault()
	if o.listen != "" {
		if _, _, err := net.SplitHostPort(o.listen); err != nil {
			return fmt.Errorf("invalid --listen %q: %w", o.listen, err)
		}
		listen = o.listen
	}
	username, hash := cfg.Web.Username, cfg.Web.PasswordHash
	if o.username != "" || o.password != "" {
		if o.username == "" {
			return errors.New("--password needs --username")
		}
		pw := o.password
		if pw == "" {
			if pw, err = askPassword(); err != nil {
				return err
			}
		}
		username = strings.TrimSpace(o.username)
		// Reuse the saved hash for the same login: a fresh salt would sign
		// every browser out on each restart.
		if username != cfg.Web.Username || !web.PasswordMatches(cfg.Web.PasswordHash, pw) {
			if hash, err = web.HashPassword(pw); err != nil {
				return err
			}
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	opts := web.Options{
		Listen:       listen,
		Username:     username,
		PasswordHash: hash,
		Version:      Version,
		Command:      sessionCommand(exe),
		Spawn:        spawnHost(exe),
		Logger:       log.New(os.Stderr, "", log.LstdFlags),
	}
	// New validates the address and refuses a public one without a login.
	srv, err := web.New(opts)
	if err != nil {
		return err
	}
	if o.service {
		if o.listen != "" || o.username != "" {
			cfg.Web.Listen = listen
			if listen == config.DefaultWebListen {
				cfg.Web.Listen = ""
			}
			cfg.Web.Username, cfg.Web.PasswordHash = username, hash
			if err := config.SaveConfig(cfg); err != nil {
				return fmt.Errorf("saving the web settings: %w", err)
			}
		}
		env := map[string]string{}
		for _, k := range web.ServiceEnvKeys {
			if v := os.Getenv(k); v != "" {
				env[k] = v
			}
		}
		lines, err := web.InstallService(web.ServiceSpec{Exe: exe, Args: []string{"web"}, Path: os.Getenv("PATH"), Home: home, Env: env})
		if err != nil {
			return err
		}
		for _, l := range lines {
			ui.Info(l)
		}
		ui.Successf("ExitBox web runs at %s", webURL(listen))
		return nil
	}

	// One server owns the sessions: a second one would take every host
	// over and then fail to listen.
	lock, err := srv.Lock()
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	n, err := srv.Restore()
	if err != nil {
		ui.Warnf("Could not reconnect to running sessions: %v", err)
	} else if n > 0 {
		ui.Infof("Reconnected to %d running session(s)", n)
	}
	url := webURL(ln.Addr().String())
	ui.Successf("ExitBox web runs at %s", url)
	if username != "" {
		ui.Infof("Login required for user %s", username)
	}
	ui.Info("Press Ctrl+C to stop. Sessions keep running; close a window to end its session.")
	if o.open {
		openBrowser(url)
	}

	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := srv.Serve(ctx, ln); err != nil && !errors.Is(err, context.Canceled) {
		return err
	}
	return nil
}

// spawnHost starts a session host: this binary in web-session-host mode,
// detached so it outlives exitbox web. The channel closes when it exits.
func spawnHost(exe string) web.SpawnFunc {
	return func(dir, id string) (<-chan struct{}, error) {
		logFile, err := os.OpenFile(filepath.Join(dir, id+".log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return nil, err
		}
		defer func() { _ = logFile.Close() }()
		c := exec.Command(exe, "web-session-host", "--state", dir, "--id", id)
		c.Stdout, c.Stderr = logFile, logFile
		web.DetachProcess(c)
		if err := c.Start(); err != nil {
			return nil, err
		}
		exited := make(chan struct{})
		// Reap it, or an ended host stays a zombie of this server.
		go func() {
			defer close(exited)
			_ = c.Wait()
		}()
		return exited, nil
	}
}

func newWebSessionHostCmd() *cobra.Command {
	var dir, id string
	cmd := &cobra.Command{
		Use:    "web-session-host",
		Short:  "Run one exitbox web session (started by exitbox web)",
		Hidden: true,
		Args:   cobra.NoArgs,
		// The error goes to the host's log for exitbox web to show; usage
		// would only bury it.
		SilenceUsage: true,
		RunE: func(*cobra.Command, []string) error {
			return web.RunHost(dir, id)
		},
	}
	cmd.Flags().StringVar(&dir, "state", "", "Session state directory")
	cmd.Flags().StringVar(&id, "id", "", "Session id")
	return cmd
}

// sessionCommand runs `exitbox run ...` for a browser session, or attaches
// to the tmux of a session started in a terminal.
func sessionCommand(exe string) web.CommandFunc {
	return func(info web.SessionInfo) *exec.Cmd {
		var c *exec.Cmd
		if info.Kind == web.KindAttach {
			rt := "podman"
			if r := container.Detect(); r != nil {
				rt = container.Cmd(r)
			}
			// The entrypoint names the tmux session exitbox-<agent>.
			c = exec.Command(rt, "exec", "-it", "-e", "TERM=xterm-256color", info.Container,
				"tmux", "attach-session", "-t", "exitbox-"+info.Agent)
		} else {
			c = exec.Command(exe, info.Args...)
			c.Dir = info.Dir
		}
		env := make([]string, 0, len(os.Environ())+2)
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			switch k {
			// The browser terminal is xterm.js, not whatever started exitbox web.
			case "TERM", "COLORTERM", "TERM_PROGRAM", "TERM_PROGRAM_VERSION", "TMUX", "TMUX_PANE", "KITTY_WINDOW_ID", "KITTY_PID", "WINDOWID":
				continue
			}
			env = append(env, kv)
		}
		c.Env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
		return c
	}
}

func webURL(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

func askPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", errors.New("--username needs --password when not run in a terminal")
	}
	fmt.Print("Password for exitbox web: ")
	pw, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Repeat it: ")
	again, err := term.ReadPassword(fd)
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(pw) != string(again) {
		return "", errors.New("the passwords do not match")
	}
	return string(pw), nil
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	if err := c.Start(); err != nil {
		ui.Warnf("Could not open a browser: %v", err)
		return
	}
	go func() { _ = c.Wait() }()
}

func init() {
	rootCmd.AddCommand(newWebCmd())
	rootCmd.AddCommand(newWebSessionHostCmd())
}
