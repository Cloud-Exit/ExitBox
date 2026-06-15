package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	envstore "github.com/cloud-exit/exitbox/internal/env"
)

func TestEditEnvProfileSupportsMultiWordEditor(t *testing.T) {
	oldData := config.Data
	config.Data = t.TempDir()
	t.Cleanup(func() { config.Data = oldData })

	editorPath := filepath.Join(t.TempDir(), "editor.sh")
	if err := os.WriteFile(editorPath, []byte("#!/bin/sh\nprintf 'FOO=bar\\n' > \"$1\"\n"), 0755); err != nil {
		t.Fatalf("write editor script: %v", err)
	}
	t.Setenv("EDITOR", "sh "+editorPath)

	editEnvProfile("test-ws", "multiword", true)

	profile, err := envstore.Load("test-ws", "multiword")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := profile.Vars["FOO"]; got != "bar" {
		t.Fatalf("FOO = %q, want bar", got)
	}
}
