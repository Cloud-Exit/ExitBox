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

// exitbox-router is the model router of the claude-router plugin. It runs
// inside an ExitBox container, in front of Claude Code: requests for Claude
// models pass through to the Anthropic API untouched, requests for a routed
// model ID go to a local backend (llama.cpp, vLLM).
//
// Usage:
//
//	exitbox-router              # serve; configured through EXITBOX_ROUTER_* variables
//	exitbox-router vault-keys   # print the vault keys the routes refer to, one per line
//	exitbox-router health [port]  # exit 0 when a router answers on the port
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/cloud-exit/exitbox/internal/router"
)

// maxLogSize is the size at which the request log is set aside at start.
const maxLogSize = 5 << 20

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		os.Exit(serve())
	case "vault-keys":
		os.Exit(vaultKeys())
	case "health":
		os.Exit(health(os.Args[2:]))
	default:
		fmt.Fprintln(os.Stderr, "Usage: exitbox-router [serve | vault-keys | health [port]]")
		os.Exit(2)
	}
}

func vaultKeys() int {
	routes, err := router.ParseRoutes(os.Getenv(router.EnvRoutes))
	if err != nil {
		fmt.Fprintf(os.Stderr, "exitbox-router: %v\n", err)
		return 2
	}
	for _, key := range router.VaultKeys(routes) {
		fmt.Println(key)
	}
	return 0
}

func health(args []string) int {
	port := os.Getenv(router.EnvPort)
	if len(args) > 0 {
		port = args[0]
	}
	if port == "" {
		port = strconv.Itoa(router.DefaultPort)
	}
	// Never through the firewall proxy: the router is on this machine.
	client := &http.Client{Timeout: time.Second, Transport: &http.Transport{Proxy: nil}}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", port) + router.HealthPath)
	if err != nil {
		return 1
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func serve() int {
	settings, err := router.FromEnv(os.Getenv)
	if err != nil {
		fmt.Fprintf(os.Stderr, "exitbox-router: %v\n", err)
		return 2
	}
	if len(settings.Config.Routes) == 0 {
		fmt.Fprintf(os.Stderr, "exitbox-router: no routes in %s\n", router.EnvRoutes)
		return 2
	}

	var log io.Writer = os.Stderr
	if settings.LogPath != "" {
		f, err := openLog(settings.LogPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "exitbox-router: %v\n", err)
			return 1
		}
		defer func() { _ = f.Close() }()
		log = f
	}
	settings.Config.Log = log

	rt, err := router.New(settings.Config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "exitbox-router: %v\n", err)
		return 2
	}

	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(settings.Port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "exitbox-router: %v\n", err)
		return 1
	}
	// No read or write timeout: a response is a stream that can run for
	// many minutes.
	srv := &http.Server{Handler: rt, ReadHeaderTimeout: 30 * time.Second}

	fmt.Fprintf(os.Stderr, "exitbox-router: listening on %s\n", addr)
	for _, r := range settings.Config.Routes {
		auth := "no token"
		if r.VaultKey != "" {
			auth = "token from vault key " + r.VaultKey
			if contains(settings.Missing, r.VaultKey) {
				auth = "vault key " + r.VaultKey + " was not available, sending no token"
			}
		}
		fmt.Fprintf(os.Stderr, "exitbox-router: %s -> %s as %s (%s)\n", r.ID, r.URL, r.ModelName(), auth)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "exitbox-router: %v\n", err)
		return 1
	}
	return 0
}

// openLog opens the request log for appending. A log that has grown past
// maxLogSize is kept as <name>.1 and a new one is started.
func openLog(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err == nil && info.Size() > maxLogSize {
		if err := os.Rename(path, path+".1"); err != nil {
			return nil, err
		}
	}
	return os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
