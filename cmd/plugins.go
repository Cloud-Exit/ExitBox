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
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/plugin"
	"github.com/cloud-exit/exitbox/internal/plugins"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/internal/wizard"
	"github.com/spf13/cobra"
)

// pluginWorkspace is the --workspace override shared by every plugins
// subcommand. Plugin configuration is per workspace.
var pluginWorkspace string

func newPluginsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plugins",
		Aliases: []string{"plugin"},
		Short:   "Manage agent plugins",
		Long: `Configure plugins that extend an agent inside its ExitBox image.

Plugins are enabled per workspace. Enabling one adds it to the agent's
plugins image layer (built on the next run or rebuild); its settings are
applied when the container starts.

Every command also works agent-first, so these are the same:
  exitbox plugins list codex
  exitbox plugins codex list
  exitbox plugins edit codex codex-router
  exitbox plugins codex edit codex-router`,
	}

	cmd.PersistentFlags().StringVarP(&pluginWorkspace, "workspace", "w", "", "Workspace to configure (default: active workspace)")

	cmd.AddCommand(newPluginsListCmd())
	cmd.AddCommand(newPluginsShowCmd())
	cmd.AddCommand(newPluginsEnableCmd())
	cmd.AddCommand(newPluginsDisableCmd())
	cmd.AddCommand(newPluginsEditCmd())
	cmd.AddCommand(newPluginsSetCmd())
	cmd.AddCommand(newPluginsInstallCmd())
	cmd.AddCommand(newPluginsRemoveCmd())

	// Agent-first form: `exitbox plugins codex list`.
	for _, agentName := range plugins.Agents() {
		cmd.AddCommand(newPluginsAgentCmd(agentName))
	}
	return cmd
}

// newPluginsAgentCmd builds the agent-scoped command tree, so both
// `exitbox plugins edit codex codex-router` and
// `exitbox plugins codex edit codex-router` work.
func newPluginsAgentCmd(agentName string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   agentName,
		Short: fmt.Sprintf("Manage %s plugins", agentName),
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List plugins available for this agent",
		Args:  cobra.NoArgs,
		Run:   func(cmd *cobra.Command, args []string) { runPluginsList(agentName) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "show <plugin>",
		Short: "Show a plugin's configuration",
		Args:  cobra.ExactArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { runPluginsShow(agentName, args[0]) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "enable <plugin>",
		Short: "Enable a plugin for this agent",
		Args:  cobra.ExactArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { runPluginsSetEnabled(agentName, args[0], true) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "disable <plugin>",
		Short: "Disable a plugin for this agent",
		Args:  cobra.ExactArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { runPluginsSetEnabled(agentName, args[0], false) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "edit <plugin>",
		Short: "Open a plugin's configuration menu",
		Args:  cobra.ExactArgs(1),
		Run:   func(cmd *cobra.Command, args []string) { runPluginsEdit(agentName, args[0]) },
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "set <plugin> <key> [value]",
		Short: "Set one plugin setting without the menu",
		Args:  cobra.RangeArgs(2, 3),
		Run: func(cmd *cobra.Command, args []string) {
			value := ""
			if len(args) == 3 {
				value = args[2]
			}
			runPluginsSetSetting(agentName, args[0], args[1], value)
		},
	})
	return cmd
}

func newPluginsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [agent]",
		Short: "List plugins and their status",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 1 {
				runPluginsList(mustPluginAgent(args[0]))
				return
			}
			for i, agentName := range plugins.Agents() {
				if i > 0 {
					fmt.Println()
				}
				runPluginsList(agentName)
			}
		},
	}
}

func newPluginsShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <agent> <plugin>",
		Short: "Show a plugin's configuration",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsShow(mustPluginAgent(args[0]), args[1])
		},
	}
}

func newPluginsEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable <agent> <plugin>",
		Short: "Enable a plugin for an agent",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsSetEnabled(mustPluginAgent(args[0]), args[1], true)
		},
	}
}

func newPluginsDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable <agent> <plugin>",
		Short: "Disable a plugin for an agent",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsSetEnabled(mustPluginAgent(args[0]), args[1], false)
		},
	}
}

func newPluginsEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit <agent> <plugin>",
		Short: "Open a plugin's configuration menu",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			runPluginsEdit(mustPluginAgent(args[0]), args[1])
		},
	}
}

func newPluginsSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <agent> <plugin> <key> [value]",
		Short: "Set one plugin setting without the menu",
		Long: `Set one plugin setting directly.

An empty value clears the setting. Secret settings store a vault key
reference, never the secret itself:

  exitbox plugins set codex codex-router anthropic_api_key MY_VAULT_KEY
  exitbox vault set MY_VAULT_KEY <the-api-key>`,
		Args: cobra.RangeArgs(3, 4),
		Run: func(cmd *cobra.Command, args []string) {
			value := ""
			if len(args) == 4 {
				value = args[3]
			}
			runPluginsSetSetting(mustPluginAgent(args[0]), args[1], args[2], value)
		},
	}
}

func newPluginsInstallCmd() *cobra.Command {
	var name string

	cmd := &cobra.Command{
		Use:   "install <agent> <git-url-or-path>",
		Short: "Install a marketplace plugin into the current project",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			agentName := args[0]
			source := args[1]

			if agents.Get(agentName) == nil {
				ui.Errorf("Unknown agent: %s", agentName)
			}

			projectDir, err := os.Getwd()
			if err != nil {
				ui.Errorf("Failed to determine current directory: %v", err)
			}
			installed, err := plugins.Install(projectDir, agentName, source, name)
			if err != nil {
				ui.Errorf("Failed to install plugin: %v", err)
			}
			if len(installed) == 1 {
				ui.Successf("Installed plugin '%s' for %s in project '%s'", installed[0].Name, agentName, projectDir)
				fmt.Printf("Host path: %s\n", installed[0].Dir)
				return
			}
			ui.Successf("Installed %d plugins for %s in project '%s'", len(installed), agentName, projectDir)
			for _, installedPlugin := range installed {
				fmt.Printf("%s -> %s\n", installedPlugin.Name, installedPlugin.Dir)
			}
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "Override plugin directory name")
	return cmd
}

func newPluginsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <agent> <name>",
		Short: "Remove a marketplace plugin from the current project",
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			agentName := args[0]
			name := args[1]

			if agents.Get(agentName) == nil {
				ui.Errorf("Unknown agent: %s", agentName)
			}

			projectDir, err := os.Getwd()
			if err != nil {
				ui.Errorf("Failed to determine current directory: %v", err)
			}
			if err := plugins.Remove(projectDir, agentName, name); err != nil {
				ui.Errorf("Failed to remove plugin: %v", err)
			}
			ui.Successf("Removed plugin '%s' for %s from project '%s'", name, agentName, projectDir)
		},
	}
}

