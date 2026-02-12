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
	"os/exec"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/profile"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newVaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Manage encrypted secret vaults",
		Long: "Encrypted key-value secret storage for workspaces.\n" +
			"Secrets are encrypted with AES-256 using a password-derived key (Argon2id)\n" +
			"and stored in an embedded Badger database. Inside containers, secrets\n" +
			"are accessed via IPC with per-read approval prompts.",
	}

	cmd.AddCommand(newVaultInitCmd())
	cmd.AddCommand(newVaultSetCmd())
	cmd.AddCommand(newVaultGetCmd())
	cmd.AddCommand(newVaultListCmd())
	cmd.AddCommand(newVaultDeleteCmd())
	cmd.AddCommand(newVaultImportCmd())
	cmd.AddCommand(newVaultEditCmd())
	cmd.AddCommand(newVaultStatusCmd())
	cmd.AddCommand(newVaultDestroyCmd())
	return cmd
}

func newVaultInitCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a new vault for a workspace",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)

			if vault.IsInitialized(ws) {
				ui.Errorf("Vault already initialized for workspace '%s'", ws)
			}

			password := promptPassword("Enter vault password: ")
			confirm := promptPassword("Confirm vault password: ")
			if password != confirm {
				ui.Error("Passwords do not match")
			}
			if password == "" {
				ui.Error("Password cannot be empty")
			}

			if err := vault.Init(ws, password); err != nil {
				ui.Errorf("Failed to initialize vault: %v", err)
			}

			// Enable vault in workspace config.
			cfg := config.LoadOrDefault()
			for i := range cfg.Workspaces.Items {
				if cfg.Workspaces.Items[i].Name == ws {
					cfg.Workspaces.Items[i].Vault.Enabled = true
					break
				}
			}
			if err := config.SaveConfig(cfg); err != nil {
				ui.Warnf("Failed to save config: %v", err)
			}

			ui.Successf("Vault initialized for workspace '%s'", ws)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newVaultSetCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "set <KEY>",
		Short: "Set a secret in the vault (value prompted securely)",
		Long: "Set a secret key-value pair in the vault. The value is read from\n" +
			"stdin with masked input to avoid leaking secrets in shell history.",
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)
			requireInitialized(ws)

			key := args[0]
			password := promptPassword("Enter vault password: ")

			// Prompt for the secret value securely (masked).
			value := promptPassword(fmt.Sprintf("Enter value for %s: ", key))
			if value == "" {
				ui.Error("Value cannot be empty")
			}

			if err := vault.QuickSet(ws, password, key, value); err != nil {
				ui.Errorf("Failed to set secret: %v", err)
			}
			ui.Successf("Set '%s' in vault for workspace '%s'", key, ws)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newVaultGetCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "get <KEY>",
		Short: "Get a secret from the vault",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)
			requireInitialized(ws)

			password := promptPassword("Enter vault password: ")
			val, err := vault.QuickGet(ws, password, args[0])
			if err != nil {
				ui.Errorf("%v", err)
			}
			fmt.Println(val)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newVaultListCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List secret keys in the vault",
		Aliases: []string{"ls"},
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)
			requireInitialized(ws)

			password := promptPassword("Enter vault password: ")
			keys, err := vault.QuickList(ws, password)
			if err != nil {
				ui.Errorf("%v", err)
			}
			if len(keys) == 0 {
				ui.Info("Vault is empty")
				return
			}
			for _, k := range keys {
				fmt.Println(k)
			}
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newVaultDeleteCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "delete <KEY>",
		Short: "Delete a secret from the vault",
		Aliases: []string{"rm"},
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)
			requireInitialized(ws)

			password := promptPassword("Enter vault password: ")
			if err := vault.QuickDelete(ws, password, args[0]); err != nil {
				ui.Errorf("%v", err)
			}
			ui.Successf("Deleted '%s' from vault for workspace '%s'", args[0], ws)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newVaultImportCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "import <file>",
		Short: "Import key-value pairs from a .env file into the vault",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)
			requireInitialized(ws)

			password := promptPassword("Enter vault password: ")
			if err := vault.ImportEnvFile(ws, password, args[0]); err != nil {
				ui.Errorf("Failed to import: %v", err)
			}
			ui.Successf("Imported %s into vault for workspace '%s'", args[0], ws)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newVaultEditCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit vault secrets in $EDITOR (KEY=VALUE format)",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)
			requireInitialized(ws)

			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "vi"
			}

			password := promptPassword("Enter vault password: ")
			s, err := vault.Open(ws, password)
			if err != nil {
				ui.Errorf("Failed to unlock vault: %v", err)
			}

			store, err := s.All()
			if closeErr := s.Close(); closeErr != nil {
				ui.Warnf("Failed to close vault: %v", closeErr)
			}
			if err != nil {
				ui.Errorf("Failed to read vault: %v", err)
			}

			// Write current contents to temp file.
			tmpFile, err := os.CreateTemp("", "exitbox-vault-*.env")
			if err != nil {
				ui.Errorf("Failed to create temp file: %v", err)
			}
			tmpPath := tmpFile.Name()
			defer os.Remove(tmpPath)

			content := "# Edit secrets below (KEY=VALUE format)\n# Lines starting with # are ignored\n"
			content += vault.ExportEnvFormat(store)
			if _, err := tmpFile.WriteString(content); err != nil {
				tmpFile.Close()
				ui.Errorf("Failed to write temp file: %v", err)
			}
			if err := tmpFile.Close(); err != nil {
				ui.Errorf("Failed to close temp file: %v", err)
			}

			c := exec.Command(editor, tmpPath)
			c.Stdin = os.Stdin
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				ui.Errorf("Editor exited with error: %v", err)
			}

			// Read back edited file.
			data, err := os.ReadFile(tmpPath)
			if err != nil {
				ui.Errorf("Failed to read edited file: %v", err)
			}

			newStore := vault.ParseEnvFile(data)
			if err := vault.ReplaceAll(ws, password, newStore); err != nil {
				ui.Errorf("Failed to save vault: %v", err)
			}

			ui.Success("Vault updated")
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newVaultStatusCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show vault state for a workspace",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)

			initialized := vault.IsInitialized(ws)

			cfg := config.LoadOrDefault()
			enabled := false
			for _, w := range cfg.Workspaces.Items {
				if w.Name == ws {
					enabled = w.Vault.Enabled
					break
				}
			}

			fmt.Println()
			fmt.Printf("Workspace:    %s\n", ws)
			fmt.Printf("Initialized:  %s\n", boolStatus(initialized))
			fmt.Printf("Enabled:      %s\n", boolStatus(enabled))
			fmt.Printf("Vault dir:    %s\n", config.VaultDir(ws))
			fmt.Println()
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

