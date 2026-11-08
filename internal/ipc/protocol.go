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
	// FirewallEnabled reports whether a Squid proxy is actually filtering
	// this container. False means the request was approved automatically
	// because every domain is already reachable.
	FirewallEnabled bool `json:"firewall_enabled"`
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

// VaultSetRequest is the payload for "vault_set" requests.
type VaultSetRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// VaultSetResponse is the payload for "vault_set" responses.
type VaultSetResponse struct {
	Approved bool   `json:"approved"`
	Error    string `json:"error,omitempty"`
}

// KVGetRequest is the payload for "kv_get" requests.
type KVGetRequest struct {
	Key string `json:"key"`
}

// KVGetResponse is the payload for "kv_get" responses.
type KVGetResponse struct {
	Value string `json:"value,omitempty"`
	Found bool   `json:"found"`
	Error string `json:"error,omitempty"`
}

// KVSetRequest is the payload for "kv_set" requests.
type KVSetRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KVSetResponse is the payload for "kv_set" responses.
type KVSetResponse struct {
	Error string `json:"error,omitempty"`
}

// KVDeleteRequest is the payload for "kv_delete" requests.
type KVDeleteRequest struct {
	Key string `json:"key"`
}

// KVDeleteResponse is the payload for "kv_delete" responses.
type KVDeleteResponse struct {
	Error string `json:"error,omitempty"`
}

// KVListRequest is the payload for "kv_list" requests.
type KVListRequest struct {
	Prefix string `json:"prefix"`
}

// KVListEntry is a single key-value pair returned by kv_list.
type KVListEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// KVListResponse is the payload for "kv_list" responses.
type KVListResponse struct {
	Entries []KVListEntry `json:"entries,omitempty"`
	Error   string        `json:"error,omitempty"`
}