// mustPluginAgent validates an agent name and exits with a helpful message
// when it is unknown or has no plugins.
func mustPluginAgent(agentName string) string {
	if agents.Get(agentName) == nil {
		ui.Errorf("Unknown agent: %s. Known agents: %s", agentName, strings.Join(sorted(agents.Names()), ", "))
	}
	if len(plugins.For(agentName)) == 0 {
		ui.Errorf("No plugins available for %s. Agents with plugins: %s", agentName, strings.Join(plugins.Agents(), ", "))
	}
	return agentName
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// pluginWorkspaceTarget resolves the workspace whose plugin configuration a
// command edits, returning a pointer into the loaded config.
func pluginWorkspaceTarget() (*config.Config, *config.Workspace) {
	cfg := config.LoadOrDefault()
	projectDir, err := os.Getwd()
	if err != nil {
		projectDir = ""
	}
	active, err := profile.ResolveActiveWorkspace(cfg, projectDir, pluginWorkspace)
	if err != nil {
		ui.Errorf("%v", err)
	}
	if active == nil {
		ui.Error("No workspace configured. Run `exitbox setup` first.")
	}
	ws := cfg.Workspace(active.Workspace.Name)
	if ws == nil {
		ui.Errorf("Workspace '%s' not found in config", active.Workspace.Name)
	}
	return cfg, ws
}

func runPluginsList(agentName string) {
	_, ws := pluginWorkspaceTarget()
	available := plugins.For(agentName)

	ui.Cecho(fmt.Sprintf("Plugins for %s (workspace: %s):", agentName, ws.Name), ui.Cyan)
	fmt.Println()
	fmt.Printf("  %-16s %-10s %-12s %s\n", "PLUGIN", "STATUS", "VERSION", "DESCRIPTION")
	fmt.Printf("  %-16s %-10s %-12s %s\n", "------", "------", "-------", "-----------")
	for _, p := range available {
		entry, _ := ws.PluginEntry(agentName, p.Name)
		status := "disabled"
		if entry.Enabled {
			status = "enabled"
		}
		fmt.Printf("  %-16s %-10s %-12s %s\n", p.Name, status, p.VersionOrDefault(entry.Version), p.Description)
	}
	fmt.Println()
	fmt.Printf("Configure one with: exitbox plugins edit %s <plugin>\n", agentName)
}

func runPluginsShow(agentName, pluginName string) {
	_, ws := pluginWorkspaceTarget()
	p, err := plugins.Lookup(agentName, pluginName)
	if err != nil {
		ui.Errorf("%v", err)
	}

	entry, _ := ws.PluginEntry(agentName, p.Name)
	settings := plugins.Settings(ws, agentName, p)

	ui.Cecho(fmt.Sprintf("%s (%s, workspace: %s)", p.Display, agentName, ws.Name), ui.Cyan)
	fmt.Println()
	fmt.Printf("  %-22s %s\n", "Status:", enabledLabel(entry.Enabled))
	fmt.Printf("  %-22s %s\n", "Version:", p.VersionOrDefault(entry.Version))
	fmt.Printf("  %-22s %s\n", "Upstream:", p.Repo)
	fmt.Println()
	for _, s := range p.Settings {
		if !s.Visible(settings) {
			continue
		}
		fmt.Printf("  %-22s %s\n", s.Key+":", settingDisplay(s, settings[s.Key]))
	}
	if domains := plugins.Domains(ws, agentName); len(domains) > 0 && entry.Enabled {
		fmt.Println()
		fmt.Printf("  %-22s %s\n", "Allowed domains:", strings.Join(domains, ", "))
	}
}

func runPluginsSetEnabled(agentName, pluginName string, enabled bool) {
	cfg, ws := pluginWorkspaceTarget()
	p, err := plugins.Lookup(agentName, pluginName)
	if err != nil {
		ui.Errorf("%v", err)
	}

	entry, existed := ws.PluginEntry(agentName, p.Name)
	if entry.Enabled == enabled && existed {
		ui.Infof("%s is already %s for %s in workspace '%s'", p.Name, enabledLabel(enabled), agentName, ws.Name)
		return
	}
	if enabled {
		if err := plugins.Conflict(agentName, p.Name, ws.EnabledPlugins(agentName)); err != nil {
			ui.Errorf("%v", err)
		}
	}
	if !existed && enabled {
		entry.Settings = p.DefaultSettings()
	}
	entry.Enabled = enabled
	ws.SetPluginEntry(agentName, p.Name, entry)

	if err := config.SaveConfig(cfg); err != nil {
		ui.Errorf("Failed to save config: %v", err)
	}
	ui.Successf("%s %s for %s in workspace '%s'", p.Name, enabledLabel(enabled), agentName, ws.Name)
	fmt.Printf("Apply it with: exitbox rebuild %s (or the next `exitbox run %s`)\n", agentName, agentName)
	if enabled {
		fmt.Printf("Configure it with: exitbox plugins edit %s %s\n", agentName, p.Name)
	}
}

func runPluginsSetSetting(agentName, pluginName, key, value string) {
	cfg, ws := pluginWorkspaceTarget()
	p, err := plugins.Lookup(agentName, pluginName)
	if err != nil {
		ui.Errorf("%v", err)
	}
	setting := p.Setting(key)
	if setting == nil {
		ui.Errorf("Plugin %s has no setting %q. Available settings: %s", p.Name, key, strings.Join(settingKeys(p), ", "))
	}

	normalized := setting.Normalize(value)
	if err := setting.Validate(normalized); err != nil {
		ui.Errorf("%v", err)
	}
	ws.SetPluginSetting(agentName, p.Name, key, normalized)

	if err := config.SaveConfig(cfg); err != nil {
		ui.Errorf("Failed to save config: %v", err)
	}
	if normalized == "" {
		ui.Successf("Cleared %s.%s in workspace '%s'", p.Name, key, ws.Name)
		return
	}
	ui.Successf("Set %s.%s = %s in workspace '%s'", p.Name, key, settingDisplay(*setting, normalized), ws.Name)
	for _, vaultKey := range p.SecretKeys(map[string]string{key: normalized}) {
		fmt.Printf("Store the secret with: exitbox vault set %s <value> -w %s\n", vaultKey, ws.Name)
	}
}

func runPluginsEdit(agentName, pluginName string) {
	cfg, ws := pluginWorkspaceTarget()
	p, err := plugins.Lookup(agentName, pluginName)
	if err != nil {
		ui.Errorf("%v", err)
	}

	entry, _ := ws.PluginEntry(agentName, p.Name)
	result, err := wizard.RunPluginForm(agentName, p, entry)
	if err != nil {
		ui.Errorf("Failed to open the configuration menu: %v", err)
	}
	if !result.Saved {
		ui.Info("No changes saved.")
		return
	}

	wasEnabled := entry.Enabled
	if result.Enabled && !wasEnabled {
		if err := plugins.Conflict(agentName, p.Name, ws.EnabledPlugins(agentName)); err != nil {
			ui.Errorf("%v", err)
		}
	}
	entry.Enabled = result.Enabled
	entry.Settings = prunedSettings(p, result.Settings)
	ws.SetPluginEntry(agentName, p.Name, entry)

	if err := config.SaveConfig(cfg); err != nil {
		ui.Errorf("Failed to save config: %v", err)
	}
	ui.Successf("Saved %s settings for %s in workspace '%s'", p.Name, agentName, ws.Name)

	for _, vaultKey := range p.SecretKeys(entry.Settings) {
		fmt.Printf("Secret expected in the vault: %s (exitbox vault set %s <value> -w %s)\n", vaultKey, vaultKey, ws.Name)
	}
	if wasEnabled != entry.Enabled {
		fmt.Printf("Apply it with: exitbox rebuild %s (or the next `exitbox run %s`)\n", agentName, agentName)
	}
}

// prunedSettings drops values that match the plugin's defaults or no longer
// apply, keeping config.yaml free of noise.
func prunedSettings(p *plugin.Plugin, settings map[string]string) map[string]string {
	defaults := p.DefaultSettings()
	out := map[string]string{}
	for _, s := range p.Settings {
		value := settings[s.Key]
		if value == "" || value == defaults[s.Key] {
			continue
		}
		if !s.Visible(settings) {
			continue
		}
		out[s.Key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func settingKeys(p *plugin.Plugin) []string {
	keys := make([]string, 0, len(p.Settings))
	for _, s := range p.Settings {
		keys = append(keys, s.Key)
	}
	return keys
}

// settingDisplay renders a stored value for terminal output. A secret shows
// its vault key name; the secret value itself never reaches config.
func settingDisplay(s plugin.Setting, value string) string {
	if value == "" {
		return "(unset)"
	}
	if s.Type == plugin.TypeSecret {
		if key, ok := plugin.VaultKey(value); ok {
			return "vault:" + key
		}
	}
	return value
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func init() {
	rootCmd.AddCommand(newPluginsCmd())
}
