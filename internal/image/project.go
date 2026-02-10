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
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/profile"
	proj "github.com/cloud-exit/exitbox/internal/project"
	"github.com/cloud-exit/exitbox/internal/ui"
)

// WorkspaceHash computes a short hash encoding the active workspace
// configuration. Each distinct workspace produces a different hash,
// which becomes part of the image name. Global tool config (cfg.Tools.User,
// cfg.Tools.Binaries) is NOT included — those live in the shared tools
// layer and are tracked via its own hash/label.
func WorkspaceHash(cfg *config.Config, projectDir string, overrideName string) string {
	active, _ := profile.ResolveActiveWorkspace(cfg, projectDir, overrideName)
	var parts []string
	if active != nil {
		parts = append(parts, active.Scope, active.Workspace.Name)
		parts = append(parts, active.Workspace.Development...)
		parts = append(parts, active.Workspace.Packages...)
	}
	parts = append(parts, SessionTools...)
	h := sha256.Sum256([]byte(strings.Join(parts, ",")))
	return fmt.Sprintf("%x", h[:8])
}

// BuildProject builds the agent project image (with workspaces).
// When force is true, the image is rebuilt even if it already exists.
// workspaceOverride selects a specific workspace (empty = use resolution chain).
func BuildProject(ctx context.Context, rt container.Runtime, agentName, projectDir, workspaceOverride string, force bool) error {
	cfg := config.LoadOrDefault()
	wh := WorkspaceHash(cfg, projectDir, workspaceOverride)
	imageName := proj.ImageName(agentName, projectDir, wh)
	toolsImage := fmt.Sprintf("exitbox-%s-tools", agentName)
	cmd := container.Cmd(rt)

	// Ensure tools image exists (tools → core → base cascade)
	if err := BuildTools(ctx, rt, agentName, false); err != nil {
		return err
	}

	// Each workspace has its own image name. If it already exists, skip.
	if !force && rt.ImageExists(imageName) {
		// Check if tools is newer (e.g. user changed tool categories)
		toolsCreated, _ := rt.ImageInspect(toolsImage, "{{.Created}}")
		projectCreated, _ := rt.ImageInspect(imageName, "{{.Created}}")
		if toolsCreated == "" || projectCreated == "" || toolsCreated <= projectCreated {
			return nil
		}
		ui.Info("Tools image updated, rebuilding project image...")
	}

	// Resolve active workspace.
	active, err := profile.ResolveActiveWorkspace(cfg, projectDir, workspaceOverride)
	if err != nil {
		ui.Warnf("Failed to resolve active workspace: %v", err)
	}
	var developmentProfiles []string
	if active != nil {
		developmentProfiles = append(developmentProfiles, active.Workspace.Development...)
	}

	ui.Infof("Building %s project image with %s...", agentName, cmd)

	buildCtx := filepath.Join(config.Cache, "build-"+agentName+"-project")
	if err := os.MkdirAll(buildCtx, 0755); err != nil {
		return fmt.Errorf("failed to create build context dir: %w", err)
	}

	dockerfilePath := filepath.Join(buildCtx, "Dockerfile")
	var df strings.Builder

	df.WriteString("# syntax=docker/dockerfile:1\n")
	fmt.Fprintf(&df, "FROM %s\n\n", toolsImage)

	// Switch to root for package installation (tools image stays as root,
	// but be explicit in case that changes)
	df.WriteString("USER root\n\n")

	// Validate all development profiles up front.
	for _, p := range developmentProfiles {
		if !profile.Exists(p) {
			return fmt.Errorf("unknown development profile '%s'. Run 'exitbox setup' to configure your development stack", p)
		}
	}

	// Collect ALL Alpine packages into a single apk add call:
	// workspace packages + profile packages + session tools.
	var allPkgs []string
	if active != nil {
		allPkgs = append(allPkgs, active.Workspace.Packages...)
	}
	allPkgs = append(allPkgs, profile.CollectPackages(developmentProfiles)...)
	allPkgs = append(allPkgs, SessionTools...)
	allPkgs = dedup(allPkgs)

	if len(allPkgs) > 0 {
		fmt.Fprintf(&df, "RUN --mount=type=cache,target=/var/cache/apk apk add --no-cache %s\n\n", strings.Join(allPkgs, " "))
	}

	// Add non-apk custom install steps (Go download, Python venv, etc.).
	for _, p := range developmentProfiles {
		snippet := profile.CustomSnippet(p)
		if snippet != "" {
			df.WriteString(snippet)
			df.WriteString("\n")
		}
	}

	// Fix home dir ownership after root package installs
	df.WriteString("RUN chown -R user:user /home/user\n\n")

	// Switch back to non-root user
	df.WriteString("USER user\n")

	if err := os.WriteFile(dockerfilePath, []byte(df.String()), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
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

	if err := buildImage(rt, args, fmt.Sprintf("Building %s project image...", agentName)); err != nil {
		return fmt.Errorf("failed to build %s project image: %w", agentName, err)
	}

	ui.Successf("%s project image built", agentName)
	return nil
}

// dedup returns a new slice with duplicate strings removed, preserving order.
func dedup(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
