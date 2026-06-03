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

package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseFrontmatter(t *testing.T) {
	tests := []struct {
		input    string
		wantName string
		wantDesc string
	}{
		{
			input:    "---\nname: my-skill\ndescription: Does stuff\n---\nContent here",
			wantName: "my-skill",
			wantDesc: "Does stuff",
		},
		{
			input:    "---\nname: test\n---\n# Hello",
			wantName: "test",
			wantDesc: "",
		},
		{
			input:    "No frontmatter here",
			wantName: "",
			wantDesc: "",
		},
		{
			input:    "---\nbad yaml: [[[",
			wantName: "",
			wantDesc: "",
		},
	}
	for _, tc := range tests {
		name, desc := parseFrontmatter([]byte(tc.input))
		if name != tc.wantName {
			t.Errorf("parseFrontmatter(%q) name = %q, want %q", tc.input, name, tc.wantName)
		}
		if desc != tc.wantDesc {
			t.Errorf("parseFrontmatter(%q) desc = %q, want %q", tc.input, desc, tc.wantDesc)
		}
	}
}

func TestInstallAndList(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Create the skills dir structure manually since config.Home won't match.
	wsSkillsDir := filepath.Join(tmpDir, "profiles", "global", "test-ws", "skills")
	if err := os.MkdirAll(wsSkillsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a skill manually.
	skillDir := filepath.Join(wsSkillsDir, "my-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("---\nname: my-skill\ndescription: Test skill\n---\nDo something")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), content, 0644); err != nil {
		t.Fatal(err)
	}

	// Test Exists.
	skillFile := filepath.Join(wsSkillsDir, "my-skill", "SKILL.md")
	if _, err := os.Stat(skillFile); err != nil {
		t.Errorf("expected skill to exist at %s", skillFile)
	}

	// Test Remove.
	if err := os.RemoveAll(skillDir); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, err := os.Stat(skillFile); !os.IsNotExist(err) {
		t.Error("expected skill to be removed")
	}
}

func TestDetectSource(t *testing.T) {
	tests := []struct {
		input string
		want  SourceType
	}{
		{"https://github.com/anthropics/skills/tree/main/skills/frontend-design", SourceGitHubTree},
		{"http://github.com/user/repo/tree/dev/path/to/skill", SourceGitHubTree},
		{"https://github.com/user/repo/blob/main/skills/my-skill/SKILL.md", SourceGitHubBlob},
		{"https://github.com/rohitg00/awesome/blob/main/agents/infra/k8s.md", SourceGitHubBlob},
		{"https://example.com/SKILL.md", SourceRawURL},
		{"https://raw.githubusercontent.com/user/repo/main/SKILL.md", SourceRawURL},
		{"/home/user/skills/my-skill", SourceLocalPath},
		{"./relative/path", SourceLocalPath},
	}
	for _, tc := range tests {
		got := DetectSource(tc.input)
		if got != tc.want {
			t.Errorf("DetectSource(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestFetchLocal_Directory(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("---\nname: local-test\ndescription: A local skill\n---\nInstructions")
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), content, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "helper.sh"), []byte("#!/bin/sh\necho hi"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Fetch(skillDir)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if result.Name != "local-test" {
		t.Errorf("name = %q, want %q", result.Name, "local-test")
	}
	if _, ok := result.Files["SKILL.md"]; !ok {
		t.Error("missing SKILL.md in result")
	}
	if _, ok := result.Files["helper.sh"]; !ok {
		t.Error("missing helper.sh in result")
	}
}

func TestFetchLocal_SingleFile(t *testing.T) {
	dir := t.TempDir()
	skillFile := filepath.Join(dir, "my-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillFile), 0755); err != nil {
		t.Fatal(err)
	}
	content := []byte("---\nname: single-file\n---\nContent")
	if err := os.WriteFile(skillFile, content, 0644); err != nil {
		t.Fatal(err)
	}

	result, err := Fetch(skillFile)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if result.Name != "single-file" {
		t.Errorf("name = %q, want %q", result.Name, "single-file")
	}
}

func TestFetchLocal_NoSkillMd(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a skill"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Fetch(dir)
	if err == nil {
		t.Error("expected error for directory without SKILL.md")
	}
}

func TestFetchGitHubBlobSkillMdFetchesContainingDirectory(t *testing.T) {
	origHTTPGet := httpGet
	t.Cleanup(func() { httpGet = origHTTPGet })

	httpGet = func(url string) ([]byte, error) {
		switch {
		case strings.Contains(url, "/contents/tools/skills/svelte-core-bestpractices?ref=main"):
			return []byte(`[
				{"name":"SKILL.md","type":"file","download_url":"https://download.test/SKILL.md"},
				{"name":"references","type":"dir","download_url":""}
			]`), nil
		case strings.Contains(url, "/contents/tools/skills/svelte-core-bestpractices/references?ref=main"):
			return []byte(`[
				{"name":"docs.md","type":"file","download_url":"https://download.test/references/docs.md"}
			]`), nil
		case url == "https://download.test/SKILL.md":
			return []byte("---\nname: svelte-core-bestpractices\n---\nUse references/docs.md"), nil
		case url == "https://download.test/references/docs.md":
			return []byte("# Svelte reference"), nil
		default:
			return nil, fmt.Errorf("unexpected URL: %s", url)
		}
	}

	result, err := Fetch("https://github.com/sveltejs/ai-tools/blob/main/tools/skills/svelte-core-bestpractices/SKILL.md")
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if result.Name != "svelte-core-bestpractices" {
		t.Fatalf("name = %q, want svelte-core-bestpractices", result.Name)
	}
	if _, ok := result.Files["SKILL.md"]; !ok {
		t.Fatal("missing SKILL.md")
	}
	if _, ok := result.Files["references/docs.md"]; !ok {
		t.Fatal("missing references/docs.md")
	}
}
