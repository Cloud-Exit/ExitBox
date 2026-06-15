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
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/generate"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/internal/vault"
	"github.com/spf13/cobra"
)

func newGenerateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate agent config for third-party LLM servers",
		Long: `Generate agent configuration files for third-party LLM servers.

Prompts for server details, tests connectivity via GET /v1/models,
and writes the correct config file into the workspace profile directory.

Examples:
  exitbox generate opencode              Configure OpenCode for a local server
  exitbox generate claude -w work        Configure Claude Code in 'work' workspace
  exitbox generate codex                 Configure Codex for a custom provider`,
	}

	for _, agt := range agents.All() {
		cmd.AddCommand(newGenerateAgentCmd(agt.Name(), agt.DisplayName()))
	}
	return cmd
}

func newGenerateAgentCmd(agentName, displayName string) *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   agentName,
		Short: fmt.Sprintf("Generate %s config for a third-party LLM server", displayName),
		Run: func(cmd *cobra.Command, args []string) {
			runGenerate(agentName, displayName, workspace)
		},
	}

	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Target workspace (default: active workspace)")
	return cmd
}

func runGenerate(agentName, displayName, workspaceFlag string) {
	// Resolve workspace.
	cfg := config.LoadOrDefault()
	workspaceName := resolveGenerateWorkspace(cfg, workspaceFlag)

	fmt.Printf("\nConfiguring %s for workspace '%s'\n\n", displayName, workspaceName)

	// Prompt for provider details.
	providerName := promptString("Provider name", "Local Server")
	baseURL := promptString("Base URL", "http://localhost:8080/v1")

	// Normalize: strip trailing /v1 for internal storage, we'll append /models for test.
	baseURL = strings.TrimRight(baseURL, "/")

	// Prompt for API key (optional).
	apiKey := promptPassword("API key (press Enter to skip): ")

	// Handle vault storage for API key.
	var vaultKeyName string
	if apiKey != "" {
		w := profile.FindWorkspace(cfg, workspaceName)
		if w != nil && w.Vault.Enabled && vault.IsInitialized(workspaceName) {
			if promptYesNo("Store API key in vault?", true) {
				vaultKeyName = promptString("Vault key name", strings.ToUpper(agentName)+"_API_KEY")
				password := promptPassword("Enter vault password: ")
				if err := vault.QuickSet(workspaceName, password, vaultKeyName, apiKey); err != nil {
					ui.Warnf("Failed to store in vault: %v", err)
					ui.Info("API key will be written to config file instead.")
					vaultKeyName = ""
				} else {
					ui.Successf("API key stored in vault as '%s'", vaultKeyName)
				}
			}
		}
	}

	// Test server connectivity (with retry loop).
	var models []generate.ModelInfo
	for {
		fmt.Printf("\nTesting connection to %s/models...\n", baseURL)
		var err error
		models, err = generate.TestServer(baseURL, apiKey)
		if err == nil {
			ui.Successf("Found %d model(s):", len(models))
			for i, m := range models {
				fmt.Printf("  %d) %s\n", i+1, m.ID)
			}
			fmt.Println()
			break
		}
		ui.Warnf("Server test failed: %v", err)
		switch promptRetry() {
		case "retry":
			continue
		case "continue":
			break
		default: // abort
			ui.Info("Aborted.")
			return
		}
		break
	}

	// Select model.
	var modelID string
	if len(models) > 0 {
		modelID = promptString("Model ID", models[0].ID)
	} else {
		modelID = promptString("Model ID", "")
	}
	if modelID == "" {
		ui.Error("Model ID is required")
	}

	modelName := promptString("Model display name", modelID)

	// Provider ID/slug.
	defaultSlug := slugify(providerName)
	providerID := promptString("Provider ID (slug)", defaultSlug)

	// Build server config.
	serverCfg := config.ServerConfig{
		ProviderID:   providerID,
		ProviderName: providerName,
		BaseURL:      baseURL,
		APIKey:       apiKey,
		ModelID:      modelID,
		ModelName:    modelName,
		VaultKeyName: vaultKeyName,
	}

	// OpenCode-specific: ask about compaction.
	if agentName == "opencode" {
		serverCfg.Compaction = promptYesNo("Enable auto compaction with pruning?", true)
	}

	// Generate agent-specific config.
	agt := agents.Get(agentName)
	if agt == nil {
		ui.Errorf("Unknown agent: %s", agentName)
		return
	}
	configData, _ := agt.GenerateConfig(serverCfg)

	// Ensure agent config directory exists.
	if err := profile.EnsureAgentConfig(workspaceName, agentName); err != nil {
		ui.Warnf("Failed to ensure agent config dir: %v", err)
	}

	// Write config.
	agentDir := profile.WorkspaceAgentDir(workspaceName, agentName)
	configPath := agt.ConfigFilePath(agentDir)
	if err := generate.WriteAgentConfig(agt, configPath, configData); err != nil {
		ui.Errorf("Failed to write config: %v", err)
	}

	// Summary.
	fmt.Println()
	ui.Successf("Config written to %s", configPath)
	if vaultKeyName != "" {
		ui.Infof("API key stored in vault as '%s' (not written to config file)", vaultKeyName)
	}
	fmt.Println()
}

// resolveGenerateWorkspace determines which workspace to target.
func resolveGenerateWorkspace(cfg *config.Config, override string) string {
	if override != "" {
		w := profile.FindWorkspace(cfg, override)
		if w == nil {
			available := profile.WorkspaceNames(cfg)
			if len(available) > 0 {
				ui.Errorf("Unknown workspace '%s'. Available: %s", override, strings.Join(available, ", "))
			} else {
				ui.Errorf("Unknown workspace '%s'. No workspaces configured. Run 'exitbox setup' first.", override)
			}
		}
		return w.Name
	}

	projectDir, _ := os.Getwd()
	active, _ := profile.ResolveActiveWorkspace(cfg, projectDir, "")
	if active != nil {
		return active.Workspace.Name
	}

	if len(cfg.Workspaces.Items) > 0 {
		return cfg.Workspaces.Items[0].Name
	}
	return "default"
}

// promptString prompts for a string value with a default.
func promptString(label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		ui.Errorf("Failed to read input: %v", err)
	}
	line = strings.TrimRight(line, "\n\r")
	if line == "" {
		return defaultVal
	}
	return line
}

// promptYesNo prompts for a yes/no answer.
func promptYesNo(label string, defaultYes bool) bool {
	suffix := " [y/N]: "
	if defaultYes {
		suffix = " [Y/n]: "
	}
	fmt.Print(label + suffix)

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		ui.Errorf("Failed to read input: %v", err)
	}
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}

// promptRetry asks the user to retry, continue, or abort.
// Returns "retry", "continue", or "abort".
func promptRetry() string {
	fmt.Print("[r]etry / [c]ontinue / [a]bort (default: r): ")

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		ui.Errorf("Failed to read input: %v", err)
	}
	line = strings.TrimSpace(strings.ToLower(line))
	switch line {
	case "", "r", "retry":
		return "retry"
	case "c", "continue":
		return "continue"
	default:
		return "abort"
	}
}

// slugify converts a display name to a URL-safe slug.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "provider"
	}
	return s
}

func init() {
	rootCmd.AddCommand(newGenerateCmd())
}
