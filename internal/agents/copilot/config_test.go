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

package copilot

import (
	"path/filepath"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestGenerateConfig_Copilot(t *testing.T) {
	c := &Copilot{}

	result, err := c.GenerateConfig(config.ServerConfig{ModelID: "gpt-5"})
	if err != nil {
		t.Fatalf("GenerateConfig error: %v", err)
	}
	if result["model"] != "gpt-5" {
		t.Errorf("expected model gpt-5, got %v", result["model"])
	}
}

func TestGenerateConfig_Copilot_DefaultModel(t *testing.T) {
	c := &Copilot{}
	result, err := c.GenerateConfig(config.ServerConfig{})
	if err != nil {
		t.Fatalf("GenerateConfig error: %v", err)
	}
	if result["model"] != "auto" {
		t.Errorf("expected model auto when unset, got %v", result["model"])
	}
}

func TestConfigFilePath_Copilot(t *testing.T) {
	c := &Copilot{}
	got := c.ConfigFilePath("/base")
	want := filepath.Join("/base", ".copilot", "settings.json")
	if got != want {
		t.Errorf("ConfigFilePath(/base) = %q, want %q", got, want)
	}
}

func TestExtractConfigServerURLs_Copilot(t *testing.T) {
	c := &Copilot{}
	if urls := c.ExtractConfigServerURLs(map[string]interface{}{"model": "gpt-5"}); urls != nil {
		t.Errorf("expected no URLs (fixed GitHub backend), got %v", urls)
	}
}
