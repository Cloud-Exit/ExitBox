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

import "strings"

// Packages returns the space-separated Alpine packages for a profile,
// or empty string if the profile has none or only custom install steps.
func Packages(name string) string {
	// The node/javascript profiles have apk packages too.
	switch name {
	case "node", "javascript":
		return "nodejs npm"
	}
	p := Get(name)
	if p == nil {
		return ""
	}
	return p.Packages
}

// CollectPackages returns a deduplicated list of all Alpine packages
// needed by the given profiles.
func CollectPackages(profiles []string) []string {
	seen := make(map[string]bool)
	var pkgs []string
	for _, name := range profiles {
		raw := Packages(name)
		if raw == "" {
			continue
		}
		for _, pkg := range strings.Fields(raw) {
			if !seen[pkg] {
				seen[pkg] = true
				pkgs = append(pkgs, pkg)
			}
		}
	}
	return pkgs
}

// CustomSnippet returns the non-apk Dockerfile instructions for a profile
// (e.g. Go download, Python venv, Flutter install, node npm globals).
// Returns empty string if the profile only needs apk packages.
func CustomSnippet(name string) string {
	switch name {
	case "python":
		return `# Python profile - venv with pip, setuptools, wheel
RUN python3 -m venv /home/user/.venv && \
    /home/user/.venv/bin/pip install --upgrade pip setuptools wheel
ENV PATH="/home/user/.venv/bin:$PATH"
`
	case "go":
		return `RUN set -e && \
    case "$(uname -m)" in \
        x86_64|amd64) GO_ARCH="amd64" ;; \
        aarch64|arm64) GO_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    GO_VERSION="$(wget -qO- https://go.dev/VERSION?m=text | head -n1)" && \
    GO_TARBALL="${GO_VERSION}.linux-${GO_ARCH}.tar.gz" && \
    GO_SHA256="$(wget -qO- https://go.dev/dl/?mode=json | jq -r --arg f "$GO_TARBALL" '.[0].files[] | select(.filename == $f) | .sha256')" && \
    test -n "$GO_SHA256" && \
    wget -q -O /tmp/go.tar.gz "https://go.dev/dl/${GO_TARBALL}" && \
    echo "${GO_SHA256}  /tmp/go.tar.gz" | sha256sum -c - && \
    tar -C /usr/local -xzf /tmp/go.tar.gz && \
    rm -f /tmp/go.tar.gz && \
    ln -sf /usr/local/go/bin/go /usr/local/bin/go && \
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
RUN set -e && \
    case "$(uname -m)" in \
        x86_64|amd64) LINT_ARCH="amd64" ;; \
        aarch64|arm64) LINT_ARCH="arm64" ;; \
        *) echo "Unsupported architecture" >&2; exit 1 ;; \
    esac && \
    LINT_VERSION="v1.64.8" && \
    wget -q -O /tmp/golangci-lint.tar.gz "https://github.com/golangci/golangci-lint/releases/download/${LINT_VERSION}/golangci-lint-${LINT_VERSION#v}-linux-${LINT_ARCH}.tar.gz" && \
    tar -xzf /tmp/golangci-lint.tar.gz -C /tmp && \
    mv /tmp/golangci-lint-${LINT_VERSION#v}-linux-${LINT_ARCH}/golangci-lint /usr/local/bin/golangci-lint && \
    chmod +x /usr/local/bin/golangci-lint && \
    rm -rf /tmp/golangci-lint*
`
	case "flutter":
		return `RUN set -e && \
    case "$(uname -m)" in \
        x86_64|amd64) FLUTTER_ARCH="x64" ;; \
        aarch64|arm64) FLUTTER_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    RELEASES_JSON="$(wget -qO- https://storage.googleapis.com/flutter_infra_release/releases/releases_linux.json)" && \
    STABLE_HASH="$(printf '%s' "$RELEASES_JSON" | jq -r '.current_release.stable')" && \
    FLUTTER_ARCHIVE="$(printf '%s' "$RELEASES_JSON" | jq -r --arg h "$STABLE_HASH" --arg a "$FLUTTER_ARCH" '.releases[] | select(.hash == $h and .dart_sdk_arch == $a) | .archive' | head -n1)" && \
    FLUTTER_SHA256="$(printf '%s' "$RELEASES_JSON" | jq -r --arg h "$STABLE_HASH" --arg a "$FLUTTER_ARCH" '.releases[] | select(.hash == $h and .dart_sdk_arch == $a) | .sha256' | head -n1)" && \
    test -n "$FLUTTER_ARCHIVE" && \
    test -n "$FLUTTER_SHA256" && \
    wget -q -O /tmp/flutter.tar.xz "https://storage.googleapis.com/flutter_infra_release/releases/${FLUTTER_ARCHIVE}" && \
    echo "${FLUTTER_SHA256}  /tmp/flutter.tar.xz" | sha256sum -c - && \
    rm -rf /opt/flutter && \
    mkdir -p /opt && \
    tar -xJf /tmp/flutter.tar.xz -C /opt && \
    rm -f /tmp/flutter.tar.xz && \
    ln -sf /opt/flutter/bin/flutter /usr/local/bin/flutter && \
    ln -sf /opt/flutter/bin/dart /usr/local/bin/dart
`
	case "node", "javascript":
		// apk packages (nodejs npm) are collected separately;
		// this is just the npm global installs.
		return `RUN npm install -g typescript eslint prettier yarn pnpm
`
	case "ml":
		return "# ML profile uses build-tools for compilation\n"
	}
	return ""
}

// DockerfileSnippet returns the full Dockerfile instructions for a profile.
// Deprecated: use CollectPackages + CustomSnippet for batched apk installs.
func DockerfileSnippet(name string) string {
	pkgs := Packages(name)
	custom := CustomSnippet(name)

	var parts []string
	if pkgs != "" {
		parts = append(parts, "RUN apk add --no-cache "+pkgs)
	}
	if custom != "" {
		parts = append(parts, custom)
	}
	return strings.Join(parts, "\n")
}
