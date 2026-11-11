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

package clauderouter

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/cloud-exit/exitbox/internal/plugin"
	"github.com/cloud-exit/exitbox/internal/router"
)

func TestDescriptor(t *testing.T) {
	p := New()
	if p.Name != "claude-router" {
		t.Errorf("Name = %q", p.Name)
	}
	if !reflect.DeepEqual(p.Agents, []string{"claude"}) {
		t.Errorf("Agents = %v, want only claude", p.Agents)
	}
	if !reflect.DeepEqual(p.Conflicts, []string{"codex-router"}) {
		t.Errorf("Conflicts = %v, want codex-router", p.Conflicts)
	}
	if !strings.Contains(p.Description, "Experimental") {
		t.Errorf("the plugin must be marked experimental: %q", p.Description)
	}
	want := map[string]string{"port": "4203", "auto_start": "true", "sanitize": "true"}
	if got := p.DefaultSettings(); !reflect.DeepEqual(got, want) {
		t.Errorf("DefaultSettings = %v, want %v", got, want)
	}
	if got := p.EnvName("routes"); got != "EXITBOX_PLUGIN_CLAUDE_ROUTER_ROUTES" {
		t.Errorf("EnvName(routes) = %q", got)
	}
}

func TestRoutesSettingIsChecked(t *testing.T) {
	s := New().Setting("routes")
	if s == nil {
		t.Fatal("no routes setting")
	}
	for _, ok := range []string{
		"",
		"qwen3.6-27b=http://10.0.0.21:8080",
		"qwen=http://10.0.0.21:8080=qwen3-coder=vault:LLM_KEY,gemma=http://localhost:8000",
	} {
		if err := s.Validate(s.Normalize(ok)); err != nil {
			t.Errorf("Validate(%q): %v", ok, err)
		}
	}
	for _, bad := range []string{
		"qwen",
		"opus=http://10.0.0.21:8080",
		"claude-opus-5=http://10.0.0.21:8080",
		"qwen=10.0.0.21:8080",
		"qwen=http://10.0.0.21:8080,qwen=http://10.0.0.22:8080",
	} {
		if err := s.Validate(s.Normalize(bad)); err == nil {
			t.Errorf("Validate(%q) succeeded", bad)
		}
	}
}

func TestRoutesAndActive(t *testing.T) {
	settings := map[string]string{"routes": "a=http://h:1,b=http://h:2", "auto_start": "true"}
	if got := router.IDs(Routes(settings)); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf("Routes = %v", got)
	}
	if !Active(settings) {
		t.Error("routes with auto_start must be active")
	}
	if Active(map[string]string{"routes": "a=http://h:1", "auto_start": "false"}) {
		t.Error("auto_start=false must not be active")
	}
	if Active(map[string]string{"auto_start": "true"}) {
		t.Error("no routes must not be active")
	}
	if Routes(map[string]string{"routes": "not a route"}) != nil {
		t.Error("a value that does not parse must yield no routes")
	}
}

func TestDomainsAndHostPorts(t *testing.T) {
	routes, err := router.ParseRoutes(strings.Join([]string{
		"a=http://10.0.0.21:8080",
		"b=http://10.0.0.21:8081", // same host again
		"c=https://llm.example.com",
		"d=http://127.0.0.1:8000",
		"e=http://localhost:8001",
		"f=http://host.docker.internal:8000", // same port again
	}, ","))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := Domains(routes), []string{"10.0.0.21", "llm.example.com", "host.docker.internal"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Domains = %v, want %v", got, want)
	}
	if got, want := HostPorts(routes), []int{8000, 8001}; !reflect.DeepEqual(got, want) {
		t.Errorf("HostPorts = %v, want %v", got, want)
	}

	p := New()
	settings := map[string]string{"routes": "a=http://10.0.0.21:8080=vault:K1,d=http://127.0.0.1:8000=vault:K2,x=http://h:1=vault:K1"}
	if got := p.SettingDomains(settings); !reflect.DeepEqual(got, []string{"10.0.0.21", "host.docker.internal", "h"}) {
		t.Errorf("SettingDomains = %v", got)
	}
	if got := p.HostPorts(settings); !reflect.DeepEqual(got, []int{8000}) {
		t.Errorf("HostPorts = %v", got)
	}
	if got := p.VaultKeys(settings); !reflect.DeepEqual(got, []string{"K1", "K2"}) {
		t.Errorf("VaultKeys = %v", got)
	}
	if got := p.SecretKeys(settings); !reflect.DeepEqual(got, []string{"K1", "K2"}) {
		t.Errorf("SecretKeys = %v", got)
	}
}

