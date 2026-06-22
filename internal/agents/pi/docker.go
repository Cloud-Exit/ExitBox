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

package pi

import (
	"fmt"
	"os"

	"github.com/cloud-exit/exitbox/internal/agent"
)

// piNPMPackage is the official Pi Coding Agent npm package (bin: pi). Installed with
// --ignore-scripts per upstream guidance; Pi needs no lifecycle scripts.
const piNPMPackage = "@earendil-works/pi-coding-agent"

func (p *Pi) GetDockerfileInstall(buildCtx string) (string, error) {
	return `# Install Node.js and the Pi Coding Agent via npm
ARG PI_VERSION
RUN apk add --no-cache nodejs npm && \
    npm install -g --ignore-scripts ` + piNPMPackage + `@${PI_VERSION} && \
    pi --version
LABEL exitbox.agent.version="${PI_VERSION}"`, nil
}

func (p *Pi) GetFullDockerfile(version string) (string, error) {
	install, err := p.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version == "" {
		version = "latest"
	}
	df += fmt.Sprintf("ARG PI_VERSION=%s\n", version)
	df += install
	return df, nil
}

func (p *Pi) PrepareBuild(in agent.PrepareBuildInput) error {
	version := in.Version
	if version == "" {
		var err error
		version, err = p.GetLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to get latest Pi Coding Agent version: %w", err)
		}
	}
	if in.Logf != nil {
		in.Logf("Building Pi Coding Agent image with version %s (npm install at build time)", version)
	}
	df, err := p.GetFullDockerfile(version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(in.DockerfilePath, []byte(df), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	return nil
}
