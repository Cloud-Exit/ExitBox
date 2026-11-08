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

package opencode

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestGenerateConfig_OpenCode(t *testing.T) {
	t.Run("without compaction", func(t *testing.T) {
		cfg := config.ServerConfig{
			ProviderID:   "local",
			ProviderName: "Local Server",
			BaseURL:      "http://localhost:8080/v1",
			ModelID:      "qwen3",
			ModelName:    "Qwen3",
		}

		o := &OpenCode{}
		result, err := o.GenerateConfig(cfg)
		if err != nil {
			t.Fatalf("GenerateConfig error: %v", err)
		}

		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		if parsed["$schema"] != "https://opencode.ai/config.json" {
			t.Error("missing or wrong $schema")
		}

		providers, ok := parsed["provider"].(map[string]interface{})
		if !ok {
			t.Fatal("missing provider")
		}
		local, ok := providers["local"].(map[string]interface{})
		if !ok {
			t.Fatal("missing local provider")
		}
		if local["name"] != "Local Server" {
			t.Errorf("expected Local Server, got %v", local["name"])
		}

		if parsed["model"] != "local/qwen3" {
			t.Errorf("expected local/qwen3, got %v", parsed["model"])
		}

		if _, ok := parsed["compaction"]; ok {
			t.Error("compaction should not be present when disabled")
		}
	})

	t.Run("with compaction", func(t *testing.T) {
		cfg := config.ServerConfig{
			ProviderID:   "local",
			ProviderName: "Local Server",
			BaseURL:      "http://localhost:8080/v1",
			ModelID:      "qwen3",
			ModelName:    "Qwen3",
			Compaction:   true,
		}

		o := &OpenCode{}
		result, err := o.GenerateConfig(cfg)
		if err != nil {
			t.Fatalf("GenerateConfig error: %v", err)
		}

		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("marshal error: %v", err)
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}

		comp, ok := parsed["compaction"].(map[string]interface{})
		if !ok {
			t.Fatal("missing compaction")
		}
		if comp["auto"] != true {
			t.Errorf("expected compaction.auto=true, got %v", comp["auto"])
		}
		if comp["prune"] != true {
			t.Errorf("expected compaction.prune=true, got %v", comp["prune"])
		}
	})
}

func TestConfigFilePath_OpenCode(t *testing.T) {
	o := &OpenCode{}
	got := o.ConfigFilePath("/base")
	want := filepath.Join("/base", ".config", "opencode", "opencode.json")
	if got != want {
		t.Errorf("ConfigFilePath(/base) = %q, want %q", got, want)
	}
}

func TestExtractConfigServerURLs_OpenCode(t *testing.T) {
	o := &OpenCode{}

	if urls := o.ExtractConfigServerURLs(map[string]interface{}{"model": "a/b"}); len(urls) != 0 {
		t.Errorf("expected no URLs, got %v", urls)
	}

	var data map[string]interface{}
	raw := `{
		"provider": {"v1": {"options": {"baseURL": "http://v1:8080/v1"}}, "bad": "x"},
		"providers": {
			"v2": {"package": "aisdk:@ai-sdk/openai-compatible", "settings": {"baseURL": "http://v2:8080/v1"}},
			"empty": {"settings": {"baseURL": ""}}
		}
	}`
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	urls := o.ExtractConfigServerURLs(data)
	sort.Strings(urls)
	if len(urls) != 2 || urls[0] != "http://v1:8080/v1" || urls[1] != "http://v2:8080/v1" {
		t.Errorf("ExtractConfigServerURLs() = %v, want v1 and v2 base URLs", urls)
	}
}
