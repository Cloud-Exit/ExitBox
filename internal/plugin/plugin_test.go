// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package plugin

import (
	"fmt"
	"reflect"
	"testing"
)

func testPlugin() *Plugin {
	return &Plugin{
		Name:    "codex-router",
		Display: "Codex Router",
		Agents:  []string{"codex", "claude"},
		Version: "main",
		Settings: []Setting{
			{Key: "providers", Type: TypeMulti, Options: []string{"anthropic-api", "deepseek"}},
			{Key: "anthropic_api_key", Type: TypeSecret, DependsOn: "providers", DependsValue: "anthropic-api"},
			{Key: "port", Type: TypeInt, Default: "4202"},
			{Key: "auto_start", Type: TypeBool, Default: "true"},
			{Key: "mode", Type: TypeEnum, Options: []string{"fast", "slow"}},
		},
	}
}

func TestSupportsAgent(t *testing.T) {
	p := testPlugin()
	if !p.SupportsAgent("codex") {
		t.Error("expected codex to be supported")
	}
	if p.SupportsAgent("qwen") {
		t.Error("qwen is not in Agents but was reported as supported")
	}
}

func TestVersionOrDefault(t *testing.T) {
	p := testPlugin()
	if got := p.VersionOrDefault(""); got != "main" {
		t.Errorf("VersionOrDefault(\"\") = %q, want main", got)
	}
	if got := p.VersionOrDefault("v1.2.3"); got != "v1.2.3" {
		t.Errorf("VersionOrDefault(v1.2.3) = %q", got)
	}
}

func TestEnvNames(t *testing.T) {
	p := testPlugin()
	if got := p.EnvPrefix(); got != "EXITBOX_PLUGIN_CODEX_ROUTER" {
		t.Errorf("EnvPrefix() = %q", got)
	}
	if got := p.EnvName("anthropic_api_key"); got != "EXITBOX_PLUGIN_CODEX_ROUTER_ANTHROPIC_API_KEY" {
		t.Errorf("EnvName() = %q", got)
	}
	// Dashes in a setting key must map to the same variable the container
	// derives from the provider id.
	if got := p.EnvName("anthropic-api_key"); got != "EXITBOX_PLUGIN_CODEX_ROUTER_ANTHROPIC_API_KEY" {
		t.Errorf("EnvName() with dash = %q", got)
	}
}

func TestEnvVarsIncludesDefaultsAndSkipsHidden(t *testing.T) {
	p := testPlugin()
	vars := p.EnvVars(map[string]string{
		"providers":         "deepseek",
		"anthropic_api_key": "vault:SOME_KEY",
	})
	want := []string{
		"EXITBOX_PLUGIN_CODEX_ROUTER_AUTO_START=true",
		"EXITBOX_PLUGIN_CODEX_ROUTER_PORT=4202",
		"EXITBOX_PLUGIN_CODEX_ROUTER_PROVIDERS=deepseek",
	}
	if !reflect.DeepEqual(vars, want) {
		t.Errorf("EnvVars() = %v, want %v", vars, want)
	}
}

func TestEnvVarsIncludesVisibleSecret(t *testing.T) {
	p := testPlugin()
	vars := p.EnvVars(map[string]string{
		"providers":         "anthropic-api",
		"anthropic_api_key": "vault:SOME_KEY",
	})
	found := false
	for _, v := range vars {
		if v == "EXITBOX_PLUGIN_CODEX_ROUTER_ANTHROPIC_API_KEY=vault:SOME_KEY" {
			found = true
		}
	}
	if !found {
		t.Errorf("secret env var missing from %v", vars)
	}
}

func TestDefaultSettings(t *testing.T) {
	got := testPlugin().DefaultSettings()
	want := map[string]string{"port": "4202", "auto_start": "true"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultSettings() = %v, want %v", got, want)
	}
}

func TestSettingVisible(t *testing.T) {
	s := Setting{Key: "k", DependsOn: "providers", DependsValue: "anthropic-api"}
	if s.Visible(map[string]string{"providers": "deepseek"}) {
		t.Error("setting should be hidden when its provider is not selected")
	}
	if !s.Visible(map[string]string{"providers": "deepseek,anthropic-api"}) {
		t.Error("setting should be visible when its provider is in the list")
	}
	always := Setting{Key: "k"}
	if !always.Visible(nil) {
		t.Error("a setting without DependsOn is always visible")
	}
	any := Setting{Key: "k", DependsOn: "enabled"}
	if any.Visible(map[string]string{"enabled": "false"}) {
		t.Error("DependsOn without a value should treat false as off")
	}
	if !any.Visible(map[string]string{"enabled": "true"}) {
		t.Error("DependsOn without a value should treat true as on")
	}
}

