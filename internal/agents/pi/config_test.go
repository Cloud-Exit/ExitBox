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

package pi

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestGenerateConfig_Pi(t *testing.T) {
	cfg := config.ServerConfig{
		ProviderID: "local",
		BaseURL:    "http://10.10.10.185:8088/v1",
		ModelID:    "qwen2.5-coder",
		ModelName:  "Qwen 2.5 Coder",
	}
	p := &Pi{}
	result, err := p.GenerateConfig(cfg)
	if err != nil {
		t.Fatalf("GenerateConfig error: %v", err)
	}

	// Round-trip through JSON to validate the structure the framework writes.
	data, _ := json.Marshal(result)
	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	providers, ok := parsed["providers"].(map[string]interface{})
	if !ok {
		t.Fatal("missing providers")
	}
	prov, ok := providers["local"].(map[string]interface{})
	if !ok {
		t.Fatal("missing providers.local")
	}
	if prov["baseUrl"] != "http://10.10.10.185:8088/v1" {
		t.Errorf("baseUrl = %v", prov["baseUrl"])
	}
	if prov["api"] != "openai-completions" {
		t.Errorf("api = %v, want openai-completions", prov["api"])
	}
	models, ok := prov["models"].([]interface{})
	if !ok || len(models) != 1 {
		t.Fatalf("models should be a single-element array, got %v", prov["models"])
	}
	m := models[0].(map[string]interface{})
	if m["id"] != "qwen2.5-coder" {
		t.Errorf("model id = %v", m["id"])
	}
}

func TestGenerateConfig_Pi_VaultKeyRef(t *testing.T) {
	cfg := config.ServerConfig{
		ProviderID:   "local",
		BaseURL:      "http://localhost:8080/v1",
		ModelID:      "m",
		APIKey:       "sk-secret",
		VaultKeyName: "MY_KEY",
	}
	p := &Pi{}
	result, _ := p.GenerateConfig(cfg)
	data, _ := json.Marshal(result)
	if s := string(data); !strings.Contains(s, "$MY_KEY") || strings.Contains(s, "sk-secret") {
		t.Errorf("vault key must be an $ENV reference, not the literal secret: %s", s)
	}
}

func TestConfigFilePath_Pi(t *testing.T) {
	p := &Pi{}
	got := p.ConfigFilePath("/base")
	want := filepath.Join("/base", ".pi", "agent", "models.json")
	if got != want {
		t.Errorf("ConfigFilePath(/base) = %q, want %q", got, want)
	}
}

func TestExtractConfigServerURLs_Pi(t *testing.T) {
	p := &Pi{}

	t.Run("empty", func(t *testing.T) {
		if urls := p.ExtractConfigServerURLs(map[string]interface{}{}); len(urls) != 0 {
			t.Errorf("expected no URLs, got %v", urls)
		}
	})

	t.Run("provider baseUrl", func(t *testing.T) {
		data := map[string]interface{}{
			"providers": map[string]interface{}{
				"local": map[string]interface{}{
					"baseUrl": "https://api.example.com/v1",
				},
			},
		}
		urls := p.ExtractConfigServerURLs(data)
		if len(urls) != 1 || urls[0] != "https://api.example.com/v1" {
			t.Errorf("expected one URL https://api.example.com/v1, got %v", urls)
		}
	})
}
