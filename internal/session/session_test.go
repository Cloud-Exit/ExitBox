package session

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/kvstore"
	"github.com/cloud-exit/exitbox/internal/project"
)

func withTempConfigHome(t *testing.T) {
	t.Helper()
	oldHome := config.Home
	config.Home = t.TempDir()
	t.Cleanup(func() {
		config.Home = oldHome
	})
}

func openTestStore(t *testing.T) *kvstore.Store {
	t.Helper()
	s, err := kvstore.Open(kvstore.Options{InMemory: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	return s
}

func writeSessionDir(t *testing.T, workspace, agent, projectDir, dirName, sessionName string) string {
	t.Helper()
	root := ProjectSessionsDir(workspace, agent, projectDir)
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".name"), []byte(sessionName+"\n"), 0644); err != nil {
		t.Fatalf("write session name: %v", err)
	}
	return dir
}

func TestProjectResumeDir(t *testing.T) {
	withTempConfigHome(t)
	projectDir := "/tmp/myproject"
	got := ProjectResumeDir("work", "claude", projectDir)
	want := filepath.Join(config.Home, "profiles", "global", "work", "claude", "projects", project.GenerateFolderName(projectDir))
	if got != want {
		t.Fatalf("ProjectResumeDir() = %q, want %q", got, want)
	}
}

func TestListNames(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()

	writeSessionDir(t, "default", "claude", projectDir, "a", "2026-02-11 12:00:00")
	writeSessionDir(t, "default", "claude", projectDir, "b", "2026-02-11 13:00:00")
	writeSessionDir(t, "default", "claude", projectDir, "c", "2026-02-11 12:00:00") // duplicate name

	got, err := ListNames("default", "claude", projectDir)
	if err != nil {
		t.Fatalf("ListNames() error: %v", err)
	}
	want := []string{"2026-02-11 12:00:00", "2026-02-11 13:00:00"}
	if !slices.Equal(got, want) {
		t.Fatalf("ListNames() = %v, want %v", got, want)
	}
}

func TestListNames_NoDir(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()
	got, err := ListNames("default", "claude", projectDir)
	if err != nil {
		t.Fatalf("ListNames() error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %v", got)
	}
}

func TestNamedDirs(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()
	a := writeSessionDir(t, "default", "claude", projectDir, "a", "same")
	c := writeSessionDir(t, "default", "claude", projectDir, "c", "same")
	b := writeSessionDir(t, "default", "claude", projectDir, "b", "other")

	got, err := NamedDirs("default", "claude", projectDir)
	if err != nil {
		t.Fatalf("NamedDirs() error: %v", err)
	}
	if !slices.Equal(got["same"], []string{a, c}) || !slices.Equal(got["other"], []string{b}) || len(got) != 2 {
		t.Fatalf("NamedDirs() = %v", got)
	}
}

func TestRemoveByName(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()

	d1 := writeSessionDir(t, "default", "claude", projectDir, "a", "keep")
	d2 := writeSessionDir(t, "default", "claude", projectDir, "b", "drop")
	d3 := writeSessionDir(t, "default", "claude", projectDir, "c", "drop")

	activeFile := filepath.Join(ProjectResumeDir("default", "claude", projectDir), ".active-session")
	if err := os.MkdirAll(filepath.Dir(activeFile), 0755); err != nil {
		t.Fatalf("mkdir active dir: %v", err)
	}
	if err := os.WriteFile(activeFile, []byte("drop\n"), 0644); err != nil {
		t.Fatalf("write active file: %v", err)
	}

	removed, err := RemoveByName("default", "claude", projectDir, "drop")
	if err != nil {
		t.Fatalf("RemoveByName() error: %v", err)
	}
	if !removed {
		t.Fatalf("expected RemoveByName to remove at least one directory")
	}

	if _, err := os.Stat(d1); err != nil {
		t.Fatalf("expected keep dir to remain, stat err: %v", err)
	}
	if _, err := os.Stat(d2); !os.IsNotExist(err) {
		t.Fatalf("expected drop dir #1 removed, stat err: %v", err)
	}
	if _, err := os.Stat(d3); !os.IsNotExist(err) {
		t.Fatalf("expected drop dir #2 removed, stat err: %v", err)
	}
	if _, err := os.Stat(activeFile); !os.IsNotExist(err) {
		t.Fatalf("expected active session pointer removed, stat err: %v", err)
	}
}

