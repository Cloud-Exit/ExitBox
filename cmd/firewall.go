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
	"strconv"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/network"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/spf13/cobra"
)

func newFirewallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "firewall",
		Short: "Manage firewall exclusions",
	}
	ports := &cobra.Command{
		Use:   "ports",
		Short: "Manage host ports reachable as localhost:<port> from agents",
	}
	ports.AddCommand(newFirewallPortsListCmd())
	ports.AddCommand(newFirewallPortsAddCmd())
	ports.AddCommand(newFirewallPortsRemoveCmd())
	cmd.AddCommand(ports)
	return cmd
}

func newFirewallPortsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List persistent host port exclusions",
		Run: func(cmd *cobra.Command, args []string) {
			al := config.LoadAllowlistOrDefault()
			ports := network.NormalizeHostPorts(al.HostPorts)
			if len(ports) == 0 {
				ui.Info("No host ports configured.")
				return
			}
			for _, port := range ports {
				fmt.Println(port)
			}
		},
	}
}

func newFirewallPortsAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <port> [port...]",
		Short: "Allow host ports from agents via localhost:<port>",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ports := parseFirewallPorts(args)
			al := config.LoadAllowlistOrDefault()
			al.HostPorts = network.NormalizeHostPorts(append(al.HostPorts, ports...))
			if err := config.SaveAllowlist(al); err != nil {
				ui.Errorf("Failed to save firewall ports: %v", err)
			}
			ui.Successf("Allowed host ports: %s", network.HostPortsEnvValue(al.HostPorts))
		},
	}
}

func newFirewallPortsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <port> [port...]",
		Short: "Remove persistent host port exclusions",
		Args:  cobra.MinimumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ports := parseFirewallPorts(args)
			remove := make(map[int]struct{}, len(ports))
			for _, port := range ports {
				remove[port] = struct{}{}
			}

			al := config.LoadAllowlistOrDefault()
			var kept []int
			for _, port := range network.NormalizeHostPorts(al.HostPorts) {
				if _, ok := remove[port]; !ok {
					kept = append(kept, port)
				}
			}
			al.HostPorts = kept
			if err := config.SaveAllowlist(al); err != nil {
				ui.Errorf("Failed to save firewall ports: %v", err)
			}
			if len(al.HostPorts) == 0 {
				ui.Success("No host ports allowed.")
			} else {
				ui.Successf("Allowed host ports: %s", network.HostPortsEnvValue(al.HostPorts))
			}
		},
	}
}

func parseFirewallPorts(args []string) []int {
	var ports []int
	for _, arg := range args {
		port, err := strconv.Atoi(arg)
		if err != nil || port < 1 || port > 65535 {
			ui.Errorf("Invalid port %q: must be 1-65535", arg)
		}
		ports = append(ports, port)
	}
	return ports
}

func init() {
	rootCmd.AddCommand(newFirewallCmd())
}
