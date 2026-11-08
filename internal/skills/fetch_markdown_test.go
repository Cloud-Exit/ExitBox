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
	"sort"
	"strings"
	"testing"
)

func TestFetchMarkdown_LocalDir(t *testing.T) {
	dir := t.TempDir()
	for rel, body := range map[string]string{
		"a.md":        "a",
		"sub/b.md":    "b",
		"notes.txt":   "x",
		".git/c.md":   "hidden",
		"sub/.d/e.md": "hidden",
	} {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := FetchMarkdown(dir)
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "a.md,sub/b.md" {
		t.Errorf("keys = %v", keys)
	}
}

func TestFetchMarkdown_LocalFileAndEmptyDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.md")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := FetchMarkdown(p)
	if err != nil || string(files["x.md"]) != "x" {
		t.Errorf("files = %v, err = %v", files, err)
	}
	if _, err := FetchMarkdown(t.TempDir()); err == nil {
		t.Error("empty dir should fail")
	}
}

func TestFetchMarkdown_GitHubTree(t *testing.T) {
	orig := httpGet
	t.Cleanup(func() { httpGet = orig })
	httpGet = func(url string) ([]byte, error) {
		switch url {
		case "https://api.github.com/repos/o/r/contents/agents?ref=main":
			return []byte(`[{"name":"a.md","type":"file","download_url":"https://dl/a.md"},
				{"name":"README.txt","type":"file","download_url":"https://dl/readme"},
				{"name":"team","type":"dir"}]`), nil
		case "https://api.github.com/repos/o/r/contents/agents/team?ref=main":
			return []byte(`[{"name":"b.md","type":"file","download_url":"https://dl/b.md"}]`), nil
		case "https://dl/a.md":
			return []byte("A"), nil
		case "https://dl/b.md":
			return []byte("B"), nil
		}
		return nil, fmt.Errorf("unexpected URL %s", url)
	}
	files, err := FetchMarkdown("https://github.com/o/r/tree/main/agents")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 || string(files["a.md"]) != "A" || string(files["team/b.md"]) != "B" {
		t.Errorf("files = %v", files)
	}
}

func TestFetchMarkdown_GitHubBlobAndRaw(t *testing.T) {
	orig := httpGet
	t.Cleanup(func() { httpGet = orig })
	httpGet = func(url string) ([]byte, error) {
		switch url {
		case "https://raw.githubusercontent.com/o/r/main/agents/rev.md":
			return []byte("blob"), nil
		case "https://example.com/x/raw.md?token=1":
			return []byte("raw"), nil
		}
		return nil, fmt.Errorf("unexpected URL %s", url)
	}
	files, err := FetchMarkdown("https://github.com/o/r/blob/main/agents/rev.md")
	if err != nil || string(files["rev.md"]) != "blob" {
		t.Errorf("blob: files = %v, err = %v", files, err)
	}
	files, err = FetchMarkdown("https://example.com/x/raw.md?token=1")
	if err != nil || string(files["raw.md"]) != "raw" {
		t.Errorf("raw: files = %v, err = %v", files, err)
	}
}
