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
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

var rebuildWorkspace string

var rebuildCmd = &cobra.Command{
	Use:   "rebuild <agent|all>",
	Short: "Force rebuild of agent image(s)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]

		rt := container.Detect()
		if rt == nil {
			ui.Error("No container runtime found. Install Podman or Docker.")
		}

		image.Version = Version
		image.AutoUpdate = true // rebuild always checks for latest

		var agents []string
		if name == "all" {
			cfg := config.LoadOrDefault()
			for _, a := range agent.AgentNames {
				if cfg.IsAgentEnabled(a) {
					agents = append(agents, a)
				}
			}
			if len(agents) == 0 {
				ui.Error("No agents are enabled. Run 'exitbox setup' first.")
			}
		} else {
			if !agent.IsValidAgent(name) {
				ui.Errorf("Unknown agent: %s", name)
			}
			agents = []string{name}
		}

		projectDir, _ := os.Getwd()
		ctx := context.Background()

		for _, a := range agents {
			ui.Infof("Rebuilding %s container image...", agent.DisplayName(a))
			if err := image.BuildCore(ctx, rt, a, true); err != nil {
				ui.Errorf("Failed to rebuild %s core image: %v", agent.DisplayName(a), err)
			}
			if err := image.BuildProject(ctx, rt, a, projectDir, rebuildWorkspace, true); err != nil {
				ui.Errorf("Failed to rebuild %s project image: %v", agent.DisplayName(a), err)
			}
			ui.Successf("%s image rebuilt successfully", agent.DisplayName(a))
		}
	},
}

func init() {
	rebuildCmd.Flags().StringVarP(&rebuildWorkspace, "workspace", "w", "", "Rebuild image for a specific workspace")
	rootCmd.AddCommand(rebuildCmd)
}
