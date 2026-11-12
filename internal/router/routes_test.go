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

package router

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseRoutes(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []Route
	}{
		{"empty", "", nil},
		{"only separators", " , \n ,", nil},
		{"id and url", "qwen3.6-27b=http://10.0.0.21:8080",
			[]Route{{ID: "qwen3.6-27b", URL: "http://10.0.0.21:8080"}}},
		{"served name", "qwen=http://10.0.0.21:8080=qwen3-coder",
			[]Route{{ID: "qwen", URL: "http://10.0.0.21:8080", Served: "qwen3-coder"}}},
		{"vault key", "qwen=http://10.0.0.21:8080=vault:LLM_KEY",
			[]Route{{ID: "qwen", URL: "http://10.0.0.21:8080", VaultKey: "LLM_KEY"}}},
		{"served name and vault key", "qwen=https://llm.example.com=qwen3-coder=vault:LLM_KEY",
			[]Route{{ID: "qwen", URL: "https://llm.example.com", Served: "qwen3-coder", VaultKey: "LLM_KEY"}}},
		{"vault key before served name", "qwen=https://llm.example.com=vault:LLM_KEY=qwen3-coder",
			[]Route{{ID: "qwen", URL: "https://llm.example.com", Served: "qwen3-coder", VaultKey: "LLM_KEY"}}},
		{"several, with spaces and a newline", " a=http://h1:1 ,\n b=http://h2:2=served ",
			[]Route{{ID: "a", URL: "http://h1:1"}, {ID: "b", URL: "http://h2:2", Served: "served"}}},
		{"trailing slash dropped", "a=http://h:8080/", []Route{{ID: "a", URL: "http://h:8080"}}},
		{"trailing /v1 dropped", "a=http://h:8080/v1", []Route{{ID: "a", URL: "http://h:8080"}}},
		{"trailing /v1/ dropped, prefix kept", "a=http://h:8080/llm/v1/", []Route{{ID: "a", URL: "http://h:8080/llm"}}},
		{"IPv6 literal", "a=http://[::1]:8080", []Route{{ID: "a", URL: "http://[::1]:8080"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRoutes(tt.value)
			if err != nil {
				t.Fatalf("ParseRoutes(%q): %v", tt.value, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseRoutes(%q) = %+v, want %+v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseRoutesErrors(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string // part of the error
	}{
		{"no url", "qwen", "model-id=backend-url"},
		{"empty id", "=http://h:1", "needs a model ID"},
		{"id with space", "my model=http://h:1", "one word"},
		{"claude id", "claude-opus-5=http://h:1", "starts with \"claude\""},
		{"claude id, capitals", "Claude-Local=http://h:1", "starts with \"claude\""},
		{"alias sonnet", "sonnet=http://h:1", "model alias"},
		{"alias opus", "opus=http://h:1", "model alias"},
		{"alias haiku", "haiku=http://h:1", "model alias"},
		{"alias fable", "fable=http://h:1", "model alias"},
		{"alias inherit", "inherit=http://h:1", "model alias"},
		{"alias, capitals", "Opus=http://h:1", "model alias"},
		{"duplicate id", "a=http://h:1,a=http://h:2", "listed twice"},
		{"empty url", "a=", "missing backend URL"},
		{"no scheme", "a=10.0.0.21:8080", "http:// or https://"},
		{"other scheme", "a=ftp://h:1", "http:// or https://"},
		{"no host", "a=http://", "no host"},
		{"credentials in url", "a=http://user:pw@h:1", "must not contain credentials"},
		{"query", "a=http://h:1/?x", "query or fragment"},
		{"bad port", "a=http://h:99999", "invalid port"},
		{"two vault keys", "a=http://h:1=vault:A=vault:B", "more than one vault key"},
		{"two served names", "a=http://h:1=x=y", "more than one served name"},
		{"empty vault key", "a=http://h:1=vault:", "not a vault key name"},
		{"vault key with space", "a=http://h:1=vault:MY KEY", "not a vault key name"},
		{"empty served name", "a=http://h:1=", "must be one word"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseRoutes(tt.value)
			if err == nil {
				t.Fatalf("ParseRoutes(%q) succeeded, want an error with %q", tt.value, tt.want)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("ParseRoutes(%q) error = %q, want it to contain %q", tt.value, err, tt.want)
			}
		})
	}
}

func TestRouteStringRoundTrips(t *testing.T) {
	const value = "a=http://h:1,b=https://x.example.com/llm=served,c=http://10.0.0.5:8000=vault:K,d=http://h:2=name=vault:K2"
	routes, err := ParseRoutes(value)
	if err != nil {
		t.Fatal(err)
	}
	var parts []string
	for _, r := range routes {
		parts = append(parts, r.String())
	}
	if got := strings.Join(parts, ","); got != value {
		t.Errorf("round trip = %q, want %q", got, value)
	}
}

func TestRouteAddress(t *testing.T) {
	tests := []struct {
		url      string
		host     string
		port     int
		loopback bool
	}{
		{"http://10.0.0.21:8080", "10.0.0.21", 8080, false},
		{"http://llm.lan", "llm.lan", 80, false},
		{"https://llm.example.com", "llm.example.com", 443, false},
		{"http://127.0.0.1:8080", "127.0.0.1", 8080, true},
		{"http://127.1.2.3:8080", "127.1.2.3", 8080, true},
		{"http://localhost:8000", "localhost", 8000, true},
		{"http://LOCALHOST:8000", "LOCALHOST", 8000, true},
		{"http://[::1]:8000", "::1", 8000, true},
		{"http://host.docker.internal:11434", "host.docker.internal", 11434, true},
	}
	for _, tt := range tests {
		r := Route{ID: "m", URL: tt.url}
		if r.Host() != tt.host || r.Port() != tt.port || r.OnHostLoopback() != tt.loopback {
			t.Errorf("%s: host=%q port=%d loopback=%v, want %q %d %v",
				tt.url, r.Host(), r.Port(), r.OnHostLoopback(), tt.host, tt.port, tt.loopback)
		}
	}
}

func TestVaultKeysAreUniqueAndOrdered(t *testing.T) {
	routes, err := ParseRoutes("a=http://h:1=vault:B,b=http://h:2,c=http://h:3=vault:A,d=http://h:4=vault:B")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := VaultKeys(routes), []string{"B", "A"}; !reflect.DeepEqual(got, want) {
		t.Errorf("VaultKeys = %v, want %v", got, want)
	}
	if got, want := IDs(routes), []string{"a", "b", "c", "d"}; !reflect.DeepEqual(got, want) {
		t.Errorf("IDs = %v, want %v", got, want)
	}
	if r, ok := Find(routes, "c"); !ok || r.VaultKey != "A" {
		t.Errorf("Find(c) = %+v, %v", r, ok)
	}
	if _, ok := Find(routes, "C"); ok {
		t.Error("Find must match the model ID exactly")
	}
}

func TestModelName(t *testing.T) {
	if got := (Route{ID: "a"}).ModelName(); got != "a" {
		t.Errorf("ModelName without a served name = %q, want the ID", got)
	}
	if got := (Route{ID: "a", Served: "b"}).ModelName(); got != "b" {
		t.Errorf("ModelName = %q, want the served name", got)
	}
}

func TestFromEnv(t *testing.T) {
	env := map[string]string{
		EnvRoutes:              "a=http://h:1=vault:FIRST,b=http://h:2=vault:SECOND,c=http://h:3=vault:FIRST",
		EnvPort:                "4999",
		EnvLog:                 "/tmp/router.log",
		EnvUpstream:            "http://127.0.0.1:9",
		EnvSecretPrefix + "0":  "token-first",
		EnvSecretPrefix + "17": "ignored",
	}
	s, err := FromEnv(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if s.Port != 4999 || s.LogPath != "/tmp/router.log" || s.Config.Upstream != "http://127.0.0.1:9" {
		t.Errorf("settings = %+v", s)
	}
	if !s.Config.Sanitize {
		t.Error("sanitize must default to on")
	}
	if got := s.Config.Tokens; len(got) != 1 || got["FIRST"] != "token-first" {
		t.Errorf("tokens = %v, want only FIRST", got)
	}
	if !reflect.DeepEqual(s.Missing, []string{"SECOND"}) {
		t.Errorf("missing = %v, want [SECOND]", s.Missing)
	}
}

func TestFromEnvDefaultsAndErrors(t *testing.T) {
	s, err := FromEnv(func(k string) string {
		if k == EnvSanitize {
			return "false"
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	if s.Port != DefaultPort || s.Config.Sanitize || len(s.Config.Routes) != 0 {
		t.Errorf("defaults = %+v", s)
	}
	for _, port := range []string{"0", "70000", "http"} {
		if _, err := FromEnv(func(k string) string {
			if k == EnvPort {
				return port
			}
			return ""
		}); err == nil {
			t.Errorf("port %q accepted", port)
		}
	}
	if _, err := FromEnv(func(k string) string {
		if k == EnvRoutes {
			return "opus=http://h:1"
		}
		return ""
	}); err == nil {
		t.Error("a route that hijacks a Claude alias was accepted")
	}
}
