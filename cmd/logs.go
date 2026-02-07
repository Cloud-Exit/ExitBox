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

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs <agent>",
	Short: "Show latest agent log file",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		name := args[0]
		if !agent.IsValidAgent(name) {
			ui.Errorf("Unknown agent: %s", name)
		}

		home := os.Getenv("HOME")
		agentCfgDir := config.AgentDir(name)

		var searchDirs []string
		switch name {
		case "claude":
			searchDirs = []string{
				filepath.Join(home, ".claude"),
				filepath.Join(agentCfgDir, ".claude"),
			}
		case "codex":
			searchDirs = []string{
				filepath.Join(home, ".codex"),
				filepath.Join(home, ".config", "codex"),
				filepath.Join(agentCfgDir, ".codex"),
				filepath.Join(agentCfgDir, ".config", "codex"),
			}
		case "opencode":
			searchDirs = []string{
				filepath.Join(home, ".local", "share", "opencode", "log"),
				filepath.Join(home, ".local", "share", "opencode", "logs"),
				filepath.Join(home, ".opencode"),
				filepath.Join(agentCfgDir, ".opencode"),
				filepath.Join(agentCfgDir, ".config", "opencode"),
			}
		}
		searchDirs = append(searchDirs, agentCfgDir)

		// Find log files
		var logFiles []string
		seen := make(map[string]bool)
		for _, dir := range searchDirs {
			if seen[dir] || dir == "" {
				continue
			}
			seen[dir] = true
			_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if !info.IsDir() && strings.HasSuffix(path, ".log") {
					logFiles = append(logFiles, path)
				}
				return nil
			})
		}

		if len(logFiles) == 0 {
			ui.Errorf("No log files found for %s", name)
		}

		// Find most recent
		var latest string
		var latestTime int64
		for _, f := range logFiles {
			info, err := os.Stat(f)
			if err != nil {
				continue
			}
			if info.ModTime().Unix() > latestTime {
				latestTime = info.ModTime().Unix()
				latest = f
			}
		}

		if latest == "" {
			ui.Errorf("No readable log files found for %s", name)
		}

		fmt.Printf("==> %s <==\n", latest)
		tail := exec.Command("tail", "-n", "200", latest)
		tail.Stdout = os.Stdout
		tail.Stderr = os.Stderr
		_ = tail.Run()
	},
}

func init() {
	rootCmd.AddCommand(logsCmd)
}
