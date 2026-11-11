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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/subagents"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage per-workspace agent instructions, master skills and subagents",
		Long: "Manage what the agents of a workspace are told to do.\n\n" +
			"agents.md (edit, show, path)\n" +
			"  Each workspace has its own agents.md file. On container start, ExitBox\n" +
			"  combines your agents.md with its sandbox instructions into the agent's\n" +
			"  instruction file (e.g. ~/.claude/CLAUDE.md).\n" +
			"  Your agents.md is never modified by ExitBox. It is fully user-owned.\n\n" +
			"Master skill (skill)\n" +
			"  One SKILL.md per coding agent that tells it which tasks to hand to which\n" +
			"  subagent or third-party agent. Edit it to set your own routing rules.\n\n" +
			"Subagents (subagents)\n" +
			"  The subagents of each coding agent that tasks can be handed to. Same\n" +
			"  commands as 'exitbox subagents'.",
	}

	cmd.AddCommand(newAgentsEditCmd())
	cmd.AddCommand(newAgentsShowCmd())
	cmd.AddCommand(newAgentsPathCmd())
	cmd.AddCommand(newAgentsSkillCmd())
	cmd.AddCommand(newSubagentsCmd())
	return cmd
}

func newAgentsSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Manage a coding agent's master skill (task routing rules)",
		Long: `Manage the master skill of a coding agent in a workspace: a SKILL.md that
tells the agent which tasks to hand to which subagent or third-party agent.

'edit' creates it on first use, with one route for every subagent stored for
that agent. After that the file is yours; ExitBox never rewrites it.

Each agent has its own master skill, because each has its own subagents.
Pick the agent with --agent (` + strings.Join(agentNames(hasSkills), ", ") + `); you can leave it
out when only one enabled agent can have one.

On container start the skill is linked into the agent's skill directory as
'` + subagents.MasterSkillName + `'. It lives in the workspace's subagent store, which is
mounted read-only, so a session cannot change the rules the next one follows.`,
		SilenceUsage: true,
	}
	cmd.AddCommand(
		newAgentsSkillEditCmd(),
		newAgentsSkillShowCmd(),
		newAgentsSkillPathCmd(),
		newAgentsSkillRmCmd(),
		newAgentsSkillReviewerCmd(),
	)
	for _, c := range cmd.Commands() {
		c.SilenceUsage = true
	}
	return cmd
}

// skillTarget resolves the workspace and coding agent of a skill command.
func skillTarget(workspace, agent string) (string, *subagents.Harness, error) {
	ws, err := resolveSubagentsWorkspace(workspace)
	if err != nil {
		return "", nil, err
	}
	h, err := pickHarness(agent, hasSkills)
	return ws, h, err
}

