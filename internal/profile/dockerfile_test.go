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

package profile

import (
	"strings"
	"testing"
)

func TestGoProfileUsesAPKGoPackage(t *testing.T) {
	pkgs := CollectPackages([]string{"go"})
	if len(pkgs) != 1 || pkgs[0] != "go" {
		t.Fatalf("CollectPackages([go]) = %v, want [go]", pkgs)
	}
}

func TestGoCustomSnippetDoesNotDownloadGoDevToolchain(t *testing.T) {
	snippet := CustomSnippet("go")
	for _, forbidden := range []string{
		"go.dev/VERSION",
		"go.dev/dl",
		"GO_TARBALL",
		"/usr/local/go",
	} {
		if strings.Contains(snippet, forbidden) {
			t.Fatalf("Go CustomSnippet contains %q; compiler should be installed via apk", forbidden)
		}
	}
	if !strings.Contains(snippet, "golangci-lint") {
		t.Fatal("Go CustomSnippet should still install golangci-lint")
	}
}
