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

package session

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/kvstore"
	"github.com/cloud-exit/exitbox/internal/project"
)

// Outputs of session_key_for_name in static/build/docker-entrypoint.
func TestKeyMatchesEntrypoint(t *testing.T) {
	for name, want := range map[string]string{
		"2026-11-12 14:05:09": "2026-11-12_14_05_09_1392163774",
		"__a  b__":            "__a__b___3417753927",
		"feature x/ü":         "feature_x____55553653",
		"":                    "session_4294967295",
		`a\+b`:                "a__b_4126097554",
	} {
		if got := Key(name); got != want {
			t.Errorf("Key(%q) = %q, want %q", name, got, want)
		}
	}
}

func setupSession(t *testing.T, store *kvstore.Store, ws, agent, dir, name, token string) {
	t.Helper()
	key := Key(name)
	sdir := filepath.Join(ProjectSessionsDir(ws, agent, dir), key)
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdir, ".name"), []byte(name), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sdir, ".resume-token"), []byte(token), 0o644); err != nil {
		t.Fatal(err)
	}
	pk := project.GenerateFolderName(dir)
	if err := SaveSessionName(store, agent, pk, key, name); err != nil {
		t.Fatal(err)
	}
	if err := SaveResumeToken(store, agent, pk, key, token); err != nil {
		t.Fatal(err)
	}
}

func TestRename(t *testing.T) {
	oldHome := config.Home
	config.Home = t.TempDir()
	t.Cleanup(func() { config.Home = oldHome })
	store, err := kvstore.Open(kvstore.Options{InMemory: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()

	dir := "/home/u/code/app"
	pk := project.GenerateFolderName(dir)
	setupSession(t, store, "work", "codex", dir, "2026-11-12 14:05:09", "tok-1")
	setupSession(t, store, "work", "codex", dir, "other", "tok-2")
	if err := SetActiveSession(store, "codex", pk, "2026-11-12 14:05:09"); err != nil {
		t.Fatal(err)
	}
	activeFile := filepath.Join(ProjectResumeDir("work", "codex", dir), ".active-session")
	if err := os.WriteFile(activeFile, []byte("2026-11-12 14:05:09"), 0o644); err != nil {
		t.Fatal(err)
	}
	codexOld := codexSessionState("work", Key("2026-11-12 14:05:09"))
	for _, d := range codexOld {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Refusals are typed: a queued rename drops them instead of retrying.
	var refused *RefusedError
	if err := Rename(store, "work", "codex", dir, "2026-11-12 14:05:09", "other"); !errors.As(err, &refused) {
		t.Fatalf("rename onto an existing session: %v", err)
	}
	if err := Rename(store, "work", "codex", dir, "missing", "x"); !errors.As(err, &refused) || err.Error() != "no saved session named 'missing'" {
		t.Fatalf("rename of a missing session: %v", err)
	}
	if err := Rename(store, "work", "codex", dir, "2026-11-12 14:05:09", "invoice export"); err != nil {
		t.Fatal(err)
	}

	names, err := ListNames("work", "codex", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 2 || names[0] != "invoice export" || names[1] != "other" {
		t.Fatalf("names after rename: %v", names)
	}
	kvNames, err := ListNamesKV(store, "codex", pk, "work", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(kvNames) != 2 || kvNames[0] != "invoice export" {
		t.Fatalf("KV names after rename: %v", kvNames)
	}
	token, err := LoadResumeToken(store, "codex", pk, Key("invoice export"), "work", dir)
	if err != nil || token != "tok-1" {
		t.Fatalf("resume token under the new name: %q, %v", token, err)
	}
	if _, err := store.Get([]byte(kvResumeTokenKey("codex", pk, Key("2026-11-12 14:05:09")))); err == nil {
		t.Fatal("old KV token left behind")
	}
	if active, err := GetActiveSession(store, "codex", pk, "work", dir); err != nil || active != "invoice export" {
		t.Fatalf("active session %q, %v", active, err)
	}
	if raw, err := os.ReadFile(activeFile); err != nil || string(raw) != "invoice export" {
		t.Fatalf("active session file %q, %v", raw, err)
	}
	for i, d := range codexSessionState("work", Key("invoice export")) {
		if _, err := os.Stat(d); err != nil {
			t.Errorf("codex state not moved to %s", d)
		}
		if _, err := os.Stat(codexOld[i]); err == nil {
			t.Errorf("codex state left at %s", codexOld[i])
		}
	}

	// Without a KV store the files still move.
	if err := Rename(nil, "work", "codex", dir, "other", "other-2"); err != nil {
		t.Fatal(err)
	}
	if names, _ := ListNames("work", "codex", dir); len(names) != 2 || names[1] != "other-2" {
		t.Fatalf("names after file-only rename: %v", names)
	}
}
