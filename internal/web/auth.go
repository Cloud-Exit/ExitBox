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
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	cookieName = "exitbox_web"
	cookieTTL  = 7 * 24 * time.Hour
	// csrfHeader must be on every state-changing request. A page on another
	// origin cannot add it without a CORS preflight, which is never allowed.
	csrfHeader = "X-ExitBox"
	// maxFailures logins may fail per address within failureWindow.
	maxFailures   = 10
	failureWindow = 10 * time.Minute
)

// HashPassword returns the bcrypt hash stored in the config.
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("the password needs at least 8 characters")
	}
	if len(password) > 72 {
		return "", errors.New("the password can have at most 72 bytes")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// auth checks logins and remembers signed-in browsers.
type auth struct {
	mu       sync.Mutex
	username string
	hash     string
	tokens   map[string]time.Time // cookie value -> expiry
	failures map[string][]time.Time
	now      func() time.Time
}

func newAuth(username, hash string) *auth {
	a := &auth{tokens: map[string]time.Time{}, failures: map[string][]time.Time{}, now: time.Now}
	a.set(username, hash)
	return a
}

// set replaces the credentials and signs everyone out.
func (a *auth) set(username, hash string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.username, a.hash = username, hash
	a.tokens = map[string]time.Time{}
}

func (a *auth) enabled() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.username != "" && a.hash != ""
}

func (a *auth) user() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.username
}

var errTooManyFailures = errors.New("too many failed logins; wait a few minutes")

// bcryptSlots bounds how many password checks run at once: each costs
// tens of milliseconds of CPU at cost 12.
var bcryptSlots = make(chan struct{}, 2)

// login checks the credentials and returns a new cookie value.
func (a *auth) login(remote, username, password string) (string, error) {
	a.mu.Lock()
	now := a.now()
	recent := a.failures[remote][:0]
	for _, t := range a.failures[remote] {
		if now.Sub(t) < failureWindow {
			recent = append(recent, t)
		}
	}
	if len(recent) >= maxFailures {
		a.failures[remote] = recent
		a.mu.Unlock()
		return "", errTooManyFailures
	}
	// Count the attempt before checking it, so parallel requests cannot
	// all pass the limit; a success takes it back.
	a.failures[remote] = append(recent, now)
	wantUser, hash := a.username, a.hash
	a.mu.Unlock()

	bcryptSlots <- struct{}{}
	userOK := subtle.ConstantTimeCompare([]byte(username), []byte(wantUser)) == 1
	// Always run bcrypt so a wrong username takes as long as a wrong password.
	passErr := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	<-bcryptSlots
	if !userOK || passErr != nil || wantUser == "" {
		return "", errors.New("wrong username or password")
	}

	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	a.mu.Lock()
	delete(a.failures, remote)
	a.tokens[token] = now.Add(cookieTTL)
	for t, exp := range a.tokens {
		if now.After(exp) {
			delete(a.tokens, t)
		}
	}
	a.mu.Unlock()
	return token, nil
}

func (a *auth) logout(token string) {
	a.mu.Lock()
	delete(a.tokens, token)
	a.mu.Unlock()
}

// valid reports whether the request carries a live session cookie.
func (a *auth) valid(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	exp, ok := a.tokens[c.Value]
	if !ok {
		return false
	}
	if a.now().After(exp) {
		delete(a.tokens, c.Value)
		return false
	}
	return true
}

func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// isLoopbackListen reports whether addr only accepts local connections.
func isLoopbackListen(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// loopbackHost reports whether a Host header names this machine. A
// loopback server refuses other names so a web page cannot reach it
// through DNS rebinding.
func loopbackHost(hostHeader string) bool {
	host := hostHeader
	if h, _, err := net.SplitHostPort(hostHeader); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// sameOrigin reports whether the Origin header, when present, is this
// server. requireOrigin makes a missing header fail too.
func sameOrigin(r *http.Request, requireOrigin bool) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return !requireOrigin
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}