func TestRemoveByName_NotFound(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()
	writeSessionDir(t, "default", "claude", projectDir, "a", "keep")

	removed, err := RemoveByName("default", "claude", projectDir, "missing")
	if err != nil {
		t.Fatalf("RemoveByName() error: %v", err)
	}
	if removed {
		t.Fatalf("expected removed=false when session does not exist")
	}
}

func TestResolveSelector_ByName(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()
	writeSessionDir(t, "default", "claude", projectDir, "id_111", "2026-02-11 12:00:00")

	name, ok, err := ResolveSelector("default", "claude", projectDir, "2026-02-11 12:00:00")
	if err != nil {
		t.Fatalf("ResolveSelector() error: %v", err)
	}
	if !ok || name != "2026-02-11 12:00:00" {
		t.Fatalf("ResolveSelector() = (%q, %v), want (%q, true)", name, ok, "2026-02-11 12:00:00")
	}
}

func TestResolveSelector_ByID(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()
	writeSessionDir(t, "default", "claude", projectDir, "id_abc123", "session-a")

	name, ok, err := ResolveSelector("default", "claude", projectDir, "id_abc123")
	if err != nil {
		t.Fatalf("ResolveSelector() error: %v", err)
	}
	if !ok || name != "session-a" {
		t.Fatalf("ResolveSelector() = (%q, %v), want (%q, true)", name, ok, "session-a")
	}
}

func TestResolveSelector_ByUniqueIDPrefix(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()
	writeSessionDir(t, "default", "claude", projectDir, "id_abc123", "session-a")
	writeSessionDir(t, "default", "claude", projectDir, "id_def456", "session-b")

	name, ok, err := ResolveSelector("default", "claude", projectDir, "id_abc")
	if err != nil {
		t.Fatalf("ResolveSelector() error: %v", err)
	}
	if !ok || name != "session-a" {
		t.Fatalf("ResolveSelector() = (%q, %v), want (%q, true)", name, ok, "session-a")
	}
}

func TestResolveSelector_AmbiguousIDPrefix(t *testing.T) {
	withTempConfigHome(t)
	projectDir := t.TempDir()
	writeSessionDir(t, "default", "claude", projectDir, "id_abc123", "session-a")
	writeSessionDir(t, "default", "claude", projectDir, "id_abc999", "session-b")

	_, ok, err := ResolveSelector("default", "claude", projectDir, "id_abc")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("expected ambiguous error, got: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false for ambiguous selector")
	}
}

// KV-backed function tests

func TestSaveAndLoadResumeToken(t *testing.T) {
	store := openTestStore(t)

	err := SaveResumeToken(store, "claude", "proj1", "sess1", "token123")
	if err != nil {
		t.Fatalf("SaveResumeToken: %v", err)
	}

	token, err := LoadResumeToken(store, "claude", "proj1", "sess1", "default", "/tmp/proj")
	if err != nil {
		t.Fatalf("LoadResumeToken: %v", err)
	}
	if token != "token123" {
		t.Errorf("token = %q, want %q", token, "token123")
	}
}

func TestLoadResumeToken_FallbackToFile(t *testing.T) {
	withTempConfigHome(t)
	store := openTestStore(t)
	projectDir := t.TempDir()

	// Write token to filesystem only.
	d := writeSessionDir(t, "default", "claude", projectDir, "sess1", "my-session")
	if err := os.WriteFile(filepath.Join(d, ".resume-token"), []byte("file-token\n"), 0644); err != nil {
		t.Fatalf("write token file: %v", err)
	}

	projectKey := project.GenerateFolderName(projectDir)
	token, err := LoadResumeToken(store, "claude", projectKey, "sess1", "default", projectDir)
	if err != nil {
		t.Fatalf("LoadResumeToken: %v", err)
	}
	if token != "file-token" {
		t.Errorf("token = %q, want %q", token, "file-token")
	}
}

func TestSetAndGetActiveSession(t *testing.T) {
	store := openTestStore(t)

	err := SetActiveSession(store, "claude", "proj1", "my-session")
	if err != nil {
		t.Fatalf("SetActiveSession: %v", err)
	}

	name, err := GetActiveSession(store, "claude", "proj1", "default", "/tmp/proj")
	if err != nil {
		t.Fatalf("GetActiveSession: %v", err)
	}
	if name != "my-session" {
		t.Errorf("name = %q, want %q", name, "my-session")
	}
}

