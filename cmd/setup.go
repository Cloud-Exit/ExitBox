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

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/internal/vault"
	"github.com/cloud-exit/exitbox/internal/wizard"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run the setup wizard",
	Long:  "Interactive setup wizard to configure roles, languages, tools, and agents.",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSetup()
	},
}

func runSetup() error {
	// Non-interactive terminal: fall back to defaults
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		ui.Warn("Non-interactive terminal detected. Writing default configuration.")
		if err := config.WriteDefaults(); err != nil {
			return fmt.Errorf("writing defaults: %w", err)
		}
		ui.Success("Default configuration written. Run 'exitbox setup' interactively to customize.")
		return nil
	}

	// Load existing config to pre-populate wizard, or nil for fresh start
	var existingCfg *config.Config
	if config.ConfigExists() {
		existingCfg = config.LoadOrDefault()
	}

	result, err := wizard.Run(existingCfg)
	if err != nil {
		if err.Error() == "setup cancelled" {
			if existingCfg != nil {
				ui.Info("Setup cancelled. Existing configuration unchanged.")
			} else {
				ui.Info("Setup cancelled. Writing default configuration.")
				if err := config.WriteDefaults(); err != nil {
					return fmt.Errorf("writing defaults: %w", err)
				}
			}
			ui.Info("Run 'exitbox setup' to configure later.")
			return nil
		}
		return err
	}

	// Handle credential import/copy from wizard selection.
	if result.CopyFrom != "" && result.WorkspaceName != "" {
		handleCredentialSetup(result.WorkspaceName, result.CopyFrom)
	}

	// Initialize vault if enabled during setup.
	if result.VaultEnabled && result.VaultPassword != "" && result.WorkspaceName != "" {
		if !vault.IsInitialized(result.WorkspaceName) {
			if err := vault.Init(result.WorkspaceName, result.VaultPassword); err != nil {
				ui.Warnf("Failed to initialize vault: %v", err)
			} else {
				ui.Successf("Vault initialized for workspace '%s'", result.WorkspaceName)
			}
		}
	}

	ui.Success("ExitBox configured!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Navigate to your project:  cd /path/to/project")

	// Build the run command reflecting the wizard choices.
	agentName := "claude"
	if len(result.Agents) > 0 {
		agentName = result.Agents[0]
	}
	runCmd := fmt.Sprintf("exitbox run %s", agentName)
	if !result.IsDefault && result.WorkspaceName != "" {
		runCmd += fmt.Sprintf(" --workspace %s", result.WorkspaceName)
	}
	fmt.Printf("  2. Run an agent:              %s\n", runCmd)
	fmt.Println()
	return nil
}

// handleCredentialSetup imports or copies credentials into a workspace.
// copyFrom is either a workspace name, "__host__" (import from host), or "" (skip).
func handleCredentialSetup(workspaceName, copyFrom string) {
	if copyFrom == "__host__" {
		// Import from host config.
		for _, name := range agent.AgentNames {
			a := agent.Get(name)
			if a == nil {
				continue
			}
			src, err := a.DetectHostConfig()
			if err != nil {
				continue
			}
			dst := profile.WorkspaceAgentDir(workspaceName, name)
			if err := os.MkdirAll(dst, 0755); err != nil {
				ui.Warnf("Failed to create workspace dir for %s: %v", name, err)
				continue
			}
			if err := a.ImportConfig(src, dst); err != nil {
				ui.Warnf("Failed to import %s config: %v", name, err)
				continue
			}
			ui.Successf("Imported %s credentials from host", name)
		}
	} else {
		// Copy from another workspace.
		if err := profile.CopyWorkspaceCredentials(copyFrom, workspaceName, agent.AgentNames); err != nil {
			ui.Warnf("Failed to copy credentials from '%s': %v", copyFrom, err)
		} else {
			ui.Successf("Copied credentials from workspace '%s'", copyFrom)
		}
	}
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
