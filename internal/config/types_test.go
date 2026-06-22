package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestAllDomains_Basic(t *testing.T) {
	al := &Allowlist{
		AIProviders: []string{"anthropic.com", "openai.com"},
		Development: []string{"github.com"},
	}
	domains := al.AllDomains()
	if len(domains) != 3 {
		t.Fatalf("AllDomains() returned %d domains, want 3", len(domains))
	}
}

func TestAllDomains_Deduplication(t *testing.T) {
	al := &Allowlist{
		AIProviders:   []string{"example.com"},
		Development:   []string{"example.com"},
		CloudServices: []string{"example.com"},
	}
	domains := al.AllDomains()
	if len(domains) != 1 {
		t.Errorf("AllDomains() returned %d domains, want 1 (deduped)", len(domains))
	}
}

func TestAllDomains_Empty(t *testing.T) {
	al := &Allowlist{}
	domains := al.AllDomains()
	if len(domains) != 0 {
		t.Errorf("AllDomains() returned %d domains for empty allowlist, want 0", len(domains))
	}
}

func TestAllDomains_PreservesOrder(t *testing.T) {
	al := &Allowlist{
		AIProviders: []string{"a.com", "b.com"},
		Development: []string{"c.com"},
	}
	domains := al.AllDomains()
	expected := []string{"a.com", "b.com", "c.com"}
	for i, d := range domains {
		if d != expected[i] {
			t.Errorf("AllDomains()[%d] = %q, want %q", i, d, expected[i])
		}
	}
}

func TestAllDomains_CustomIncluded(t *testing.T) {
	al := &Allowlist{
		Custom: []string{"my-custom.example.com"},
	}
	domains := al.AllDomains()
	if len(domains) != 1 || domains[0] != "my-custom.example.com" {
		t.Errorf("AllDomains() = %v, want [my-custom.example.com]", domains)
	}
}

func TestIsAgentEnabled(t *testing.T) {
	cfg := &Config{
		Agents: AgentConfig{
			Claude:   AgentEntry{Enabled: true},
			Codex:    AgentEntry{Enabled: false},
			Copilot:  AgentEntry{Enabled: true},
			Cursor:   AgentEntry{Enabled: false},
			Kimi:     AgentEntry{Enabled: true},
			OpenCode: AgentEntry{Enabled: true},
		},
	}

	tests := []struct {
		name     string
		expected bool
	}{
		{"claude", true},
		{"codex", false},
		{"copilot", true},
		{"cursor", false},
		{"kimi", true},
		{"opencode", true},
		{"unknown", false},
		{"", false},
	}
	for _, tc := range tests {
		got := cfg.IsAgentEnabled(tc.name)
		if got != tc.expected {
			t.Errorf("IsAgentEnabled(%q) = %v, want %v", tc.name, got, tc.expected)
		}
	}
}

func TestSetAgentEnabled(t *testing.T) {
	cfg := &Config{}

	cfg.SetAgentEnabled("claude", true)
	if !cfg.Agents.Claude.Enabled {
		t.Error("SetAgentEnabled(claude, true) did not enable claude")
	}

	cfg.SetAgentEnabled("codex", true)
	if !cfg.Agents.Codex.Enabled {
		t.Error("SetAgentEnabled(codex, true) did not enable codex")
	}

	cfg.SetAgentEnabled("opencode", true)
	if !cfg.Agents.OpenCode.Enabled {
		t.Error("SetAgentEnabled(opencode, true) did not enable opencode")
	}

	cfg.SetAgentEnabled("copilot", true)
	if !cfg.Agents.Copilot.Enabled {
		t.Error("SetAgentEnabled(copilot, true) did not enable copilot")
	}

	cfg.SetAgentEnabled("cursor", true)
	if !cfg.Agents.Cursor.Enabled {
		t.Error("SetAgentEnabled(cursor, true) did not enable cursor")
	}

	cfg.SetAgentEnabled("kimi", true)
	if !cfg.Agents.Kimi.Enabled {
		t.Error("SetAgentEnabled(kimi, true) did not enable kimi")
	}

	cfg.SetAgentEnabled("claude", false)
	if cfg.Agents.Claude.Enabled {
		t.Error("SetAgentEnabled(claude, false) did not disable claude")
	}

	// Unknown agent should be a no-op
	cfg.SetAgentEnabled("unknown", true)
}

