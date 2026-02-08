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

import "fmt"

// DockerfileSnippet returns the Dockerfile instructions for installing a profile.
func DockerfileSnippet(name string) string {
	switch name {
	case "core", "base":
		return apkSnippet("base")
	case "build-tools":
		return apkSnippet("build-tools")
	case "shell":
		return apkSnippet("shell")
	case "networking":
		return apkSnippet("networking")
	case "c":
		return apkSnippet("c")
	case "rust":
		return apkSnippet("rust")
	case "java":
		return apkSnippet("java")
	case "ruby":
		return apkSnippet("ruby")
	case "php":
		return apkSnippet("php")
	case "database":
		return apkSnippet("database")
	case "devops":
		return apkSnippet("devops")
	case "web":
		return apkSnippet("web")
	case "embedded":
		return apkSnippet("embedded")
	case "datascience":
		return apkSnippet("datascience")
	case "security":
		return apkSnippet("security")
	case "ml":
		return "# ML profile uses build-tools for compilation\n"
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
		return `RUN apk add --no-cache nodejs npm && \
    npm install -g typescript eslint prettier yarn pnpm
`
	}
	return ""
}

func apkSnippet(name string) string {
	p := Get(name)
	if p == nil || p.Packages == "" {
		return ""
	}
	return fmt.Sprintf("RUN apk add --no-cache %s\n", p.Packages)
}
