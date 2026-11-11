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

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/router"
	"github.com/cloud-exit/exitbox/internal/subagents"
)

// setClaudeRouter adds a workspace whose claude-router plugin is enabled
// (or not) with the given routes.
func setClaudeRouter(t *testing.T, workspace string, enabled bool, routes string) {
	t.Helper()
	cfg := config.LoadOrDefault()
	ws := cfg.Workspace(workspace)
	if ws == nil {
		cfg.Workspaces.Items = append(cfg.Workspaces.Items, config.Workspace{Name: workspace})
		ws = cfg.Workspace(workspace)
	}
	entry := config.PluginEntry{Enabled: enabled}
	if routes != "" {
		entry.Settings = map[string]string{"routes": routes}
	}
	ws.SetPluginEntry("claude", "claude-router", entry)
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

// writeModelAgent stores a Claude Code subagent with a model and, when
// window is not empty, an autoCompactWindow.
func writeModelAgent(t *testing.T, workspace, name, model, window string) {
	t.Helper()
	dir := subagents.Claude.StoreDir(workspace)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data := "---\nname: " + name + "\ndescription: d\nmodel: " + model + "\n"
	if window != "" {
		data += "autoCompactWindow: " + window + "\n"
	}
	data += "---\nPrompt.\n"
	if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSubagentsValidate_ModelRoutes(t *testing.T) {
	withSubagentsHome(t)
	writeModelAgent(t, "work", "local", "qwen3.6-27b", "")

	// Plugin not enabled: a hint, validation passes.
	out, _, err := runSubagents(t, "validate", "-w", "work")
	if err != nil || !strings.Contains(out, "needs a claude-router route") {
		t.Fatalf("plugin off: out = %q, err = %v", out, err)
	}

	// Enabled with a route for the model: clean.
	setClaudeRouter(t, "work", true, "qwen3.6-27b=http://10.0.0.21:8080")
	out, _, err = runSubagents(t, "validate", "-w", "work")
	if err != nil || !strings.Contains(out, "local (local.md): ok") {
		t.Fatalf("routed: out = %q, err = %v", out, err)
	}

	// Enabled, but the route is for another model: the subagent cannot run.
	setClaudeRouter(t, "work", true, "gemma=http://10.0.0.21:8080")
	out, _, err = runSubagents(t, "validate", "-w", "work")
	if err == nil || !strings.Contains(out, `"qwen3.6-27b" has no claude-router route (routes: gemma)`) {
		t.Fatalf("unrouted: out = %q, err = %v", out, err)
	}
	// The same through the single-name form and in the list.
	if out, _, err := runSubagents(t, "validate", "local", "-w", "work"); err == nil || !strings.Contains(out, "no claude-router route") {
		t.Errorf("validate local: out = %q, err = %v", out, err)
	}
	if out, _, err := runSubagents(t, "list", "-w", "work"); err != nil || !strings.Contains(out, "managed  error") || !strings.Contains(out, "no claude-router route") {
		t.Errorf("list: out = %q, err = %v", out, err)
	}

	// Routes of another workspace do not count.
	setClaudeRouter(t, "other", true, "qwen3.6-27b=http://10.0.0.21:8080")
	if _, _, err := runSubagents(t, "validate", "-w", "work"); err == nil {
		t.Error("a route in another workspace satisfied this one")
	}

	// Disabled again: back to the hint, even with routes stored.
	setClaudeRouter(t, "work", false, "gemma=http://10.0.0.21:8080")
	if out, _, err := runSubagents(t, "validate", "-w", "work"); err != nil || !strings.Contains(out, "needs a claude-router route") {
		t.Errorf("disabled: out = %q, err = %v", out, err)
	}
}

// stubProbe replaces the backend probe. It answers per route ID and counts
// the calls and the tokens it was given.
type stubProbe struct {
	ctx    map[string]int
	err    map[string]error
	calls  []string
	tokens []string
	// needsToken lists routes that answer ErrUnauthorized without a token.
	needsToken map[string]bool
}

func withStubProbe(t *testing.T, s *stubProbe) {
	t.Helper()
	old := probeContext
	probeContext = func(r router.Route, token string) (int, error) {
		s.calls = append(s.calls, r.ID)
		s.tokens = append(s.tokens, token)
		if s.needsToken[r.ID] && token == "" {
			return 0, router.ErrUnauthorized
		}
		if err := s.err[r.ID]; err != nil {
			return 0, err
		}
		return s.ctx[r.ID], nil
	}
	t.Cleanup(func() { probeContext = old })
}

func TestSubagentsValidate_Probe(t *testing.T) {
	withSubagentsHome(t)
	setClaudeRouter(t, "work", true, strings.Join([]string{
		"small=http://10.0.0.21:8080",
		"big=http://10.0.0.22:8000",
		"mid=http://10.0.0.23:8000",
		"down=http://10.0.0.24:8000",
	}, ","))
	writeModelAgent(t, "work", "tiny-ctx", "small", "")
	writeModelAgent(t, "work", "no-window", "big", "")
	writeModelAgent(t, "work", "no-window-2", "big", "") // same backend again
	writeModelAgent(t, "work", "too-large", "mid", "200000")
	writeModelAgent(t, "work", "fits", "big", "200000")
	writeModelAgent(t, "work", "unreachable", "down", "")
	writeModelAgent(t, "work", "on-claude", "sonnet", "")

	probe := &stubProbe{
		ctx: map[string]int{"small": 32768, "big": 262144, "mid": 131072},
		err: map[string]error{"down": errors.New("connection refused")},
	}
	withStubProbe(t, probe)

	// Without --probe no backend is contacted.
	out, _, err := runSubagents(t, "validate", "-w", "work")
	if err != nil || len(probe.calls) != 0 {
		t.Fatalf("plain validate: out = %q, err = %v, probes = %v", out, err, probe.calls)
	}

	out, _, err = runSubagents(t, "validate", "-w", "work", "--probe")
	if err != nil {
		t.Fatalf("validate --probe: out = %q, err = %v (probe findings are warnings)", out, err)
	}
	for name, want := range map[string]string{
		"tiny-ctx":    "http://10.0.0.21:8080 serves 32768 tokens of context for \"small\", less than the smallest autoCompactWindow Claude Code accepts (100000)",
		"no-window":   "set autoCompactWindow: 262144 or lower",
		"no-window-2": "set autoCompactWindow: 262144 or lower",
		"too-large":   "200000 is more than the 131072 tokens of context http://10.0.0.23:8000 serves",
		"unreachable": "could not read the context size of http://10.0.0.24:8000: connection refused",
	} {
		section := sectionOf(out, "claude: "+name+" ")
		if !strings.Contains(section, want) {
			t.Errorf("%s: section %q lacks %q", name, section, want)
		}
	}
	for _, name := range []string{"fits", "on-claude"} {
		if !strings.Contains(out, "claude: "+name+" ("+name+".md): ok") {
			t.Errorf("%s should be clean:\n%s", name, out)
		}
	}
	// Four backends, each asked once although "big" serves three subagents.
	if len(probe.calls) != 4 {
		t.Errorf("probes = %v, want one per backend", probe.calls)
	}

	// The single-name form probes only that subagent's backend.
	probe.calls = nil
	if out, _, err := runSubagents(t, "validate", "too-large", "-w", "work", "--probe"); err != nil || !strings.Contains(out, "200000 is more than") || len(probe.calls) != 1 {
		t.Errorf("validate too-large --probe: out = %q, err = %v, probes = %v", out, err, probe.calls)
	}
}

// sectionOf returns the lines of a validate report that belong to the entry
// whose header starts with prefix.
func sectionOf(out, prefix string) string {
	var b strings.Builder
	in := false
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, " ") {
			in = strings.HasPrefix(line, prefix)
		}
		if in {
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

func TestSubagentsValidate_ProbeReadsTokensFromTheVault(t *testing.T) {
	withSubagentsHome(t)
	setClaudeRouter(t, "work", true, "a=http://10.0.0.21:8080=vault:KEY_A,b=http://10.0.0.22:8080=vault:KEY_B,open=http://10.0.0.23:8080=vault:KEY_C")
	writeModelAgent(t, "work", "one", "a", "150000")
	writeModelAgent(t, "work", "two", "b", "150000")
	writeModelAgent(t, "work", "three", "open", "150000")

	probe := &stubProbe{
		ctx:        map[string]int{"a": 200000, "b": 200000, "open": 200000},
		needsToken: map[string]bool{"a": true, "b": true},
	}
	withStubProbe(t, probe)

	// No vault in the workspace: the reason is reported, nobody is asked
	// for a password.
	prompts := 0
	oldPassword, oldSecret, oldReady := vaultPassword, vaultSecret, vaultReady
	t.Cleanup(func() { vaultPassword, vaultSecret, vaultReady = oldPassword, oldSecret, oldReady })
	vaultPassword = func(string) string { prompts++; return "pw" }
	vaultSecret = func(ws, password, key string) (string, error) {
		if ws != "work" || password != "pw" {
			return "", errors.New("wrong vault or password")
		}
		return "token-" + key, nil
	}
	vaultReady = func(string) bool { return false }
	out, _, err := runSubagents(t, "validate", "-w", "work", "--probe")
	if err != nil || prompts != 0 || !strings.Contains(out, "the backend wants a token and vault key KEY_A could not be read: workspace 'work' has no vault") {
		t.Fatalf("no vault: out = %q, err = %v, prompts = %d", out, err, prompts)
	}

	// With a vault: one password prompt serves both backends that want a
	// token, and the backend that answers without one is never given any.
	vaultReady = func(string) bool { return true }
	probe.calls, probe.tokens = nil, nil
	out, _, err = runSubagents(t, "validate", "-w", "work", "--probe")
	if err != nil || strings.Contains(out, "could not read") {
		t.Fatalf("with a vault: out = %q, err = %v", out, err)
	}
	if prompts != 1 {
		t.Errorf("asked for the vault password %d times, want once", prompts)
	}
	got := map[string]bool{}
	for i, id := range probe.calls {
		got[id+"/"+probe.tokens[i]] = true
	}
	for _, want := range []string{"a/", "a/token-KEY_A", "b/", "b/token-KEY_B", "open/"} {
		if !got[want] {
			t.Errorf("no probe %q in %v", want, got)
		}
	}
	if got["open/token-KEY_C"] {
		t.Error("a backend that answers without a token was sent one")
	}
	if strings.Contains(out, "token-KEY") {
		t.Errorf("a token was printed:\n%s", out)
	}
}

func TestClaudeRouterRoutes(t *testing.T) {
	withSubagentsHome(t)
	if routes, enabled := claudeRouterRoutes("work"); enabled || routes != nil {
		t.Errorf("unknown workspace: %v %v", routes, enabled)
	}
	setClaudeRouter(t, "work", true, "a=http://h:1,b=http://h:2")
	routes, enabled := claudeRouterRoutes("work")
	if !enabled || strings.Join(router.IDs(routes), ",") != "a,b" {
		t.Errorf("enabled: %v %v", routes, enabled)
	}
	if got := subagents.RoutesFor("work"); !got.Enabled || strings.Join(got.IDs, ",") != "a,b" {
		t.Errorf("subagents.RoutesFor = %+v", got)
	}
	setClaudeRouter(t, "work", false, "a=http://h:1")
	if routes, enabled := claudeRouterRoutes("work"); enabled || routes != nil {
		t.Errorf("disabled: %v %v", routes, enabled)
	}
}
