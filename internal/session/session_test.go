package session

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
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
