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

package cursor

import (
	"fmt"
	"os"

	"github.com/cloud-exit/exitbox/internal/agent"
)

// GetDockerfileInstall installs the Cursor CLI via the official install script.
// The installer drops cursor-agent under $HOME/.local; we point HOME at a shared,
// world-readable prefix (/opt/cursor) and symlink the real binary into
// /usr/local/bin so the non-root runtime user can execute it. We expose it under
// both "cursor-agent" (the upstream name) and "cursor" (the agent's registry name),
// since the entrypoint launches the agent by its registry name. The script always
// installs the latest published cursor-agent, so CURSOR_VERSION is informational
// (recorded as the image label) rather than a pin.
func (c *Cursor) GetDockerfileInstall(buildCtx string) (string, error) {
	return `# Install the Cursor CLI (cursor-agent) via the official install script.
ARG CURSOR_VERSION
RUN set -e && \
    apk add --no-cache curl bash && \
    mkdir -p /opt/cursor && \
    export HOME=/opt/cursor && \
    curl -fsSL https://cursor.com/install | bash && \
    AGENT_BIN="$(find /opt/cursor -type f -name cursor-agent 2>/dev/null | head -n1)" && \
    if [ -z "$AGENT_BIN" ]; then echo "ERROR: cursor-agent not found after install" >&2; exit 1; fi && \
    ln -sf "$AGENT_BIN" /usr/local/bin/cursor-agent && \
    ln -sf "$AGENT_BIN" /usr/local/bin/cursor && \
    chmod -R a+rX /opt/cursor && \
    cursor-agent --version
LABEL exitbox.agent.version="${CURSOR_VERSION}"`, nil
}

func (c *Cursor) GetFullDockerfile(version string) (string, error) {
	install, err := c.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version == "" {
		version = "latest"
	}
	df += fmt.Sprintf("ARG CURSOR_VERSION=%s\n", version)
	df += install
	return df, nil
}

func (c *Cursor) PrepareBuild(in agent.PrepareBuildInput) error {
	version := in.Version
	if version == "" {
		var err error
		version, err = c.GetLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to resolve Cursor CLI version: %w", err)
		}
	}
	if in.Logf != nil {
		in.Logf("Building Cursor CLI image (version %s, official install script at build time)", version)
	}
	df, err := c.GetFullDockerfile(version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(in.DockerfilePath, []byte(df), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	return nil
}
