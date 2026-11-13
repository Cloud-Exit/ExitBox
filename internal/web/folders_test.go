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

package web

import (
	"net/http"
	"testing"
)

func TestFolders(t *testing.T) {
	_, st, ts := newTestServer(t, Options{})
	dir := st.dirs[0]
	key := sessionKey("work", "claude", dir, "feature-x")

	_, state := do(t, ts, "GET", "/api/state", nil, "")
	if f := state["folders"].([]any); len(f) != 0 {
		t.Fatalf("folders before any: %v", f)
	}

	bad := []map[string]any{
		{"folders": []map[string]any{{"id": "a", "name": "  ", "items": []string{}}}},
		{"folders": []map[string]any{{"id": "../x", "name": "x", "items": []string{}}}},
		{"folders": []map[string]any{{"id": "a", "name": "x"}, {"id": "a", "name": "y"}}},
	}
	for _, b := range bad {
		if resp, _ := do(t, ts, "PUT", "/api/folders", b, ""); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("bad folders %v: %d", b, resp.StatusCode)
		}
	}

	resp, out := do(t, ts, "PUT", "/api/folders", map[string]any{"folders": []map[string]any{
		{"id": "f1", "name": "Billing", "items": []string{key, "not a key"}},
		{"id": "f2", "name": "Infra", "items": []string{key}},
	}}, "")
	if resp.StatusCode != 200 {
		t.Fatalf("put folders: %d %v", resp.StatusCode, out)
	}
	_, state = do(t, ts, "GET", "/api/state", nil, "")
	folders := state["folders"].([]any)
	f1 := folders[0].(map[string]any)
	f2 := folders[1].(map[string]any)
	if f1["name"] != "Billing" || len(f1["items"].([]any)) != 1 || len(f2["items"].([]any)) != 0 {
		t.Fatalf("folders %v; a session sits in one folder and junk keys go", folders)
	}

	// A renamed session stays in its folder.
	resp, _ = do(t, ts, "POST", "/api/saved/rename", map[string]string{"workspace": "work", "agent": "claude", "dir": dir, "name": "feature-x", "newName": "feature-y"}, "")
	if resp.StatusCode != 200 {
		t.Fatalf("rename: %d", resp.StatusCode)
	}
	_, state = do(t, ts, "GET", "/api/state", nil, "")
	items := state["folders"].([]any)[0].(map[string]any)["items"].([]any)
	if len(items) != 1 || items[0] != sessionKey("work", "claude", dir, "feature-y") {
		t.Fatalf("folder items after rename: %v", items)
	}
}

func TestLayoutDocument(t *testing.T) {
	_, _, ts := newTestServer(t, Options{})
	resp, out := do(t, ts, "GET", "/api/layout", nil, "")
	if resp.StatusCode != 200 || len(out) != 0 {
		t.Fatalf("empty layout: %d %v", resp.StatusCode, out)
	}
	doc := map[string]any{"tabs": []any{map[string]any{"id": "main", "name": "Main"}}, "win.abc": map[string]any{"tab": "main"}}
	if resp, _ := do(t, ts, "PUT", "/api/layout", doc, ""); resp.StatusCode != 200 {
		t.Fatalf("put: %d", resp.StatusCode)
	}
	_, out = do(t, ts, "GET", "/api/layout", nil, "")
	if out["win.abc"] == nil || out["tabs"] == nil {
		t.Fatalf("layout after put: %v", out)
	}
	if resp, _ := do(t, ts, "PUT", "/api/layout", []int{1}, ""); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-object layout: %d", resp.StatusCode)
	}
}
