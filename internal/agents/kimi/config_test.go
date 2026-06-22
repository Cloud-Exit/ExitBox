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
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestGenerateConfig_Kimi(t *testing.T) {
	cfg := config.ServerConfig{
		ProviderID: "local",
		BaseURL:    "http://localhost:8080/v1",
		ModelID:    "kimi-for-coding",
	}
	k := &Kimi{}
	result, err := k.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig error: %v", err)
	}
	if result["default_model"] != "kimi-for-coding" {
		t.Errorf("expected default_model kimi-for-coding, got %v", result["default_model"])
	}
	providers, ok := result["providers"].(map[string]interface{})
	if !ok {
		t.Fatal("missing providers")
	}
	p, ok := providers["local"].(map[string]interface{})
	if !ok {
		t.Fatal("missing providers.local")
	}
	if p["base_url"] != "http://localhost:8080/v1" {
		t.Errorf("expected base_url, got %v", p["base_url"])
	}
	if p["type"] != "openai" {
		t.Errorf("expected type openai, got %v", p["type"])
	}
}

func TestSerializeConfig_Kimi(t *testing.T) {
	cfg := config.ServerConfig{
		ProviderID: "local",
		BaseURL:    "http://localhost:8080/v1",
		ModelID:    "kimi-for-coding",
		APIKey:     "sk-test",
	}
	k := &Kimi{}
	data, err := k.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig error: %v", err)
	}
	out, err := k.SerializeConfig(data)
	if err != nil {
		t.Fatalf("SerializeConfig error: %v", err)
	}
	toml := string(out)

	for _, want := range []string{
		`default_model = "kimi-for-coding"`,
		`[providers."local"]`,
		`type = "openai"`,
		`base_url = "http://localhost:8080/v1"`,
		`api_key = "sk-test"`,
		`[models."kimi-for-coding"]`,
		`provider = "local"`,
		`model = "kimi-for-coding"`,
		`max_context_size = 131072`,
	} {
		if !strings.Contains(toml, want) {
			t.Errorf("serialized TOML missing %q\n---\n%s", want, toml)
		}
	}
}

func TestSerializeConfig_Kimi_NoAPIKeyWhenVault(t *testing.T) {
	cfg := config.ServerConfig{
		ProviderID:   "local",
		BaseURL:      "http://localhost:8080/v1",
		ModelID:      "m",
		APIKey:       "sk-secret",
		VaultKeyName: "MY_KEY",
	}
	k := &Kimi{}
	data, _ := k.GenerateConfig(cfg)
	out, err := k.SerializeConfig(data)
	if err != nil {
		t.Fatalf("SerializeConfig error: %v", err)
	}
	if strings.Contains(string(out), "sk-secret") {
		t.Errorf("API key must not be written to config when stored in vault:\n%s", out)
	}
}

func TestConfigFilePath_Kimi(t *testing.T) {
	k := &Kimi{}
	got := k.ConfigFilePath("/base")
	want := filepath.Join("/base", ".kimi-code", "config.toml")
	if got != want {
		t.Errorf("ConfigFilePath(/base) = %q, want %q", got, want)
	}
}

func TestExtractConfigServerURLs_Kimi(t *testing.T) {
	k := &Kimi{}

	t.Run("empty", func(t *testing.T) {
		if urls := k.ExtractConfigServerURLs(map[string]interface{}{}); len(urls) != 0 {
			t.Errorf("expected no URLs, got %v", urls)
		}
	})

	t.Run("provider base_url", func(t *testing.T) {
		data := map[string]interface{}{
			"providers": map[string]interface{}{
				"local": map[string]interface{}{
					"base_url": "https://api.example.com/v1",
				},
			},
		}
		urls := k.ExtractConfigServerURLs(data)
		if len(urls) != 1 || urls[0] != "https://api.example.com/v1" {
			t.Errorf("expected one URL https://api.example.com/v1, got %v", urls)
		}
	})
}
