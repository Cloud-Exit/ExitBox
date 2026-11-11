package run

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExpandPath_Absolute(t *testing.T) {
	if runtime.GOOS == "windows" {
		got := expandPath(`C:\Users\test`, `C:\project`)
		if got != `C:\Users\test` {
			t.Errorf("expandPath(C:\\Users\\test) = %q, want C:\\Users\\test", got)
		}
	} else {
		got := expandPath("/usr/local/bin", "/project")
		if got != "/usr/local/bin" {
			t.Errorf("expandPath(/usr/local/bin) = %q, want /usr/local/bin", got)
		}
	}
}

func TestExpandPath_Relative(t *testing.T) {
	projectDir := t.TempDir()
	got := expandPath("subdir", projectDir)
	expected := filepath.Join(projectDir, "subdir")
	if got != expected {
		t.Errorf("expandPath(subdir) = %q, want %q", got, expected)
	}
}

func TestExpandPath_Tilde(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tilde expansion uses $HOME which is not set on Windows runners")
	}
	home := os.Getenv("HOME")
	got := expandPath("~/docs", "/project")
	expected := filepath.Join(home, "docs")
	if got != expected {
		t.Errorf("expandPath(~/docs) = %q, want %q", got, expected)
	}
}

func TestExpandPath_TildeNoSlash(t *testing.T) {
	// "~docs" should be treated as relative, not tilde expansion
	projectDir := t.TempDir()
	got := expandPath("~docs", projectDir)
	expected := filepath.Join(projectDir, "~docs")
	if got != expected {
		t.Errorf("expandPath(~docs) = %q, want %q", got, expected)
	}
}

func TestGetEnvOr_Default(t *testing.T) {
	os.Unsetenv("TEST_GETENV_OR_KEY")
	got := getEnvOr("TEST_GETENV_OR_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("getEnvOr(unset) = %q, want %q", got, "fallback")
	}
}

func TestGetEnvOr_Set(t *testing.T) {
	t.Setenv("TEST_GETENV_OR_KEY", "custom")
	got := getEnvOr("TEST_GETENV_OR_KEY", "fallback")
	if got != "custom" {
		t.Errorf("getEnvOr(set) = %q, want %q", got, "custom")
	}
}

func TestGetEnvOr_EmptyValue(t *testing.T) {
	t.Setenv("TEST_GETENV_OR_KEY", "")
	got := getEnvOr("TEST_GETENV_OR_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("getEnvOr(empty) = %q, want %q (empty treated as unset)", got, "fallback")
	}
}

func TestIsReservedEnvVar(t *testing.T) {
	reserved := []string{
		"EXITBOX_AGENT",
		"EXITBOX_PROJECT_NAME",
		"EXITBOX_PROJECT_KEY",
		"EXITBOX_WORKSPACE_SCOPE",
		"EXITBOX_WORKSPACE_NAME",
		"EXITBOX_VERSION",
		"EXITBOX_STATUS_BAR",
		"EXITBOX_AUTO_RESUME",
		"EXITBOX_SESSION_NAME",
		"EXITBOX_KEYBINDINGS",
		"EXITBOX_HOST_PORTS",
		"EXITBOX_VAULT_ENABLED",
		"EXITBOX_VAULT_READONLY",
		"EXITBOX_IDE_PORT",
		"CLAUDE_CODE_SSE_PORT",
		"ENABLE_IDE_INTEGRATION",
		"TERM",
		"http_proxy",
		"https_proxy",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"no_proxy",
		"NO_PROXY",
		"SSH_AUTH_SOCK",
	}
	for _, key := range reserved {
		if !isReservedEnvVar(key) {
			t.Errorf("isReservedEnvVar(%q) = false, want true", key)
		}
	}
}

func TestIsReservedEnvVar_NotReserved(t *testing.T) {
	notReserved := []string{
		"HOME",
		"PATH",
		"MY_CUSTOM_VAR",
		"NODE_ENV",
		"GOPATH",
		"",
	}
	for _, key := range notReserved {
		if isReservedEnvVar(key) {
			t.Errorf("isReservedEnvVar(%q) = true, want false", key)
		}
	}
}

// TestNewIPCServer_AlwaysCreated ensures the IPC server is created
// regardless of firewall mode (so vault/kv still work with --no-firewall).
func TestNewIPCServer_AlwaysCreated(t *testing.T) {
	// Firewall enabled
	_, err := newIPCServer(true, nil, "test")
	if err != nil {
		t.Fatalf("newIPCServer(firewall=true) failed: %v", err)
	}

	// Firewall disabled — must still succeed
	_, err = newIPCServer(false, nil, "test")
	if err != nil {
		t.Fatalf("newIPCServer(firewall=false) failed: %v", err)
	}
}

