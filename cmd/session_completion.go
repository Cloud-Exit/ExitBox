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
	"sort"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/session"
	"github.com/spf13/cobra"
)

func completeWorkspaceFlagValues(_ *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cfg := config.LoadOrDefault()
	var out []string
	for _, name := range profile.WorkspaceNames(cfg) {
		if strings.HasPrefix(name, toComplete) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

func completeSessionNamesForProject(workspaceOverride string, agents []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	projectDir, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	cfg := config.LoadOrDefault()

	workspaceName, err := resolveSessionsWorkspace(cfg, projectDir, workspaceOverride)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	seen := make(map[string]struct{})
	var out []string
	for _, a := range agents {
		names, listErr := session.ListNames(workspaceName, a, projectDir)
		if listErr != nil {
			continue
		}
		for _, name := range names {
			if !strings.HasPrefix(name, toComplete) {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, cobra.ShellCompDirectiveNoFileComp
}

func parseWorkspaceOverrideFromRunArgs(args []string) string {
	workspace := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-w" || arg == "--workspace" {
			if i+1 < len(args) {
				i++
				workspace = args[i]
			}
			continue
		}
		if strings.HasPrefix(arg, "--workspace=") {
			workspace = strings.TrimPrefix(arg, "--workspace=")
		}
	}
	return workspace
}

func completeAgentRunArgs(agentName string, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	prev := args[len(args)-1]
	switch prev {
	case "--resume":
		workspaceOverride := parseWorkspaceOverrideFromRunArgs(args[:len(args)-1])
		return completeSessionNamesForProject(workspaceOverride, []string{agentName}, toComplete)
	case "-w", "--workspace":
		return completeWorkspaceFlagValues(nil, nil, toComplete)
	}

	return nil, cobra.ShellCompDirectiveNoFileComp
}
