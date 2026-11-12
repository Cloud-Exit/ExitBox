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

package claude

import (
	"path/filepath"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestGenerateConfig_Claude(t *testing.T) {
	envOf := func(t *testing.T, m map[string]interface{}) map[string]interface{} {
		t.Helper()
		e, ok := m["env"].(map[string]interface{})
		if !ok {
			t.Fatalf("env block missing: %v", m)
		}
		return e
	}

	t.Run("with api key no vault", func(t *testing.T) {
		cfg := config.ServerConfig{
			BaseURL: "http://localhost:8080/v1",
			ModelID: "gpt-4",
			APIKey:  "sk-test",
		}

		c := &Claude{}
		result, err := c.GenerateConfig(cfg)
		if err != nil {
			t.Fatalf("GenerateConfig error: %v", err)
		}

		env := envOf(t, result)
		if env["ANTHROPIC_BASE_URL"] != "http://localhost:8080" {
			t.Errorf("wrong ANTHROPIC_BASE_URL: %v", env["ANTHROPIC_BASE_URL"])
		}
		if env["ANTHROPIC_AUTH_TOKEN"] != "sk-test" {
			t.Errorf("wrong ANTHROPIC_AUTH_TOKEN: %v", env["ANTHROPIC_AUTH_TOKEN"])
		}
		if result["model"] != "gpt-4" {
			t.Errorf("wrong model: %v", result["model"])
		}
		for _, legacy := range []string{"apiBaseUrl", "apiKey"} {
			if _, ok := result[legacy]; ok {
				t.Errorf("%s is not a Claude Code setting and must not be written", legacy)
			}
		}
	})

	t.Run("with vault key", func(t *testing.T) {
		cfg := config.ServerConfig{
			BaseURL:      "http://localhost:8080/v1",
			ModelID:      "gpt-4",
			APIKey:       "sk-test",
			VaultKeyName: "API_KEY",
		}

		c := &Claude{}
		result, err := c.GenerateConfig(cfg)
		if err != nil {
			t.Fatalf("GenerateConfig error: %v", err)
		}

		if _, ok := envOf(t, result)["ANTHROPIC_AUTH_TOKEN"]; ok {
			t.Error("token should not be set when vault key is used")
		}
	})

	t.Run("no api key", func(t *testing.T) {
		cfg := config.ServerConfig{
			BaseURL: "http://localhost:8080/",
			ModelID: "gpt-4",
		}

		c := &Claude{}
		result, err := c.GenerateConfig(cfg)
		if err != nil {
			t.Fatalf("GenerateConfig error: %v", err)
		}

		env := envOf(t, result)
		if _, ok := env["ANTHROPIC_AUTH_TOKEN"]; ok {
			t.Error("token should not be set when empty")
		}
		if env["ANTHROPIC_BASE_URL"] != "http://localhost:8080" {
			t.Errorf("wrong ANTHROPIC_BASE_URL: %v", env["ANTHROPIC_BASE_URL"])
		}
	})
}

func TestExtractConfigServerURLs_Claude(t *testing.T) {
	c := &Claude{}
	generated, err := c.GenerateConfig(config.ServerConfig{BaseURL: "http://10.0.0.5:8080/v1", ModelID: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ExtractConfigServerURLs(generated); len(got) != 1 || got[0] != "http://10.0.0.5:8080" {
		t.Errorf("generated config URLs = %v", got)
	}
	legacy := map[string]interface{}{"apiBaseUrl": "http://old:9000"}
	if got := c.ExtractConfigServerURLs(legacy); len(got) != 1 || got[0] != "http://old:9000" {
		t.Errorf("legacy config URLs = %v", got)
	}
	if got := c.ExtractConfigServerURLs(map[string]interface{}{"model": "x"}); got != nil {
		t.Errorf("no URL config = %v", got)
	}
}

func TestConfigFilePath_Claude(t *testing.T) {
	c := &Claude{}
	got := c.ConfigFilePath("/base")
	want := filepath.Join("/base", ".claude", "settings.json")
	if got != want {
		t.Errorf("ConfigFilePath(/base) = %q, want %q", got, want)
	}
}
