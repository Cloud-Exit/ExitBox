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
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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

	// New credentials sign everyone out.
	a.set("me", hash)
	if a.valid(r) {
		t.Fatal("cookie still valid after the credentials changed")
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
