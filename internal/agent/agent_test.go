package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDisplayName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude", "Claude Code"},
		{"codex", "OpenAI Codex"},
		{"opencode", "OpenCode"},
		{"unknown", "unknown"},
		{"", ""},
	}
	for _, tc := range tests {
		got := DisplayName(tc.input)
		if got != tc.expected {
			t.Errorf("DisplayName(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsValidAgent(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"claude", true},
		{"codex", true},
		{"opencode", true},
		{"unknown", false},
		{"", false},
		{"Claude", false},
	}
	for _, tc := range tests {
		got := IsValidAgent(tc.input)
		if got != tc.expected {
			t.Errorf("IsValidAgent(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestAgentNames(t *testing.T) {
	if len(AgentNames) != 3 {
		t.Fatalf("expected 3 agent names, got %d", len(AgentNames))
	}
	expected := map[string]bool{"claude": true, "codex": true, "opencode": true}
	for _, name := range AgentNames {
		if !expected[name] {
			t.Errorf("unexpected agent name: %s", name)
		}
	}
}

func TestRegistryGetAll(t *testing.T) {
	for _, name := range AgentNames {
		a := Get(name)
		if a == nil {
			t.Errorf("Get(%q) returned nil", name)
			continue
		}
		if a.Name() != name {
			t.Errorf("Get(%q).Name() = %q", name, a.Name())
		}
	}
}

func TestGetUnknown(t *testing.T) {
	if a := Get("nonexistent"); a != nil {
		t.Errorf("Get(\"nonexistent\") should return nil, got %v", a)
	}
}

func TestClaudeAgent(t *testing.T) {
	c := &Claude{}

	if c.Name() != "claude" {
		t.Errorf("Name() = %q, want %q", c.Name(), "claude")
	}
	if c.DisplayName() != "Claude Code" {
		t.Errorf("DisplayName() = %q, want %q", c.DisplayName(), "Claude Code")
	}

	// HostConfigPaths
	paths := c.HostConfigPaths()
	if len(paths) != 1 {
		t.Fatalf("HostConfigPaths() returned %d paths, want 1", len(paths))
	}
	if !strings.HasSuffix(paths[0], ".claude") {
		t.Errorf("HostConfigPaths()[0] = %q, should end with .claude", paths[0])
	}

	// ContainerMounts
	mounts := c.ContainerMounts("/cfg")
	if len(mounts) != 3 {
		t.Fatalf("ContainerMounts() returned %d mounts, want 3", len(mounts))
	}
	if mounts[0].Target != "/home/user/.claude" {
		t.Errorf("mounts[0].Target = %q, want /home/user/.claude", mounts[0].Target)
	}
	if mounts[1].Target != "/home/user/.claude.json" {
		t.Errorf("mounts[1].Target = %q, want /home/user/.claude.json", mounts[1].Target)
	}
	if mounts[2].Target != "/home/user/.config" {
		t.Errorf("mounts[2].Target = %q, want /home/user/.config", mounts[2].Target)
	}

	// GetDockerfileInstall
	df, err := c.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "sha256sum") {
		t.Error("GetDockerfileInstall() should contain sha256sum verification")
	}
	if !strings.Contains(df, "claude") {
		t.Error("GetDockerfileInstall() should reference claude")
	}

	// GetFullDockerfile
	full, err := c.GetFullDockerfile("1.0.0")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
}

func TestCodexAgent(t *testing.T) {
	c := &Codex{}

	if c.Name() != "codex" {
		t.Errorf("Name() = %q, want %q", c.Name(), "codex")
	}
	if c.DisplayName() != "OpenAI Codex" {
		t.Errorf("DisplayName() = %q, want %q", c.DisplayName(), "OpenAI Codex")
	}

	// BinaryName
	bn := c.BinaryName()
	switch runtime.GOARCH {
	case "amd64":
		if bn != "codex-x86_64-unknown-linux-musl.tar.gz" {
			t.Errorf("BinaryName() = %q on amd64", bn)
		}
	case "arm64":
		if bn != "codex-aarch64-unknown-linux-musl.tar.gz" {
			t.Errorf("BinaryName() = %q on arm64", bn)
		}
	}

	// HostConfigPaths
	paths := c.HostConfigPaths()
	if len(paths) != 2 {
		t.Fatalf("HostConfigPaths() returned %d paths, want 2", len(paths))
	}

	// ContainerMounts
	mounts := c.ContainerMounts("/cfg")
	if len(mounts) != 2 {
		t.Fatalf("ContainerMounts() returned %d mounts, want 2", len(mounts))
	}
	if mounts[0].Target != "/home/user/.codex" {
		t.Errorf("mounts[0].Target = %q, want /home/user/.codex", mounts[0].Target)
	}

	// GetDockerfileInstall
	df, err := c.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "sha256sum") {
		t.Error("GetDockerfileInstall() should contain sha256sum verification")
	}

	// GetFullDockerfile
	full, err := c.GetFullDockerfile("v0.1.0")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
	if !strings.Contains(full, "CODEX_VERSION=v0.1.0") {
		t.Error("GetFullDockerfile() should include CODEX_VERSION ARG")
	}
}

func TestOpenCodeAgent(t *testing.T) {
	o := &OpenCode{}

	if o.Name() != "opencode" {
		t.Errorf("Name() = %q, want %q", o.Name(), "opencode")
	}
	if o.DisplayName() != "OpenCode" {
		t.Errorf("DisplayName() = %q, want %q", o.DisplayName(), "OpenCode")
	}

	// BinaryName
	bn := o.BinaryName()
	switch runtime.GOARCH {
	case "amd64":
		if bn != "opencode-linux-x64-musl.tar.gz" {
			t.Errorf("BinaryName() = %q on amd64", bn)
		}
	case "arm64":
		if bn != "opencode-linux-arm64-musl.tar.gz" {
			t.Errorf("BinaryName() = %q on arm64", bn)
		}
	}

	// HostConfigPaths
	paths := o.HostConfigPaths()
	if len(paths) != 2 {
		t.Fatalf("HostConfigPaths() returned %d paths, want 2", len(paths))
	}

	// ContainerMounts
	mounts := o.ContainerMounts("/cfg")
	if len(mounts) != 3 {
		t.Fatalf("ContainerMounts() returned %d mounts, want 3", len(mounts))
	}
	if mounts[0].Target != "/home/user/.opencode" {
		t.Errorf("mounts[0].Target = %q, want /home/user/.opencode", mounts[0].Target)
	}

	// GetDockerfileInstall
	df, err := o.GetDockerfileInstall("")
	if err != nil {
		t.Fatalf("GetDockerfileInstall() error: %v", err)
	}
	if !strings.Contains(df, "sha256sum") {
		t.Error("GetDockerfileInstall() should contain sha256sum verification")
	}

	// GetFullDockerfile
	full, err := o.GetFullDockerfile("0.2.0")
	if err != nil {
		t.Fatalf("GetFullDockerfile() error: %v", err)
	}
	if !strings.HasPrefix(full, "FROM exitbox-base") {
		t.Error("GetFullDockerfile() should start with FROM exitbox-base")
	}
	if !strings.Contains(full, "OPENCODE_VERSION=0.2.0") {
		t.Error("GetFullDockerfile() should include OPENCODE_VERSION ARG")
	}
}

func TestGetInstalledVersion_NilRuntime(t *testing.T) {
	agents := []Agent{&Claude{}, &Codex{}, &OpenCode{}}
	for _, a := range agents {
		_, err := a.GetInstalledVersion(nil, "some-image")
		if err == nil {
			t.Errorf("%s.GetInstalledVersion(nil, ...) should return error", a.Name())
		}
	}
}

func TestCopyDirContents(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	// Create source structure
	_ = os.WriteFile(filepath.Join(src, "file1.txt"), []byte("hello"), 0644)
	_ = os.MkdirAll(filepath.Join(src, "subdir"), 0755)
	_ = os.WriteFile(filepath.Join(src, "subdir", "file2.txt"), []byte("world"), 0644)

	err := copyDirContents(src, dst)
	if err != nil {
		t.Fatalf("copyDirContents() error: %v", err)
	}

	// Verify
	data, err := os.ReadFile(filepath.Join(dst, "file1.txt"))
	if err != nil {
		t.Fatalf("reading file1.txt: %v", err)
	}
	if string(data) != "hello" {
		t.Errorf("file1.txt = %q, want %q", string(data), "hello")
	}

	data, err = os.ReadFile(filepath.Join(dst, "subdir", "file2.txt"))
	if err != nil {
		t.Fatalf("reading subdir/file2.txt: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("subdir/file2.txt = %q, want %q", string(data), "world")
	}
}

func TestCopyDirContents_EmptyDir(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()

	err := copyDirContents(src, dst)
	if err != nil {
		t.Fatalf("copyDirContents(empty) error: %v", err)
	}
}

func TestCopyDirContents_NonexistentSrc(t *testing.T) {
	dst := t.TempDir()
	err := copyDirContents("/nonexistent-path-xyz", dst)
	if err == nil {
		t.Error("copyDirContents(nonexistent) should return error")
	}
}

func TestImportConfig_Claude(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{"key":"val"}`), 0644)

	c := &Claude{}
	err := c.ImportConfig(src, dst)
	if err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dst, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("reading imported file: %v", err)
	}
	if string(data) != `{"key":"val"}` {
		t.Errorf("imported content = %q", string(data))
	}
}

func TestImportConfig_Codex(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "config.json"), []byte(`{}`), 0644)

	c := &Codex{}
	err := c.ImportConfig(src, dst)
	if err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".codex", "config.json")); err != nil {
		t.Errorf("expected .codex/config.json to exist: %v", err)
	}
}

func TestImportConfig_CodexConfigDir(t *testing.T) {
	src := filepath.Join(t.TempDir(), ".config", "codex")
	dst := t.TempDir()
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "config.json"), []byte(`{}`), 0644)

	c := &Codex{}
	err := c.ImportConfig(src, dst)
	if err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".config", "codex", "config.json")); err != nil {
		t.Errorf("expected .config/codex/config.json to exist: %v", err)
	}
}

func TestImportConfig_OpenCode(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "config.json"), []byte(`{}`), 0644)

	o := &OpenCode{}
	err := o.ImportConfig(src, dst)
	if err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".opencode", "config.json")); err != nil {
		t.Errorf("expected .opencode/config.json to exist: %v", err)
	}
}

func TestImportConfig_OpenCodeConfigDir(t *testing.T) {
	src := filepath.Join(t.TempDir(), ".config", "opencode")
	dst := t.TempDir()
	_ = os.MkdirAll(src, 0755)
	_ = os.WriteFile(filepath.Join(src, "settings.json"), []byte(`{}`), 0644)

	o := &OpenCode{}
	err := o.ImportConfig(src, dst)
	if err != nil {
		t.Fatalf("ImportConfig() error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, ".config", "opencode", "settings.json")); err != nil {
		t.Errorf("expected .config/opencode/settings.json to exist: %v", err)
	}
}
