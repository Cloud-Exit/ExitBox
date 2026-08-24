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

// Package wizard implements the interactive TUI setup wizard for ExitBox.
package wizard

import (
	"os"
	"path/filepath"

	"github.com/cloud-exit/exitbox/internal/agents"
	"github.com/cloud-exit/exitbox/internal/agents/jstools"
)

// Role represents a developer role preset (Frontend, Backend, etc.) that
// composes one or more dev roles (python, go, etc.) plus default tools.
type Role struct {
	Name           string
	Description    string
	Roles          []string // Default dev roles (python, go, ml, ...) to activate
	Languages      []string // Pre-checked language names in language step
	ToolCategories []string // Pre-checked tool category names in tools step
}

// Language represents a selectable programming language.
type Language struct {
	Name    string // Display name
	Profile string // Maps to an exitbox profile name
}

// ToolCategory represents a selectable tool category.
type ToolCategory struct {
	Name     string   // Display name
	Packages []string // Alpine packages in this category
	Binaries []Binary // Extra binaries to download (not available via apk)
}

// Binary represents a tool installed via direct download rather than apk.
type Binary struct {
	Name       string // Binary name (installed to /usr/local/bin)
	URLPattern string // Download URL with {arch} placeholder (amd64/arm64)
}

// AgentOption represents a selectable agent.
type AgentOption struct {
	Name        string
	DisplayName string
	Description string
}

// Roles defines the available developer roles.
var Roles = []Role{
	{
		Name:           "Frontend",
		Description:    "Web frontend development",
		Roles:          []string{"node", "web", "build-tools"},
		Languages:      []string{"Node/JS"},
		ToolCategories: []string{"Build Tools", "Web"},
	},
	{
		Name:           "Backend",
		Description:    "Server-side development",
		Roles:          []string{"python", "database", "build-tools"},
		Languages:      []string{"Python", "Go"},
		ToolCategories: []string{"Build Tools", "Database"},
	},
	{
		Name:           "Fullstack",
		Description:    "Full-stack web development",
		Roles:          []string{"node", "python", "database", "web", "dotnet", "build-tools"},
		Languages:      []string{"Node/JS", "Python", ".NET"},
		ToolCategories: []string{"Build Tools", "Database", "Web"},
	},
	{
		Name:           "DevOps",
		Description:    "Infrastructure and operations",
		Roles:          []string{"devops", "node", "networking", "shell", "build-tools"},
		Languages:      []string{"Go", "Python", "Node/JS"},
		ToolCategories: []string{"Build Tools", "Networking", "DevOps", "Shell Utils"},
	},
	{
		Name:           "Kubernetes",
		Description:    "Kubernetes development and operations",
		Roles:          []string{"kubernetes", "devops", "networking", "shell", "build-tools"},
		Languages:      []string{"Go", "Python"},
		ToolCategories: []string{"Build Tools", "Networking", "Kubernetes", "DevOps", "Shell Utils"},
	},
	{
		Name:           "Data Science",
		Description:    "Data analysis and machine learning",
		Roles:          []string{"python", "datascience", "database"},
		Languages:      []string{"Python"},
		ToolCategories: []string{"Database"},
	},
	{
		Name:           "Mobile",
		Description:    "Mobile application development",
		Roles:          []string{"flutter", "node"},
		Languages:      []string{"Flutter/Dart", "Node/JS"},
		ToolCategories: []string{"Build Tools"},
	},
	{
		Name:           "Embedded",
		Description:    "Embedded systems and IoT",
		Roles:          []string{"c", "embedded", "build-tools"},
		Languages:      []string{"C/C++", "Rust"},
		ToolCategories: []string{"Build Tools"},
	},
	{
		Name:           "Security",
		Description:    "Security research and tooling",
		Roles:          []string{"security", "networking", "shell"},
		Languages:      []string{"Python", "Go"},
		ToolCategories: []string{"Networking", "Security", "Shell Utils"},
	},
	{
		Name:           "AI Developer",
		Description:    "AI/ML development (huggingface-cli, model tooling)",
		Roles:          []string{"python", "ml", "build-tools"},
		Languages:      []string{"Python"},
		ToolCategories: []string{"Build Tools"},
	},
}

// AllLanguages defines the available language choices.
var AllLanguages = []Language{
	{Name: "Go", Profile: "go"},
	{Name: "Python", Profile: "python"},
	{Name: "Node/JS", Profile: "node"},
	{Name: "Rust", Profile: "rust"},
	{Name: "Java", Profile: "java"},
	{Name: ".NET", Profile: "dotnet"},
	{Name: "Ruby", Profile: "ruby"},
	{Name: "PHP", Profile: "php"},
	{Name: "C/C++", Profile: "c"},
	{Name: "Flutter/Dart", Profile: "flutter"},
}

