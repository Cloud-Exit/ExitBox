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
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/internal/wizard"
	"github.com/spf13/cobra"
)

// formatWorkspaceEntry formats a single workspace line for display.
func formatWorkspaceEntry(w config.Workspace, isDefault bool) string {
	marker := " "
	if isDefault {
		marker = "*"
	}
	dev := ""
	if len(w.Development) > 0 {
		dev = strings.Join(w.Development, ", ")
	}
	dirInfo := ""
	if w.Directory != "" {
		dirInfo = fmt.Sprintf(" [dir: %s]", w.Directory)
	}
	if dev != "" {
		return fmt.Sprintf("  %s %-15s %s%s", marker, w.Name, dev, dirInfo)
	}
	return fmt.Sprintf("  %s %-15s%s", marker, w.Name, dirInfo)
}

func newWorkspacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspaces",
		Short: "Manage workspaces",
		Long: "Workspaces are named contexts (e.g. personal/work) with:\n" +
			"- development stack (go/python/node/etc)\n" +
			"- separate claude/codex/opencode config storage\n" +
			"- optional directory scoping (--dir)",
	}

	cmd.AddCommand(newWorkspacesListCmd())
	cmd.AddCommand(newWorkspacesStatusCmd())
	cmd.AddCommand(newWorkspacesAddCmd())
	cmd.AddCommand(newWorkspacesRemoveCmd())
	cmd.AddCommand(newWorkspacesUseCmd())
	cmd.AddCommand(newWorkspacesDefaultCmd())
	return cmd
}

func newWorkspacesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all workspaces",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.LoadOrDefault()

			fmt.Println()
			ui.Cecho("Workspaces", ui.Cyan)
			fmt.Println()
			if len(cfg.Workspaces.Items) == 0 {
				fmt.Println("  No workspaces configured. Run 'exitbox workspaces add <name>' to create one.")
			} else {
				for _, w := range cfg.Workspaces.Items {
					fmt.Println(formatWorkspaceEntry(w, strings.EqualFold(cfg.Settings.DefaultWorkspace, w.Name)))
				}
				fmt.Println()
				fmt.Println("  * = default workspace")
			}
			fmt.Println()
		},
	}
}

func newWorkspacesStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show workspace resolution chain",
		Run: func(cmd *cobra.Command, args []string) {
			projectDir, _ := os.Getwd()
			cfg := config.LoadOrDefault()
			active, err := profile.ResolveActiveWorkspace(cfg, projectDir, "")
			if err != nil {
				ui.Errorf("%v", err)
			}

			fmt.Println()
			fmt.Printf("Default workspace: %s\n", emptyAsNone(cfg.Settings.DefaultWorkspace))
			fmt.Printf("Active workspace:  %s\n", emptyAsNone(cfg.Workspaces.Active))
			fmt.Printf("Current directory:  %s\n", projectDir)
			if active == nil {
				fmt.Println("Resolved:          none")
			} else {
				fmt.Printf("Resolved:          %s/%s\n", active.Scope, active.Workspace.Name)
				if len(active.Workspace.Development) > 0 {
					fmt.Printf("Development:       %s\n", strings.Join(active.Workspace.Development, ", "))
				} else {
					fmt.Println("Development:       none")
				}
				if active.Workspace.Directory != "" {
					fmt.Printf("Scoped to dir:     %s\n", active.Workspace.Directory)
				}
			}
			fmt.Println()
			fmt.Println("Use Ctrl+Alt+P for the workspace menu and Ctrl+Alt+S for the session menu inside an ExitBox session.")
		},
	}
}

func newWorkspacesAddCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a workspace using setup wizard",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.LoadOrDefault()

			ui.Info("Starting workspace setup wizard...")
			result, err := wizard.RunWorkspaceCreation(cfg, args[0])
			if err != nil {
				ui.Errorf("%v", err)
			}

			w := result.Workspace
			if dir != "" {
				absDir, err := filepath.Abs(dir)
				if err != nil {
					ui.Errorf("invalid directory: %v", err)
				}
				w.Directory = absDir
			}

			if err := profile.AddWorkspace(*w, cfg); err != nil {
				ui.Errorf("%v", err)
			}

			if result.MakeDefault {
				if err := profile.SetActiveWorkspace(w.Name, cfg); err != nil {
					ui.Errorf("%v", err)
				}
			}

			// Handle credential import/copy.
			if result.CopyFrom != "" {
				handleCredentialSetup(w.Name, result.CopyFrom)
			} else {
				// Seed from host config if no copy source.
				for _, a := range agent.AgentNames {
					if !cfg.IsAgentEnabled(a) {
						continue
					}
					if err := profile.EnsureAgentConfig(w.Name, a); err != nil {
						ui.Warnf("Could not seed %s config for workspace '%s': %v", a, w.Name, err)
					}
				}
			}

			scope := "global"
			if w.Directory != "" {
				scope = "directory-scoped"
			}
			ui.Successf("Created %s workspace '%s'", scope, w.Name)
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "Lock workspace to a specific directory")
	return cmd
}

func newWorkspacesRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a workspace",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.LoadOrDefault()
			if err := profile.RemoveWorkspace(args[0], cfg); err != nil {
				ui.Errorf("%v", err)
			}
			ui.Successf("Removed workspace '%s'", args[0])
		},
	}
}

func newWorkspacesUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <name>",
		Short: "Set active workspace",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.LoadOrDefault()
			if err := profile.SetActiveWorkspace(args[0], cfg); err != nil {
				ui.Errorf("%v", err)
			}
			ui.Successf("Active workspace set to '%s'", args[0])
		},
	}
}

func newWorkspacesDefaultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "default [name]",
		Short: "Get or set default workspace",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			cfg := config.LoadOrDefault()

			if len(args) == 0 {
				fmt.Println(emptyAsNone(cfg.Settings.DefaultWorkspace))
				return
			}

			name := args[0]
			w := profile.FindWorkspace(cfg, name)
			if w == nil {
				ui.Errorf("unknown workspace: %s. Available: %s", name, strings.Join(profile.WorkspaceNames(cfg), ", "))
			}

			cfg.Settings.DefaultWorkspace = w.Name
			cfg.Workspaces.Active = w.Name
			if err := config.SaveConfig(cfg); err != nil {
				ui.Errorf("failed to save config: %v", err)
			}
			ui.Successf("Default workspace set to '%s'", name)
		},
	}
}

func emptyAsNone(v string) string {
	if strings.TrimSpace(v) == "" {
		return "none"
	}
	return v
}

func init() {
	rootCmd.AddCommand(newWorkspacesCmd())
}
