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

package ipc

import (
	"bytes"
	cryptoRand "crypto/rand"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/vault"
)

// VaultHandlerConfig holds dependencies for vault IPC handlers.
type VaultHandlerConfig struct {
	Runtime       container.Runtime
	ContainerName string
	WorkspaceName string
	// PromptPasswordFunc overrides the tmux popup password prompt for testing.
	PromptPasswordFunc func() (string, error)
	// PromptApproveFunc overrides the tmux popup approval prompt for testing.
	PromptApproveFunc func(key string) (bool, error)
	// OpenFunc overrides vault.Open for testing.
	OpenFunc func(workspace, password string) (map[string]string, error)
}

// VaultState holds the decrypted vault store in memory between IPC requests.
// After the first successful password entry, the store is cached so subsequent
// requests don't require re-entering the password.
type VaultState struct {
	mu    sync.Mutex
	store map[string]string // nil = not yet unlocked
}

// Cleanup resets the in-memory vault state.
func (vs *VaultState) Cleanup() {
	vs.mu.Lock()
	defer vs.mu.Unlock()
	vs.store = nil
}

// NewVaultGetHandler returns a HandlerFunc for "vault_get" requests.
func NewVaultGetHandler(cfg VaultHandlerConfig, state *VaultState) HandlerFunc {
	promptApprove := cfg.PromptApproveFunc
	if promptApprove == nil {
		promptApprove = func(key string) (bool, error) {
			return promptVaultApproval(cfg.Runtime, cfg.ContainerName, key)
		}
	}

	promptPassword := cfg.PromptPasswordFunc
	if promptPassword == nil {
		promptPassword = func() (string, error) {
			return promptVaultPassword(cfg.Runtime, cfg.ContainerName)
		}
	}

	openFn := cfg.OpenFunc
	if openFn == nil {
		openFn = openVaultAsMap
	}

	return func(req *Request) (interface{}, error) {
		var payload VaultGetRequest
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return VaultGetResponse{Error: "invalid payload"}, nil
		}

		key := strings.TrimSpace(payload.Key)
		if key == "" {
			return VaultGetResponse{Error: "empty key"}, nil
		}

		// Prompt user for approval.
		approved, err := promptApprove(key)
		if err != nil {
			return VaultGetResponse{Error: fmt.Sprintf("approval prompt failed: %v", err)}, nil
		}
		if !approved {
			return VaultGetResponse{Approved: false}, nil
		}

		// Ensure vault is unlocked.
		store, err := ensureUnlocked(state, cfg.WorkspaceName, promptPassword, openFn)
		if err != nil {
			return VaultGetResponse{Error: fmt.Sprintf("vault unlock failed: %v", err)}, nil
		}

		val, ok := store[key]
		if !ok {
			return VaultGetResponse{Error: fmt.Sprintf("key %q not found in vault", key)}, nil
		}

		return VaultGetResponse{Value: val, Approved: true}, nil
	}
}

// NewVaultListHandler returns a HandlerFunc for "vault_list" requests.
func NewVaultListHandler(cfg VaultHandlerConfig, state *VaultState) HandlerFunc {
	promptApprove := cfg.PromptApproveFunc
	if promptApprove == nil {
		promptApprove = func(_ string) (bool, error) {
			return promptVaultApproval(cfg.Runtime, cfg.ContainerName, "list keys")
		}
	}

	promptPassword := cfg.PromptPasswordFunc
	if promptPassword == nil {
		promptPassword = func() (string, error) {
			return promptVaultPassword(cfg.Runtime, cfg.ContainerName)
		}
	}

	openFn := cfg.OpenFunc
	if openFn == nil {
		openFn = openVaultAsMap
	}

	return func(req *Request) (interface{}, error) {
		// Prompt user for approval.
		approved, err := promptApprove("list keys")
		if err != nil {
			return VaultListResponse{Error: fmt.Sprintf("approval prompt failed: %v", err)}, nil
		}
		if !approved {
			return VaultListResponse{Approved: false}, nil
		}

		// Ensure vault is unlocked.
		store, err := ensureUnlocked(state, cfg.WorkspaceName, promptPassword, openFn)
		if err != nil {
			return VaultListResponse{Error: fmt.Sprintf("vault unlock failed: %v", err)}, nil
		}

		keys := make([]string, 0, len(store))
		for k := range store {
			keys = append(keys, k)
		}

		return VaultListResponse{Keys: keys, Approved: true}, nil
	}
}

