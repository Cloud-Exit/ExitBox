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

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/internal/wizard"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run the setup wizard",
	Long:  "Interactive setup wizard to configure roles, languages, tools, and agents.",
	RunE: func(cmd *cobra.Command, args []string) error {
		if config.ConfigExists() {
			ui.Warn("Config already exists at " + config.ConfigFile())
			fmt.Print("Re-running setup will overwrite your configuration. Continue? [y/N] ")
			var resp string
			_, _ = fmt.Scanln(&resp)
			if resp != "y" && resp != "Y" {
				ui.Info("Cancelled")
				return nil
			}
		}

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

	if err := wizard.Run(); err != nil {
		if err.Error() == "setup cancelled" {
			ui.Info("Setup cancelled. Writing default configuration.")
			if err := config.WriteDefaults(); err != nil {
				return fmt.Errorf("writing defaults: %w", err)
			}
			ui.Info("Run 'exitbox setup' to configure later.")
			return nil
		}
		return err
	}

	ui.Success("ExitBox configured!")
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Println("  1. Navigate to your project:  cd /path/to/project")
	fmt.Println("  2. Run an agent:              exitbox claude")
	fmt.Println()
	return nil
}

func init() {
	rootCmd.AddCommand(setupCmd)
}
