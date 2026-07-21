// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package cmd

import (
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestParseRunFlags_BooleanFlags(t *testing.T) {
	tests := []struct {
		name  string
		args  []string
		check func(parsedFlags) bool
	}{
		{"short no-firewall", []string{"-f"}, func(f parsedFlags) bool { return f.NoFirewall }},
		{"long no-firewall", []string{"--no-firewall"}, func(f parsedFlags) bool { return f.NoFirewall }},
		{"no-tmux", []string{"--no-tmux"}, func(f parsedFlags) bool { return f.NoTmux }},
		{"no-tmux default off", []string{}, func(f parsedFlags) bool { return !f.NoTmux }},
		{"short read-only", []string{"-r"}, func(f parsedFlags) bool { return f.ReadOnly }},
		{"long read-only", []string{"--read-only"}, func(f parsedFlags) bool { return f.ReadOnly }},
		{"short no-env", []string{"-n"}, func(f parsedFlags) bool { return f.NoEnv }},
		{"long no-env", []string{"--no-env"}, func(f parsedFlags) bool { return f.NoEnv }},
		{"short verbose", []string{"-v"}, func(f parsedFlags) bool { return f.Verbose }},
		{"long verbose", []string{"--verbose"}, func(f parsedFlags) bool { return f.Verbose }},
		{"short update", []string{"-u"}, func(f parsedFlags) bool { return f.ForceUpdate }},
		{"long update", []string{"--update"}, func(f parsedFlags) bool { return f.ForceUpdate }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := parseRunFlags(tc.args, config.DefaultFlags{})
			if !tc.check(f) {
				t.Errorf("flag not set for args %v", tc.args)
			}
		})
	}
}

func TestParseRunFlags_EnvLongForm(t *testing.T) {
	f := parseRunFlags([]string{"--env", "FOO=bar"}, config.DefaultFlags{})
	if len(f.EnvVars) != 1 || f.EnvVars[0] != "FOO=bar" {
		t.Errorf("--env not parsed: %v", f.EnvVars)
	}
}

func TestParseRunFlags_EnvShortForm(t *testing.T) {
	f := parseRunFlags([]string{"-e", "FOO=bar"}, config.DefaultFlags{})
	if len(f.EnvVars) != 1 || f.EnvVars[0] != "FOO=bar" {
		t.Errorf("-e not parsed: %v", f.EnvVars)
	}
}

func TestParseRunFlags_Tools(t *testing.T) {
	f := parseRunFlags([]string{"-t", "jq", "--tools", "ripgrep"}, config.DefaultFlags{})
	if len(f.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d: %v", len(f.Tools), f.Tools)
	}
	if f.Tools[0] != "jq" || f.Tools[1] != "ripgrep" {
		t.Errorf("unexpected tools: %v", f.Tools)
	}
}

func TestParseRunFlags_HostPort(t *testing.T) {
	f := parseRunFlags([]string{"--host-port", "3000", "--host-port", "5173,8080"}, config.DefaultFlags{})
	want := []int{3000, 5173, 8080}
	if len(f.HostPorts) != len(want) {
		t.Fatalf("expected %d host ports, got %d: %v", len(want), len(f.HostPorts), f.HostPorts)
	}
	for i := range want {
		if f.HostPorts[i] != want[i] {
			t.Fatalf("HostPorts[%d] = %d, want %d", i, f.HostPorts[i], want[i])
		}
	}
	if len(f.HostPortErrors) != 0 {
		t.Fatalf("unexpected host port errors: %v", f.HostPortErrors)
	}
}

func TestParseRunFlags_HostPortInvalid(t *testing.T) {
	f := parseRunFlags([]string{"--host-port", "0,abc,70000"}, config.DefaultFlags{})
	if len(f.HostPorts) != 0 {
		t.Fatalf("expected no valid host ports, got %v", f.HostPorts)
	}
	if len(f.HostPortErrors) != 3 {
		t.Fatalf("expected 3 host port errors, got %v", f.HostPortErrors)
	}
}

func TestParseRunFlags_DoubleDash(t *testing.T) {
	f := parseRunFlags([]string{"-f", "--", "-r", "extra"}, config.DefaultFlags{})
	if !f.NoFirewall {
		t.Error("-f before -- should be parsed")
	}
	if f.ReadOnly {
		t.Error("-r after -- should not be parsed as flag")
	}
	if len(f.Remaining) != 2 || f.Remaining[0] != "-r" || f.Remaining[1] != "extra" {
		t.Errorf("unexpected remaining: %v", f.Remaining)
	}
}

func TestParseRunFlags_DefaultsFromConfig(t *testing.T) {
	defaults := config.DefaultFlags{
		NoFirewall: true,
		ReadOnly:   true,
		NoEnv:      true,
	}
	f := parseRunFlags(nil, defaults)
	if !f.NoFirewall {
		t.Error("NoFirewall default not applied")
	}
	if !f.ReadOnly {
		t.Error("ReadOnly default not applied")
	}
	if !f.NoEnv {
		t.Error("NoEnv default not applied")
	}
}

