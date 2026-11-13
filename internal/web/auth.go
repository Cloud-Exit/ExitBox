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
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
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

// PasswordMatches reports whether password matches a stored bcrypt hash.
func PasswordMatches(hash, password string) bool {
	return hash != "" && bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

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
	mu        sync.Mutex
	username  string
	hash      string
	tokens    map[string]time.Time // sha256 of the cookie value -> expiry
	failures  map[string][]time.Time
	lastSweep time.Time
	now       func() time.Time
	// path keeps the sessions across restarts; empty keeps them in memory.
	path string
	logf func(format string, args ...any)
	// streams holds the open WebSockets per token key ("" without a
	// login); dropping the token closes their channels.
	streams map[string]map[chan struct{}]struct{}
}

// maxFailureClients caps the failure map; the stalest client goes first.
const maxFailureClients = 4096

func newAuth(username, hash string) *auth {
	a := &auth{tokens: map[string]time.Time{}, failures: map[string][]time.Time{}, streams: map[string]map[chan struct{}]struct{}{}, now: time.Now, logf: func(string, ...any) {}}
	a.username, a.hash = username, hash
	return a
}

// sessionsFile keeps signed-in browsers across restarts.
func sessionsFile() string {
	return filepath.Join(config.Home, "web", "sessions.json")
}

// sessionsDoc is the sessions file. Only token hashes are stored, so the
// file cannot be replayed as cookies.
type sessionsDoc struct {
	// Credential fingerprints the login the sessions belong to.
	Credential string               `json:"credential"`
	Sessions   map[string]time.Time `json:"sessions"`
}

func tokenKey(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func credentialKey(username, hash string) string {
	sum := sha256.Sum256([]byte(username + "\x00" + hash))
	return hex.EncodeToString(sum[:])
}

// persist loads the sessions saved at path and keeps saving them there.
// Sessions of other credentials, or past their expiry, are dropped.
func (a *auth) persist(path string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.path = path
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var doc sessionsDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if a.username == "" || a.hash == "" || doc.Credential != credentialKey(a.username, a.hash) {
		return a.saveLocked()
	}
	now := a.now()
	for k, exp := range doc.Sessions {
		if now.Before(exp) {
			a.tokens[k] = exp
		}
	}
	if len(a.tokens) != len(doc.Sessions) {
		return a.saveLocked()
	}
	return nil
}

// saveLocked writes the sessions file; a.mu must be held.
func (a *auth) saveLocked() error {
	if a.path == "" {
		return nil
	}
	doc := sessionsDoc{Sessions: a.tokens}
	if a.username != "" && a.hash != "" {
		doc.Credential = credentialKey(a.username, a.hash)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return err
	}
	tmp := a.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.path)
}

// save writes the sessions file. A failure keeps the sessions working in
// memory; they just do not outlive this process.
func (a *auth) save() {
	if err := a.saveLocked(); err != nil {
		a.logf("web: saving login sessions: %v", err)
	}
}

// set replaces the credentials. New credentials sign everyone out; the
// same ones keep the sessions.
func (a *auth) set(username, hash string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if username == a.username && hash == a.hash {
		return
	}
	a.username, a.hash = username, hash
	a.tokens = map[string]time.Time{}
	for k := range a.streams {
		a.endStreamsLocked(k)
	}
	a.save()
}

// endStreamsLocked ends the WebSockets of a token key; a.mu must be held.
func (a *auth) endStreamsLocked(key string) {
	for ch := range a.streams[key] {
		close(ch)
	}
	delete(a.streams, key)
}

// streamKey is the login a WebSocket request belongs to: its token key,
// or "" without a login.
func (a *auth) streamKey(r *http.Request) string {
	if !a.enabled() {
		return ""
	}
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "-"
	}
	return tokenKey(c.Value)
}

// track registers a WebSocket of key. The channel closes when that login
// ends; untrack forgets the stream.
func (a *auth) track(key string) (<-chan struct{}, func()) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ch := make(chan struct{})
	if !a.liveLocked(key) {
		close(ch)
		return ch, func() {}
	}
	if a.streams[key] == nil {
		a.streams[key] = map[chan struct{}]struct{}{}
	}
	a.streams[key][ch] = struct{}{}
	return ch, func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		delete(a.streams[key], ch)
		if len(a.streams[key]) == 0 {
			delete(a.streams, key)
		}
	}
}

