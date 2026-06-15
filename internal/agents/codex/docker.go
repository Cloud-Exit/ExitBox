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

package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agent"
)

func (c *Codex) GetDockerfileInstall(buildCtx string) (string, error) {
	binaryName := c.BinaryName()
	if binaryName == "" {
		return "", fmt.Errorf("unsupported architecture for Codex")
	}
	binaryInside := strings.TrimSuffix(binaryName, ".tar.gz")

	return fmt.Sprintf(`# Install Codex runtime dependencies and binary with SHA-256 verification
RUN apk add --no-cache bubblewrap
ARG CODEX_VERSION
ARG CODEX_CHECKSUM
COPY %s /tmp/codex.tar.gz
RUN echo "${CODEX_CHECKSUM}  /tmp/codex.tar.gz" | sha256sum -c - && \
    mkdir -p $HOME/.local/bin && \
    tar -xzf /tmp/codex.tar.gz -C /tmp && \
    mv /tmp/%s $HOME/.local/bin/codex && \
    chmod +x $HOME/.local/bin/codex && \
    rm -f /tmp/codex.tar.gz && \
    $HOME/.local/bin/codex --version`, binaryName, binaryInside), nil
}

func (c *Codex) GetFullDockerfile(version string) (string, error) {
	install, err := c.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version != "" {
		df += fmt.Sprintf("ARG CODEX_VERSION=%s\n", version)
	}
	df += install
	return df, nil
}

func (c *Codex) PrepareBuild(in agent.PrepareBuildInput) error {
	version := in.Version
	if version == "" {
		var err error
		version, err = c.GetLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to get latest Codex version: %w", err)
		}
	}
	binaryName := c.BinaryName()
	if binaryName == "" {
		return fmt.Errorf("unsupported architecture for Codex")
	}
	if in.Download == nil || in.FileSHA256 == nil {
		return fmt.Errorf("PrepareBuildInput.Download and FileSHA256 are required for Codex")
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", codexGitHubRepo, version, binaryName)
	if in.Logf != nil {
		in.Logf("Downloading Codex %s...", version)
	}
	dlPath := filepath.Join(in.BuildDir, binaryName)
	if err := in.Download(in.Ctx, url, dlPath); err != nil {
		return fmt.Errorf("failed to download Codex: %w", err)
	}
	checksum := in.FileSHA256(dlPath)
	if in.Logf != nil {
		in.Logf("Codex SHA-256: %s", checksum)
	}
	df := fmt.Sprintf("FROM exitbox-base\n\nARG CODEX_VERSION=%s\nARG CODEX_CHECKSUM=%s\n", version, checksum)
	install, err := c.GetDockerfileInstall(in.BuildDir)
	if err != nil {
		return fmt.Errorf("failed to get Codex install instructions: %w", err)
	}
	df += install
	if err := os.WriteFile(in.DockerfilePath, []byte(df), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	return nil
}
