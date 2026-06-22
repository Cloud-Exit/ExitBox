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

package copilot

import (
	"fmt"
	"os"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/agents/jstools"
)

// copilotNPMPackage is the official GitHub Copilot CLI npm package.
// The base image (Alpine 3.21) ships Node 22, satisfying Copilot's Node 22+ requirement.
const copilotNPMPackage = "@github/copilot"

func (c *Copilot) GetDockerfileInstall(buildCtx string) (string, error) {
	return `# Install Node.js and GitHub Copilot CLI via npm (requires Node 22+)
ARG COPILOT_VERSION
` + jstools.InstallDependencies([]string{"nodejs", "npm"}, []string{copilotNPMPackage + "@${COPILOT_VERSION}"}) + ` && \
    copilot --version
LABEL exitbox.agent.version="${COPILOT_VERSION}"`, nil
}

func (c *Copilot) GetFullDockerfile(version string) (string, error) {
	install, err := c.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version == "" {
		version = "latest"
	}
	df += fmt.Sprintf("ARG COPILOT_VERSION=%s\n", version)
	df += install
	return df, nil
}

func (c *Copilot) PrepareBuild(in agent.PrepareBuildInput) error {
	version := in.Version
	if version == "" {
		var err error
		version, err = c.GetLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to get latest GitHub Copilot CLI version: %w", err)
		}
	}
	if in.Logf != nil {
		in.Logf("Building GitHub Copilot CLI image with version %s (npm install at build time)", version)
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
