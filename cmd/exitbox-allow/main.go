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

// exitbox-allow is a standalone binary for requesting domain access from
// inside an ExitBox container. It communicates with the host via a Unix
// domain socket using JSON-lines protocol.
//
// Usage: exitbox-allow <domain> [domain...]
package main

import (
	"bufio"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net"
	"os"
)

type request struct {
	Type    string      `json:"type"`
	ID      string      `json:"id"`
	Payload interface{} `json:"payload"`
}

type allowDomainPayload struct {
	Domain string `json:"domain"`
}

type response struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

type allowDomainResponse struct {
	Approved bool   `json:"approved"`
	Error    string `json:"error,omitempty"`
	// Pointer so an older host that omits the field is treated as
	// firewall-enabled (the pre-existing behaviour) rather than as
	// "no firewall", which would mislabel a real approve/deny.
	FirewallEnabled *bool `json:"firewall_enabled,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: exitbox-allow <domain> [domain...]")
		os.Exit(1)
	}

	socketPath := os.Getenv("EXITBOX_IPC_SOCKET")
	if socketPath == "" {
		socketPath = "/run/exitbox/host.sock"
	}

	hasFailure := false
	for _, domain := range os.Args[1:] {
		approved, firewallEnabled, err := requestAllow(socketPath, domain)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %s: %v\n", domain, err)
			hasFailure = true
			continue
		}
		switch {
		case !firewallEnabled:
			// Nothing is blocked, so the request is a no-op. Say so instead
			// of implying a firewall rule was added.
			fmt.Printf("Approved: %s (no firewall, all domains reachable)\n", domain)
		case approved:
			fmt.Printf("Approved: %s\n", domain)
		default:
			fmt.Printf("Denied: %s\n", domain)
			hasFailure = true
		}
	}

	if hasFailure {
		os.Exit(1)
	}
}

func requestAllow(socketPath, domain string) (approved, firewallEnabled bool, err error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		// Distinguish "socket missing" from "socket exists but connect denied"
		// to help diagnose stale bind-mount scenarios.
		if _, statErr := os.Stat(socketPath); statErr == nil {
			return false, true, fmt.Errorf("IPC socket exists but connect failed (%v). The host exitbox process may have exited while this container is still running", err)
		}
		return false, true, fmt.Errorf("IPC socket not available (%v). Domain allow requests need the host exitbox process to be running", err)
	}
	defer conn.Close()

	id := randomID()
	req := request{
		Type: "allow_domain",
		ID:   id,
		Payload: allowDomainPayload{
			Domain: domain,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		return false, true, err
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return false, true, err
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, true, err
		}
		return false, true, fmt.Errorf("no response from host")
	}

	var resp response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return false, true, err
	}

	var payload allowDomainResponse
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return false, true, err
	}

	if payload.Error != "" {
		return false, true, fmt.Errorf("%s", payload.Error)
	}

	// Absent field means an older host: assume the firewall is on.
	fwEnabled := payload.FirewallEnabled == nil || *payload.FirewallEnabled

	return payload.Approved, fwEnabled, nil
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
