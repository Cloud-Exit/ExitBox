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

// Package static embeds build assets and default configuration files.
package static

import _ "embed"

//go:embed build/Dockerfile.base
var DockerfileBase []byte

//go:embed build/Dockerfile.local
var DockerfileLocal []byte

//go:embed build/Dockerfile.squid
var DockerfileSquid []byte

//go:embed build/docker-entrypoint
var DockerEntrypoint []byte

//go:embed build/dockerignore
var Dockerignore []byte

//go:embed build/exitbox-allow-amd64
var ExitboxAllowAmd64 []byte

//go:embed build/exitbox-allow-arm64
var ExitboxAllowArm64 []byte

//go:embed config/allowlist.txt
var DefaultAllowlistTxt []byte

//go:embed config/allowlist.yaml
var DefaultAllowlistYAML []byte