func newAgentsSkillEditCmd() *cobra.Command {
	var workspace, agent string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit the master skill in $EDITOR and validate it",
		Long: `Open the agent's master skill in $EDITOR (default vi) and validate it when
the editor exits. Creates the skill first when the agent has none.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, h, err := skillTarget(workspace, agent)
			if err != nil {
				return err
			}
			path, created, err := h.EnsureMasterSkill(ws)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if created {
				_, _ = fmt.Fprintf(out, "Created %s\n", path)
			}
			if err := runEditor(path); err != nil {
				return fmt.Errorf("editor exited with error: %w", err)
			}
			fs, err := h.MasterSkillFindings(ws)
			if err != nil {
				return err
			}
			printFindings(cmd.ErrOrStderr(), "  ", fs)
			if subagents.HasErrors(fs) {
				return fmt.Errorf("the master skill has errors; %s will skip it. Fix with: exitbox agents skill edit --agent %s -w %s", h.Label, h.Agent, ws)
			}
			_, _ = fmt.Fprintln(out, nextStartNote)
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSkills)
	return cmd
}

func newAgentsSkillShowCmd() *cobra.Command {
	var workspace, agent string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Print the master skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, h, err := skillTarget(workspace, agent)
			if err != nil {
				return err
			}
			data, err := h.ReadMasterSkill(ws)
			if errors.Is(err, subagents.ErrNotFound) {
				_, _ = fmt.Fprintf(cmd.OutOrStdout(), "No %s master skill in workspace '%s'.\nCreate one with: exitbox agents skill edit --agent %s -w %s\n",
					h.Label, ws, h.Agent, ws)
				return nil
			}
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSkills)
	return cmd
}

func newAgentsSkillReviewerCmd() *cobra.Command {
	var workspace, agent string
	cmd := &cobra.Command{
		Use:   "reviewer [subagent|main]",
		Short: "Show or set who reviews delegated work",
		Long: `Show or set the Reviewer line of the master skill.

By default the main agent reviews every result its subagents return. Name a
stored subagent to hand reviews to it instead, or 'main' to take them back.
Only the Reviewer line changes; the rest of the file stays as you wrote it.`,
		Args:              cobra.MaximumNArgs(1),
		ValidArgsFunction: completeSubagentNames,
		RunE: func(cmd *cobra.Command, args []string) error {
			ws, h, err := skillTarget(workspace, agent)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(args) == 0 {
				r, err := h.MasterSkillReviewer(ws)
				if errors.Is(err, subagents.ErrNotFound) {
					return fmt.Errorf("no %s master skill in workspace '%s'; create one with: exitbox agents skill edit --agent %s -w %s", h.Label, ws, h.Agent, ws)
				}
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(out, r)
				return nil
			}
			if err := h.SetMasterSkillReviewer(ws, args[0]); err != nil {
				if errors.Is(err, subagents.ErrNoReviewerLine) {
					return fmt.Errorf("%w; add a line \"Reviewer: main agent\" with: exitbox agents skill edit --agent %s -w %s", err, h.Agent, ws)
				}
				return err
			}
			r, err := h.MasterSkillReviewer(ws)
			if err != nil {
				return err
			}
			if r == subagents.ReviewerSelf {
				_, _ = fmt.Fprintf(out, "The main agent reviews delegated work. %s\n", nextStartNote)
			} else {
				_, _ = fmt.Fprintf(out, "%s reviews delegated work. %s\n", r, nextStartNote)
			}
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSkills)
	return cmd
}

func newAgentsSkillPathCmd() *cobra.Command {
	var workspace, agent string
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print the path to the master skill",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, h, err := skillTarget(workspace, agent)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), h.MasterSkillPath(ws))
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSkills)
	return cmd
}

func newAgentsSkillRmCmd() *cobra.Command {
	var workspace, agent string
	cmd := &cobra.Command{
		Use:     "rm",
		Aliases: []string{"remove"},
		Short:   "Remove the master skill",
		Long:    "Remove the agent's master skill. Its link in the container is dropped at the next start.",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ws, h, err := skillTarget(workspace, agent)
			if err != nil {
				return err
			}
			if err := h.RemoveMasterSkill(ws); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed the %s master skill from workspace '%s'.\n%s\n", h.Label, ws, nextStartNote)
			return nil
		},
	}
	addWorkspaceFlag(cmd, &workspace)
	addAgentFlag(cmd, &agent, hasSkills)
	return cmd
}

func newAgentsEditCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit the agents.md file for a workspace",
		Long:  "Opens the workspace's agents.md in your $EDITOR (or vi).",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveAgentsWorkspace(workspace)
			p := agentsMdPath(ws)

			// Create the file with a helpful header if it doesn't exist.
			if _, err := os.Stat(p); os.IsNotExist(err) {
				if mkErr := os.MkdirAll(filepath.Dir(p), 0755); mkErr != nil {
					ui.Errorf("Failed to create directory: %v", mkErr)
				}
				header := fmt.Sprintf("# Agent Instructions — workspace: %s\n\n"+
					"# Add your custom instructions here. They will be included in\n"+
					"# the agent's instruction file (e.g. ~/.claude/CLAUDE.md) on\n"+
					"# every container start, before ExitBox's sandbox rules.\n\n", ws)
				if wErr := os.WriteFile(p, []byte(header), 0644); wErr != nil {
					ui.Errorf("Failed to create agents.md: %v", wErr)
				}
			}

			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}
			c := exec.Command(editor, p)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				ui.Errorf("Editor exited with error: %v", err)
			}
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newAgentsShowCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show the agents.md file for a workspace",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveAgentsWorkspace(workspace)
			p := agentsMdPath(ws)

			data, err := os.ReadFile(p)
			if os.IsNotExist(err) {
				ui.Infof("No agents.md for workspace '%s'.", ws)
				fmt.Printf("Create one with: exitbox agents edit -w %s\n", ws)
				return
			}
			if err != nil {
				ui.Errorf("Failed to read agents.md: %v", err)
			}
			fmt.Print(string(data))
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newAgentsPathCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "path",
		Short: "Print the path to the agents.md file",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveAgentsWorkspace(workspace)
			fmt.Println(agentsMdPath(ws))
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

// agentsMdPath returns the host-side path to the workspace's agents.md.
// The file is stored at the workspace level (not per-agent) so the same
// instructions apply to all agents in the workspace.
func agentsMdPath(workspaceName string) string {
	return filepath.Join(config.Home, "profiles", "global", workspaceName, "agents.md")
}

func resolveAgentsWorkspace(flag string) string {
	if flag != "" {
		return flag
	}
	cfg := config.LoadOrDefault()
	projectDir, _ := os.Getwd()
	active, err := profile.ResolveActiveWorkspace(cfg, projectDir, "")
	if err == nil && active != nil {
		return active.Workspace.Name
	}
	if cfg.Settings.DefaultWorkspace != "" {
		return cfg.Settings.DefaultWorkspace
	}
	ui.Error("No workspace specified. Use -w <workspace> or set a default workspace.")
	return "" // unreachable (ui.Error exits)
}

func init() {
	rootCmd.AddCommand(newAgentsCmd())
}
