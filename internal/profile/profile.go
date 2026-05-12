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

// Role defines a development role (formerly "profile") with its packages and description.
type Role struct {
	Name        string
	Description string
	Packages    string // Space-separated Alpine packages
}

// AllRoles returns all available development roles.
func AllRoles() []Role {
	return []Role{
		{"core", "Compatibility alias for base profile", "gcc g++ make git pkgconf openssl-dev libffi-dev zlib-dev tmux"},
		{"base", "Base development tools (git, vim, curl)", "gcc g++ make git pkgconf openssl-dev libffi-dev zlib-dev tmux"},
		{"build-tools", "Build toolchain helpers (cmake, autoconf, libtool)", "cmake samurai autoconf automake libtool"},
		{"shell", "Shell and file transfer utilities", "rsync openssh-client mandoc gnupg file"},
		{"networking", "Network diagnostics and tooling", "iptables ipset iproute2 bind-tools"},
		{"c", "C/C++ toolchain (gcc, clang, cmake, gdb)", "gdb valgrind clang clang-extra-tools cppcheck doxygen boost-dev ncurses-dev"},
		{"node", "Node.js runtime with npm and common JS tooling", "nodejs npm"},
		{"javascript", "Compatibility alias for node profile", "nodejs npm"},
		{"python", "Python 3 with pip and venv", ""},
		{"rust", "Rust toolchain (rust + cargo via apk)", "rust cargo"},
		{"go", "Go runtime (latest stable for host arch, checksum verified)", ""},
		{"java", "OpenJDK with Maven and Gradle", "openjdk17-jdk maven gradle"},
		{"dotnet", ".NET 8 SDK (dotnet CLI)", "dotnet8-sdk"},
		{"ruby", "Ruby runtime with bundler", "ruby ruby-dev readline-dev yaml-dev sqlite-dev sqlite libxml2-dev libxslt-dev curl-dev"},
		{"php", "PHP runtime with composer", "php83 php83-cli php83-fpm php83-mysqli php83-pgsql php83-sqlite3 php83-curl php83-gd php83-mbstring php83-xml php83-zip composer"},
		{"database", "Database CLI clients (Postgres, MySQL/MariaDB, SQLite, Redis)", "postgresql16-client mariadb-client sqlite redis"},
		{"kubernetes", "Kubernetes tooling (kubectl, helm, k9s, kustomize)", "kubectl helm k9s kustomize"},
		{"devops", "Container and IaC tooling (docker CLI, opentofu)", "docker-cli docker-cli-compose opentofu"},
		{"web", "Web server/testing tools (nginx, httpie)", "nginx apache2-utils httpie"},
		{"embedded", "Embedded systems base tooling", ""},
		{"datascience", "Data science tooling", "R"},
		{"security", "Security diagnostics (nmap, tcpdump, netcat)", "nmap tcpdump netcat-openbsd"},
		{"ml", "Machine learning helpers", ""},
		{"flutter", "Flutter SDK (stable, checksum verified)", ""},
	}
}

// RoleExists returns true if the role name is valid.
func RoleExists(name string) bool {
	for _, r := range AllRoles() {
		if r.Name == name {
			return true
		}
	}
	return false
}

// GetRole returns a role by name, or nil if not found.
func GetRole(name string) *Role {
	for _, r := range AllRoles() {
		if r.Name == name {
			return &r
		}
	}
	return nil
}