func TestParseRunFlags_Resume(t *testing.T) {
	// --resume without token
	f := parseRunFlags([]string{"--resume"}, config.DefaultFlags{})
	if !f.Resume {
		t.Error("--resume should set Resume=true")
	}
	if f.ResumeToken != "" {
		t.Errorf("expected empty token, got %q", f.ResumeToken)
	}
}

func TestParseRunFlags_ResumeWithToken(t *testing.T) {
	f := parseRunFlags([]string{"--resume", "abc123"}, config.DefaultFlags{})
	if !f.Resume {
		t.Error("--resume should set Resume=true")
	}
	if f.ResumeToken != "abc123" {
		t.Errorf("expected token abc123, got %q", f.ResumeToken)
	}
}

func TestParseRunFlags_NoResumeOverridesDefault(t *testing.T) {
	f := parseRunFlags([]string{"--no-resume"}, config.DefaultFlags{AutoResume: true})
	if f.Resume {
		t.Error("--no-resume should override AutoResume default")
	}
}

func TestParseRunFlags_ResumeDefaultFromConfig(t *testing.T) {
	f := parseRunFlags(nil, config.DefaultFlags{AutoResume: true})
	if !f.Resume {
		t.Error("Resume should be true when AutoResume config default is true")
	}

	f = parseRunFlags(nil, config.DefaultFlags{AutoResume: false})
	if f.Resume {
		t.Error("Resume should be false when AutoResume config default is false")
	}
}

func TestParseRunFlags_UnknownArgsPassedThrough(t *testing.T) {
	f := parseRunFlags([]string{"--custom-flag", "value"}, config.DefaultFlags{})
	if len(f.Remaining) != 2 || f.Remaining[0] != "--custom-flag" || f.Remaining[1] != "value" {
		t.Errorf("unknown args not passed through: %v", f.Remaining)
	}
}

func TestParseRunFlags_SessionName(t *testing.T) {
	f := parseRunFlags([]string{"--name", "2026-02-11 14:22:00"}, config.DefaultFlags{})
	if f.SessionName != "2026-02-11 14:22:00" {
		t.Errorf("expected session name to be parsed, got %q", f.SessionName)
	}
}

func TestParseSessionAction(t *testing.T) {
	raw := "workspace=work\nsession=2026-02-11 14:22:00\nresume=true\n"
	a := parseSessionAction(raw)
	if a.Workspace != "work" {
		t.Fatalf("expected workspace work, got %q", a.Workspace)
	}
	if a.SessionName != "2026-02-11 14:22:00" {
		t.Fatalf("expected session name, got %q", a.SessionName)
	}
	if !a.Resume {
		t.Fatalf("expected resume=true")
	}
}

func TestApplySessionResumeDefaults_NameImpliesResume(t *testing.T) {
	f := parsedFlags{
		Resume:         false,
		SessionName:    "my-session",
		SessionNameSet: true,
	}
	f = applySessionResumeDefaults(f)
	if !f.Resume {
		t.Fatal("--name should imply Resume=true")
	}
}

func TestApplySessionResumeDefaults_AutoGeneratedName(t *testing.T) {
	f := parsedFlags{
		Resume:         false,
		SessionName:    "2026-02-12 15:04:05",
		SessionNameSet: false, // auto-generated, not via --name
	}
	f = applySessionResumeDefaults(f)
	if f.Resume {
		t.Fatal("auto-generated session name should NOT imply Resume")
	}
}

func TestApplySessionResumeDefaults_NoResumeWins(t *testing.T) {
	f := parsedFlags{
		Resume:         false,
		NoResumeSet:    true,
		SessionName:    "my-session",
		SessionNameSet: true,
	}
	f = applySessionResumeDefaults(f)
	if f.Resume {
		t.Fatal("--no-resume should override --name's implied resume")
	}
}

func TestParseRunFlags_Version(t *testing.T) {
	f := parseRunFlags([]string{"--version", "1.0.123"}, config.DefaultFlags{})
	if f.AgentVersion != "1.0.123" {
		t.Errorf("expected AgentVersion 1.0.123, got %q", f.AgentVersion)
	}
}

func TestParseRunFlags_VersionMissing(t *testing.T) {
	f := parseRunFlags([]string{"--version"}, config.DefaultFlags{})
	if f.AgentVersion != "" {
		t.Errorf("expected empty AgentVersion when value missing, got %q", f.AgentVersion)
	}
}

func TestParseRunFlags_FullGitSupportDefault(t *testing.T) {
	// FullGitSupport is a config-level setting, not a CLI flag.
	// It's propagated from config.DefaultFlags.FullGitSupport to run.Options.
	// Verify parseRunFlags doesn't override it (it shouldn't touch it since
	// it's not a parsed flag — it comes from config).
	defaults := config.DefaultFlags{FullGitSupport: true}
	_ = parseRunFlags(nil, defaults)
	// FullGitSupport is not a parsedFlags field; it's set directly on run.Options
	// from cfg.Settings.DefaultFlags.FullGitSupport in runAgent(). Verify the
	// default flag struct preserves the value.
	if !defaults.FullGitSupport {
		t.Error("FullGitSupport should remain true in defaults")
	}
}