// AllToolCategories defines the available tool category choices.
var AllToolCategories = []ToolCategory{
	{Name: "Build Tools", Packages: []string{"cmake", "samurai", "autoconf", "automake", "libtool"}},
	{Name: "Shell Utils", Packages: []string{"rsync", "openssh-client", "mandoc", "gnupg", "file"}},
	{Name: "Networking", Packages: []string{"iptables", "ipset", "iproute2", "bind-tools"}},
	{Name: "Database", Packages: []string{"postgresql16-client", "mariadb-client", "sqlite", "redis"}},
	{Name: "Kubernetes", Packages: []string{"kubectl", "helm", "k9s", "kustomize"}, Binaries: []Binary{
		{Name: "kind", URLPattern: "https://kind.sigs.k8s.io/dl/latest/kind-linux-{arch}"},
		{Name: "kubeseal", URLPattern: "https://github.com/bitnami-labs/sealed-secrets/releases/latest/download/kubeseal-linux-{arch}"},
	}},
	{Name: "DevOps", Packages: []string{"docker-cli", "docker-cli-compose", "opentofu"}},
	{Name: "Web", Packages: []string{"nginx", "apache2-utils", "httpie"}},
	{Name: "Security", Packages: []string{"nmap", "tcpdump", "netcat-openbsd"}},
}

// ExternalToolConfig describes a host config directory that hints at tool usage.
type ExternalToolConfig struct {
	HostPath    string // relative to $HOME, e.g. ".config/gh"
	Description string // shown when detected
}

// ExternalTool represents an installable external tool with host config detection.
type ExternalTool struct {
	Name        string
	Description string
	Packages    []string             // Alpine APK packages
	InstallStep string               // Additional Dockerfile RUN instruction
	Configs     []ExternalToolConfig // Host configs to detect (for UI hints)
}

// AllExternalTools defines the available external tools.
var AllExternalTools = []ExternalTool{
	{
		Name:        "Bun",
		Description: "bun — JavaScript runtime and package manager",
		Packages:    []string{"nodejs", "npm"},
		InstallStep: jstools.InstallBun() + "\n",
	},
	{
		Name:        "GitHub CLI",
		Description: "gh — GitHub from the command line",
		Packages:    []string{"github-cli"},
		Configs: []ExternalToolConfig{
			{HostPath: ".config/gh", Description: "GitHub CLI auth"},
		},
	},
	{
		Name:        "Graphify",
		Description: "graphify — /graphify skill maps your project into a queryable knowledge graph (auto-installed into compatible agents)",
		Packages:    []string{"py3-pip"},
		InstallStep: "RUN pip install --no-cache-dir --break-system-packages graphifyy && graphify --version\n",
	},
}

// AllAgents defines the selectable agents, built from the agent registry.
var AllAgents = buildAllAgentOptions()

func buildAllAgentOptions() []AgentOption {
	list := agents.All()
	out := make([]AgentOption, 0, len(list))
	for _, a := range list {
		out = append(out, AgentOption{
			Name:        a.Name(),
			DisplayName: a.DisplayName(),
			Description: a.Description(),
		})
	}
	return out
}

// GetRole returns the role by name, or nil.
func GetRole(name string) *Role {
	for i := range Roles {
		if Roles[i].Name == name {
			return &Roles[i]
		}
	}
	return nil
}

// ComputeRoles computes the dev role list from role presets + language selections.
func ComputeRoles(roleNames []string, languages []string) []string {
	seen := make(map[string]bool)
	var result []string

	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}

	for _, roleName := range roleNames {
		if role := GetRole(roleName); role != nil {
			for _, p := range role.Roles {
				add(p)
			}
		}
	}

	for _, langName := range languages {
		for _, l := range AllLanguages {
			if l.Name == langName {
				add(l.Profile)
				break
			}
		}
	}

	return result
}

// ComputePackages computes Alpine packages from selected tool categories.
func ComputePackages(categories []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, catName := range categories {
		for _, tc := range AllToolCategories {
			if tc.Name == catName {
				for _, pkg := range tc.Packages {
					if !seen[pkg] {
						seen[pkg] = true
						result = append(result, pkg)
					}
				}
				break
			}
		}
	}

	return result
}

// ComputeBinaries computes extra binary downloads from selected tool categories.
func ComputeBinaries(categories []string) []Binary {
	seen := make(map[string]bool)
	var result []Binary

	for _, catName := range categories {
		for _, tc := range AllToolCategories {
			if tc.Name == catName {
				for _, b := range tc.Binaries {
					if !seen[b.Name] {
						seen[b.Name] = true
						result = append(result, b)
					}
				}
				break
			}
		}
	}

	return result
}

// ComputeExternalToolPackages computes Alpine packages from selected external tools.
func ComputeExternalToolPackages(names []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, name := range names {
		for _, et := range AllExternalTools {
			if et.Name == name {
				for _, pkg := range et.Packages {
					if !seen[pkg] {
						seen[pkg] = true
						result = append(result, pkg)
					}
				}
				break
			}
		}
	}

	return result
}

// ComputeExternalToolInstallSteps returns additional Dockerfile snippets
// required by selected external tools.
func ComputeExternalToolInstallSteps(names []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, name := range names {
		for _, et := range AllExternalTools {
			if et.Name == name {
				if et.InstallStep != "" && !seen[et.InstallStep] {
					seen[et.InstallStep] = true
					result = append(result, et.InstallStep)
				}
				break
			}
		}
	}

	return result
}

// DetectExternalToolConfigs checks $HOME for known external tool config
// directories and returns a map of tool name → detected host paths.
func DetectExternalToolConfigs() map[string][]string {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}

	result := make(map[string][]string)
	for _, et := range AllExternalTools {
		for _, cfg := range et.Configs {
			p := filepath.Join(home, cfg.HostPath)
			if _, err := os.Stat(p); err == nil {
				result[et.Name] = append(result[et.Name], cfg.HostPath)
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
