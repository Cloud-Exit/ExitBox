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

package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cloud-exit/exitbox/internal/container"
)

const (
	claudeGCSDefault    = "https://storage.googleapis.com/claude-code-dist-86c565f3-f756-42ad-8dfa-d59b1c096819/claude-code-releases"
	claudeInstallSHURL  = "https://claude.ai/install.sh"
)

// Claude implements the Agent interface for Claude Code.
type Claude struct{}

func (c *Claude) Name() string        { return "claude" }
func (c *Claude) DisplayName() string { return "Claude Code" }

func (c *Claude) GetLatestVersion() (string, error) {
	out, err := exec.Command("curl", "-fsSL", claudeGCSDefault+"/latest").Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch Claude latest version: %w", err)
	}
	v := strings.TrimSpace(string(out))
	if v == "" {
		return "", fmt.Errorf("empty version response")
	}
	return v, nil
}

func (c *Claude) GetInstalledVersion(rt container.Runtime, img string) (string, error) {
	if rt == nil || !rt.ImageExists(img) {
		return "", fmt.Errorf("image %s not found", img)
	}
	out, err := rt.ImageInspect(img, `{{index .Config.Labels "exitbox.agent.version"}}`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (c *Claude) GetDockerfileInstall(buildCtx string) (string, error) {
	return `# Install Claude Code via verified binary download (supply-chain hardened)
USER user
RUN set -e && \
    GCS_DEFAULT="` + claudeGCSDefault + `" && \
    INSTALL_SH_URL="` + claudeInstallSHURL + `" && \
    GCS_BUCKET="" && \
    if curl -fsSL --head "$GCS_DEFAULT/latest" >/dev/null 2>&1; then \
        GCS_BUCKET="$GCS_DEFAULT"; \
    else \
        echo "Hardcoded GCS URL unreachable, discovering from $INSTALL_SH_URL..." >&2; \
        DISCOVERED=$(curl -fsSL "$INSTALL_SH_URL" 2>/dev/null \
            | sed -n 's/^GCS_BUCKET="\(.*\)"/\1/p' | head -1) && \
        if [ -n "$DISCOVERED" ] && curl -fsSL --head "$DISCOVERED/latest" >/dev/null 2>&1; then \
            GCS_BUCKET="$DISCOVERED"; \
        fi; \
    fi && \
    if [ -z "$GCS_BUCKET" ]; then \
        echo "ERROR: Could not resolve Claude Code download URL" >&2; exit 1; \
    fi && \
    echo "Using GCS bucket: $GCS_BUCKET" && \
    case "$(uname -m)" in \
        x86_64|amd64) CLAUDE_ARCH="x64" ;; \
        aarch64|arm64) CLAUDE_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    CLAUDE_PLATFORM="linux-${CLAUDE_ARCH}-musl" && \
    CLAUDE_VERSION=$(curl -fsSL "$GCS_BUCKET/latest") && \
    echo "Installing Claude Code v${CLAUDE_VERSION} for ${CLAUDE_PLATFORM}..." && \
    MANIFEST=$(curl -fsSL "$GCS_BUCKET/$CLAUDE_VERSION/manifest.json") && \
    EXPECTED_CHECKSUM=$(printf '%s' "$MANIFEST" | jq -r ".platforms[\"$CLAUDE_PLATFORM\"].checksum // empty") && \
    if [ -z "$EXPECTED_CHECKSUM" ] || ! echo "$EXPECTED_CHECKSUM" | grep -qE '^[a-f0-9]{64}$'; then \
        echo "ERROR: No valid checksum for $CLAUDE_PLATFORM in manifest" >&2; exit 1; \
    fi && \
    mkdir -p "$HOME/.claude/downloads" && \
    BINARY_PATH="$HOME/.claude/downloads/claude-${CLAUDE_VERSION}" && \
    curl -fsSL -o "$BINARY_PATH" "$GCS_BUCKET/$CLAUDE_VERSION/$CLAUDE_PLATFORM/claude" && \
    ACTUAL_CHECKSUM=$(sha256sum "$BINARY_PATH" | cut -d' ' -f1) && \
    if [ "$ACTUAL_CHECKSUM" != "$EXPECTED_CHECKSUM" ]; then \
        echo "ERROR: Checksum verification failed!" >&2; \
        echo "  Expected: $EXPECTED_CHECKSUM" >&2; \
        echo "  Actual:   $ACTUAL_CHECKSUM" >&2; \
        rm -f "$BINARY_PATH"; exit 1; \
    fi && \
    echo "Checksum verified: $ACTUAL_CHECKSUM" && \
    chmod +x "$BINARY_PATH" && \
    "$BINARY_PATH" install && \
    rm -f "$BINARY_PATH" && \
    if [ -d "$HOME/.local/share/claude/versions" ]; then \
        latest_dir="$(ls -1d "$HOME/.local/share/claude/versions/"* | sort -V | tail -1)"; \
        if [ -x "$latest_dir/bin/claude" ]; then \
            ln -sf "$latest_dir/bin/claude" "$HOME/.local/bin/claude"; \
        fi; \
    fi && \
    command -v claude >/dev/null && \
    echo "Claude Code installed successfully"
USER root`, nil
}

func (c *Claude) GetFullDockerfile(version string) (string, error) {
	install, err := c.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	return "FROM exitbox-base\n\n" + install, nil
}

func (c *Claude) HostConfigPaths() []string {
	home := os.Getenv("HOME")
	return []string{filepath.Join(home, ".claude")}
}

func (c *Claude) ContainerMounts(cfgDir string) []Mount {
	return []Mount{
		{Source: filepath.Join(cfgDir, ".claude"), Target: "/home/user/.claude"},
		{Source: filepath.Join(cfgDir, ".claude.json"), Target: "/home/user/.claude.json"},
		{Source: filepath.Join(cfgDir, ".config"), Target: "/home/user/.config"},
	}
}

func (c *Claude) DetectHostConfig() (string, error) {
	home := os.Getenv("HOME")
	dir := filepath.Join(home, ".claude")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir, nil
	}
	return "", fmt.Errorf("no Claude config found")
}

func (c *Claude) ImportConfig(src, dst string) error {
	home := os.Getenv("HOME")

	// Copy entire ~/.claude directory
	target := filepath.Join(dst, ".claude")
	_ = os.MkdirAll(target, 0755)
	if err := copyDirContents(src, target); err != nil {
		return fmt.Errorf("copying .claude dir: %w", err)
	}

	// Also copy ~/.claude.json if it exists
	claudeJSON := filepath.Join(home, ".claude.json")
	if data, err := os.ReadFile(claudeJSON); err == nil {
		_ = os.WriteFile(filepath.Join(dst, ".claude.json"), data, 0644)
	}

	return nil
}
