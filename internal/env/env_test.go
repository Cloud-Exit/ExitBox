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

package env

import (
	"reflect"
	"testing"
)

func TestDefaultProfile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	ws := "test-ws"

	// No default set initially.
	name, err := GetDefault(ws)
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if name != "" {
		t.Fatalf("expected empty default, got %q", name)
	}

	// SetDefault fails for non-existent profile.
	if err := SetDefault(ws, "nope"); err == nil {
		t.Fatal("expected error for non-existent profile")
	}

	// Save a profile, then set it as default.
	if err := Save(ws, &Profile{Name: "prod", Vars: map[string]string{"A": "1"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := SetDefault(ws, "prod"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}

	name, err = GetDefault(ws)
	if err != nil {
		t.Fatalf("GetDefault: %v", err)
	}
	if name != "prod" {
		t.Fatalf("expected 'prod', got %q", name)
	}

	// ClearDefault removes it.
	if err := ClearDefault(ws); err != nil {
		t.Fatalf("ClearDefault: %v", err)
	}
	name, err = GetDefault(ws)
	if err != nil {
		t.Fatalf("GetDefault after clear: %v", err)
	}
	if name != "" {
		t.Fatalf("expected empty after clear, got %q", name)
	}
}

func TestParseEnvFile(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{
			name:  "simple",
			input: "FOO=bar\nBAZ=qux\n",
			want:  map[string]string{"FOO": "bar", "BAZ": "qux"},
		},
		{
			name:  "empty value (explicit unset)",
			input: "ANTHROPIC_API_KEY=\n",
			want:  map[string]string{"ANTHROPIC_API_KEY": ""},
		},
		{
			name:  "comments and blank lines",
			input: "# comment\n\nFOO=bar\n# another\n",
			want:  map[string]string{"FOO": "bar"},
		},
		{
			name:  "export prefix",
			input: "export OPENROUTER_API_KEY=secret\n",
			want:  map[string]string{"OPENROUTER_API_KEY": "secret"},
		},
		{
			name:  "double-quoted value",
			input: `URL="https://api.example.com"`,
			want:  map[string]string{"URL": "https://api.example.com"},
		},
		{
			name:  "single-quoted value",
			input: `KEY='val with spaces'`,
			want:  map[string]string{"KEY": "val with spaces"},
		},
		{
			name:  "value with equals",
			input: "TOKEN=abc=def=ghi\n",
			want:  map[string]string{"TOKEN": "abc=def=ghi"},
		},
		{
			name:    "missing equals",
			input:   "INVALID_LINE\n",
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEnvFile(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFormatEnvFile(t *testing.T) {
	vars := map[string]string{
		"BAZ":              "qux",
		"FOO":              "bar",
		"ANTHROPIC_API_KEY": "",
	}
	got := FormatEnvFile(vars)
	want := "ANTHROPIC_API_KEY=\nBAZ=qux\nFOO=bar\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestRoundTrip(t *testing.T) {
	in := map[string]string{
		"OPENROUTER_API_KEY":   "sk-or-v1-abc",
		"ANTHROPIC_BASE_URL":   "https://openrouter.ai/api",
		"ANTHROPIC_AUTH_TOKEN": "$OPENROUTER_API_KEY",
		"ANTHROPIC_API_KEY":    "",
	}
	formatted := FormatEnvFile(in)
	parsed, err := ParseEnvFile(formatted)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !reflect.DeepEqual(in, parsed) {
		t.Errorf("round-trip mismatch:\nin=%v\nout=%v", in, parsed)
	}
}
