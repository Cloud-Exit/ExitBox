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

package plugins

import (
	"crypto/sha256"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/distro"
	"github.com/cloud-exit/exitbox/internal/plugin"
	"github.com/cloud-exit/exitbox/internal/plugins/clauderouter"
	"github.com/cloud-exit/exitbox/internal/plugins/codexrouter"
)

func workspaceWithCodexRouter() *config.Workspace {
	ws := &config.Workspace{Name: "default"}
	ws.SetPluginEntry("codex", codexrouter.Name, config.PluginEntry{
		Enabled: true,
		Settings: map[string]string{
			"providers":         "anthropic-api,deepseek",
			"anthropic_api_key": "vault:CODEX_ROUTER_ANTHROPIC_API_KEY",
		},
	})
	return ws
}

func TestCatalogContainsCodexRouter(t *testing.T) {
	p := Get(codexrouter.Name)
	if p == nil {
		t.Fatalf("codex-router missing from the catalog")
	}
	for _, agent := range []string{"codex", "claude", "cursor"} {
		if !p.SupportsAgent(agent) {
			t.Errorf("codex-router should support %s", agent)
		}
	}
	if p.Dockerfile == nil {
		t.Error("codex-router must contribute an install snippet")
	}
}

func TestForAndAgents(t *testing.T) {
	if len(For("codex")) == 0 {
		t.Error("codex should have at least one plugin")
	}
	if len(For("qwen")) != 0 {
		t.Error("qwen has no plugins yet")
	}
	agents := Agents()
	found := false
	for _, a := range agents {
		if a == "codex" {
			found = true
		}
	}
	if !found {
		t.Errorf("Agents() = %v, want it to include codex", agents)
	}
}

func TestLookupErrors(t *testing.T) {
	if _, err := Lookup("codex", "not-a-plugin"); err == nil {
		t.Error("expected an error for an unknown plugin")
	}
	if _, err := Lookup("qwen", codexrouter.Name); err == nil {
		t.Error("expected an error for an unsupported agent")
	}
	if _, err := Lookup("codex", codexrouter.Name); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestEnabledIgnoresOtherAgents(t *testing.T) {
	ws := workspaceWithCodexRouter()
	if got := Enabled(ws, "codex"); len(got) != 1 {
		t.Errorf("Enabled(codex) = %d plugins, want 1", len(got))
	}
	if got := Enabled(ws, "claude"); len(got) != 0 {
		t.Errorf("Enabled(claude) = %d plugins, want 0", len(got))
	}
}

func TestEnabledSkipsUnknownPlugins(t *testing.T) {
	ws := &config.Workspace{Name: "default"}
	ws.SetPluginEntry("codex", "removed-plugin", config.PluginEntry{Enabled: true})
	if got := Enabled(ws, "codex"); len(got) != 0 {
		t.Errorf("Enabled() = %v, want nothing for a plugin not in the catalog", got)
	}
}

func TestSettingsMergesDefaults(t *testing.T) {
	ws := workspaceWithCodexRouter()
	settings := Settings(ws, "codex", Get(codexrouter.Name))
	if settings["port"] != "4202" {
		t.Errorf("default port missing: %v", settings)
	}
	if settings["providers"] != "anthropic-api,deepseek" {
		t.Errorf("stored providers not applied: %v", settings)
	}
}

func TestEnvVars(t *testing.T) {
	ws := workspaceWithCodexRouter()
	vars := EnvVars(ws, "codex")
	if len(vars) == 0 || vars[0] != "EXITBOX_PLUGINS=codex-router" {
		t.Fatalf("EnvVars() = %v, want the plugin list first", vars)
	}
	joined := strings.Join(vars, " ")
	for _, want := range []string{
		"EXITBOX_PLUGIN_CODEX_ROUTER_PROVIDERS=anthropic-api,deepseek",
		"EXITBOX_PLUGIN_CODEX_ROUTER_ANTHROPIC_API_KEY=vault:CODEX_ROUTER_ANTHROPIC_API_KEY",
		"EXITBOX_PLUGIN_CODEX_ROUTER_PORT=4202",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("EnvVars() missing %q. Got: %v", want, vars)
		}
	}
}

func TestEnvVarsEmptyWithoutPlugins(t *testing.T) {
	ws := &config.Workspace{Name: "default"}
	if vars := EnvVars(ws, "codex"); vars != nil {
		t.Errorf("EnvVars() = %v, want nil", vars)
	}
}

