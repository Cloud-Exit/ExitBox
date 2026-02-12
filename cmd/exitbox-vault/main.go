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

// exitbox-vault is a standalone binary for accessing vault secrets from
// inside an ExitBox container. It communicates with the host via a Unix
// domain socket using JSON-lines protocol.
//
// Usage:
//
//	exitbox-vault get <KEY>     # prints value to stdout
//	exitbox-vault list          # prints key names, one per line
//	exitbox-vault env           # prints KEY=VALUE pairs (for eval)
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

type response struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

type vaultGetPayload struct {
	Key string `json:"key"`
}

type vaultGetResponse struct {
	Value    string `json:"value,omitempty"`
	Approved bool   `json:"approved"`
	Error    string `json:"error,omitempty"`
}

type vaultListResponse struct {
	Keys     []string `json:"keys,omitempty"`
	Approved bool     `json:"approved"`
	Error    string   `json:"error,omitempty"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "get":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: exitbox-vault get <KEY>")
			os.Exit(1)
		}
		cmdGet(os.Args[2])
	case "list":
		cmdList()
	case "env":
		cmdEnv()
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  exitbox-vault get <KEY>     # prints secret value to stdout")
	fmt.Fprintln(os.Stderr, "  exitbox-vault list          # prints key names, one per line")
	fmt.Fprintln(os.Stderr, "  exitbox-vault env           # prints KEY=VALUE pairs (for eval)")
}

func cmdGet(key string) {
	resp, err := sendVaultGet(key)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if resp.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}
	if !resp.Approved {
		fmt.Fprintln(os.Stderr, "Denied: host user rejected the secret access request")
		os.Exit(1)
	}
	fmt.Print(resp.Value)
}

func cmdList() {
	resp, err := sendVaultList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if resp.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", resp.Error)
		os.Exit(1)
	}
	if !resp.Approved {
		fmt.Fprintln(os.Stderr, "Denied: host user rejected the secret access request")
		os.Exit(1)
	}
	for _, k := range resp.Keys {
		fmt.Println(k)
	}
}

func cmdEnv() {
	listResp, err := sendVaultList()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if listResp.Error != "" {
		fmt.Fprintf(os.Stderr, "Error: %s\n", listResp.Error)
		os.Exit(1)
	}
	if !listResp.Approved {
		fmt.Fprintln(os.Stderr, "Denied: host user rejected the secret access request")
		os.Exit(1)
	}

	hasFailure := false
	for _, key := range listResp.Keys {
		resp, getErr := sendVaultGet(key)
		if getErr != nil {
			fmt.Fprintf(os.Stderr, "Error getting %s: %v\n", key, getErr)
			hasFailure = true
			continue
		}
		if resp.Error != "" {
			fmt.Fprintf(os.Stderr, "Error getting %s: %s\n", key, resp.Error)
			hasFailure = true
			continue
		}
		if !resp.Approved {
			fmt.Fprintf(os.Stderr, "Denied: %s\n", key)
			hasFailure = true
			continue
		}
		fmt.Printf("%s=%s\n", key, resp.Value)
	}

	if hasFailure {
		os.Exit(1)
	}
}

func sendVaultGet(key string) (*vaultGetResponse, error) {
	socketPath := os.Getenv("EXITBOX_IPC_SOCKET")
	if socketPath == "" {
		socketPath = "/run/exitbox/host.sock"
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("IPC socket not available. Vault requires the IPC server to be running")
	}
	defer conn.Close()

	id := randomID()
	req := request{
		Type: "vault_get",
		ID:   id,
		Payload: vaultGetPayload{
			Key: key,
		},
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if scanErr := scanner.Err(); scanErr != nil {
			return nil, scanErr
		}
		return nil, fmt.Errorf("no response from host")
	}

	var resp response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return nil, err
	}

	var payload vaultGetResponse
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func sendVaultList() (*vaultListResponse, error) {
	socketPath := os.Getenv("EXITBOX_IPC_SOCKET")
	if socketPath == "" {
		socketPath = "/run/exitbox/host.sock"
	}

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("IPC socket not available. Vault requires the IPC server to be running")
	}
	defer conn.Close()

	id := randomID()
	req := request{
		Type:    "vault_list",
		ID:      id,
		Payload: struct{}{},
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	data = append(data, '\n')

	if _, err := conn.Write(data); err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		if scanErr := scanner.Err(); scanErr != nil {
			return nil, scanErr
		}
		return nil, fmt.Errorf("no response from host")
	}

	var resp response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		return nil, err
	}

	var payload vaultListResponse
	if err := json.Unmarshal(resp.Payload, &payload); err != nil {
		return nil, err
	}

	return &payload, nil
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
