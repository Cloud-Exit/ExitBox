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

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/skills"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage agent skills",
		Long: `Install, list, and remove skills for AI coding agents.

Skills are SKILL.md files that provide specialized capabilities.
Installed skills are shared across skill-capable agents (Claude, Codex,
OpenCode, Pi, Kimi Code) in a workspace via automatic symlinking at
container start.`,
	}

	cmd.AddCommand(newSkillsInstallCmd())
	cmd.AddCommand(newSkillsListCmd())
	cmd.AddCommand(newSkillsRemoveCmd())
	return cmd
}

func newSkillsInstallCmd() *cobra.Command {
	var workspace string
	var name string

	cmd := &cobra.Command{
		Use:   "install <source>",
		Short: "Install a skill from a URL, GitHub repo, or local path",
		Long: `Install a skill into the active workspace.

Supports multiple source types:
  - GitHub directory:  https://github.com/anthropics/skills/tree/main/skills/frontend-design
  - Raw URL:           https://example.com/path/to/SKILL.md
  - Local directory:   /path/to/my-skill/
  - Local file:        /path/to/SKILL.md

The skill name is derived from the SKILL.md frontmatter or the source path.
Use --name to override.

Examples:
  exitbox skills install https://github.com/anthropics/skills/tree/main/skills/frontend-design
  exitbox skills install ./my-skill
  exitbox skills install https://example.com/SKILL.md --name custom-name
  exitbox skills install ./deploy-skill -w work`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			source := args[0]

			cfg := resolveConfigForSkills(workspace)
			ws := cfg

			ui.Infof("Fetching skill from %s...", source)
			result, err := skills.Fetch(source)
			if err != nil {
				ui.Errorf("Failed to fetch skill: %v", err)
			}

			skillName := result.Name
			if name != "" {
				skillName = name
			}

			if skills.Exists(ws, skillName) {
				ui.Warnf("Skill '%s' already exists, overwriting", skillName)
			}

			if err := skills.Install(ws, skillName, result.Files); err != nil {
				ui.Errorf("Failed to install skill: %v", err)
			}

			fileCount := len(result.Files)
			ui.Successf("Installed skill '%s' (%d file(s)) into workspace '%s'", skillName, fileCount, ws)
		},
	}

	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Target workspace (default: active)")
	cmd.Flags().StringVar(&name, "name", "", "Override skill name")
	return cmd
}

func newSkillsListCmd() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveConfigForSkills(workspace)

			installed, err := skills.List(ws)
			if err != nil {
				ui.Errorf("Failed to list skills: %v", err)
			}

			if len(installed) == 0 {
				ui.Info("No skills installed.")
				return
			}

			for _, s := range installed {
				if s.Description != "" {
					fmt.Printf("  %s — %s\n", s.Name, s.Description)
				} else {
					fmt.Printf("  %s\n", s.Name)
				}
			}
		},
	}

	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Target workspace (default: active)")
	return cmd
}

func newSkillsRemoveCmd() *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an installed skill",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			ws := resolveConfigForSkills(workspace)

			if !skills.Exists(ws, name) {
				ui.Errorf("Skill '%s' not found in workspace '%s'", name, ws)
			}

			if err := skills.Remove(ws, name); err != nil {
				ui.Errorf("Failed to remove skill: %v", err)
			}

			ui.Successf("Removed skill '%s' from workspace '%s'", name, ws)
		},
	}

	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Target workspace (default: active)")
	return cmd
}

// resolveConfigForSkills resolves the workspace name for skills commands.
func resolveConfigForSkills(override string) string {
	cfg := config.LoadOrDefault()
	return resolveConfigWorkspace(cfg, override)
}

func init() {
	rootCmd.AddCommand(newSkillsCmd())
}