func TestCheckRun(t *testing.T) {
	routed := map[string]string{"routes": "qwen=http://10.0.0.21:8080", "auto_start": "true"}
	tests := []struct {
		name string
		in   plugin.RunInput
		want string // part of the error, "" for none
	}{
		{"plain session", plugin.RunInput{Settings: routed, Firewall: true}, ""},
		{"--ollama", plugin.RunInput{Settings: routed, Firewall: true, Ollama: true}, "--ollama"},
		{"base URL set elsewhere", plugin.RunInput{Settings: routed, Firewall: true, BaseURLFrom: "env profile 'openrouter'"}, "env profile 'openrouter' already sets it"},
		{"no routes: nothing to conflict with", plugin.RunInput{Settings: map[string]string{"auto_start": "true"}, Ollama: true, BaseURLFrom: "x"}, ""},
		{"auto_start off: nothing to conflict with", plugin.RunInput{Settings: map[string]string{"routes": "qwen=http://h:1", "auto_start": "false"}, Ollama: true}, ""},
		{"https on another port behind the firewall", plugin.RunInput{Settings: map[string]string{"routes": "qwen=https://10.0.0.21:8443"}, Firewall: true}, "https on port 8443"},
		{"https on another port without the firewall", plugin.RunInput{Settings: map[string]string{"routes": "qwen=https://10.0.0.21:8443"}}, ""},
		{"https on 443", plugin.RunInput{Settings: map[string]string{"routes": "qwen=https://llm.example.com"}, Firewall: true}, ""},
		{"https on a relayed host port", plugin.RunInput{Settings: map[string]string{"routes": "qwen=https://localhost:8443"}, Firewall: true}, ""},
		{"http on any port", plugin.RunInput{Settings: map[string]string{"routes": "qwen=http://10.0.0.21:8080"}, Firewall: true}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New().CheckRun(tt.in)
			switch {
			case tt.want == "" && err != nil:
				t.Errorf("CheckRun: %v", err)
			case tt.want != "" && err == nil:
				t.Errorf("CheckRun succeeded, want an error with %q", tt.want)
			case tt.want != "" && !strings.Contains(err.Error(), tt.want):
				t.Errorf("CheckRun error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestDockerfileCopiesTheRouter(t *testing.T) {
	df := New().Dockerfile(plugin.BuildInput{Agent: "claude"})
	if !strings.Contains(df, "COPY exitbox-router /usr/local/bin/exitbox-router") {
		t.Errorf("Dockerfile does not copy the router:\n%s", df)
	}
	for _, network := range []string{"git clone", "npm", "curl", "wget", "apk add", "apt-get"} {
		if strings.Contains(df, network) {
			t.Errorf("the layer must build without network or packages, found %q:\n%s", network, df)
		}
	}
}

// The router is shipped prebuilt for both image architectures.
func TestFilesHoldAStaticLinuxBinaryPerArch(t *testing.T) {
	p := New()
	seen := map[string]bool{}
	for arch, machine := range map[string]byte{"amd64": 0x3e, "arm64": 0xb7} {
		files := p.Files(arch)
		bin, ok := files[Binary]
		if !ok || len(files) != 1 {
			t.Fatalf("Files(%s) = %d files, want only %s", arch, len(files), Binary)
		}
		if !bytes.HasPrefix(bin, []byte("\x7fELF")) {
			t.Fatalf("%s binary is not an ELF file", arch)
		}
		// e_machine, little endian, at offset 18.
		if bin[18] != machine {
			t.Errorf("%s binary is built for machine %#x, want %#x", arch, bin[18], machine)
		}
		if bytes.Contains(bin, []byte("/lib/ld-")) || bytes.Contains(bin, []byte("ld-musl")) {
			t.Errorf("%s binary needs a dynamic loader; it must run on Alpine and on Ubuntu", arch)
		}
		id := p.BuildID(arch)
		if len(id) != 12 || seen[id] {
			t.Errorf("BuildID(%s) = %q, want 12 hex digits that differ per architecture", arch, id)
		}
		seen[id] = true
		if p.BuildID(arch) != id {
			t.Errorf("BuildID(%s) is not stable", arch)
		}
	}
}
