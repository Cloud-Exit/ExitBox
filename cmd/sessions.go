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
	"github.com/cloud-exit/exitbox/internal/session"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage resumable sessions",
		Long:  "List and remove named resumable sessions for the current project.",
	}
	cmd.AddCommand(newSessionsListCmd())
	cmd.AddCommand(newSessionsRemoveCmd())
	return cmd
}

func newSessionsListCmd() *cobra.Command {
	var workspaceOverride string
	var agentFilter string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved named sessions",
		Run: func(cmd *cobra.Command, args []string) {
			projectDir, _ := os.Getwd()
			cfg := config.LoadOrDefault()

			workspaceName, err := resolveSessionsWorkspace(cfg, projectDir, workspaceOverride)
			if err != nil {
				ui.Errorf("%v", err)
			}
			agents, err := resolveSessionAgents(agentFilter)
			if err != nil {
				ui.Errorf("%v", err)
			}

			fmt.Println()
			fmt.Printf("Workspace: %s\n", workspaceName)
			fmt.Printf("Project:   %s\n", filepath.Base(projectDir))
			fmt.Println()

			total := 0
			for _, a := range agents {
				names, err := session.ListNames(workspaceName, a, projectDir)
				if err != nil {
					ui.Errorf("failed to list sessions for %s: %v", a, err)
				}
				if len(names) == 0 {
					continue
				}
				if len(agents) > 1 {
					fmt.Printf("%s:\n", a)
				}
				for _, name := range names {
					fmt.Printf("  - %s\n", name)
					total++
				}
				if len(agents) > 1 {
					fmt.Println()
				}
			}

			if total == 0 {
				fmt.Println("No saved sessions found.")
			}
		},
	}

	cmd.Flags().StringVarP(&workspaceOverride, "workspace", "w", "", "Workspace to inspect (defaults to resolved active workspace)")
	cmd.Flags().StringVar(&agentFilter, "agent", "all", "Agent filter: claude|codex|opencode|all")
	_ = cmd.RegisterFlagCompletionFunc("workspace", completeWorkspaceFlagValues)
	_ = cmd.RegisterFlagCompletionFunc("agent", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		candidates := []string{"all", "claude", "codex", "opencode"}
		var out []string
		for _, c := range candidates {
			if strings.HasPrefix(c, toComplete) {
				out = append(out, c)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func newSessionsRemoveCmd() *cobra.Command {
	var workspaceOverride string
	var agentFilter string

	cmd := &cobra.Command{
		Use:   "rm <session>",
		Short: "Remove a saved named session",
		Args:  cobra.ExactArgs(1),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			workspaceOverride, _ := cmd.Flags().GetString("workspace")
			agentFilter, _ := cmd.Flags().GetString("agent")
			agents, err := resolveSessionAgents(agentFilter)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			return completeSessionNamesForProject(workspaceOverride, agents, toComplete)
		},
		Run: func(cmd *cobra.Command, args []string) {
			projectDir, _ := os.Getwd()
			cfg := config.LoadOrDefault()
			sessionName := strings.TrimSpace(args[0])
			if sessionName == "" {
				ui.Errorf("session name cannot be empty")
			}

			workspaceName, err := resolveSessionsWorkspace(cfg, projectDir, workspaceOverride)
			if err != nil {
				ui.Errorf("%v", err)
			}
			agents, err := resolveSessionAgents(agentFilter)
			if err != nil {
				ui.Errorf("%v", err)
			}

			removedAny := false
			for _, a := range agents {
				removed, err := session.RemoveByName(workspaceName, a, projectDir, sessionName)
				if err != nil {
					ui.Errorf("failed to remove session for %s: %v", a, err)
				}
				if removed {
					removedAny = true
					ui.Successf("Removed session '%s' (%s, workspace %s)", sessionName, a, workspaceName)
				}
			}

			if !removedAny {
				if agentFilter == "" {
					agentFilter = "all"
				}
				ui.Errorf("Session '%s' not found in workspace '%s' (agent=%s)", sessionName, workspaceName, agentFilter)
			}
		},
	}

	cmd.Flags().StringVarP(&workspaceOverride, "workspace", "w", "", "Workspace to modify (defaults to resolved active workspace)")
	cmd.Flags().StringVar(&agentFilter, "agent", "all", "Agent filter: claude|codex|opencode|all")
	_ = cmd.RegisterFlagCompletionFunc("workspace", completeWorkspaceFlagValues)
	_ = cmd.RegisterFlagCompletionFunc("agent", func(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		candidates := []string{"all", "claude", "codex", "opencode"}
		var out []string
		for _, c := range candidates {
			if strings.HasPrefix(c, toComplete) {
				out = append(out, c)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	})
	return cmd
}

func resolveSessionsWorkspace(cfg *config.Config, projectDir, workspaceOverride string) (string, error) {
	if workspaceOverride != "" {
		w := profile.FindWorkspace(cfg, workspaceOverride)
		if w == nil {
			return "", fmt.Errorf("unknown workspace '%s'. Available: %s", workspaceOverride, strings.Join(profile.WorkspaceNames(cfg), ", "))
		}
		return w.Name, nil
	}

	active, err := profile.ResolveActiveWorkspace(cfg, projectDir, "")
	if err != nil {
		return "", err
	}
	if active == nil {
		return "", fmt.Errorf("no active workspace resolved")
	}
	return active.Workspace.Name, nil
}

func resolveSessionAgents(filter string) ([]string, error) {
	filter = strings.TrimSpace(filter)
	if filter == "" || filter == "all" {
		return agent.AgentNames, nil
	}
	if !agent.IsValidAgent(filter) {
		return nil, fmt.Errorf("unknown agent '%s'. Expected one of: claude, codex, opencode, all", filter)
	}
	return []string{filter}, nil
}

func init() {
	rootCmd.AddCommand(newSessionsCmd())
}