// live reports whether the login of key still holds, ending its streams
// once it has expired.
func (a *auth) live(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.liveLocked(key)
}

func (a *auth) liveLocked(key string) bool {
	if a.username == "" || a.hash == "" {
		return key == ""
	}
	exp, ok := a.tokens[key]
	if ok && a.now().After(exp) {
		delete(a.tokens, key)
		ok = false
	}
	if !ok {
		a.endStreamsLocked(key)
	}
	return ok
}

// credentials returns the active login, which flags may have given.
func (a *auth) credentials() (username, hash string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.username, a.hash
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

// clientKey is what failed logins count against: the address, or the /64
// of an IPv6 one, since a single host usually holds the whole /64.
func clientKey(remote string) string {
	ip := net.ParseIP(remote)
	if ip == nil || ip.To4() != nil {
		return remote
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

// recentFailures drops the failures of a client past failureWindow.
func recentFailures(times []time.Time, now time.Time) []time.Time {
	recent := times[:0]
	for _, t := range times {
		if now.Sub(t) < failureWindow {
			recent = append(recent, t)
		}
	}
	return recent
}

// sweepFailuresLocked forgets expired failures and keeps the map under
// maxFailureClients; a.mu must be held.
func (a *auth) sweepFailuresLocked(now time.Time, key string) {
	if _, ok := a.failures[key]; (!ok && len(a.failures) >= maxFailureClients) || now.Sub(a.lastSweep) >= failureWindow {
		a.lastSweep = now
		for k, times := range a.failures {
			if recent := recentFailures(times, now); len(recent) > 0 {
				a.failures[k] = recent
			} else {
				delete(a.failures, k)
			}
		}
	}
	if _, ok := a.failures[key]; ok || len(a.failures) < maxFailureClients {
		return
	}
	stalest, at := "", now
	for k, times := range a.failures {
		if last := times[len(times)-1]; last.Before(at) || stalest == "" {
			stalest, at = k, last
		}
	}
	delete(a.failures, stalest)
}

// login checks the credentials and returns a new cookie value.
func (a *auth) login(ctx context.Context, remote, username, password string) (string, error) {
	key := clientKey(remote)
	a.mu.Lock()
	now := a.now()
	a.sweepFailuresLocked(now, key)
	recent := recentFailures(a.failures[key], now)
	if len(recent) >= maxFailures {
		a.failures[key] = recent
		a.mu.Unlock()
		return "", errTooManyFailures
	}
	// Count the attempt before checking it, so parallel requests cannot
	// all pass the limit; a success takes it back.
	a.failures[key] = append(recent, now)
	wantUser, hash := a.username, a.hash
	a.mu.Unlock()

	select {
	case bcryptSlots <- struct{}{}:
	case <-ctx.Done():
		// Not a wrong password: take the attempt back.
		a.mu.Lock()
		if n := len(a.failures[key]); n > 0 {
			a.failures[key] = a.failures[key][:n-1]
		}
		if len(a.failures[key]) == 0 {
			delete(a.failures, key)
		}
		a.mu.Unlock()
		return "", ctx.Err()
	}
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
	defer a.mu.Unlock()
	delete(a.failures, key)
	// Credentials changed while bcrypt ran: this login is for the old ones.
	if a.username != wantUser || a.hash != hash {
		return "", errors.New("the login changed; sign in again")
	}
	a.tokens[tokenKey(token)] = now.Add(cookieTTL)
	for t, exp := range a.tokens {
		if now.After(exp) {
			delete(a.tokens, t)
			a.endStreamsLocked(t)
		}
	}
	a.save()
	return token, nil
}

func (a *auth) logout(token string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	k := tokenKey(token)
	a.endStreamsLocked(k)
	if _, ok := a.tokens[k]; !ok {
		return
	}
	delete(a.tokens, k)
	a.save()
}

// valid reports whether the request carries a live session cookie.
func (a *auth) valid(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	if err != nil || c.Value == "" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	k := tokenKey(c.Value)
	exp, ok := a.tokens[k]
	if !ok {
		return false
	}
	if a.now().After(exp) {
		delete(a.tokens, k)
		a.endStreamsLocked(k)
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