func TestDomainsIncludeProviderHosts(t *testing.T) {
	ws := workspaceWithCodexRouter()
	domains := Domains(ws, "codex")
	joined := strings.Join(domains, ",")
	for _, want := range []string{"anthropic.com", "deepseek.com", "github.com"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Domains() = %v, want it to include %q", domains, want)
		}
	}
	if len(Domains(&config.Workspace{Name: "default"}, "codex")) != 0 {
		t.Error("a workspace without plugins needs no extra domains")
	}
}

func TestDockerfileSnippets(t *testing.T) {
	ws := workspaceWithCodexRouter()
	snippets := DockerfileSnippets(ws, "codex", distro.Default())
	if len(snippets) != 1 {
		t.Fatalf("DockerfileSnippets() = %d snippets, want 1", len(snippets))
	}
	if !strings.Contains(snippets[0], codexrouter.Repo) {
		t.Errorf("snippet does not clone the upstream repo: %s", snippets[0])
	}
	if !strings.Contains(snippets[0], "--branch main") {
		t.Errorf("snippet does not pin the default ref: %s", snippets[0])
	}
}

func TestDockerfileSnippetsUsesPinnedVersion(t *testing.T) {
	ws := workspaceWithCodexRouter()
	entry, _ := ws.PluginEntry("codex", codexrouter.Name)
	entry.Version = "v0.6.0"
	ws.SetPluginEntry("codex", codexrouter.Name, entry)

	snippets := DockerfileSnippets(ws, "codex", distro.Default())
	if len(snippets) != 1 || !strings.Contains(snippets[0], "--branch v0.6.0") {
		t.Errorf("pinned version not used: %v", snippets)
	}
}

func TestBuildHash(t *testing.T) {
	ws := workspaceWithCodexRouter()
	first := BuildHash(ws, "codex")
	if first == "" {
		t.Fatal("BuildHash() should not be empty when a plugin is enabled")
	}
	if BuildHash(ws, "codex") != first {
		t.Error("BuildHash() is not deterministic")
	}
	if BuildHash(ws, "claude") != "" {
		t.Error("BuildHash() should be empty for an agent without enabled plugins")
	}

	// Settings never change the image, versions do.
	ws.SetPluginSetting("codex", codexrouter.Name, "port", "4300")
	if BuildHash(ws, "codex") != first {
		t.Error("changing a setting must not force an image rebuild")
	}
	entry, _ := ws.PluginEntry("codex", codexrouter.Name)
	entry.Version = "v0.6.0"
	ws.SetPluginEntry("codex", codexrouter.Name, entry)
	if BuildHash(ws, "codex") == first {
		t.Error("changing the version must change the image hash")
	}
}

func TestCodexRouterKeySettingsExistForEveryProvider(t *testing.T) {
	p := Get(codexrouter.Name)
	for _, id := range codexrouter.Providers() {
		if id == "github-copilot" {
			continue // OAuth based: no API key field
		}
		if p.Setting(codexrouter.KeySettingKey(id)) == nil {
			t.Errorf("provider %q has no API key setting", id)
		}
	}
}

func workspaceWithClaudeRouter(routes string) *config.Workspace {
	ws := &config.Workspace{Name: "default"}
	ws.SetPluginEntry("claude", clauderouter.Name, config.PluginEntry{
		Enabled:  true,
		Settings: map[string]string{"routes": routes},
	})
	return ws
}

func TestCatalogContainsClaudeRouterForClaudeOnly(t *testing.T) {
	p := Get(clauderouter.Name)
	if p == nil {
		t.Fatal("claude-router missing from the catalog")
	}
	for _, a := range []string{"codex", "cursor", "opencode", "qwen"} {
		if p.SupportsAgent(a) {
			t.Errorf("claude-router must not be offered for %s", a)
		}
	}
	if _, err := Lookup("claude", clauderouter.Name); err != nil {
		t.Errorf("Lookup(claude, claude-router): %v", err)
	}
	if _, err := Lookup("codex", clauderouter.Name); err == nil {
		t.Error("Lookup(codex, claude-router) succeeded")
	}
}

func TestClaudeRouterEnvDomainsAndHostPorts(t *testing.T) {
	ws := workspaceWithClaudeRouter("qwen=http://10.0.0.21:8080=vault:LLM_KEY,gemma=http://127.0.0.1:8000")
	env := strings.Join(EnvVars(ws, "claude"), "\n")
	for _, want := range []string{
		"EXITBOX_PLUGINS=claude-router",
		"EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES=qwen=http://10.0.0.21:8080=vault:LLM_KEY,gemma=http://127.0.0.1:8000",
		"EXITBOX_PLUGIN_CLAUDE_ROUTER_PORT=4203",
		"EXITBOX_PLUGIN_CLAUDE_ROUTER_AUTO_START=true",
		"EXITBOX_PLUGIN_CLAUDE_ROUTER_SANITIZE=true",
	} {
		if !strings.Contains(env, want) {
			t.Errorf("EnvVars missing %q:\n%s", want, env)
		}
	}
	if got := strings.Join(Domains(ws, "claude"), ","); got != "10.0.0.21,host.docker.internal" {
		t.Errorf("Domains = %q", got)
	}
	if got := HostPorts(ws, "claude"); len(got) != 1 || got[0] != 8000 {
		t.Errorf("HostPorts = %v, want [8000]", got)
	}
	// Another agent in the same workspace gets none of it.
	if EnvVars(ws, "codex") != nil || len(Domains(ws, "codex")) != 0 || len(HostPorts(ws, "codex")) != 0 {
		t.Error("claude-router leaked into the codex session")
	}
}

