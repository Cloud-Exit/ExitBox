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

package image

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/static"
)

// Version is set from cmd package.
var Version = "3.2.0"

// BuildBase builds the exitbox-base image.
func BuildBase(ctx context.Context, rt container.Runtime, force bool) error {
	imageName := "exitbox-base"
	cmd := container.Cmd(rt)

	if !force && rt.ImageExists(imageName) {
		v, _ := rt.ImageInspect(imageName, `{{index .Config.Labels "exitbox.version"}}`)
		if v == Version {
			return nil
		}
		ui.Infof("Base image version mismatch (%s != %s). Rebuilding...", v, Version)
	}

	ui.Infof("Building base image with %s...", cmd)

	buildCtx := filepath.Join(config.Cache, "build")
	os.MkdirAll(buildCtx, 0755)

	// Copy build files from embedded assets
	os.WriteFile(filepath.Join(buildCtx, "Dockerfile"), static.DockerfileBase, 0644)
	os.WriteFile(filepath.Join(buildCtx, "docker-entrypoint"), static.DockerEntrypoint, 0755)
	os.WriteFile(filepath.Join(buildCtx, ".dockerignore"), static.Dockerignore, 0644)

	// Copy tools.txt and merge user tools
	tools := string(static.DefaultTools)
	cfg := config.LoadOrDefault()
	if len(cfg.Tools.User) > 0 {
		tools += "\n# User tools\n" + strings.Join(cfg.Tools.User, "\n") + "\n"
	}
	os.WriteFile(filepath.Join(buildCtx, "tools.txt"), []byte(tools), 0644)

	// Append binary download steps for tools not available via apk
	if len(cfg.Tools.Binaries) > 0 {
		dockerfilePath := filepath.Join(buildCtx, "Dockerfile")
		var extra string
		for _, b := range cfg.Tools.Binaries {
			extra += fmt.Sprintf("\n# Install %s (binary download)\n", b.Name)
			extra += "RUN ARCH=$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/') && \\\n"
			url := strings.ReplaceAll(b.URLPattern, "{arch}", "${ARCH}")
			extra += fmt.Sprintf("    curl -sL \"%s\" -o /usr/local/bin/%s && \\\n", url, b.Name)
			extra += fmt.Sprintf("    chmod +x /usr/local/bin/%s\n", b.Name)
		}
		appendToFile(dockerfilePath, extra)
	}

	args := buildArgs(cmd)
	args = append(args,
		"--build-arg", fmt.Sprintf("USER_ID=%d", os.Getuid()),
		"--build-arg", fmt.Sprintf("GROUP_ID=%d", os.Getgid()),
		"--build-arg", "USERNAME=user",
		"--build-arg", fmt.Sprintf("EXITBOX_VERSION=%s", Version),
		"-t", imageName,
		"-f", filepath.Join(buildCtx, "Dockerfile"),
		buildCtx,
	)

	if err := container.BuildInteractive(rt, args); err != nil {
		if len(cfg.Tools.User) > 0 {
			ui.Warnf("User tools in config: %s", strings.Join(cfg.Tools.User, ", "))
			ui.Warnf("If a package was not found, check your config: %s", config.ConfigFile())
			ui.Warnf("Packages must be valid Alpine Linux (apk) package names.")
		}
		return fmt.Errorf("failed to build base image: %w", err)
	}

	ui.Success("Base image built")
	return nil
}

func buildArgs(cmd string) []string {
	var args []string
	if cmd == "podman" {
		args = append(args, "--layers", "--pull=newer")
	} else {
		os.Setenv("DOCKER_BUILDKIT", "1")
		args = append(args, "--progress=auto")
	}
	return args
}