func TestSettingValidate(t *testing.T) {
	tests := []struct {
		name    string
		setting Setting
		value   string
		wantErr bool
	}{
		{"empty is always valid", Setting{Key: "k", Type: TypeInt}, "", false},
		{"bool ok", Setting{Key: "k", Type: TypeBool}, "true", false},
		{"bool bad", Setting{Key: "k", Type: TypeBool}, "yes", true},
		{"int ok", Setting{Key: "k", Type: TypeInt}, "4202", false},
		{"int bad", Setting{Key: "k", Type: TypeInt}, "port", true},
		{"enum ok", Setting{Key: "k", Type: TypeEnum, Options: []string{"a", "b"}}, "b", false},
		{"enum bad", Setting{Key: "k", Type: TypeEnum, Options: []string{"a", "b"}}, "c", true},
		{"multi ok", Setting{Key: "k", Type: TypeMulti, Options: []string{"a", "b"}}, "a,b", false},
		{"multi bad", Setting{Key: "k", Type: TypeMulti, Options: []string{"a", "b"}}, "a,z", true},
		{"secret ok", Setting{Key: "k", Type: TypeSecret}, "vault:MY_KEY", false},
		{"secret bare", Setting{Key: "k", Type: TypeSecret}, "MY_KEY", true},
		{"secret empty ref", Setting{Key: "k", Type: TypeSecret}, "vault:", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.setting.Validate(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}

func TestSettingNormalize(t *testing.T) {
	secret := Setting{Key: "k", Type: TypeSecret}
	if got := secret.Normalize(" MY_KEY "); got != "vault:MY_KEY" {
		t.Errorf("Normalize(bare key) = %q", got)
	}
	if got := secret.Normalize("vault:MY_KEY"); got != "vault:MY_KEY" {
		t.Errorf("Normalize(reference) = %q", got)
	}
	multi := Setting{Key: "k", Type: TypeMulti}
	if got := multi.Normalize("a, b ,,c"); got != "a,b,c" {
		t.Errorf("Normalize(multi) = %q", got)
	}
	b := Setting{Key: "k", Type: TypeBool}
	if got := b.Normalize("TRUE"); got != "true" {
		t.Errorf("Normalize(bool) = %q", got)
	}
	if got := b.Normalize("  "); got != "" {
		t.Errorf("Normalize(blank) = %q", got)
	}
}

func TestVaultKey(t *testing.T) {
	key, ok := VaultKey("vault:MY_KEY")
	if !ok || key != "MY_KEY" {
		t.Errorf("VaultKey() = %q, %v", key, ok)
	}
	if _, ok := VaultKey("MY_KEY"); ok {
		t.Error("a bare value is not a vault reference")
	}
	if got := VaultRef("MY_KEY"); got != "vault:MY_KEY" {
		t.Errorf("VaultRef() = %q", got)
	}
}

func TestSplitMulti(t *testing.T) {
	got := SplitMulti(" a , b ,, c ")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SplitMulti() = %v, want %v", got, want)
	}
	if len(SplitMulti("")) != 0 {
		t.Error("SplitMulti(\"\") should be empty")
	}
}

func TestPluginSettingLookup(t *testing.T) {
	p := testPlugin()
	if s := p.Setting("port"); s == nil || s.Type != TypeInt {
		t.Errorf("Setting(port) = %v", s)
	}
	if s := p.Setting("nope"); s != nil {
		t.Errorf("Setting(nope) = %v, want nil", s)
	}
}

func TestSettingCheckRunsAfterTheTypeCheck(t *testing.T) {
	calls := 0
	s := Setting{Key: "routes", Type: TypeString, Check: func(v string) error {
		calls++
		if v == "bad" {
			return fmt.Errorf("no such route")
		}
		return nil
	}}
	if err := s.Validate("good"); err != nil {
		t.Errorf("Validate(good): %v", err)
	}
	err := s.Validate("bad")
	if err == nil || err.Error() != "routes: no such route" {
		t.Errorf("Validate(bad) = %v, want the check's error with the setting name", err)
	}
	// An empty value clears the setting and is never checked.
	if err := s.Validate(""); err != nil || calls != 2 {
		t.Errorf("Validate(\"\") = %v after %d checks, want nil and no extra check", err, calls)
	}
	// The type check comes first.
	n := Setting{Key: "port", Type: TypeInt, Check: func(string) error { t.Error("checked a value of the wrong type"); return nil }}
	if err := n.Validate("abc"); err == nil {
		t.Error("a non-number passed an int setting")
	}
}

func TestConflictsWith(t *testing.T) {
	a := &Plugin{Name: "a", Conflicts: []string{"b"}}
	b := &Plugin{Name: "b"}
	c := &Plugin{Name: "c"}
	if !a.ConflictsWith(b) || !b.ConflictsWith(a) {
		t.Error("a conflict declared by one side must hold for both")
	}
	if a.ConflictsWith(c) || c.ConflictsWith(b) || a.ConflictsWith(a) || a.ConflictsWith(nil) {
		t.Error("unexpected conflict")
	}
}

func TestSecretKeys(t *testing.T) {
	p := testPlugin()
	p.VaultKeys = func(settings map[string]string) []string {
		return []string{"FROM_ROUTES", "ANTHROPIC", ""}
	}
	got := p.SecretKeys(map[string]string{
		"providers":         "anthropic-api",
		"anthropic_api_key": "vault:ANTHROPIC",
	})
	if want := []string{"ANTHROPIC", "FROM_ROUTES"}; !reflect.DeepEqual(got, want) {
		t.Errorf("SecretKeys = %v, want %v", got, want)
	}
	// A secret whose setting is hidden is not expected in the vault.
	p.VaultKeys = nil
	if got := p.SecretKeys(map[string]string{"providers": "deepseek", "anthropic_api_key": "vault:ANTHROPIC"}); len(got) != 0 {
		t.Errorf("SecretKeys with the setting hidden = %v, want none", got)
	}
}