// TestNewIPCServer_AllowDomainHandler_OnlyWithFirewall ensures the
// allow_domain handler is only registered when firewall is enabled.
func TestNewIPCServer_AllowDomainHandler_OnlyWithFirewall(t *testing.T) {
	withFirewall, err := newIPCServer(true, nil, "test")
	if err != nil {
		t.Fatalf("newIPCServer(firewall=true) failed: %v", err)
	}

	withoutFirewall, err := newIPCServer(false, nil, "test")
	if err != nil {
		t.Fatalf("newIPCServer(firewall=false) failed: %v", err)
	}

	if !withFirewall.HasHandler("allow_domain") {
		t.Error("allow_domain handler missing with firewall enabled")
	}
	if withoutFirewall.HasHandler("allow_domain") {
		t.Error("allow_domain handler present with firewall disabled; should be absent")
	}
}

func TestIsReservedEnvVar_PluginSettings(t *testing.T) {
	// Plugin settings are rendered from the workspace config; a hand-passed
	// value would silently disagree with what `exitbox plugins` shows.
	reserved := []string{
		"EXITBOX_PLUGINS",
		"EXITBOX_GPU",
		"EXITBOX_PLUGIN_CODEX_ROUTER_PROVIDERS",
		"EXITBOX_PLUGIN_CODEX_ROUTER_ANTHROPIC_API_KEY",
	}
	for _, key := range reserved {
		if !isReservedEnvVar(key) {
			t.Errorf("isReservedEnvVar(%q) = false, want true", key)
		}
	}
	if isReservedEnvVar("MY_PLUGIN_SETTING") {
		t.Error("only EXITBOX_PLUGIN_* names are reserved")
	}
}

func TestBaseURLSource(t *testing.T) {
	home := withConfigHome(t)
	claudeDir := filepath.Join(home, "profiles", "global", "work", "claude")
	writeSettings := func(dir, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".claude", "settings.json"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Nothing sets it.
	if got := baseURLSource(Options{Agent: "claude"}, "work"); got != "" {
		t.Errorf("no settings: %q", got)
	}
	// Passed with -e.
	if got := baseURLSource(Options{Agent: "claude", EnvVars: []string{"FOO=1", "ANTHROPIC_BASE_URL=https://openrouter.ai/api"}}, "work"); got != "-e ANTHROPIC_BASE_URL" {
		t.Errorf("-e: %q", got)
	}
	// From an env profile (its variables arrive merged with -e).
	got := baseURLSource(Options{Agent: "claude", EnvProfile: "openrouter", EnvVars: []string{"ANTHROPIC_BASE_URL=https://openrouter.ai/api"}}, "work")
	if !strings.Contains(got, "env profile 'openrouter'") {
		t.Errorf("env profile: %q", got)
	}
	// An empty value unsets the variable, a similar name is another one.
	if got := baseURLSource(Options{Agent: "claude", EnvVars: []string{"ANTHROPIC_BASE_URL=", "MY_ANTHROPIC_BASE_URL=x"}}, "work"); got != "" {
		t.Errorf("empty value: %q", got)
	}
	// Other agents do not read the variable.
	if got := baseURLSource(Options{Agent: "codex", EnvVars: []string{"ANTHROPIC_BASE_URL=x"}}, "work"); got != "" {
		t.Errorf("codex: %q", got)
	}

	// The legacy key older ExitBox versions wrote is not read by Claude Code.
	writeSettings(claudeDir, `{"apiBaseUrl":"http://10.0.0.5:8080","env":{"OTHER":"x"}}`)
	if got := baseURLSource(Options{Agent: "claude"}, "work"); got != "" {
		t.Errorf("legacy key: %q", got)
	}
	// The env block of settings.json is.
	writeSettings(claudeDir, `{"env":{"ANTHROPIC_BASE_URL":"http://10.0.0.5:8080"}}`)
	want := filepath.Join(claudeDir, ".claude", "settings.json")
	if got := baseURLSource(Options{Agent: "claude"}, "work"); got != want {
		t.Errorf("settings.json: %q, want %q", got, want)
	}
	// Another workspace has its own settings.
	if got := baseURLSource(Options{Agent: "claude"}, "personal"); got != "" {
		t.Errorf("other workspace: %q", got)
	}

	// With an env profile the config is scoped to the profile's directory
	// once it exists; before that the workspace's own settings are copied.
	if got := baseURLSource(Options{Agent: "claude", EnvProfile: "local"}, "work"); got != want {
		t.Errorf("profile without a directory: %q, want %q", got, want)
	}
	scoped := filepath.Join(claudeDir, "profiles", "local")
	writeSettings(scoped, `{"env":{}}`)
	if got := baseURLSource(Options{Agent: "claude", EnvProfile: "local"}, "work"); got != "" {
		t.Errorf("profile directory without the variable: %q", got)
	}
	writeSettings(scoped, `not json`)
	if got := baseURLSource(Options{Agent: "claude", EnvProfile: "local"}, "work"); got != "" {
		t.Errorf("settings that do not parse: %q", got)
	}
}
