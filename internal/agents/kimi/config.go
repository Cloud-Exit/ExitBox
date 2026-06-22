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

package kimi

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
)

// GenerateConfig produces a Kimi Code CLI config as a logical map. Kimi Code CLI is
// configured through TOML (~/.kimi-code/config.toml), so the map is serialized to TOML
// by SerializeConfig rather than the framework's default JSON writer. The generated
// config points Kimi at the given OpenAI-compatible server as a provider + model.
func (k *Kimi) GenerateConfig(cfg config.ServerConfig) (map[string]interface{}, error) {
	providerKey := cfg.ProviderID
	if providerKey == "" {
		providerKey = "local"
	}
	modelKey := cfg.ModelID
	if modelKey == "" {
		modelKey = "default"
	}

	provider := map[string]interface{}{
		"type":     "openai",
		"base_url": cfg.BaseURL,
	}
	if cfg.APIKey != "" && cfg.VaultKeyName == "" {
		provider["api_key"] = cfg.APIKey
	}

	return map[string]interface{}{
		"default_model": modelKey,
		"providers": map[string]interface{}{
			providerKey: provider,
		},
		"models": map[string]interface{}{
			modelKey: map[string]interface{}{
				"provider": providerKey,
				"model":    modelKey,
				// max_context_size is required by Kimi Code CLI. Use a sane default;
				// users can raise/lower it in config.toml to match their server.
				"max_context_size": kimiDefaultContextSize,
			},
		},
	}, nil
}

// kimiDefaultContextSize is the default model context window written to config.toml.
// Kimi Code CLI requires models.<alias>.max_context_size; 128K is a safe baseline for
// most served models and is easily overridden by editing the generated config.
const kimiDefaultContextSize = 131072

// SerializeConfig renders the config map as TOML (Kimi Code CLI's native format).
// It implements generate.ConfigSerializer so `exitbox generate kimi` writes a valid
// ~/.kimi-code/config.toml instead of JSON.
func (k *Kimi) SerializeConfig(data map[string]interface{}) ([]byte, error) {
	var b strings.Builder

	if dm, ok := data["default_model"].(string); ok && dm != "" {
		fmt.Fprintf(&b, "default_model = %s\n", tomlString(dm))
	}

	if providers, ok := data["providers"].(map[string]interface{}); ok {
		for _, key := range sortedKeys(providers) {
			p, ok := providers[key].(map[string]interface{})
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "\n[providers.%s]\n", tomlKey(key))
			writeStringField(&b, "type", p["type"])
			writeStringField(&b, "base_url", p["base_url"])
			writeStringField(&b, "api_key", p["api_key"])
		}
	}

	if models, ok := data["models"].(map[string]interface{}); ok {
		for _, key := range sortedKeys(models) {
			m, ok := models[key].(map[string]interface{})
			if !ok {
				continue
			}
			fmt.Fprintf(&b, "\n[models.%s]\n", tomlKey(key))
			writeStringField(&b, "provider", m["provider"])
			writeStringField(&b, "model", m["model"])
			writeIntField(&b, "max_context_size", m["max_context_size"])
		}
	}

	return []byte(b.String()), nil
}

// writeIntField emits an unquoted TOML integer field. It accepts the common numeric
// types a config map may hold (int from generation, float64 from JSON round-trips).
func writeIntField(b *strings.Builder, name string, val interface{}) {
	switch v := val.(type) {
	case int:
		fmt.Fprintf(b, "%s = %d\n", name, v)
	case int64:
		fmt.Fprintf(b, "%s = %d\n", name, v)
	case float64:
		fmt.Fprintf(b, "%s = %d\n", name, int64(v))
	}
}

func writeStringField(b *strings.Builder, name string, val interface{}) {
	s, ok := val.(string)
	if !ok || s == "" {
		return
	}
	fmt.Fprintf(b, "%s = %s\n", name, tomlString(s))
}

// tomlString renders a Go string as a TOML basic string with the required escapes.
func tomlString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// tomlKey renders a map key as a quoted TOML key, always valid for dotted table
// headers (e.g. [providers."gpt-4.1"]).
func tomlKey(s string) string {
	return tomlString(s)
}

// sortedKeys returns map keys in deterministic order so generated TOML is stable.
func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// insertion sort keeps it dependency-free and the maps here are tiny.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// LogSearchDirs returns directories to search for Kimi Code CLI log files.
func (k *Kimi) LogSearchDirs(home, agentCfgDir string) []string {
	return []string{
		filepath.Join(home, ".kimi-code", "logs"),
		filepath.Join(home, ".kimi-code"),
		filepath.Join(agentCfgDir, ".kimi-code", "logs"),
		filepath.Join(agentCfgDir, ".kimi-code"),
	}
}

func (k *Kimi) ConfigFilePath(agentDir string) string {
	return filepath.Join(agentDir, ".kimi-code", "config.toml")
}

// ExtractConfigServerURLs walks providers.*.base_url in the parsed config map.
func (k *Kimi) ExtractConfigServerURLs(data map[string]interface{}) []string {
	providers, ok := data["providers"].(map[string]interface{})
	if !ok {
		return nil
	}
	var urls []string
	for _, key := range sortedKeys(providers) {
		p, ok := providers[key].(map[string]interface{})
		if !ok {
			continue
		}
		if baseURL, ok := p["base_url"].(string); ok && baseURL != "" {
			urls = append(urls, baseURL)
		}
	}
	return urls
}
