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
	"runtime"
	"strings"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/static"
)

const (
	// BaseImageRegistry is the GHCR URL for the published base image.
	BaseImageRegistry = "ghcr.io/cloud-exit/exitbox-base"

	// SquidImageRegistry is the GHCR URL for the published squid image.
	SquidImageRegistry = "ghcr.io/cloud-exit/exitbox-squid"
)

// Version is set from cmd package.
var Version = "3.2.0"

// SessionTools are extra packages requested via --tools for this build only.
var SessionTools []string

// ForceRebuild forces image rebuilds (from --update flag only).
var ForceRebuild bool

// AutoUpdate enables checking for new agent versions on launch.
var AutoUpdate bool

// isReleaseVersion returns true if the version string looks like a release
// (starts with "v", e.g. "v3.2.0").
func isReleaseVersion(v string) bool {
	return strings.HasPrefix(v, "v")
}

// BuildBase builds the exitbox-base image.
func BuildBase(ctx context.Context, rt container.Runtime, force bool) error {
	imageName := "exitbox-base"
	cmd := container.Cmd(rt)

	if !force && !ForceRebuild && rt.ImageExists(imageName) {
		v, _ := rt.ImageInspect(imageName, `{{index .Config.Labels "exitbox.version"}}`)
		if v == Version {
			return nil
		}
		ui.Infof("Base image version mismatch (%s != %s). Rebuilding...", v, Version)
	}

	// For release versions, try pulling the pre-built base image from GHCR
	// and building only the thin local intermediary layer.
	if isReleaseVersion(Version) {
		remoteRef := BaseImageRegistry + ":" + Version
		if err := pullImage(rt, remoteRef, "Pulling base image..."); err == nil {
			if err := buildLocalIntermediary(ctx, rt, cmd, remoteRef, imageName); err == nil {
				ui.Success("Base image ready (from registry)")
				return nil
			}
			ui.Warnf("Local intermediary build failed, falling back to full build")
		} else {
			ui.Warnf("Could not pull %s, building locally", remoteRef)
		}
	}

	// Full local build (dev versions or when pull/intermediary fails).
	return buildBaseFull(ctx, rt, cmd, imageName)
}

// buildLocalIntermediary builds the thin local layer (user creation + exitbox-allow)
// on top of the pulled base image.
func buildLocalIntermediary(ctx context.Context, rt container.Runtime, cmd, baseRef, imageName string) error {
	buildCtx := filepath.Join(config.Cache, "build-local")
	if err := os.MkdirAll(buildCtx, 0755); err != nil {
		return fmt.Errorf("failed to create build context dir: %w", err)
	}

	dockerfilePath := filepath.Join(buildCtx, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, static.DockerfileLocal, 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile.local: %w", err)
	}

	// Write pre-built exitbox-allow binary for the container's architecture.
	if extra, err := writeExitboxAllow(buildCtx); err == nil && extra != "" {
		if err := appendToFile(dockerfilePath, extra); err != nil {
			ui.Warnf("Failed to append exitbox-allow to Dockerfile: %v", err)
		}
	}

	args := buildArgs(cmd)
	args = append(args,
		"--build-arg", fmt.Sprintf("BASE_IMAGE=%s", baseRef),
		"--build-arg", fmt.Sprintf("USER_ID=%d", os.Getuid()),
		"--build-arg", fmt.Sprintf("GROUP_ID=%d", os.Getgid()),
		"--build-arg", "USERNAME=user",
		"-t", imageName,
		"-f", dockerfilePath,
		buildCtx,
	)

	return buildImage(rt, args, "Building local intermediary...")
}

