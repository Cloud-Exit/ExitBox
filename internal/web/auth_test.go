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
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPasswordLimits(t *testing.T) {
	if _, err := HashPassword("short"); err == nil {
		t.Fatal("a 5 character password was accepted")
	}
	if _, err := HashPassword(strings.Repeat("a", 73)); err == nil {
		t.Fatal("a 73 byte password was accepted; bcrypt would cut it")
	}
	h, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$2") {
		t.Fatalf("hash %q is not bcrypt", h)
	}
}

func TestLoginAndCookie(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	a := newAuth("me", hash)
	if !a.enabled() {
		t.Fatal("auth with user and hash is not enabled")
	}
	if _, err := a.login("1.2.3.4", "me", "wrong password"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := a.login("1.2.3.4", "you", "correct horse"); err == nil {
		t.Fatal("wrong username accepted")
	}
	token, err := a.login("1.2.3.4", "me", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/api/state", nil)
	if a.valid(r) {
		t.Fatal("request without cookie is valid")
	}
	r.Header.Set("Cookie", cookieName+"="+token)
	if !a.valid(r) {
		t.Fatal("request with the login cookie is not valid")
	}

	// The same credentials keep the sessions; new ones sign everyone out.
	a.set("me", hash)
	if !a.valid(r) {
		t.Fatal("cookie invalid after setting the same credentials")
	}
	hash2, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	a.set("me", hash2)
	if a.valid(r) {
		t.Fatal("cookie still valid after the credentials changed")
	}
}

func cookieRequest(token string) *http.Request {
	r := httptest.NewRequest("GET", "/api/state", nil)
	r.Header.Set("Cookie", cookieName+"="+token)
	return r
}

func restartedAuth(t *testing.T, user, hash, path string) *auth {
	t.Helper()
	a := newAuth(user, hash)
	if err := a.persist(path); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestSessionsSurviveRestart(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "web", "sessions.json")
	a := restartedAuth(t, "me", hash, path)
	token, err := a.login("1.2.3.4", "me", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	other, err := a.login("1.2.3.4", "me", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("sessions file mode %v, want 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), token) || strings.Contains(string(data), hash) {
		t.Fatal("the sessions file holds a cookie value or the password hash")
	}

	b := restartedAuth(t, "me", hash, path)
	if !b.valid(cookieRequest(token)) || !b.valid(cookieRequest(other)) {
		t.Fatal("login lost across a restart")
	}
	if b.valid(cookieRequest(token + "0")) {
		t.Fatal("unknown cookie accepted")
	}

	b.logout(token)
	c := restartedAuth(t, "me", hash, path)
	if c.valid(cookieRequest(token)) {
		t.Fatal("logged out cookie valid after a restart")
	}
	if !c.valid(cookieRequest(other)) {
		t.Fatal("logout of one browser signed out another")
	}

	// Expired sessions are dropped on load.
	d := newAuth("me", hash)
	d.now = func() time.Time { return time.Now().Add(cookieTTL + time.Minute) }
	if err := d.persist(path); err != nil {
		t.Fatal(err)
	}
	if len(d.tokens) != 0 {
		t.Fatal("expired session loaded")
	}
}

func TestSessionsDroppedWhenCredentialsChange(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	hash2, err := HashPassword("battery staple")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sessions.json")
	a := restartedAuth(t, "me", hash, path)
	token, err := a.login("1.2.3.4", "me", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	// Changed while the server was stopped (exitbox setup).
	if restartedAuth(t, "me", hash2, path).valid(cookieRequest(token)) {
		t.Fatal("session kept across a password change")
	}
	if restartedAuth(t, "me", hash, path).valid(cookieRequest(token)) {
		t.Fatal("loading with other credentials did not clear the file")
	}

	b := restartedAuth(t, "me", hash, path)
	token, err = b.login("1.2.3.4", "me", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	b.set("you", hash)
	if restartedAuth(t, "me", hash, path).valid(cookieRequest(token)) {
		t.Fatal("session kept after the username changed at runtime")
	}
}

func TestSessionsFileCorruptStartsEmpty(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := newAuth("me", hash)
	if err := a.persist(path); err == nil {
		t.Fatal("corrupt sessions file not reported")
	}
	token, err := a.login("1.2.3.4", "me", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !restartedAuth(t, "me", hash, path).valid(cookieRequest(token)) {
		t.Fatal("a login after a corrupt file was not saved")
	}
}

func TestLoginFailuresPerIPv6Slash64(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("correct horse"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	a := newAuth("me", string(hash))
	for i := 0; i < maxFailures; i++ {
		if _, err := a.login(fmt.Sprintf("2001:db8:1:2::%x", i+1), "me", "nope"); err == nil {
			t.Fatal("wrong password accepted")
		}
	}
	if _, err := a.login("2001:db8:1:2:ffff::1", "me", "correct horse"); err != errTooManyFailures {
		t.Fatalf("same /64: got %v, want errTooManyFailures", err)
	}
	if _, err := a.login("2001:db8:1:3::1", "me", "correct horse"); err != nil {
		t.Fatalf("another /64 is locked out too: %v", err)
	}
	if got := clientKey("10.0.0.1"); got != "10.0.0.1" {
		t.Fatalf("IPv4 key %q", got)
	}
}

func TestLoginFailuresPrunedAndCapped(t *testing.T) {
	a := newAuth("me", "x")
	now := time.Now()
	a.lastSweep = now
	for i := 0; i < maxFailureClients; i++ {
		a.failures[fmt.Sprintf("10.%d.%d.1", i/256, i%256)] = []time.Time{now.Add(-time.Duration(maxFailureClients-i) * time.Millisecond)}
	}
	// Full map: a new client pushes out the stalest one.
	a.sweepFailuresLocked(now, "192.0.2.1")
	if len(a.failures) != maxFailureClients-1 {
		t.Fatalf("%d clients after the cap, want %d", len(a.failures), maxFailureClients-1)
	}
	if _, ok := a.failures["10.0.0.1"]; ok {
		t.Fatal("the stalest client was kept")
	}
	// Past the window everything expired goes.
	later := now.Add(failureWindow + time.Second)
	a.failures["192.0.2.2"] = []time.Time{later}
	a.sweepFailuresLocked(later, "192.0.2.3")
	if len(a.failures) != 1 {
		t.Fatalf("%d clients after the window, want 1", len(a.failures))
	}
}

func TestLoginExpiry(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	a := newAuth("me", hash)
	now := time.Now()
	a.now = func() time.Time { return now }
	token, err := a.login("h", "me", "correct horse")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cookie", cookieName+"="+token)
	now = now.Add(cookieTTL + time.Minute)
	if a.valid(r) {
		t.Fatal("expired cookie is valid")
	}
}

func TestLoginLockout(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	a := newAuth("me", hash)
	now := time.Now()
	a.now = func() time.Time { return now }
	for i := 0; i < maxFailures; i++ {
		if _, err := a.login("9.9.9.9", "me", "nope"); err == nil {
			t.Fatal("wrong password accepted")
		}
	}
	if _, err := a.login("9.9.9.9", "me", "correct horse"); err != errTooManyFailures {
		t.Fatalf("after %d failures: got %v, want errTooManyFailures", maxFailures, err)
	}
	if _, err := a.login("8.8.8.8", "me", "correct horse"); err != nil {
		t.Fatalf("another address is locked out too: %v", err)
	}
	now = now.Add(failureWindow + time.Second)
	if _, err := a.login("9.9.9.9", "me", "correct horse"); err != nil {
		t.Fatalf("still locked out after the window: %v", err)
	}
}

func TestLoopbackChecks(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:4848": true, "[::1]:4848": true, "localhost:1": true,
		"0.0.0.0:4848": false, ":4848": false, "192.168.1.5:4848": false, "bad": false,
	} {
		if got := isLoopbackListen(addr); got != want {
			t.Errorf("isLoopbackListen(%q) = %v, want %v", addr, got, want)
		}
	}
	for host, want := range map[string]bool{
		"127.0.0.1:4848": true, "localhost:4848": true, "LOCALHOST": true, "[::1]:4848": true,
		"evil.example:4848": false, "127.0.0.1.nip.io": false, "": false,
	} {
		if got := loopbackHost(host); got != want {
			t.Errorf("loopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestSameOrigin(t *testing.T) {
	r := httptest.NewRequest("POST", "http://127.0.0.1:4848/api/x", nil)
	if !sameOrigin(r, false) {
		t.Error("no Origin header refused when not required")
	}
	if sameOrigin(r, true) {
		t.Error("no Origin header accepted when required")
	}
	r.Header.Set("Origin", "http://127.0.0.1:4848")
	if !sameOrigin(r, true) {
		t.Error("own origin refused")
	}
	r.Header.Set("Origin", "http://evil.example")
	if sameOrigin(r, false) {
		t.Error("foreign origin accepted")
	}
}

func TestLoginLimitHoldsUnderParallelAttempts(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	a := newAuth("me", hash)
	var wg sync.WaitGroup
	var mu sync.Mutex
	checked := 0
	for i := 0; i < 3*maxFailures; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := a.login("7.7.7.7", "me", "nope")
			if err != errTooManyFailures {
				mu.Lock()
				checked++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if checked > maxFailures {
		t.Fatalf("%d passwords checked in parallel, limit %d", checked, maxFailures)
	}
}

func loginCookie(t *testing.T, ts *httptest.Server, user, password string) string {
	t.Helper()
	resp, out := do(t, ts, "POST", "/api/login", map[string]string{"username": user, "password": password}, "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %v", resp.StatusCode, out)
	}
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatal("no session cookie")
	return ""
}

func TestServerRestartKeepsLogin(t *testing.T) {
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sessions.json")
	_, _, ts := newTestServer(t, Options{Username: "me", PasswordHash: hash, SessionsFile: path})
	cookie := loginCookie(t, ts, "me", "correct horse")
	ts.Close()

	_, _, ts2 := newTestServer(t, Options{Username: "me", PasswordHash: hash, SessionsFile: path})
	if resp, _ := do(t, ts2, "GET", "/api/state", nil, cookie); resp.StatusCode != http.StatusOK {
		t.Fatalf("state after a restart: %d", resp.StatusCode)
	}
	do(t, ts2, "POST", "/api/logout", map[string]string{}, cookie)
	ts2.Close()

	_, _, ts3 := newTestServer(t, Options{Username: "me", PasswordHash: hash, SessionsFile: path})
	if resp, _ := do(t, ts3, "GET", "/api/state", nil, cookie); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("state after logout and a restart: %d", resp.StatusCode)
	}
}

func TestPasswordMatches(t *testing.T) {
	hash, err := HashPassword("s3cret-pass")
	if err != nil {
		t.Fatal(err)
	}
	if !PasswordMatches(hash, "s3cret-pass") {
		t.Fatal("right password does not match its hash")
	}
	if PasswordMatches(hash, "wrong") || PasswordMatches("", "s3cret-pass") {
		t.Fatal("wrong password or empty hash matched")
	}
}
