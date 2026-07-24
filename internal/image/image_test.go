package image

import (
	"fmt"
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

// withDriverDetection swaps the buildx driver detector for a test and resets
// the process-level cache so the stub is consulted. It restores the original
// on cleanup.
func withDriverDetection(t *testing.T, driver string) {
	t.Helper()
	orig := detectBuilderDriver
	detectBuilderDriver = func(string) string { return driver }
	resetBuilderDriverCache()
	t.Cleanup(func() {
		detectBuilderDriver = orig
		resetBuilderDriverCache()
	})
}

func TestBuildArgs_Docker_DefaultDriverNoCacheExport(t *testing.T) {
	// The default "docker" driver cannot export cache, so --cache-to must be
	// omitted (it aborts the build otherwise).
	withDriverDetection(t, "docker")
	args := buildArgs("docker")
	if len(args) < 3 {
		t.Fatalf("buildArgs(docker) = %v, want at least 3 args (progress + cache-from + src)", args)
	}
	if args[0] != "--progress=auto" {
		t.Errorf("buildArgs(docker)[0] = %q, want %q", args[0], "--progress=auto")
	}
	if args[1] != "--cache-from" {
		t.Errorf("buildArgs(docker)[1] = %q, want %q", args[1], "--cache-from")
	}
	if !strings.HasPrefix(args[2], "type=local,src=") {
		t.Errorf("buildArgs(docker)[2] = %q, want prefix %q", args[2], "type=local,src=")
	}
	for _, a := range args {
		if a == "--cache-to" || strings.HasPrefix(a, "--cache-to=") {
			t.Errorf("buildArgs(docker) must NOT include --cache-to on default driver, got %q", a)
		}
	}
}

func TestBuildArgs_Docker_ContainerDriverWithCacheExport(t *testing.T) {
	// A docker-container driver supports cache export, so --cache-to is wired up.
	// It also does not auto-load images into the local store, so --load is added
	// (otherwise FROM <local-image> in a follow-up build fails to resolve).
	withDriverDetection(t, "docker-container")
	args := buildArgs("docker")
	if len(args) < 6 {
		t.Fatalf("buildArgs(docker) = %v, want at least 6 args (progress + cache-from + src + cache-to + dest + load)", args)
	}
	if args[0] != "--progress=auto" {
		t.Errorf("buildArgs(docker)[0] = %q, want %q", args[0], "--progress=auto")
	}
	if args[1] != "--cache-from" {
		t.Errorf("buildArgs(docker)[1] = %q, want %q", args[1], "--cache-from")
	}
	if !strings.HasPrefix(args[2], "type=local,src=") {
		t.Errorf("buildArgs(docker)[2] = %q, want prefix %q", args[2], "type=local,src=")
	}
	if args[3] != "--cache-to" {
		t.Errorf("buildArgs(docker)[3] = %q, want %q", args[3], "--cache-to")
	}
	if !strings.HasPrefix(args[4], "type=local,dest=") || !strings.HasSuffix(args[4], ",mode=max") {
		t.Errorf("buildArgs(docker)[4] = %q, want type=local,dest=...,mode=max", args[4])
	}
	hasLoad := false
	for _, a := range args {
		if a == "--load" {
			hasLoad = true
		}
	}
	if !hasLoad {
		t.Errorf("buildArgs(docker) must include --load for docker-container driver, got %v", args)
	}
}

func TestBuildArgs_Docker_UnknownDriverNoCacheExport(t *testing.T) {
	// When the driver cannot be detected, treat it as the safe default (no
	// export, no --load — matches the plain docker daemon behavior).
	withDriverDetection(t, "")
	args := buildArgs("docker")
	for _, a := range args {
		if a == "--cache-to" || strings.HasPrefix(a, "--cache-to=") {
			t.Errorf("buildArgs(docker) must NOT include --cache-to when driver is unknown, got %q", a)
		}
		if a == "--load" {
			t.Errorf("buildArgs(docker) must NOT include --load when driver is unknown, got %q", a)
		}
	}
}

func TestBuildArgs_Docker_DefaultDriverNoLoad(t *testing.T) {
	// The default "docker" driver auto-loads, so --load must NOT be added.
	withDriverDetection(t, "docker")
	args := buildArgs("docker")
	for _, a := range args {
		if a == "--load" {
			t.Errorf("buildArgs(docker) must NOT include --load on default driver, got %q", a)
		}
	}
}

func TestBuildArgs_Podman(t *testing.T) {
	args := buildArgs("podman")
	if len(args) != 2 || args[0] != "--layers" || args[1] != "--pull=newer" {
		t.Errorf("buildArgs(podman) = %v, want [--layers --pull=newer]", args)
	}
}

func TestSupportsCacheExport(t *testing.T) {
	cases := []struct {
		cmd, driver string
		want         bool
	}{
		{"podman", "", true},          // podman never needs --cache-to
		{"docker", "docker", false},   // default driver cannot export
		{"docker", "", false},          // unknown driver is treated as unsafe
		{"docker", "docker-container", true},
		{"docker", "remote", true},
		{"docker", "kubernetes", true},
		{"docker", "cloud", true},
	}
	for _, c := range cases {
		withDriverDetection(t, c.driver)
		if got := supportsCacheExport(c.cmd); got != c.want {
			t.Errorf("supportsCacheExport(%q) [driver=%q] = %v, want %v", c.cmd, c.driver, got, c.want)
		}
	}
}

func TestNeedsLoad(t *testing.T) {
	cases := []struct {
		cmd, driver string
		want        bool
	}{
		{"podman", "", false},          // podman loads by default
		{"podman", "docker-container", false},
		{"docker", "docker", false},    // default driver auto-loads
		{"docker", "", false},           // unknown: safe default, no --load
		{"docker", "docker-container", true},
		{"docker", "remote", true},
		{"docker", "kubernetes", true},
		{"docker", "cloud", true},
	}
	for _, c := range cases {
		withDriverDetection(t, c.driver)
		if got := needsLoad(c.cmd); got != c.want {
			t.Errorf("needsLoad(%q) [driver=%q] = %v, want %v", c.cmd, c.driver, got, c.want)
		}
	}
}

func TestStripCacheExportArgs(t *testing.T) {
	in := []string{
		"--progress=auto",
		"--cache-from", "type=local,src=/x",
		"--cache-to", "type=local,dest=/x,mode=max",
		"-t", "img",
		".",
	}
	out := stripCacheExportArgs(in)
	if len(out) != len(in)-2 {
		t.Fatalf("stripCacheExportArgs len = %d, want %d (in=%d, expected removal of --cache-to + value)", len(out), len(in)-2, len(in))
	}
	for _, a := range out {
		if a == "--cache-to" || strings.HasPrefix(a, "--cache-to=") {
			t.Errorf("stripCacheExportArgs left a --cache-to flag: %q", a)
		}
	}
	// --cache-from and its value must be preserved.
	foundFrom := false
	for _, a := range out {
		if a == "--cache-from" {
			foundFrom = true
		}
	}
	if !foundFrom {
		t.Error("stripCacheExportArgs removed --cache-from, which should be preserved")
	}

	// Single-token "--cache-to=..." form.
	in2 := []string{"--cache-to=type=local,dest=/x", "-t", "img"}
	out2 := stripCacheExportArgs(in2)
	if len(out2) != 2 {
		t.Errorf("stripCacheExportArgs single-token form len = %d, want 2: %v", len(out2), out2)
	}

	// No --cache-to present: args unchanged in content.
	in3 := []string{"-t", "img", "."}
	out3 := stripCacheExportArgs(in3)
	if len(out3) != len(in3) {
		t.Errorf("stripCacheExportArgs altered args without --cache-to: %v", out3)
	}
}

func TestIsCacheExportError(t *testing.T) {
	if isCacheExportError(nil, "") {
		t.Error("isCacheExportError(nil) should be false")
	}
	if !isCacheExportError(fmt.Errorf("ERROR: failed to build: Cache export is not supported for the docker driver."), "") {
		t.Error("isCacheExportError should match the docker-driver error message")
	}
	if !isCacheExportError(fmt.Errorf("build failed"), "stderr: Cache export is not supported for the docker driver.\nLearn more...") {
		t.Error("isCacheExportError should match the error in captured output")
	}
	if isCacheExportError(fmt.Errorf("some other build error"), "unrelated output") {
		t.Error("isCacheExportError must not match unrelated errors")
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

func TestToolsHash_Deterministic(t *testing.T) {
	cfg := &config.Config{
		Tools: config.ToolsConfig{
			User: []string{"git", "curl"},
		},
	}

	h1 := ToolsHash(cfg)
	h2 := ToolsHash(cfg)
	if h1 != h2 {
		t.Errorf("ToolsHash not deterministic: %q != %q", h1, h2)
	}
	if len(h1) != 16 { // 8 bytes hex-encoded
		t.Errorf("ToolsHash length = %d, want 16", len(h1))
	}
}

func TestToolsHash_DifferentInputs(t *testing.T) {
	cfg1 := &config.Config{
		Tools: config.ToolsConfig{User: []string{"git"}},
	}
	cfg2 := &config.Config{
		Tools: config.ToolsConfig{User: []string{"git", "curl"}},
	}

	h1 := ToolsHash(cfg1)
	h2 := ToolsHash(cfg2)
	if h1 == h2 {
		t.Error("ToolsHash should differ for different user tools")
	}
}

func TestToolsHash_IncludesBinaries(t *testing.T) {
	cfg1 := &config.Config{}
	cfg2 := &config.Config{
		Tools: config.ToolsConfig{
			Binaries: []config.BinaryConfig{
				{Name: "mytool", URLPattern: "https://example.com/{arch}/mytool"},
			},
		},
	}

	h1 := ToolsHash(cfg1)
	h2 := ToolsHash(cfg2)
	if h1 == h2 {
		t.Error("ToolsHash should differ when binaries are added")
	}
}

func TestToolsHash_IncludesExternalTools(t *testing.T) {
	cfg1 := &config.Config{}
	cfg2 := &config.Config{
		ExternalTools: []string{"Bun"},
	}

	h1 := ToolsHash(cfg1)
	h2 := ToolsHash(cfg2)
	if h1 == h2 {
		t.Error("ToolsHash should differ when external tools are added")
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

func TestWorkspaceHash_ExcludesGlobalTools(t *testing.T) {
	dir := t.TempDir()
	cfg1 := config.DefaultConfig()
	cfg2 := config.DefaultConfig()
	cfg2.Tools.User = []string{"htop", "curl"}
	cfg2.Tools.Binaries = []config.BinaryConfig{
		{Name: "mytool", URLPattern: "https://example.com/{arch}/mytool"},
	}

	h1 := WorkspaceHash(cfg1, dir, "")
	h2 := WorkspaceHash(cfg2, dir, "")
	if h1 != h2 {
		t.Error("WorkspaceHash should NOT change when only global tools/binaries change (those are in tools layer)")
	}
}

func TestWorkspaceHash_IncludesSessionTools(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultConfig()

	h1 := WorkspaceHash(cfg, dir, "")

	oldTools := SessionTools
	SessionTools = []string{"extra-pkg"}
	h2 := WorkspaceHash(cfg, dir, "")
	SessionTools = oldTools

	if h1 == h2 {
		t.Error("WorkspaceHash should differ when SessionTools are set")
	}
}

func TestIsReleaseVersion(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{"v3.2.0", true},
		{"v1.0.0", true},
		{"v0.1.0-rc1", true},
		{"3.2.0", false},
		{"latest", false},
		{"", false},
		{"dev", false},
	}
	for _, tc := range tests {
		got := isReleaseVersion(tc.version)
		if got != tc.want {
			t.Errorf("isReleaseVersion(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

func TestRegistryConstants(t *testing.T) {
	if !strings.HasPrefix(BaseImageRegistry, "ghcr.io/") {
		t.Errorf("BaseImageRegistry = %q, want ghcr.io/ prefix", BaseImageRegistry)
	}
	if !strings.HasPrefix(SquidImageRegistry, "ghcr.io/") {
		t.Errorf("SquidImageRegistry = %q, want ghcr.io/ prefix", SquidImageRegistry)
	}
	if strings.HasSuffix(BaseImageRegistry, "/") {
		t.Errorf("BaseImageRegistry = %q, should not end with /", BaseImageRegistry)
	}
	if strings.HasSuffix(SquidImageRegistry, "/") {
		t.Errorf("SquidImageRegistry = %q, should not end with /", SquidImageRegistry)
	}
}
