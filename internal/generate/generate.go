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

package generate

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/cloud-exit/exitbox/internal/agents"
)

// ModelInfo describes a model returned by the /models endpoint.
type ModelInfo struct {
	ID string `json:"id"`
}

// modelsResponse is the OpenAI-compatible /models response.
type modelsResponse struct {
	Data []ModelInfo `json:"data"`
}

// TestServer sends GET {baseURL}/models and returns discovered models.
func TestServer(baseURL, apiKey string) ([]ModelInfo, error) {
	url := baseURL + "/models"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connecting to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var result modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("server returned no models")
	}

	return result.Data, nil
}

// MergeJSON deep-merges generated values into existing, returning a new map.
// Generated values override existing at leaf level; existing keys not in
// generated are preserved.
func MergeJSON(existing, generated map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Copy existing keys.
	for k, v := range existing {
		result[k] = v
	}

	// Merge generated keys.
	for k, genVal := range generated {
		existVal, exists := result[k]
		if !exists {
			result[k] = genVal
			continue
		}

		// If both are maps, recurse.
		existMap, existIsMap := existVal.(map[string]interface{})
		genMap, genIsMap := genVal.(map[string]interface{})
		if existIsMap && genIsMap {
			result[k] = MergeJSON(existMap, genMap)
		} else {
			result[k] = genVal
		}
	}

	return result
}

// ConfigSerializer is implemented by agents whose on-disk config format is not
// JSON (e.g. TOML). When an agent implements it, WriteAgentConfig renders the
// config via SerializeConfig instead of the default JSON merge-and-write.
type ConfigSerializer interface {
	SerializeConfig(data map[string]interface{}) ([]byte, error)
}

// WriteAgentConfig writes an agent's generated config to path. If the agent
// implements ConfigSerializer, its serialized output is written verbatim;
// otherwise the data is deep-merged into any existing JSON via WriteConfig.
func WriteAgentConfig(agt interface{}, path string, data map[string]interface{}) error {
	if s, ok := agt.(ConfigSerializer); ok {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
		out, err := s.SerializeConfig(data)
		if err != nil {
			return fmt.Errorf("serializing config: %w", err)
		}
		return os.WriteFile(path, out, 0644)
	}
	return WriteConfig(path, data)
}

// WriteConfig reads an existing config file (if present), deep-merges the
// generated data into it, and writes the result back with 2-space indentation.
func WriteConfig(path string, data map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	existing := make(map[string]interface{})
	if raw, err := os.ReadFile(path); err == nil {
		if jsonErr := json.Unmarshal(raw, &existing); jsonErr != nil {
			// Existing file is not valid JSON; overwrite it.
			existing = make(map[string]interface{})
		}
	}

	merged := MergeJSON(existing, data)

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	out = append(out, '\n')

	return os.WriteFile(path, out, 0644)
}

// ExtractConfigHosts reads the agent config file and extracts any non-local
// server hosts (with port) that the agent is configured to connect to.
func ExtractConfigHosts(agentDir, agentName string) []string {
	agt := agents.Get(agentName)
	if agt == nil {
		return nil
	}
	path := agt.ConfigFilePath(agentDir)
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}

	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil
	}

	urls := agt.ExtractConfigServerURLs(data)

	return deduplicateHosts(urls)
}

// deduplicateHosts extracts host:port from URLs and returns a unique list,
// skipping localhost and 127.0.0.1.
func deduplicateHosts(urls []string) []string {
	seen := make(map[string]struct{})
	var hosts []string
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil {
			continue
		}
		host := u.Host
		if host == "" {
			continue
		}
		// Skip loopback addresses.
		hostname := u.Hostname()
		if hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}
