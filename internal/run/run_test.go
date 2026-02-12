package run

import (
	"os"
	"path/filepath"
	"runtime"
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
		"TERM",
		"http_proxy",
		"https_proxy",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"no_proxy",
		"NO_PROXY",
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