func TestGetAgentVersion(t *testing.T) {
	cfg := &Config{
		Agents: AgentConfig{
			Claude:   AgentEntry{Enabled: true, Version: "1.0.123"},
			Codex:    AgentEntry{Enabled: true},
			OpenCode: AgentEntry{Enabled: true, Version: "0.5.0"},
		},
	}

	tests := []struct {
		name     string
		expected string
	}{
		{"claude", "1.0.123"},
		{"codex", ""},
		{"opencode", "0.5.0"},
		{"unknown", ""},
		{"", ""},
	}
	for _, tc := range tests {
		got := cfg.GetAgentVersion(tc.name)
		if got != tc.expected {
			t.Errorf("GetAgentVersion(%q) = %q, want %q", tc.name, got, tc.expected)
		}
	}
}

func TestAgentEntry_VersionYAML(t *testing.T) {
	input := "enabled: true\nversion: 1.0.123\n"
	var entry AgentEntry
	if err := yaml.Unmarshal([]byte(input), &entry); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !entry.Enabled {
		t.Error("expected Enabled=true")
	}
	if entry.Version != "1.0.123" {
		t.Errorf("expected Version=1.0.123, got %q", entry.Version)
	}

	// Version omitted when empty
	entry2 := AgentEntry{Enabled: true}
	data, err := yaml.Marshal(entry2)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(data) != "enabled: true\n" {
		t.Errorf("expected no version field, got %q", string(data))
	}
}

func TestDefaultConfig_Values(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Version != 1 {
		t.Errorf("Version = %d, want 1", cfg.Version)
	}
	if cfg.Workspaces.Active != "default" {
		t.Errorf("Workspaces.Active = %q, want %q", cfg.Workspaces.Active, "default")
	}
	if len(cfg.Workspaces.Items) != 1 {
		t.Fatalf("Workspaces.Items len = %d, want 1", len(cfg.Workspaces.Items))
	}
	if cfg.Workspaces.Items[0].Name != "default" {
		t.Errorf("Workspaces.Items[0].Name = %q, want %q", cfg.Workspaces.Items[0].Name, "default")
	}
	if cfg.Settings.AutoUpdate {
		t.Error("Settings.AutoUpdate should be false by default")
	}
	if !cfg.Settings.StatusBar {
		t.Error("Settings.StatusBar should be true by default")
	}
	if cfg.Settings.DefaultFlags.AutoResume {
		t.Error("Settings.DefaultFlags.AutoResume should be false by default")
	}

	// All agents disabled by default
	for _, name := range []string{"claude", "codex", "opencode"} {
		if cfg.IsAgentEnabled(name) {
			t.Errorf("%s should be disabled by default", name)
		}
	}
}

func TestDefaultAllowlist(t *testing.T) {
	al := DefaultAllowlist()

	if al.Version != 1 {
		t.Errorf("Version = %d, want 1", al.Version)
	}
	if len(al.AIProviders) == 0 {
		t.Error("AIProviders should not be empty")
	}
	if len(al.Development) == 0 {
		t.Error("Development should not be empty")
	}

	// Check some critical domains are present
	domains := al.AllDomains()
	domainSet := make(map[string]bool)
	for _, d := range domains {
		domainSet[d] = true
	}

	critical := []string{"anthropic.com", "openai.com", "github.com", "npmjs.org"}
	for _, d := range critical {
		if !domainSet[d] {
			t.Errorf("default allowlist missing critical domain: %s", d)
		}
	}
}

func TestVaultConfig_YAMLRoundTrip(t *testing.T) {
	tests := []struct {
		name     string
		input    VaultConfig
		wantYAML string
	}{
		{
			name:     "enabled read-write",
			input:    VaultConfig{Enabled: true},
			wantYAML: "enabled: true\n",
		},
		{
			name:     "enabled read-only",
			input:    VaultConfig{Enabled: true, ReadOnly: true},
			wantYAML: "enabled: true\nread_only: true\n",
		},
		{
			name:     "disabled",
			input:    VaultConfig{},
			wantYAML: "enabled: false\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := yaml.Marshal(tt.input)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(data) != tt.wantYAML {
				t.Errorf("Marshal = %q, want %q", string(data), tt.wantYAML)
			}
			var got VaultConfig
			if err := yaml.Unmarshal(data, &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if got != tt.input {
				t.Errorf("round-trip: got %+v, want %+v", got, tt.input)
			}
		})
	}
}
