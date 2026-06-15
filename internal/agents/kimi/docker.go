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

package kimi

import (
	"fmt"
	"os"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/agents/jstools"
)

// kimiNPMPackage is the official Moonshot AI Kimi Code CLI npm package. The package
// ships the CLI as a Node bundle (bin: kimi) with no platform restriction, so a global
// npm install on the Node-equipped base image is reliable across amd64/arm64.
const kimiNPMPackage = "@moonshot-ai/kimi-code"

func (k *Kimi) GetDockerfileInstall(buildCtx string) (string, error) {
	return `# Install Node.js and the Kimi Code CLI via npm
ARG KIMI_VERSION
` + jstools.InstallDependencies([]string{"nodejs", "npm"}, []string{kimiNPMPackage + "@${KIMI_VERSION}"}) + ` && \
    kimi --version
LABEL exitbox.agent.version="${KIMI_VERSION}"`, nil
}

func (k *Kimi) GetFullDockerfile(version string) (string, error) {
	install, err := k.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version == "" {
		version = "latest"
	}
	df += fmt.Sprintf("ARG KIMI_VERSION=%s\n", version)
	df += install
	return df, nil
}

func (k *Kimi) PrepareBuild(in agent.PrepareBuildInput) error {
	version := in.Version
	if version == "" {
		var err error
		version, err = k.GetLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to get latest Kimi Code CLI version: %w", err)
		}
	}
	if in.Logf != nil {
		in.Logf("Building Kimi Code CLI image with version %s (npm install at build time)", version)
	}
	df, err := k.GetFullDockerfile(version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(in.DockerfilePath, []byte(df), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	return nil
}
