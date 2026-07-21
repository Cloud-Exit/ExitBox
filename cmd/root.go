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
	"runtime"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

// Version is set by ldflags at build time.
var Version = "3.2.0"

// skipWizardCommands are commands that should not trigger the first-run wizard.
var skipWizardCommands = map[string]bool{
	"setup":      true,
	"version":    true,
	"help":       true,
	"completion": true,
	"update":     true,
}

var rootCmd = &cobra.Command{
	Use:   "exitbox",
	Short: "Multi-Agent Container Sandbox",
	Long:  "ExitBox – Run AI coding assistants (Claude, Codex, OpenCode) in isolated containers",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		v, _ := cmd.Flags().GetBool("verbose")
		ui.Verbose = v

		// Trigger setup wizard on first run
		if !config.ConfigExists() && !skipWizardCommands[cmd.Name()] {
			ui.Info("No configuration found. Running setup wizard...")
			fmt.Println()
			if err := runSetup(); err != nil {
				return err
			}
			fmt.Println()
		}
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		listCmd.Run(cmd, args)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("exitbox version %s\n", Version)
	},
}

func init() {
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().BoolP("no-firewall", "f", false, "Disable network firewall")
	rootCmd.PersistentFlags().BoolP("read-only", "r", false, "Mount workspace as read-only")
	rootCmd.PersistentFlags().BoolP("no-env", "n", false, "Don't pass host environment variables")
	rootCmd.PersistentFlags().BoolP("update", "u", false, "Check for and apply agent updates")
	rootCmd.PersistentFlags().StringSliceP("env", "e", nil, "Pass environment variables (KEY=VALUE)")
	rootCmd.PersistentFlags().StringSliceP("include-dir", "i", nil, "Mount host dir inside /workspace")
	rootCmd.PersistentFlags().StringSliceP("tools", "t", nil, "Add Alpine packages to the image")
	rootCmd.PersistentFlags().StringSliceP("allow-urls", "a", nil, "Allow extra domains for this session")
	rootCmd.PersistentFlags().IntSlice("host-port", nil, "Allow localhost:PORT in the agent to reach the host")

	rootCmd.AddCommand(versionCmd)

	rootCmd.SetVersionTemplate("exitbox version {{.Version}}\n")
	rootCmd.Version = Version
}

// Execute runs the root command.
func Execute() {
	if runtime.GOOS != "windows" && os.Getuid() == 0 {
		ui.Error("Refusing to run as root. ExitBox requires a non-root user.")
	}

	config.EnsureDirs()

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