// buildBaseFull performs a full local build of the base image from scratch.
// It first builds the published base image locally, then layers the local
// intermediary (user creation + exitbox-allow) on top.
func buildBaseFull(ctx context.Context, rt container.Runtime, cmd, imageName string) error {
	ui.Infof("Building base image locally with %s...", cmd)

	// Step 1: Build the published base image locally.
	publishedName := "exitbox-base-published"
	buildCtx := filepath.Join(config.Cache, "build")
	if err := os.MkdirAll(buildCtx, 0755); err != nil {
		return fmt.Errorf("failed to create build context dir: %w", err)
	}

	dockerfilePath := filepath.Join(buildCtx, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, static.DockerfileBase, 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	if err := os.WriteFile(filepath.Join(buildCtx, "docker-entrypoint"), static.DockerEntrypoint, 0755); err != nil {
		return fmt.Errorf("failed to write entrypoint: %w", err)
	}
	if err := os.WriteFile(filepath.Join(buildCtx, ".dockerignore"), static.Dockerignore, 0644); err != nil {
		return fmt.Errorf("failed to write .dockerignore: %w", err)
	}

	args := buildArgs(cmd)
	args = append(args,
		"--build-arg", fmt.Sprintf("EXITBOX_VERSION=%s", Version),
		"-t", publishedName,
		"-f", dockerfilePath,
		buildCtx,
	)

	if err := buildImage(rt, args, "Building base image..."); err != nil {
		return fmt.Errorf("failed to build base image: %w", err)
	}

	// Step 2: Build the local intermediary on top.
	if err := buildLocalIntermediary(ctx, rt, cmd, publishedName, imageName); err != nil {
		return fmt.Errorf("failed to build local intermediary: %w", err)
	}

	ui.Success("Base image built")
	return nil
}

// writeExitboxAllow writes the exitbox-allow binary into the build context
// and returns the Dockerfile snippet to COPY it. Returns empty string if
// the binary could not be written.
func writeExitboxAllow(buildCtx string) (string, error) {
	var allowBin []byte
	switch runtime.GOARCH {
	case "arm64":
		allowBin = static.ExitboxAllowArm64
	default:
		allowBin = static.ExitboxAllowAmd64
	}
	if err := os.WriteFile(filepath.Join(buildCtx, "exitbox-allow"), allowBin, 0755); err != nil {
		ui.Warnf("Failed to write exitbox-allow: %v", err)
		return "", err
	}
	return "\n# IPC client\nCOPY exitbox-allow /usr/local/bin/exitbox-allow\n", nil
}

// pullImage pulls a container image, using a spinner in quiet mode or
// full output in verbose mode.
func pullImage(rt container.Runtime, ref, label string) error {
	if ui.Verbose {
		start := time.Now()
		err := container.PullInteractive(rt, ref)
		ui.Infof("Pull took %s", formatDuration(time.Since(start)))
		return err
	}
	spin := ui.NewSpinner(label)
	spin.Start()
	output, err := container.PullQuiet(rt, ref)
	elapsed := spin.Stop()
	if err != nil {
		ui.Debugf("Pull output: %s", output)
		return err
	}
	ui.Infof("Pull took %s", formatDuration(elapsed))
	return nil
}

// buildImage runs a container build, using a spinner in quiet mode or
// full output in verbose mode. On failure in quiet mode, the captured
// build output is printed to stderr.
func buildImage(rt container.Runtime, args []string, label string) error {
	if ui.Verbose {
		start := time.Now()
		err := container.BuildInteractive(rt, args)
		ui.Infof("Build took %s", formatDuration(time.Since(start)))
		return err
	}
	spin := ui.NewSpinner(label)
	spin.Start()
	output, err := container.BuildQuiet(rt, args)
	elapsed := spin.Stop()
	if err != nil {
		fmt.Fprint(os.Stderr, output)
		return err
	}
	ui.Infof("Build took %s", formatDuration(elapsed))
	return nil
}

// formatDuration formats a duration as a human-friendly string (e.g., "12s", "1m 23s").
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

func buildArgs(cmd string) []string {
	var args []string
	if cmd == "podman" {
		args = append(args, "--layers", "--pull=newer")
	} else {
		os.Setenv("DOCKER_BUILDKIT", "1")
		cacheDir := filepath.Join(config.Cache, "buildx")
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			ui.Warnf("Failed to create buildx cache dir: %v", err)
		}
		args = append(args,
			"--progress=auto",
			"--cache-from", "type=local,src="+cacheDir,
			"--cache-to", "type=local,dest="+cacheDir+",mode=max",
		)
	}
	return args
}
