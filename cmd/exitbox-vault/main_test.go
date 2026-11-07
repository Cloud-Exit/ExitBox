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

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSetArgs(t *testing.T) {
	pem := "-----BEGIN KEY-----\nabc=\n-----END KEY-----\n"
	p := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(p, []byte(pem), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		args      []string
		stdin     string
		wantValue string
		wantErr   string
	}{
		{"positional", []string{"K", "v"}, "", "v", ""},
		{"file", []string{"K", "-f", p}, "", pem, ""},
		{"long flag", []string{"K", "--file", p}, "", pem, ""},
		{"stdin", []string{"K", "-f", "-"}, "piped\n", "piped\n", ""},
		{"empty stdin", []string{"K", "-f", "-"}, "", "", "is empty"},
		{"flag no path", []string{"K", "-f"}, "", "", "requires a path"},
		{"missing value", []string{"K"}, "", "", "wrong number"},
		{"extra positional", []string{"K", "a", "b"}, "", "", "unexpected argument"},
		{"missing file", []string{"K", "-f", p + ".nope"}, "", "", "no such file"},
		{"oversize stdin", []string{"K", "-f", "-"}, strings.Repeat("a", maxFileValueSize+1), "", "exceeds"},
		{"binary stdin", []string{"K", "-f", "-"}, "\xff\xfe", "", "not valid UTF-8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, value, err := parseSetArgs(tt.args, strings.NewReader(tt.stdin))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if key != "K" || value != tt.wantValue {
				t.Errorf("got (%q, %q), want (K, %q)", key, value, tt.wantValue)
			}
		})
	}
}

func TestReadValueFile_Directory(t *testing.T) {
	if _, err := readValueFile(t.TempDir(), nil); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Errorf("err = %v, want directory error", err)
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"abc123":         "abc123",
		"https://x.io/a": "https://x.io/a",
		"":               "''",
		"has space":      "'has space'",
		"it's":           `'it'\''s'`,
		"$HOME`id`":      "'$HOME`id`'",
	}
	for in, want := range tests {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestShellQuote_EvalRoundTrip checks env output survives a real shell eval.
func TestShellQuote_EvalRoundTrip(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	values := []string{
		"-----BEGIN KEY-----\nMIIabc==\n-----END KEY-----\n",
		`it's "quoted" $HOME ` + "`id` \\ ;",
		"plain",
	}
	for _, v := range values {
		script := "eval \"$1\"; printf %s \"$K\""
		out, err := exec.Command(sh, "-c", script, "sh", "K="+shellQuote(v)).Output()
		if err != nil {
			t.Fatalf("eval failed for %q: %v", v, err)
		}
		if string(out) != v {
			t.Errorf("round-trip = %q, want %q", out, v)
		}
	}
}