// ensureUnlocked returns the decrypted store, prompting for password if needed.
func ensureUnlocked(
	state *VaultState,
	workspace string,
	promptPassword func() (string, error),
	openFn func(string, string) (map[string]string, error),
) (map[string]string, error) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.store != nil {
		return state.store, nil
	}

	password, err := promptPassword()
	if err != nil {
		return nil, fmt.Errorf("password prompt failed: %v", err)
	}

	store, err := openFn(workspace, password)
	if err != nil {
		return nil, err
	}

	state.store = store
	return store, nil
}

// openVaultAsMap opens a vault.Store, reads all entries into a map, and closes the store.
func openVaultAsMap(workspace, password string) (map[string]string, error) {
	s, err := vault.Open(workspace, password)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	return s.All()
}

// promptVaultPassword shows a tmux popup that captures a password.
// Since tmux display-popup runs the script inside the popup's terminal
// (stdout goes to the popup, not back through docker exec), we use a
// temp file inside the container to pass the password back to the host.
func promptVaultPassword(rt container.Runtime, containerName string) (string, error) {
	cmd := container.Cmd(rt)

	// Generate a random temp file path inside the container.
	randomBytes := make([]byte, 8)
	if _, err := cryptoRand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generating random path: %w", err)
	}
	tmpFile := fmt.Sprintf("/tmp/.exitbox-vault-pw-%x", randomBytes)

	// Popup script: prompt for password, write to temp file.
	// The script exits 0 only if a non-empty password was entered.
	script := `printf '\n  \033[1;33m[ExitBox Vault]\033[0m Enter vault password:\n\n  Password: '; ` +
		`stty -echo 2>/dev/null; read pw; stty echo 2>/dev/null; printf '\n'; ` +
		`echo "$pw" > ` + tmpFile + `; [ -n "$pw" ]`

	// Run the popup (blocks until user submits or dismisses).
	c := exec.Command(cmd, "exec", containerName,
		"tmux", "display-popup", "-E", "-w", "55", "-h", "7",
		"sh", "-c", script,
	)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	popupErr := c.Run()

	// Read the password from the temp file.
	readCmd := exec.Command(cmd, "exec", containerName, "cat", tmpFile)
	var stdout bytes.Buffer
	readCmd.Stdout = &stdout
	readErr := readCmd.Run()

	// Clean up the temp file regardless of outcome.
	cleanupCmd := exec.Command(cmd, "exec", containerName, "rm", "-f", tmpFile)
	_ = cleanupCmd.Run()

	if popupErr != nil {
		if exitErr, ok := popupErr.(*exec.ExitError); ok {
			if stderr.Len() == 0 {
				return "", fmt.Errorf("password entry cancelled")
			}
			return "", fmt.Errorf("popup failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
		}
		return "", fmt.Errorf("popup exec failed: %w", popupErr)
	}

	if readErr != nil {
		return "", fmt.Errorf("failed to read password: %w", readErr)
	}

	password := strings.TrimRight(stdout.String(), "\n\r")
	if password == "" {
		return "", fmt.Errorf("empty password")
	}
	return password, nil
}

// promptVaultApproval shows a tmux popup for approving secret access.
func promptVaultApproval(rt container.Runtime, containerName, key string) (bool, error) {
	cmd := container.Cmd(rt)

	safeKey := sanitizeForShell(key)

	script := `printf '\n  \033[1;33m[ExitBox Vault]\033[0m Allow secret read?\n\n  Key: \033[1m` +
		safeKey +
		`\033[0m\n\n  [y/N]: '; read ans; [ "$ans" = "y" ] || [ "$ans" = "yes" ]`

	c := exec.Command(cmd, "exec", containerName,
		"tmux", "display-popup", "-E", "-w", "55", "-h", "9",
		"sh", "-c", script,
	)

	var stderr bytes.Buffer
	c.Stderr = &stderr
	err := c.Run()

	if err == nil {
		return true, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		if stderr.Len() == 0 {
			return false, nil
		}
		return false, fmt.Errorf("popup failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
	}

	return false, fmt.Errorf("popup exec failed: %w", err)
}
