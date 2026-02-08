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
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/image"
	"github.com/cloud-exit/exitbox/internal/project"
	"github.com/cloud-exit/exitbox/internal/run"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <agent> [args...]",
	Short: "Run an agent in a container",
	Long: `Run an AI coding assistant in an isolated container.

Available agents:
  claude      Claude Code (Anthropic)
  codex       OpenAI Codex CLI
  opencode    OpenCode (open-source)

Workspaces:
  Workspaces are named contexts (e.g. personal/work) with development stacks
  and separate agent config storage.
  Manage them with: exitbox workspaces --help

Flags (passed after the agent name):
  -f, --no-firewall       Disable network firewall
  -r, --read-only         Mount workspace as read-only
  -n, --no-env            Don't pass host environment variables
      --no-resume         Don't auto-resume the previous agent session
  -u, --update            Check for and apply agent updates
  -v, --verbose           Enable verbose output
  -w, --workspace NAME    Use a specific workspace for this session
  -e, --env KEY=VALUE     Pass environment variables
  -t, --tools PKG         Add Alpine packages to the image
  -i, --include-dir DIR   Mount host dir inside /workspace
  -a, --allow-urls DOM    Allow extra domains for this session

Examples:
  exitbox run claude
  exitbox run claude -f -e GITHUB_TOKEN=$GITHUB_TOKEN
  exitbox run codex --update
  exitbox run claude --workspace work`,
}

func newAgentRunCmd(agentName string) *cobra.Command {
	display := agent.DisplayName(agentName)
	return &cobra.Command{
		Use:                agentName + " [args...]",
		Short:              "Run " + display,
		Long:               "Run " + display + " in an isolated container.",
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
			// DisableFlagParsing swallows --help; handle it manually
			for _, a := range args {
				if a == "--" {
					break
				}
				if a == "--help" || a == "-h" {
					_ = cmd.Help()
					return
				}
				if !strings.HasPrefix(a, "-") {
					break
				}
			}
			runAgent(agentName, args)
		},
	}
}

func runAgent(agentName string, passthrough []string) {
	cfg := config.LoadOrDefault()
	if !cfg.IsAgentEnabled(agentName) {
		ui.Errorf("Agent '%s' is not enabled. Run 'exitbox enable %s' first.", agentName, agentName)
	}

	rt := container.Detect()
	if rt == nil {
		ui.Error("No container runtime found. Install Podman or Docker.")
	}

	projectDir, _ := os.Getwd()
	ctx := context.Background()

	// Initialize project
	if err := project.Init(projectDir); err != nil {
		ui.Warnf("Failed to initialize project directory: %v", err)
	}

	image.Version = Version

	flags := parseRunFlags(passthrough, cfg.Settings.DefaultFlags)

	if flags.Verbose {
		ui.Verbose = true
	}

	image.SessionTools = flags.Tools
	image.ForceRebuild = flags.ForceUpdate
	image.AutoUpdate = cfg.Settings.AutoUpdate || flags.ForceUpdate

	switchFile := filepath.Join(projectDir, ".exitbox", "workspace-switch")

	// Main run loop: re-launches on workspace switch.
	for {
		// Reload config each iteration (workspace switch updates it).
		cfg = config.LoadOrDefault()

		if err := image.BuildProject(ctx, rt, agentName, projectDir, flags.Workspace, false); err != nil {
			ui.Errorf("Failed to build images: %v", err)
		}

		workspaceHash := image.WorkspaceHash(cfg, projectDir, flags.Workspace)

		opts := run.Options{
			Agent:             agentName,
			ProjectDir:        projectDir,
			WorkspaceHash:     workspaceHash,
			WorkspaceOverride: flags.Workspace,
			NoFirewall:        flags.NoFirewall,
			ReadOnly:          flags.ReadOnly,
			NoEnv:             flags.NoEnv,
			NoResume:          flags.NoResume,
			EnvVars:           flags.EnvVars,
			IncludeDirs:       flags.IncludeDirs,
			AllowURLs:         flags.AllowURLs,
			Passthrough:       flags.Remaining,
			Verbose:           flags.Verbose,
			StatusBar:         cfg.Settings.StatusBar,
			Version:           Version,
		}

		exitCode, err := run.AgentContainer(rt, opts)
		if err != nil {
			ui.Errorf("%v", err)
		}

		// Check for workspace switch signal from the container.
		if data, readErr := os.ReadFile(switchFile); readErr == nil {
			newWorkspace := strings.TrimSpace(string(data))
			_ = os.Remove(switchFile)
			if newWorkspace != "" {
				ui.Infof("Switching to workspace '%s'...", newWorkspace)
				flags.Workspace = newWorkspace
				continue
			}
		}

		os.Exit(exitCode)
	}
}

type parsedFlags struct {
	NoFirewall  bool
	ReadOnly    bool
	NoEnv       bool
	NoResume    bool
	Verbose     bool
	ForceUpdate bool
	Workspace   string
	EnvVars     []string
	IncludeDirs []string
	AllowURLs   []string
	Tools       []string
	Remaining   []string
}

func parseRunFlags(passthrough []string, defaults config.DefaultFlags) parsedFlags {
	f := parsedFlags{
		NoFirewall: defaults.NoFirewall,
		ReadOnly:   defaults.ReadOnly,
		NoEnv:      defaults.NoEnv,
		NoResume:   !defaults.AutoResume,
	}

	for i := 0; i < len(passthrough); i++ {
		arg := passthrough[i]
		switch arg {
		case "-f", "--no-firewall":
			f.NoFirewall = true
		case "-r", "--read-only":
			f.ReadOnly = true
		case "-n", "--no-env":
			f.NoEnv = true
		case "--no-resume":
			f.NoResume = true
		case "-v", "--verbose":
			f.Verbose = true
		case "-u", "--update":
			f.ForceUpdate = true
		case "-w", "--workspace":
			if i+1 < len(passthrough) {
				i++
				f.Workspace = passthrough[i]
			}
		case "-e", "--env":
			if i+1 < len(passthrough) {
				i++
				f.EnvVars = append(f.EnvVars, passthrough[i])
			}
		case "-t", "--tools":
			if i+1 < len(passthrough) {
				i++
				f.Tools = append(f.Tools, passthrough[i])
			}
		case "-i", "--include-dir":
			if i+1 < len(passthrough) {
				i++
				f.IncludeDirs = append(f.IncludeDirs, passthrough[i])
			}
		case "-a", "--allow-urls":
			if i+1 < len(passthrough) {
				i++
				f.AllowURLs = append(f.AllowURLs, passthrough[i])
			}
		case "--":
			f.Remaining = append(f.Remaining, passthrough[i+1:]...)
			i = len(passthrough)
		default:
			f.Remaining = append(f.Remaining, arg)
		}
	}

	return f
}

func init() {
	for _, name := range agent.AgentNames {
		agentCmd := newAgentRunCmd(name)
		runCmd.AddCommand(agentCmd)
	}

	rootCmd.AddCommand(runCmd)
}
