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
	"testing"

	"github.com/spf13/cobra"
)

func TestVaultSetCmd_FileFlag(t *testing.T) {
	cmd := newVaultSetCmd()
	f := cmd.Flags().Lookup("file")
	if f == nil {
		t.Fatal("vault set has no --file flag")
	}
	// -f is the global --no-firewall shorthand.
	if f.Shorthand != "" {
		t.Errorf("--file shorthand = %q, want none", f.Shorthand)
	}
	if err := cmd.ParseFlags([]string{"--file", "/tmp/key.pem"}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	if got := f.Value.String(); got != "/tmp/key.pem" {
		t.Errorf("--file = %q, want %q", got, "/tmp/key.pem")
	}
}

// TestNoFlagShorthandConflicts merges inherited flags on every command in the
// real tree; pflag panics on a duplicate shorthand.
func TestNoFlagShorthandConflicts(t *testing.T) {
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: %v", c.CommandPath(), fmt.Sprint(r))
				}
			}()
			_ = c.InheritedFlags()
			_ = c.Flags()
		}()
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}
