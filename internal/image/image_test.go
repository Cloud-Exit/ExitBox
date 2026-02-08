package image

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{0, "0s"},
		{500 * time.Millisecond, "1s"}, // rounds to nearest second
		{1 * time.Second, "1s"},
		{30 * time.Second, "30s"},
		{59 * time.Second, "59s"},
		{60 * time.Second, "1m 0s"},
		{90 * time.Second, "1m 30s"},
		{125 * time.Second, "2m 5s"},
	}
	for _, tc := range tests {
		got := formatDuration(tc.input)
		if got != tc.expected {
			t.Errorf("formatDuration(%v) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestBuildArgs_Docker(t *testing.T) {
	args := buildArgs("docker")
	if len(args) != 1 || args[0] != "--progress=auto" {
		t.Errorf("buildArgs(docker) = %v, want [--progress=auto]", args)
	}
}

func TestBuildArgs_Podman(t *testing.T) {
	args := buildArgs("podman")
	if len(args) != 2 || args[0] != "--layers" || args[1] != "--pull=newer" {
		t.Errorf("buildArgs(podman) = %v, want [--layers --pull=newer]", args)
	}
}

func TestComputeConfigHash_Deterministic(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			User: []string{"git", "curl"},
		},
	}

	h1 := computeConfigHash(cfg)
	h2 := computeConfigHash(cfg)
	if h1 != h2 {
		t.Errorf("computeConfigHash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 16 { // 8 bytes hex-encoded
		t.Errorf("computeConfigHash length = %d, want 16", len(h1))
	}
}

func TestComputeConfigHash_DifferentInputs(t *testing.T) {
	cfg1 := &config.Config{
		Tools: config.ToolsConfig{User: []string{"git"}},
	}
	cfg2 := &config.Config{
		Tools: config.ToolsConfig{User: []string{"git", "curl"}},
	}

	h1 := computeConfigHash(cfg1)
	h2 := computeConfigHash(cfg2)
	if h1 == h2 {
		t.Error("computeConfigHash should differ for different inputs")
	}
}

func TestComputeConfigHash_WithBinaries(t *testing.T) {
	cfg1 := &config.Config{
		Tools: config.ToolsConfig{
			Binaries: []config.BinaryConfig{
				{Name: "tool1", URLPattern: "https://example.com/{arch}/tool1"},
			},
		},
	}
	cfg2 := &config.Config{
		Tools: config.ToolsConfig{
			Binaries: []config.BinaryConfig{
				{Name: "tool2", URLPattern: "https://example.com/{arch}/tool2"},
			},
		},
	}

	h1 := computeConfigHash(cfg1)
	h2 := computeConfigHash(cfg2)
	if h1 == h2 {
		t.Error("computeConfigHash should differ for different binary configs")
	}
}

func TestComputeConfigHash_EmptyConfig(t *testing.T) {
	cfg := &config.Config{}
	h := computeConfigHash(cfg)
	if h == "" {
		t.Error("computeConfigHash should return non-empty hash for empty config")
	}
}

func TestAppendToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")

	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := appendToFile(path, " world"); err != nil {
		t.Fatalf("appendToFile() error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello world" {
		t.Errorf("file content = %q, want %q", string(data), "hello world")
	}
}

func TestAppendToFile_NonexistentFile(t *testing.T) {
	err := appendToFile("/nonexistent/path/file.txt", "content")
	if err == nil {
		t.Error("appendToFile to nonexistent path should return error")
	}
}

func TestFileSHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.bin")

	if err := os.WriteFile(path, []byte("test content"), 0644); err != nil {
		t.Fatal(err)
	}

	hash := fileSHA256(path)
	if hash == "" {
		t.Error("fileSHA256 should return non-empty hash")
	}
	if len(hash) != 64 { // SHA-256 hex string
		t.Errorf("fileSHA256 length = %d, want 64", len(hash))
	}

	// Deterministic
	hash2 := fileSHA256(path)
	if hash != hash2 {
		t.Errorf("fileSHA256 not deterministic: %q != %q", hash, hash2)
	}
}

func TestFileSHA256_NonexistentFile(t *testing.T) {
	hash := fileSHA256("/nonexistent/file")
	if hash != "" {
		t.Errorf("fileSHA256(nonexistent) = %q, want empty", hash)
	}
}

func TestFileSHA256_DifferentContent(t *testing.T) {
	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.bin")
	p2 := filepath.Join(dir, "b.bin")

	_ = os.WriteFile(p1, []byte("content A"), 0644)
	_ = os.WriteFile(p2, []byte("content B"), 0644)

	h1 := fileSHA256(p1)
	h2 := fileSHA256(p2)
	if h1 == h2 {
		t.Error("fileSHA256 should differ for different content")
	}
}

func TestWorkspaceHash_Deterministic(t *testing.T) {
	cfg := config.DefaultConfig()
	dir := t.TempDir()

	h1 := WorkspaceHash(cfg, dir, "")
	h2 := WorkspaceHash(cfg, dir, "")
	if h1 != h2 {
		t.Errorf("WorkspaceHash not deterministic: %q != %q", h1, h2)
	}
}

func TestWorkspaceHash_Format(t *testing.T) {
	cfg := config.DefaultConfig()
	dir := t.TempDir()

	h := WorkspaceHash(cfg, dir, "")
	if len(h) != 16 {
		t.Errorf("WorkspaceHash length = %d, want 16", len(h))
	}
	// Should be hex
	for _, c := range h {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Errorf("WorkspaceHash contains non-hex char: %c", c)
			break
		}
	}
}
