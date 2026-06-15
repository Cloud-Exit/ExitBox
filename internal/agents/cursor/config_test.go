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

package cursor

import (
	"path/filepath"
	"testing"

	"github.com/cloud-exit/exitbox/internal/config"
)

func TestGenerateConfig_Cursor(t *testing.T) {
	c := &Cursor{}
	result, err := c.GenerateConfig(config.ServerConfig{})
	if err != nil {
		t.Fatalf("GenerateConfig error: %v", err)
	}
	if result["version"] != 1 {
		t.Errorf("expected version 1, got %v", result["version"])
	}
}

func TestConfigFilePath_Cursor(t *testing.T) {
	c := &Cursor{}
	got := c.ConfigFilePath("/base")
	want := filepath.Join("/base", ".cursor", "cli-config.json")
	if got != want {
		t.Errorf("ConfigFilePath(/base) = %q, want %q", got, want)
	}
}

func TestExtractConfigServerURLs_Cursor(t *testing.T) {
	c := &Cursor{}
	if urls := c.ExtractConfigServerURLs(map[string]interface{}{"version": 1}); urls != nil {
		t.Errorf("expected no URLs (fixed Cursor backend), got %v", urls)
	}
}
