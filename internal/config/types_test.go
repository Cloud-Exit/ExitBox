package config

import "testing"

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
			OpenCode: AgentEntry{Enabled: true},
		},
	}

	tests := []struct {
		name     string
		expected bool
	}{
		{"claude", true},
		{"codex", false},
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

	cfg.SetAgentEnabled("claude", false)
	if cfg.Agents.Claude.Enabled {
		t.Error("SetAgentEnabled(claude, false) did not disable claude")
	}

	// Unknown agent should be a no-op
	cfg.SetAgentEnabled("unknown", true)
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
	if !cfg.Settings.DefaultFlags.AutoResume {
		t.Error("Settings.DefaultFlags.AutoResume should be true by default")
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