func TestGetActiveSession_FallbackToFile(t *testing.T) {
	withTempConfigHome(t)
	store := openTestStore(t)
	projectDir := t.TempDir()

	// Write active session file.
	resumeDir := ProjectResumeDir("default", "claude", projectDir)
	if err := os.MkdirAll(resumeDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(resumeDir, ".active-session"), []byte("file-session\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	projectKey := project.GenerateFolderName(projectDir)
	name, err := GetActiveSession(store, "claude", projectKey, "default", projectDir)
	if err != nil {
		t.Fatalf("GetActiveSession: %v", err)
	}
	if name != "file-session" {
		t.Errorf("name = %q, want %q", name, "file-session")
	}
}

func TestListNamesKV(t *testing.T) {
	store := openTestStore(t)

	// Save sessions to KV.
	if err := SaveSessionName(store, "claude", "proj1", "sess_a", "alpha"); err != nil {
		t.Fatalf("SaveSessionName: %v", err)
	}
	if err := SaveSessionName(store, "claude", "proj1", "sess_b", "beta"); err != nil {
		t.Fatalf("SaveSessionName: %v", err)
	}
	// Also save a token (should not appear as a name).
	if err := SaveResumeToken(store, "claude", "proj1", "sess_a", "tok"); err != nil {
		t.Fatalf("SaveResumeToken: %v", err)
	}

	names, err := ListNamesKV(store, "claude", "proj1", "default", "/tmp/proj")
	if err != nil {
		t.Fatalf("ListNamesKV: %v", err)
	}
	want := []string{"alpha", "beta"}
	if !slices.Equal(names, want) {
		t.Errorf("ListNamesKV = %v, want %v", names, want)
	}
}

func TestListNamesKV_FallbackToFile(t *testing.T) {
	withTempConfigHome(t)
	store := openTestStore(t)
	projectDir := t.TempDir()

	// Write sessions only to filesystem.
	writeSessionDir(t, "default", "claude", projectDir, "a", "from-file")

	projectKey := project.GenerateFolderName(projectDir)
	names, err := ListNamesKV(store, "claude", projectKey, "default", projectDir)
	if err != nil {
		t.Fatalf("ListNamesKV: %v", err)
	}
	if len(names) != 1 || names[0] != "from-file" {
		t.Errorf("ListNamesKV = %v, want [from-file]", names)
	}
}

func TestRemoveByNameKV(t *testing.T) {
	store := openTestStore(t)
	withTempConfigHome(t)
	projectDir := t.TempDir()

	projectKey := project.GenerateFolderName(projectDir)

	// Save sessions to KV.
	if err := SaveSessionName(store, "claude", projectKey, "sess_a", "keep"); err != nil {
		t.Fatalf("SaveSessionName: %v", err)
	}
	if err := SaveSessionName(store, "claude", projectKey, "sess_b", "drop"); err != nil {
		t.Fatalf("SaveSessionName: %v", err)
	}
	if err := SaveResumeToken(store, "claude", projectKey, "sess_b", "tok"); err != nil {
		t.Fatalf("SaveResumeToken: %v", err)
	}
	if err := SetActiveSession(store, "claude", projectKey, "drop"); err != nil {
		t.Fatalf("SetActiveSession: %v", err)
	}

	removed, err := RemoveByNameKV(store, "claude", projectKey, "default", projectDir, "drop")
	if err != nil {
		t.Fatalf("RemoveByNameKV: %v", err)
	}
	if !removed {
		t.Fatal("expected removed=true")
	}

	// Verify "keep" is still there.
	names, err := ListNamesKV(store, "claude", projectKey, "default", projectDir)
	if err != nil {
		t.Fatalf("ListNamesKV: %v", err)
	}
	if len(names) != 1 || names[0] != "keep" {
		t.Errorf("expected only [keep], got %v", names)
	}

	// Verify active session was cleared.
	_, err = GetActiveSession(store, "claude", projectKey, "default", projectDir)
	if err == nil {
		t.Error("expected active session to be cleared")
	}
}
