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
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall [agent]",
	Short: "Uninstall exitbox or a specific agent",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		rt := container.Detect()

		if len(args) == 0 {
			// Full uninstall
			fmt.Println("This will UNINSTALL EXITBOX COMPLETELY.")
			fmt.Println("Actions:")
			fmt.Println("  - Stop and remove all exitbox containers")
			fmt.Println("  - Remove all exitbox images")
			fmt.Println("  - Disable all agents")
			fmt.Println("  - Remove all agent configurations")
			fmt.Println()
			fmt.Print("Are you sure? [y/N] ")

			reader := bufio.NewReader(os.Stdin)
			response, readErr := reader.ReadString('\n')
			if readErr != nil {
				ui.Warnf("Failed to read input: %v", readErr)
				ui.Info("Cancelled")
				return
			}
			response = strings.TrimSpace(response)
			if !strings.EqualFold(response, "y") {
				ui.Info("Cancelled")
				return
			}

			if rt != nil {
				// Stop and remove containers
				ui.Info("Stopping and removing all exitbox containers...")
				names, psErr := rt.PS("name=exitbox-", "{{.ID}}")
				if psErr != nil {
					ui.Warnf("Failed to list containers: %v", psErr)
				}
				for _, id := range names {
					_ = rt.Remove(id)
				}

				// Remove images
				ui.Info("Removing all exitbox images...")
				cleanImages(rt, "all")
			}

			// Remove config
			cfg := config.LoadOrDefault()
			for _, name := range agent.AgentNames {
				cfg.SetAgentEnabled(name, false)
				_ = os.RemoveAll(config.AgentDir(name))
			}
			if saveErr := config.SaveConfig(cfg); saveErr != nil {
				ui.Warnf("Failed to save config: %v", saveErr)
			}

			// Remove cache
			_ = os.RemoveAll(config.Cache)

			ui.Success("ExitBox uninstalled successfully.")
			return
		}

		// Single agent uninstall
		name := args[0]
		if !agent.IsValidAgent(name) {
			ui.Errorf("Unknown agent: %s", name)
		}

		fmt.Printf("This will remove all %s images and configuration.\n", agent.DisplayName(name))
		fmt.Print("Are you sure? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		response, readErr := reader.ReadString('\n')
		if readErr != nil {
			ui.Warnf("Failed to read input: %v", readErr)
			ui.Info("Cancelled")
			return
		}
		response = strings.TrimSpace(response)
		if !strings.EqualFold(response, "y") {
			ui.Info("Cancelled")
			return
		}

		if rt != nil {
			// Remove agent images
			removeAgentImages(rt, name)
		}

		// Remove config
		_ = os.RemoveAll(config.AgentDir(name))
		cfg := config.LoadOrDefault()
		cfg.SetAgentEnabled(name, false)
		if saveErr := config.SaveConfig(cfg); saveErr != nil {
			ui.Warnf("Failed to save config: %v", saveErr)
		}

		ui.Successf("%s completely uninstalled", agent.DisplayName(name))
	},
}

func removeAgentImages(rt container.Runtime, agentName string) {
	images, err := rt.ImageList("exitbox-" + agentName + "-*")
	if err != nil {
		ui.Warnf("Failed to list %s images: %v", agentName, err)
		return
	}
	for _, img := range images {
		_ = rt.ImageRemove(img)
	}
}

func cleanImages(rt container.Runtime, mode string) {
	switch mode {
	case "all":
		images, err := rt.ImageList("exitbox-*")
		if err != nil {
			ui.Warnf("Failed to list exitbox images: %v", err)
			return
		}
		for _, img := range images {
			_ = rt.ImageRemove(img)
		}
	}
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}
