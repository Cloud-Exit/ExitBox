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

package qwen

import (
	"fmt"
	"os"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/agents/jstools"
)

const qwenNPMPackage = "@qwen-code/qwen-code"

func (q *Qwen) GetDockerfileInstall(buildCtx string) (string, error) {
	return `# Install Node.js and Qwen Code via npm (requires Node 20+)
ARG QWEN_VERSION
` + jstools.InstallDependencies([]string{"nodejs", "npm"}, []string{qwenNPMPackage + "@${QWEN_VERSION}"}) + ` && \
    qwen --version
LABEL exitbox.agent.version="${QWEN_VERSION}"`, nil
}

func (q *Qwen) GetFullDockerfile(version string) (string, error) {
	install, err := q.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version == "" {
		version = "latest"
	}
	df += fmt.Sprintf("ARG QWEN_VERSION=%s\n", version)
	df += install
	return df, nil
}

func (q *Qwen) PrepareBuild(in agent.PrepareBuildInput) error {
	version := in.Version
	if version == "" {
		var err error
		version, err = q.GetLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to get latest Qwen Code version: %w", err)
		}
	}
	if in.Logf != nil {
		in.Logf("Building Qwen Code image with version %s (npm install at build time)", version)
	}
	df, err := q.GetFullDockerfile(version)
	if err != nil {
		return err
	}
	if err := os.WriteFile(in.DockerfilePath, []byte(df), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	return nil
}
