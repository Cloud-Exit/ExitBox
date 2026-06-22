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
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/env"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage agent configuration",
		Long:  "Import or edit agent configuration files for a workspace.",
	}

	cmd.AddCommand(newConfigImportCmd())
	cmd.AddCommand(newConfigEditCmd())
	return cmd
}

func newConfigImportCmd() *cobra.Command {
	var workspace string
	var configFile string

	cmd := &cobra.Command{
		Use:   "import <agent|all>",
		Short: "Import agent config from host",
		Long: `Import agent configuration and credentials from the host into a workspace.

By default, imports into the active workspace. Use --workspace to target
a specific workspace. Use --config to import a specific config file instead
of the entire host config directory.

Examples:
  exitbox config import claude                    Import Claude config into active workspace
  exitbox config import all                       Import all agent configs into active workspace
  exitbox config import claude -w work            Import Claude config into 'work' workspace
  exitbox config import all --workspace personal  Import all configs into 'personal' workspace
  exitbox config import codex -c config.toml      Import a config file into Codex workspace
  exitbox config import opencode -c opencode.json Import OpenCode config file
  exitbox config import codex -c config.toml -w work  Import into specific workspace`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			target := args[0]

			// Validate --config flag constraints.
			if configFile != "" {
				if target == "all" {
					ui.Errorf("Cannot use --config with 'all'; specify a single agent")
				}
				if _, err := os.Stat(configFile); err != nil {
					ui.Errorf("Config file not found: %s", configFile)
				}
			}

			var agentNames []string
			if target == "all" {
				agentNames = agents.Names()
			} else {
				a := agents.Get(target)
				if a == nil {
					ui.Errorf("Unknown agent: %s", target)
				}
				agentNames = []string{target}
			}

			// Resolve target workspace.
			cfg := config.LoadOrDefault()
			workspaceName := resolveConfigWorkspace(cfg, workspace)

			importedAny := false
			for _, name := range agentNames {
				a := agents.Get(name)
				if a == nil {
					continue
				}

				dst := profile.WorkspaceAgentDir(workspaceName, name)
				if err := os.MkdirAll(dst, 0755); err != nil {
					ui.Warnf("Failed to create workspace dir for %s: %v", name, err)
					continue
				}

				if configFile != "" {
					// Import a specific file.
					if err := a.ImportFile(configFile, dst); err != nil {
						ui.Warnf("Failed to import file into %s: %v", name, err)
						continue
					}
					ui.Successf("Imported %s → %s workspace '%s'",
						configFile, name, workspaceName)
					importedAny = true
				} else {
					// Import entire host config directory.
					src, err := a.DetectHostConfig()
					if err != nil {
						ui.Warnf("No host config found for %s", name)
						continue
					}
					if err := a.ImportConfig(src, dst); err != nil {
						ui.Warnf("Failed to import %s config: %v", name, err)
						continue
					}
					ui.Successf("Imported %s config from %s → workspace '%s'",
						name, src, workspaceName)
					importedAny = true
				}
			}

			if !importedAny {
				if configFile != "" {
					ui.Warn("Config file was not imported.")
				} else {
					ui.Warn("No configs were imported.")
				}
			}
		},
	}

	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Target workspace (default: active workspace)")
	cmd.Flags().StringVarP(&configFile, "config", "c", "", "Import a specific config file (e.g. config.toml, opencode.json)")
	return cmd
}

func newConfigEditCmd() *cobra.Command {
	var workspace string
	var envProfile string

	cmd := &cobra.Command{
		Use:   "edit <agent>",
		Short: "Open agent config file in $EDITOR",
		Long: `Opens the agent's primary config file in your $EDITOR (or vi).

Creates the file if it doesn't exist. With --profile, edits the
profile-scoped config (seeded from the default config on first edit).

Examples:
  exitbox config edit claude                       Edit Claude settings.json (default)
  exitbox config edit codex                        Edit Codex config.toml
  exitbox config edit opencode -w work             Edit OpenCode config in 'work' workspace
  exitbox config edit claude --profile openrouter  Edit Claude settings.json scoped to 'openrouter' profile`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]
			a := agents.Get(name)
			if a == nil {
				ui.Errorf("Unknown agent: %s", name)
			}

			cfg := config.LoadOrDefault()
			workspaceName := resolveConfigWorkspace(cfg, workspace)

			// With no explicit --profile, target the workspace's default env
			// profile so `config edit` edits the same config that `exitbox run`
			// loads (run auto-applies the default profile). Falls back to the
			// base config only when no default profile is set.
			if envProfile == "" {
				if defName := defaultEnvProfile(workspaceName); defName != "" {
					envProfile = defName
					ui.Infof("Editing default env profile '%s' (use --profile to override)", defName)
				}
			}

			if envProfile != "" {
				if err := validateProfileName(envProfile); err != nil {
					ui.Errorf("%v", err)
					return
				}
			}

			// Profile-scoped config is seeded from the default config on first
			// edit. If the default contains API keys or tokens, they are copied
			// into the profile directory. Rotate secrets if you split profiles
			// between providers.
			wsDir := envProfileConfigDir(workspaceName, name, envProfile)
			p := a.ConfigFilePath(wsDir)

			// Seed from default config on first edit of a profile-scoped file.
			if _, err := os.Stat(p); os.IsNotExist(err) {
				if mkErr := os.MkdirAll(filepath.Dir(p), 0755); mkErr != nil {
					ui.Errorf("Failed to create directory: %v", mkErr)
				}
				if envProfile != "" {
					defaultPath := a.ConfigFilePath(profile.WorkspaceAgentDir(workspaceName, name))
					if data, readErr := os.ReadFile(defaultPath); readErr == nil {
						if wErr := os.WriteFile(p, data, 0644); wErr != nil {
							ui.Errorf("Failed to seed profile config: %v", wErr)
						}
					} else {
						if wErr := os.WriteFile(p, []byte{}, 0644); wErr != nil {
							ui.Errorf("Failed to create config file: %v", wErr)
						}
					}
				} else {
					if wErr := os.WriteFile(p, []byte{}, 0644); wErr != nil {
						ui.Errorf("Failed to create config file: %v", wErr)
					}
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

	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name (default: active workspace)")
	cmd.Flags().StringVar(&envProfile, "profile", "", "Scope to a named env profile (creates profile-specific config)")
	return cmd
}

// resolveConfigWorkspace determines which workspace to target.
func resolveConfigWorkspace(cfg *config.Config, override string) string {
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

	// Use the active/default workspace.
	projectDir, _ := os.Getwd()
	active, _ := profile.ResolveActiveWorkspace(cfg, projectDir, "")
	if active != nil {
		return active.Workspace.Name
	}

	// Fallback if no workspace exists at all.
	if len(cfg.Workspaces.Items) > 0 {
		return cfg.Workspaces.Items[0].Name
	}
	return "default"
}

// defaultEnvProfile returns the workspace's default env profile name, or "" if
// none is set. Mirrors what `exitbox run` resolves so config edits target the
// same profile-scoped config the agent loads at runtime.
func defaultEnvProfile(workspaceName string) string {
	defName, err := env.GetDefault(workspaceName)
	if err != nil {
		ui.Warnf("Could not read default env profile: %v", err)
		return ""
	}
	return defName
}

func init() {
	rootCmd.AddCommand(newConfigCmd())
}
