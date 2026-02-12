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

import "encoding/json"

// Request is a message sent from the container to the host.
type Request struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload"`
}

// Response is a message sent from the host back to the container.
type Response struct {
	Type    string      `json:"type"`
	ID      string      `json:"id"`
	Payload interface{} `json:"payload"`
}

// AllowDomainRequest is the payload for "allow_domain" requests.
type AllowDomainRequest struct {
	Domain string `json:"domain"`
}

// AllowDomainResponse is the payload for "allow_domain" responses.
type AllowDomainResponse struct {
	Approved bool   `json:"approved"`
	Error    string `json:"error,omitempty"`
}

// VaultGetRequest is the payload for "vault_get" requests.
type VaultGetRequest struct {
	Key string `json:"key"`
}

// VaultGetResponse is the payload for "vault_get" responses.
type VaultGetResponse struct {
	Value    string `json:"value,omitempty"`
	Approved bool   `json:"approved"`
	Error    string `json:"error,omitempty"`
}

// VaultListRequest is the payload for "vault_list" requests.
type VaultListRequest struct{}

// VaultListResponse is the payload for "vault_list" responses.
type VaultListResponse struct {
	Keys     []string `json:"keys,omitempty"`
	Approved bool     `json:"approved"`
	Error    string   `json:"error,omitempty"`
}
