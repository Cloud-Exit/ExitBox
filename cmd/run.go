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

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/image"
	"github.com/cloud-exit/exitbox/internal/project"
	"github.com/cloud-exit/exitbox/internal/run"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

func newAgentRunCmd(agentName string) *cobra.Command {
	return &cobra.Command{
		Use:                agentName + " [args...]",
		Short:              "Run " + agent.DisplayName(agentName),
		DisableFlagParsing: true,
		Run: func(cmd *cobra.Command, args []string) {
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

	// Build images
	image.Version = Version
	if err := image.BuildProject(ctx, rt, agentName, projectDir); err != nil {
		ui.Errorf("Failed to build images: %v", err)
	}

	// Parse our flags out of passthrough
	var (
		noFirewall  bool
		readOnly    bool
		noEnv       bool
		verbose     bool
		envVars     []string
		includeDirs []string
		allowURLs   []string
		remaining   []string
	)

	for i := 0; i < len(passthrough); i++ {
		arg := passthrough[i]
		switch arg {
		case "-f", "--no-firewall":
			noFirewall = true
		case "-r", "--read-only":
			readOnly = true
		case "-n", "--no-env":
			noEnv = true
		case "-v", "--verbose":
			verbose = true
			ui.Verbose = true
		case "-e":
			if i+1 < len(passthrough) {
				i++
				envVars = append(envVars, passthrough[i])
			}
		case "-i", "--include-dir":
			if i+1 < len(passthrough) {
				i++
				includeDirs = append(includeDirs, passthrough[i])
			}
		case "-a", "--allow-urls":
			if i+1 < len(passthrough) {
				i++
				allowURLs = append(allowURLs, passthrough[i])
			}
		case "--":
			remaining = append(remaining, passthrough[i+1:]...)
			i = len(passthrough)
		default:
			remaining = append(remaining, arg)
		}
	}

	opts := run.Options{
		Agent:       agentName,
		ProjectDir:  projectDir,
		NoFirewall:  noFirewall,
		ReadOnly:    readOnly,
		NoEnv:       noEnv,
		EnvVars:     envVars,
		IncludeDirs: includeDirs,
		AllowURLs:   allowURLs,
		Passthrough: remaining,
		Verbose:     verbose,
		StatusBar:   cfg.Settings.StatusBar,
		Version:     Version,
	}

	exitCode, err := run.AgentContainer(rt, opts)
	if err != nil {
		ui.Errorf("%v", err)
	}
	os.Exit(exitCode)
}

func init() {
	for _, name := range agent.AgentNames {
		agentCmd := newAgentRunCmd(name)

		// Add profile subcommand
		profileCmd := &cobra.Command{
			Use:   "profile",
			Short: "Manage profiles for " + agent.DisplayName(name),
		}
		agentName := name // capture for closure
		profileCmd.AddCommand(newProfileListCmd())
		profileCmd.AddCommand(newProfileAddCmd(agentName))
		profileCmd.AddCommand(newProfileRemoveCmd(agentName))
		profileCmd.AddCommand(newProfileStatusCmd(agentName))

		agentCmd.AddCommand(profileCmd)
		rootCmd.AddCommand(agentCmd)
	}
}
