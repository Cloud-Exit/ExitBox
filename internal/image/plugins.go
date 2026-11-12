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
	"github.com/cloud-exit/exitbox/internal/distro"
	"github.com/cloud-exit/exitbox/internal/plugins"
	"github.com/cloud-exit/exitbox/internal/ui"
)

// ToolsImageName returns the shared tools image name for an agent.
func ToolsImageName(agentName string, d *distro.Distro) string {
	return fmt.Sprintf("exitbox-%s-tools%s", agentName, d.ImageSuffix())
}

// PluginsImageName returns the plugins layer image name for an agent and
// plugin configuration hash.
func PluginsImageName(agentName, hash string, d *distro.Distro) string {
	return fmt.Sprintf("exitbox-%s-plugins%s-%s", agentName, d.ImageSuffix(), hash)
}

// PluginsDockerfile renders the plugins layer Dockerfile for an agent. It
// returns an empty string when the workspace enables no plugins for that
// agent.
func PluginsDockerfile(ws *config.Workspace, agentName, baseImage, hash string, d *distro.Distro) string {
	snippets := plugins.DockerfileSnippets(ws, agentName, d)
	if len(snippets) == 0 {
		return ""
	}

	var df strings.Builder
	df.WriteString("# syntax=docker/dockerfile:1\n")
	fmt.Fprintf(&df, "FROM %s\n\n", baseImage)
	// Plugins install system-wide, so the layer runs as root and leaves the
	// USER switch to the project layer, exactly like the tools layer.
	df.WriteString("USER root\n\n")
	for _, snippet := range snippets {
		df.WriteString(snippet)
		if !strings.HasSuffix(snippet, "\n") {
			df.WriteString("\n")
		}
		df.WriteString("\n")
	}
	fmt.Fprintf(&df, "LABEL exitbox.plugins.hash=\"%s\"\n", hash)
	return df.String()
}

// BuildPlugins builds the per-agent plugins image layer for the active
// workspace and returns the image the project layer must build FROM. With
// no plugins enabled, nothing is built and the tools image is returned, so
// setups without plugins keep the old two-layer behaviour.
func BuildPlugins(ctx context.Context, rt container.Runtime, agentName, projectDir, workspaceOverride string, force bool, d *distro.Distro) (string, error) {
	toolsImage := ToolsImageName(agentName, d)

	cfg := config.LoadOrDefault()
	ws := activeWorkspace(cfg, projectDir, workspaceOverride)
	if ws == nil {
		return toolsImage, nil
	}

	hash := plugins.BuildHash(ws, agentName)
	if hash == "" {
		return toolsImage, nil
	}
	imageName := PluginsImageName(agentName, hash, d)

	dockerfile := PluginsDockerfile(ws, agentName, toolsImage, hash, d)
	if dockerfile == "" {
		return toolsImage, nil
	}

	if !force && !ForceRebuild && rt.ImageExists(imageName) {
		toolsCreated, _ := rt.ImageInspect(toolsImage, "{{.Created}}")
		pluginsCreated, _ := rt.ImageInspect(imageName, "{{.Created}}")
		if toolsCreated == "" || pluginsCreated == "" || toolsCreated <= pluginsCreated {
			return imageName, nil
		}
		ui.Info("Tools image updated, rebuilding plugins image...")
	}

	cmd := container.Cmd(rt)
	ui.Infof("Building %s plugins image with %s...", agentName, cmd)

	buildCtx := filepath.Join(config.Cache, "build-"+agentName+"-plugins"+d.ImageSuffix())
	if err := os.MkdirAll(buildCtx, 0755); err != nil {
		return "", fmt.Errorf("failed to create build context dir: %w", err)
	}
	dockerfilePath := filepath.Join(buildCtx, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return "", fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	if err := writePluginFiles(buildCtx, plugins.BuildFiles(ws, agentName)); err != nil {
		return "", err
	}

	args := buildArgs(cmd)
	if force {
		args = append(args, "--no-cache")
	}
	args = append(args,
		"-t", imageName,
		"-f", dockerfilePath,
		buildCtx,
	)

	if err := buildImage(rt, args, fmt.Sprintf("Building %s plugins image...", agentName)); err != nil {
		names := make([]string, 0, 2)
		for _, p := range plugins.Enabled(ws, agentName) {
			names = append(names, p.Name)
		}
		ui.Warnf("Enabled plugins for %s: %s", agentName, strings.Join(names, ", "))
		ui.Warnf("Disable a plugin with: exitbox plugins disable %s <plugin>", agentName)
		return "", fmt.Errorf("failed to build %s plugins image: %w", agentName, err)
	}

	ui.Successf("%s plugins image built", agentName)
	return imageName, nil
}

// writePluginFiles puts the files plugins ship themselves (e.g. a prebuilt
// binary) into the build context, where their Dockerfile snippets COPY
// them from. A name with a path in it is refused: files stay in the context.
func writePluginFiles(buildCtx string, files map[string][]byte) error {
	for name, data := range files {
		if name == "" || name != filepath.Base(name) || name == "Dockerfile" {
			return fmt.Errorf("plugin file name %q is not allowed in the build context", name)
		}
		if err := os.WriteFile(filepath.Join(buildCtx, name), data, 0755); err != nil {
			return fmt.Errorf("failed to write plugin file %s: %w", name, err)
		}
	}
	return nil
}