func newVaultDestroyCmd() *cobra.Command {
	var workspace string
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Permanently delete a vault",
		Run: func(cmd *cobra.Command, args []string) {
			ws := resolveVaultWorkspace(workspace)

			if !vault.IsInitialized(ws) {
				ui.Errorf("No vault found for workspace '%s'", ws)
			}

			fmt.Printf("This will permanently delete the encrypted vault for workspace '%s'.\n", ws)
			fmt.Print("Type 'yes' to confirm: ")
			reader := bufio.NewReader(os.Stdin)
			line, err := reader.ReadString('\n')
			if err != nil {
				ui.Errorf("Failed to read confirmation: %v", err)
			}
			if strings.TrimSpace(line) != "yes" {
				ui.Info("Cancelled")
				return
			}

			if err := vault.Destroy(ws); err != nil {
				ui.Errorf("Failed to remove vault: %v", err)
			}

			// Disable vault in workspace config.
			cfg := config.LoadOrDefault()
			for i := range cfg.Workspaces.Items {
				if cfg.Workspaces.Items[i].Name == ws {
					cfg.Workspaces.Items[i].Vault.Enabled = false
					break
				}
			}
			if err := config.SaveConfig(cfg); err != nil {
				ui.Warnf("Failed to save config: %v", err)
			}

			ui.Successf("Vault destroyed for workspace '%s'", ws)
		},
	}
	cmd.Flags().StringVarP(&workspace, "workspace", "w", "", "Workspace name")
	return cmd
}

// resolveVaultWorkspace returns the workspace name from the flag, or the active/default workspace.
func resolveVaultWorkspace(flag string) string {
	if flag != "" {
		return flag
	}
	cfg := config.LoadOrDefault()
	projectDir, _ := os.Getwd()
	active, err := profile.ResolveActiveWorkspace(cfg, projectDir, "")
	if err == nil && active != nil {
		return active.Workspace.Name
	}
	if cfg.Settings.DefaultWorkspace != "" {
		return cfg.Settings.DefaultWorkspace
	}
	ui.Error("No workspace specified. Use -w <workspace> or set a default workspace.")
	return "" // unreachable (ui.Error exits)
}

// requireInitialized exits with an error if the vault is not initialized.
func requireInitialized(ws string) {
	if !vault.IsInitialized(ws) {
		ui.Errorf("No vault for workspace '%s'. Run 'exitbox vault init -w %s' first.", ws, ws)
	}
}

// promptPassword reads a password from the terminal with echo disabled.
func promptPassword(prompt string) string {
	fmt.Print(prompt)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Println() // newline after password input
		if err != nil {
			ui.Errorf("Failed to read password: %v", err)
		}
		return string(pw)
	}
	// Non-interactive fallback.
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		ui.Errorf("Failed to read password: %v", err)
	}
	return strings.TrimRight(line, "\n\r")
}

func boolStatus(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// Save is a convenience wrapper for the vault edit command.
// It opens the vault, replaces all entries, and closes.
func init() {
	rootCmd.AddCommand(newVaultCmd())
}
