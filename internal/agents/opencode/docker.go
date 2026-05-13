package opencode

import (
	"fmt"
	"os"

	"github.com/cloud-exit/exitbox/internal/agent"
)

func (o *OpenCode) GetDockerfileInstall(buildCtx string) (string, error) {
	if o.NpmPackageName() == "" {
		return "", fmt.Errorf("unsupported architecture for OpenCode")
	}
	return `# Install OpenCode via direct GitHub release download with SHA-256 verification.
# No "curl | bash" — fetches tarball, verifies digest from GitHub API, extracts.
# Note: base image must include curl, jq, sha256sum.
# For CI rate limits, pass GITHUB_TOKEN as build-arg or env to reduce 403s.
ARG OPENCODE_VERSION
RUN set -e && \
    apk add --no-cache curl jq && \
    case "$(uname -m)" in \
        x86_64|amd64) OC_ARCH="x64" ;; \
        aarch64|arm64) OC_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    ASSET="opencode-linux-${OC_ARCH}.tar.gz" && \
    echo "Installing OpenCode v${OPENCODE_VERSION} (${ASSET})..." && \
    META=$(curl -fsSL \
        -H "Authorization: Bearer ${GITHUB_TOKEN:-}" \
        "https://api.github.com/repos/anomalyco/opencode/releases/tags/v${OPENCODE_VERSION}") && \
    DIGEST=$(printf '%s' "$META" | jq -r --arg n "$ASSET" '.assets[] | select(.name == $n) | .digest') && \
    EXPECTED="${DIGEST#sha256:}" && \
    if ! echo "$EXPECTED" | grep -qE '^[a-f0-9]{64}$'; then \
        echo "ERROR: No valid sha256 digest found for ${ASSET}" >&2; exit 1; \
    fi && \
    curl -fsSL -o /tmp/opencode.tar.gz \
        "https://github.com/anomalyco/opencode/releases/download/v${OPENCODE_VERSION}/${ASSET}" && \
    ACTUAL=$(sha256sum /tmp/opencode.tar.gz | cut -d' ' -f1) && \
    if [ "$ACTUAL" != "$EXPECTED" ]; then \
        echo "ERROR: Checksum mismatch" >&2; \
        echo "  Expected: $EXPECTED" >&2; \
        echo "  Actual:   $ACTUAL" >&2; \
        rm -f /tmp/opencode.tar.gz; exit 1; \
    fi && \
    echo "Checksum verified: $ACTUAL" && \
    mkdir -p /tmp/opencode-extract && \
    tar -xzf /tmp/opencode.tar.gz -C /tmp/opencode-extract && \
    OC_BIN="/tmp/opencode-extract/opencode" && \
    if [ ! -f "$OC_BIN" ]; then \
        OC_BIN=$(find /tmp/opencode-extract -maxdepth 2 -type f -name opencode) && \
        OC_COUNT=$(echo "$OC_BIN" | wc -l) && \
        if [ "$OC_COUNT" -ne 1 ]; then \
            echo "ERROR: Expected exactly one opencode binary, found $OC_COUNT" >&2; exit 1; \
        fi; \
    fi && \
    install -m 755 "$OC_BIN" /usr/local/bin/opencode && \
    rm -rf /tmp/opencode.tar.gz /tmp/opencode-extract && \
    /usr/local/bin/opencode --version`, nil
}

// GetFullDockerfile returns the complete Dockerfile for OpenCode.
func (o *OpenCode) GetFullDockerfile(version string) (string, error) {
	install, err := o.GetDockerfileInstall("")
	if err != nil {
		return "", err
	}
	df := "FROM exitbox-base\n\n"
	if version != "" {
		df += fmt.Sprintf("ARG OPENCODE_VERSION=%s\n", version)
	}
	df += install
	return df, nil
}

func (o *OpenCode) PrepareBuild(in agent.PrepareBuildInput) error {
	version := in.Version
	if version == "" {
		var err error
		version, err = o.GetLatestVersion()
		if err != nil {
			return fmt.Errorf("failed to get latest OpenCode version: %w", err)
		}
	}
	if in.Logf != nil {
		in.Logf("Building OpenCode image with version %s (GitHub release tarball, SHA-256 verified)", version)
	}
	df := fmt.Sprintf("FROM exitbox-base\n\nARG OPENCODE_VERSION=%s\n", version)
	install, err := o.GetDockerfileInstall(in.BuildDir)
	if err != nil {
		return fmt.Errorf("failed to get OpenCode install instructions: %w", err)
	}
	df += install
	if err := os.WriteFile(in.DockerfilePath, []byte(df), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	return nil
}
