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
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/agent"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/ui"
)

// BuildCore builds the agent core image (exitbox-<agent>-core).
func BuildCore(ctx context.Context, rt container.Runtime, agentName string, force bool) error {
	imageName := fmt.Sprintf("exitbox-%s-core", agentName)
	cmd := container.Cmd(rt)

	a := agent.Get(agentName)
	if a == nil {
		return fmt.Errorf("unknown agent: %s", agentName)
	}

	// Fetch latest version for comparison
	latestVersion, _ := a.GetLatestVersion()

	if !force && rt.ImageExists(imageName) {
		v, _ := rt.ImageInspect(imageName, `{{index .Config.Labels "exitbox.version"}}`)
		av, _ := rt.ImageInspect(imageName, `{{index .Config.Labels "exitbox.agent.version"}}`)

		if v == Version {
			if latestVersion != "" && av != "" && latestVersion != av {
				ui.Infof("%s update available (%s -> %s). Rebuilding...", agentName, av, latestVersion)
			} else {
				if err := BuildBase(ctx, rt, false); err != nil {
					return err
				}
				return nil
			}
		} else {
			ui.Infof("Agent core image version mismatch (%s != %s). Rebuilding...", v, Version)
		}
	}

	// Build base first
	if err := BuildBase(ctx, rt, force); err != nil {
		return err
	}

	// Rebuild squid when any agent is rebuilt
	_ = BuildSquid(ctx, rt, true)

	ui.Infof("Building %s core image with %s...", agentName, cmd)

	buildCtx := filepath.Join(config.Cache, "build-"+agentName)
	os.MkdirAll(buildCtx, 0755)

	dockerfilePath := filepath.Join(buildCtx, "Dockerfile")

	switch agentName {
	case "claude":
		df, err := a.GetFullDockerfile(latestVersion)
		if err != nil {
			return err
		}
		os.WriteFile(dockerfilePath, []byte(df), 0644)

	case "codex":
		codexAgent := a.(*agent.Codex)
		version := latestVersion
		if version == "" {
			version = "latest"
		}

		binaryName := codexAgent.BinaryName()
		if binaryName == "" {
			return fmt.Errorf("unsupported architecture for Codex")
		}

		// Pre-download binary
		url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", "openai/codex", version, binaryName)
		ui.Infof("Downloading Codex %s...", version)
		dlPath := filepath.Join(buildCtx, binaryName)
		if err := downloadFile(url, dlPath); err != nil {
			return fmt.Errorf("failed to download Codex: %w", err)
		}
		checksum := fileSHA256(dlPath)
		ui.Infof("Codex SHA-256: %s", checksum)

		df := fmt.Sprintf("FROM exitbox-base\n\nARG CODEX_VERSION=%s\nARG CODEX_CHECKSUM=%s\n", version, checksum)
		install, _ := a.GetDockerfileInstall(buildCtx)
		df += install
		os.WriteFile(dockerfilePath, []byte(df), 0644)

	case "opencode":
		ocAgent := a.(*agent.OpenCode)
		version := latestVersion
		if version == "" {
			version = "latest"
		}

		binaryName := ocAgent.BinaryName()
		if binaryName == "" {
			return fmt.Errorf("unsupported architecture for OpenCode")
		}

		// Pre-download binary (use v-prefixed tag for GitHub release URL)
		url := fmt.Sprintf("https://github.com/anomalyco/opencode/releases/download/v%s/%s", version, binaryName)
		ui.Infof("Downloading OpenCode %s...", version)
		dlPath := filepath.Join(buildCtx, binaryName)
		if err := downloadFile(url, dlPath); err != nil {
			return fmt.Errorf("failed to download OpenCode: %w", err)
		}
		checksum := fileSHA256(dlPath)
		ui.Infof("OpenCode SHA-256: %s", checksum)

		df := fmt.Sprintf("FROM exitbox-base\n\nARG OPENCODE_VERSION=%s\nARG OPENCODE_CHECKSUM=%s\n", version, checksum)
		install, _ := a.GetDockerfileInstall(buildCtx)
		df += install
		os.WriteFile(dockerfilePath, []byte(df), 0644)
	}

	// Add labels
	labels := fmt.Sprintf("\nLABEL exitbox.agent=\"%s\"\nLABEL exitbox.version=\"%s\"\n", agentName, Version)
	if latestVersion != "" {
		labels += fmt.Sprintf("LABEL exitbox.agent.version=\"%s\"\n", latestVersion)
	}
	appendToFile(dockerfilePath, labels)

	args := buildArgs(cmd)
	args = append(args,
		"-t", imageName,
		"-f", dockerfilePath,
		buildCtx,
	)

	if err := container.BuildInteractive(rt, args); err != nil {
		cfg := config.LoadOrDefault()
		if len(cfg.Tools.User) > 0 {
			ui.Warnf("User tools in config: %s", strings.Join(cfg.Tools.User, ", "))
			ui.Warnf("If a package was not found, check your config: %s", config.ConfigFile())
			ui.Warnf("Packages must be valid Alpine Linux (apk) package names.")
		}
		return fmt.Errorf("failed to build %s core image: %w", agentName, err)
	}

	// Save installed version
	versionFile := filepath.Join(config.AgentDir(agentName), "installed_version")
	os.MkdirAll(filepath.Dir(versionFile), 0755)
	v := latestVersion
	if v == "" {
		v = "unknown"
	}
	os.WriteFile(versionFile, []byte(v), 0644)

	ui.Successf("%s core image built (version: %s)", agentName, v)
	return nil
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func appendToFile(path, content string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(content)
}
