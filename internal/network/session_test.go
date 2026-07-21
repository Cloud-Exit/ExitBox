// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package network

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestSessionURLLifecycle(t *testing.T) {
	// Use a temp dir for cache
	tmpDir := t.TempDir()
	origCache := config.Cache
	config.Cache = tmpDir
	defer func() { config.Cache = origCache }()

	// Register URLs for two containers
	if err := RegisterSessionURLs("container-a", []string{"example.com", "foo.com"}); err != nil {
		t.Fatalf("RegisterSessionURLs(a): %v", err)
	}
	if err := RegisterSessionURLs("container-b", []string{"bar.com", "example.com"}); err != nil {
		t.Fatalf("RegisterSessionURLs(b): %v", err)
	}

	// Collect all - should be deduplicated
	all := collectAllSessionURLs()
	sort.Strings(all)
	if len(all) != 3 {
		t.Errorf("expected 3 unique URLs, got %d: %v", len(all), all)
	}

	// Remove container-a's session
	sessionFile := filepath.Join(sessionDir(), "container-a.urls")
	os.Remove(sessionFile)

	// Remaining should only have bar.com and example.com
	remaining := collectAllSessionURLs()
	sort.Strings(remaining)
	if len(remaining) != 2 {
		t.Errorf("expected 2 remaining URLs, got %d: %v", len(remaining), remaining)
	}

	// Clean all
	os.RemoveAll(sessionDir())
	empty := collectAllSessionURLs()
	if len(empty) != 0 {
		t.Errorf("expected 0 URLs after cleanup, got %d", len(empty))
	}
}

func TestRegisterSessionURLsCreatesMarkerWithoutExtraURLs(t *testing.T) {
	tmpDir := t.TempDir()
	origCache := config.Cache
	config.Cache = tmpDir
	defer func() { config.Cache = origCache }()

	if err := RegisterSessionURLs("container-a", nil); err != nil {
		t.Fatalf("RegisterSessionURLs: %v", err)
	}
	if got := sessionFileCount(); got != 1 {
		t.Fatalf("sessionFileCount() = %d, want 1", got)
	}
	if urls := collectAllSessionURLs(); len(urls) != 0 {
		t.Fatalf("collectAllSessionURLs() = %v, want empty", urls)
	}
}

func TestSessionHostPortsLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	origCache := config.Cache
	config.Cache = tmpDir
	defer func() { config.Cache = origCache }()

	if err := RegisterSessionAccess("container-a", []string{"example.com"}, []int{3000, 5173, 3000, 0}); err != nil {
		t.Fatalf("RegisterSessionAccess(a): %v", err)
	}
	if err := RegisterSessionAccess("container-b", nil, []int{8080, 5173}); err != nil {
		t.Fatalf("RegisterSessionAccess(b): %v", err)
	}

	ports := collectAllSessionHostPorts()
	want := []int{3000, 5173, 8080}
	if len(ports) != len(want) {
		t.Fatalf("host ports = %v, want %v", ports, want)
	}
	for i := range want {
		if ports[i] != want[i] {
			t.Fatalf("host ports = %v, want %v", ports, want)
		}
	}

	urls := collectAllSessionURLs()
	if len(urls) != 1 || urls[0] != "example.com" {
		t.Fatalf("session URLs = %v, want [example.com]", urls)
	}
}