func TestConflictBetweenTheTwoRouters(t *testing.T) {
	err := Conflict("claude", clauderouter.Name, []string{codexrouter.Name})
	if err == nil || !strings.Contains(err.Error(), "exitbox plugins disable claude codex-router") {
		t.Errorf("enabling claude-router next to codex-router = %v, want a refusal that says how to resolve it", err)
	}
	// The conflict holds whichever was enabled first.
	if err := Conflict("claude", codexrouter.Name, []string{clauderouter.Name}); err == nil {
		t.Error("enabling codex-router next to claude-router was allowed")
	}
	if err := Conflict("claude", clauderouter.Name, nil); err != nil {
		t.Errorf("claude-router alone: %v", err)
	}
	if err := Conflict("claude", clauderouter.Name, []string{clauderouter.Name, "no-such-plugin"}); err != nil {
		t.Errorf("a plugin does not conflict with itself or with unknown names: %v", err)
	}
	// codex-router for Codex has no claude-router to collide with.
	if err := Conflict("codex", codexrouter.Name, []string{codexrouter.Name}); err != nil {
		t.Errorf("codex-router for codex: %v", err)
	}
}

func TestCheckRun(t *testing.T) {
	ws := workspaceWithClaudeRouter("qwen=http://10.0.0.21:8080")
	if err := CheckRun(ws, "claude", plugin.RunInput{Firewall: true}); err != nil {
		t.Errorf("CheckRun: %v", err)
	}
	if err := CheckRun(ws, "claude", plugin.RunInput{Firewall: true, Ollama: true}); err == nil {
		t.Error("--ollama with claude-router was allowed")
	}
	if err := CheckRun(ws, "claude", plugin.RunInput{Firewall: true, BaseURLFrom: "-e ANTHROPIC_BASE_URL"}); err == nil {
		t.Error("a base URL set elsewhere was allowed")
	}
	// The plugin is for Claude Code: a Codex run in the same workspace is
	// not its business.
	if err := CheckRun(ws, "codex", plugin.RunInput{Ollama: true, BaseURLFrom: "x"}); err != nil {
		t.Errorf("CheckRun(codex): %v", err)
	}
	// A config edited by hand can enable both routers; the run is refused.
	ws.SetPluginEntry("claude", codexrouter.Name, config.PluginEntry{Enabled: true})
	if err := CheckRun(ws, "claude", plugin.RunInput{Firewall: true}); err == nil || !strings.Contains(err.Error(), "cannot be enabled together") {
		t.Errorf("both routers enabled = %v, want a refusal", err)
	}
	if err := CheckRun(nil, "claude", plugin.RunInput{}); err != nil {
		t.Errorf("CheckRun without a workspace: %v", err)
	}
}

func TestClaudeRouterBuild(t *testing.T) {
	ws := workspaceWithClaudeRouter("")
	files := BuildFiles(ws, "claude")
	if len(files[clauderouter.Binary]) == 0 {
		t.Fatalf("BuildFiles = %d files, want the router binary", len(files))
	}
	if len(BuildFiles(ws, "codex")) != 0 || len(BuildFiles(workspaceWithCodexRouter(), "codex")) != 0 {
		t.Error("only claude-router ships files")
	}
	snippets := DockerfileSnippets(ws, "claude", distro.Default())
	if len(snippets) != 1 || !strings.Contains(snippets[0], "COPY "+clauderouter.Binary) {
		t.Errorf("snippets = %v", snippets)
	}

	// The image hash follows the shipped binary, so a new router rebuilds
	// the layer even though the plugin has no version to pin.
	p := Get(clauderouter.Name)
	sum := sha256.Sum256([]byte(p.Name + "@" + p.Version + "+" + p.BuildID(runtime.GOARCH)))
	if got, want := BuildHash(ws, "claude"), fmt.Sprintf("%x", sum[:8]); got != want {
		t.Errorf("BuildHash = %q, want %q", got, want)
	}
}
