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

package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/ui"
)

// ListAll lists all known projects.
func ListAll(rt container.Runtime) {
	projectsDir := config.ProjectsDir()
	entries, err := os.ReadDir(projectsDir)
	if err != nil || len(entries) == 0 {
		fmt.Println("  No projects found.")
		fmt.Println()
		return
	}

	fmt.Println()
	ui.Cecho("Known Projects:", ui.Cyan)
	fmt.Println()
	fmt.Printf("  %-40s %s\n", "PROJECT PATH", "AGENTS")
	fmt.Printf("  %-40s %s\n", "────────────", "──────")

	found := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		parentDir := filepath.Join(projectsDir, e.Name())
		pathFile := filepath.Join(parentDir, ".project_path")
		data, err := os.ReadFile(pathFile)
		if err != nil {
			continue
		}
		projectPath := strings.TrimSpace(string(data))
		found = true

		// Check which agents have images (any profile variant)
		var agentsList []string
		for _, agent := range []string{"claude", "codex", "opencode"} {
			prefix := fmt.Sprintf("exitbox-%s-%s-*", agent, e.Name())
			if rt != nil {
				if imgs, err := rt.ImageList(prefix); err == nil && len(imgs) > 0 {
					agentsList = append(agentsList, agent)
				}
			}
		}

		agentsStr := "-"
		if len(agentsList) > 0 {
			agentsStr = strings.Join(agentsList, ", ")
		}

		// Truncate path if too long
		display := projectPath
		if len(display) > 38 {
			display = "..." + display[len(display)-35:]
		}

		fmt.Printf("  %-40s %s\n", display, agentsStr)
	}

	fmt.Println()
	if !found {
		fmt.Println("  No projects found.")
		fmt.Println()
	}
}
