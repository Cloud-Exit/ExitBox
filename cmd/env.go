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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/env"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

func newEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage environment variable profiles for a workspace",
		Long: `Environment variable profiles store named bundles of KEY=VALUE
pairs in the workspace KV store. Apply a profile at run time with
'exitbox run <agent> --profile <name>' to inject the variables via
container '-e' flags.

Example use case: route Claude Code through OpenRouter by saving
ANTHROPIC_BASE_URL, OPENROUTER_API_KEY, etc. in a profile.`,
	}

	cmd.AddCommand(newEnvCreateCmd())
	cmd.AddCommand(newEnvEditCmd())
	cmd.AddCommand(newEnvListCmd())
	cmd.AddCommand(newEnvShowCmd())
	cmd.AddCommand(newEnvDeleteCmd())
	cmd.AddCommand(newEnvDefaultCmd())
	return cmd
}

func newEnvCreateCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "create <profile>",
		Short: "Create a new env profile and open it in $EDITOR",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			ws := resolveEnvWorkspace(workspace)
			if env.Exists(ws, name) {
				ui.Errorf("Env profile '%s' already exists in workspace '%s'", name, ws)
			}
			editEnvProfile(ws, name, true)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name (default: active)")
	return cmd
}

func newEnvEditCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "edit <profile>",
		Short: "Edit an env profile in $EDITOR",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			ws := resolveEnvWorkspace(workspace)
			editEnvProfile(ws, name, false)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name (default: active)")
	return cmd
}

func newEnvListCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List env profiles in a workspace",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveEnvWorkspace(workspace)
			names, err := env.List(ws)
			if err != nil {
				ui.Errorf("Failed to list env profiles: %v", err)
			}
			if len(names) == 0 {
				ui.Infof("No env profiles in workspace '%s'", ws)
				return
			}
			for _, n := range names {
				fmt.Println(n)
			}
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name (default: active)")
	return cmd
}

func newEnvShowCmd() *cobra.Command {
	var workspace string
	var unsafe bool
	cmd := &cobra.Command{
		Use:   "show <profile>",
		Short: "Print env profile contents (values redacted unless --unsafe)",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			ws := resolveEnvWorkspace(workspace)
			p, err := env.Load(ws, name)
			if err != nil {
				ui.Errorf("%v", err)
			}
			if !unsafe {
				redacted := make(map[string]string, len(p.Vars))
				for k, v := range p.Vars {
					if v == "" {
						redacted[k] = ""
					} else {
						redacted[k] = "<redacted>"
					}
				}
				fmt.Print(env.FormatEnvFile(redacted))
				return
			}
			fmt.Print(env.FormatEnvFile(p.Vars))
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name (default: active)")
	cmd.Flags().BoolVar(&unsafe, "unsafe", false, "Show raw values (default: redacted)")
	return cmd
}

func newEnvDeleteCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:     "delete <profile>",
		Aliases: []string{"rm"},
		Short:   "Delete an env profile",
		Args:    cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			ws := resolveEnvWorkspace(workspace)
			if !env.Exists(ws, name) {
				ui.Errorf("Env profile '%s' not found in workspace '%s'", name, ws)
			}
			if err := env.Delete(ws, name); err != nil {
				ui.Errorf("Failed to delete env profile: %v", err)
			}
			ui.Successf("Deleted env profile '%s' from workspace '%s'", name, ws)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name (default: active)")
	return cmd
}

func newEnvDefaultCmd() *cobra.Command {
	var workspace string
	var clear bool
	cmd := &cobra.Command{
		Use:   "default [profile]",
		Short: "Get or set the default env profile for a workspace",
		Long: `With no arguments, prints the current default profile.
With a profile name, sets it as the default (auto-loaded on every run).
Use --clear to remove the default.`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveEnvWorkspace(workspace)

			if clear {
				if err := env.ClearDefault(ws); err != nil {
					ui.Errorf("Failed to clear default: %v", err)
				}
				ui.Successf("Cleared default env profile for workspace '%s'", ws)
				return
			}

			if len(args) == 0 {
				name, err := env.GetDefault(ws)
				if err != nil {
					ui.Errorf("Failed to get default: %v", err)
				}
				if name == "" {
					ui.Infof("No default env profile set for workspace '%s'", ws)
					return
				}
				fmt.Println(name)
				return
			}

			name := args[0]
			if err := env.SetDefault(ws, name); err != nil {
				ui.Errorf("%v", err)
			}
			ui.Successf("Set default env profile to '%s' for workspace '%s'", name, ws)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name (default: active)")
	cmd.Flags().BoolVar(&clear, "clear", false, "Remove the default profile setting")
	return cmd
}

// editEnvProfile opens a tempfile with the profile contents in $EDITOR,
// then parses and saves it back to KV. If isNew is true, writes a header.
func editEnvProfile(ws, name string, isNew bool) {
	var initial string
	if isNew {
		initial = fmt.Sprintf("# Env profile '%s' for workspace '%s'\n"+
			"# One KEY=VALUE per line. Empty values (KEY=) are passed as-is.\n"+
			"# Lines starting with '#' are ignored.\n\n", name, ws)
	} else {
		p, err := env.Load(ws, name)
		if err != nil {
			ui.Errorf("%v", err)
		}
		initial = env.FormatEnvFile(p.Vars)
	}

	tmp, err := os.CreateTemp("", fmt.Sprintf("exitbox-env-%s-*.env", name))
	if err != nil {
		ui.Errorf("Failed to create tempfile: %v", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmp.WriteString(initial); err != nil {
		_ = tmp.Close()
		ui.Errorf("Failed to write tempfile: %v", err)
	}
	if err := tmp.Close(); err != nil {
		ui.Errorf("Failed to close tempfile: %v", err)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	c := exec.Command(editor, tmpPath)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		ui.Errorf("Editor exited with error: %v", err)
	}

	content, err := os.ReadFile(tmpPath)
	if err != nil {
		ui.Errorf("Failed to read tempfile: %v", err)
	}
	vars, parseErr := env.ParseEnvFile(string(content))
	if parseErr != nil {
		ui.Errorf("Parse error: %v", parseErr)
	}
	if len(vars) == 0 {
		ui.Warn("No variables defined; profile not saved.")
		return
	}
	if err := env.Save(ws, &env.Profile{Name: name, Vars: vars}); err != nil {
		ui.Errorf("Failed to save env profile: %v", err)
	}
	ui.Successf("Saved env profile '%s' to workspace '%s' (%d vars)", name, ws, len(vars))
}

// resolveEnvWorkspace resolves the workspace using the same logic as config commands.
func resolveEnvWorkspace(override string) string {
	cfg := config.LoadOrDefault()
	return resolveConfigWorkspace(cfg, override)
}

// envProfileConfigDir returns the workspace agent config dir scoped to a profile.
// If profile is empty, returns the default agent dir.
func envProfileConfigDir(workspace, agent, profile string) string {
	base := config.WorkspaceAgentDir(workspace, agent)
	if profile == "" {
		return base
	}
	return filepath.Join(base, "profiles", profile)
}

// validateProfileName returns an error if the name is unsafe for use in
// filesystem paths or KV keys.
func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("profile name cannot be empty")
	}
	if strings.ContainsAny(name, "/\\:") || name == "." || name == ".." {
		return fmt.Errorf("invalid profile name: %s", name)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(newEnvCmd())
}
