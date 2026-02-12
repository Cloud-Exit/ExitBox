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
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/network"
)

// AllowDomainHandlerConfig holds dependencies for the allow_domain handler.
type AllowDomainHandlerConfig struct {
	Runtime       container.Runtime
	ContainerName string
	// PromptFunc overrides the tmux popup prompt for testing.
	PromptFunc func(domain string) (bool, error)
	// ReloadFunc overrides domain reload for testing.
	ReloadFunc func(domain string) error
}

// NewAllowDomainHandler returns a HandlerFunc that validates a domain,
// prompts the user via a tmux popup in the container, and hot-reloads
// Squid on approval.
func NewAllowDomainHandler(cfg AllowDomainHandlerConfig) HandlerFunc {
	promptFn := cfg.PromptFunc
	if promptFn == nil {
		promptFn = func(domain string) (bool, error) {
			return promptViaTmuxPopup(cfg.Runtime, cfg.ContainerName, domain)
		}
	}

	reloadFn := cfg.ReloadFunc
	if reloadFn == nil {
		reloadFn = func(domain string) error {
			return network.AddSessionURLAndReload(cfg.Runtime, cfg.ContainerName, domain)
		}
	}

	return func(req *Request) (interface{}, error) {
		var payload AllowDomainRequest
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return AllowDomainResponse{Error: "invalid payload"}, nil
		}

		domain := strings.TrimSpace(payload.Domain)
		if domain == "" {
			return AllowDomainResponse{Error: "empty domain"}, nil
		}

		// Validate with the existing normalizer.
		normalized, err := network.NormalizeAllowlistEntry(domain)
		if err != nil {
			return AllowDomainResponse{Error: fmt.Sprintf("invalid domain: %v", err)}, nil
		}

		approved, err := promptFn(domain)
		if err != nil {
			return AllowDomainResponse{Error: fmt.Sprintf("prompt failed: %v", err)}, nil
		}

		if !approved {
			return AllowDomainResponse{Approved: false}, nil
		}

		// Use the normalized form for Squid (may have leading dot for hostnames).
		if err := reloadFn(normalized); err != nil {
			return AllowDomainResponse{Error: fmt.Sprintf("failed to update firewall: %v", err)}, nil
		}

		return AllowDomainResponse{Approved: true}, nil
	}
}

// promptViaTmuxPopup shows a tmux display-popup inside the agent container.
// The host execs into the container's tmux to present an interactive popup
// overlaying the agent session. This avoids competing with tmux for /dev/tty.
//
// The popup script reads a line and exits 0 on "y"/"yes", 1 otherwise.
// tmux display-popup -E returns the script's exit code.
func promptViaTmuxPopup(rt container.Runtime, containerName, domain string) (bool, error) {
	cmd := container.Cmd(rt)

	// Sanitize domain for shell embedding — only keep safe chars.
	safeDomain := sanitizeForShell(domain)

	// Shell script runs inside the popup. Uses `read` (not `read -n1`)
	// so the user types y + Enter, which works in all terminals.
	script := `printf '\n  \033[1;33m[ExitBox]\033[0m Allow domain access?\n\n  Domain: \033[1m` +
		safeDomain +
		`\033[0m\n\n  [y/N]: '; read ans; [ "$ans" = "y" ] || [ "$ans" = "yes" ]`

	c := exec.Command(cmd, "exec", containerName,
		"tmux", "display-popup", "-E", "-w", "50", "-h", "8",
		"sh", "-c", script,
	)

	var stderr bytes.Buffer
	c.Stderr = &stderr
	err := c.Run()

	if err == nil {
		return true, nil // exit 0 = approved
	}

	// If stderr is empty, the popup ran and the user denied or dismissed it.
	if exitErr, ok := err.(*exec.ExitError); ok {
		if stderr.Len() == 0 {
			return false, nil
		}
		return false, fmt.Errorf("popup failed (exit %d): %s", exitErr.ExitCode(), stderr.String())
	}

	return false, fmt.Errorf("popup exec failed: %w", err)
}

// sanitizeForShell strips any characters that aren't safe for embedding
// in a single-quoted shell string. Allows alphanumeric, dots, dashes, colons,
// underscores, and spaces (vault keys use underscores, display labels use spaces).
func sanitizeForShell(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '-' || r == ':' ||
			r == '_' || r == ' ' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
