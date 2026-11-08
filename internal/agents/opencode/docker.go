package opencode

import (
	"fmt"
	"os"

	"github.com/cloud-exit/exitbox/internal/agent"
)

func (o *OpenCode) GetDockerfileInstall(buildCtx string) (string, error) {
	return o.installSnippet(agent.PrepareBuildInput{})
}

// installSnippet renders the install step for the image flavor being built.
// OpenCode 2.x ships per-platform npm packages; 1.x ships GitHub release tarballs.
func (o *OpenCode) installSnippet(in agent.PrepareBuildInput) (string, error) {
	if o.NpmPackageName() == "" {
		return "", fmt.Errorf("unsupported architecture for OpenCode")
	}
	if IsV2(in.Version) {
		return v2InstallSnippet(in), nil
	}
	return v1InstallSnippet(in), nil
}

// v2InstallSnippet downloads @opencode/cli-<target> from npm and verifies it
// against the registry's sha512 integrity. Musl images get the -musl build,
// x64 hosts without AVX2 get the -baseline build, matching opencode.ai/install.
func v2InstallSnippet(in agent.PrepareBuildInput) string {
	libcSuffix := ""
	if in.IsMusl() {
		libcSuffix = "-musl"
	}
	return `# Install OpenCode 2.x from its npm platform package with SHA-512 verification.
# No "curl | bash": fetches the tarball, verifies dist.integrity, extracts.
ARG OPENCODE_VERSION
RUN set -e && \
    ` + in.InlineInstall("curl", "jq") + ` && \
    case "$(uname -m)" in \
        x86_64|amd64) OC_ARCH="x64" ;; \
        aarch64|arm64) OC_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    OC_TARGET="linux-${OC_ARCH}" && \
    if [ "$OC_ARCH" = "x64" ] && ! grep -qwi avx2 /proc/cpuinfo 2>/dev/null; then \
        OC_TARGET="${OC_TARGET}-baseline"; \
    fi && \
    OC_TARGET="${OC_TARGET}` + libcSuffix + `" && \
    echo "Installing OpenCode v${OPENCODE_VERSION} (@opencode/cli-${OC_TARGET})..." && \
    META=$(curl -fsSL "https://registry.npmjs.org/@opencode%2fcli-${OC_TARGET}/${OPENCODE_VERSION}") && \
    TARBALL=$(printf '%s' "$META" | jq -r '.dist.tarball // empty') && \
    INTEGRITY=$(printf '%s' "$META" | jq -r '.dist.integrity // empty') && \
    case "$TARBALL" in \
        https://registry.npmjs.org/@opencode/cli-*) ;; \
        *) echo "ERROR: Unexpected tarball URL: ${TARBALL}" >&2; exit 1 ;; \
    esac && \
    case "$INTEGRITY" in \
        sha512-*) ;; \
        *) echo "ERROR: No sha512 integrity for @opencode/cli-${OC_TARGET}@${OPENCODE_VERSION}" >&2; exit 1 ;; \
    esac && \
    EXPECTED=$(printf '%s' "${INTEGRITY#sha512-}" | base64 -d | od -An -v -tx1 | tr -d ' \n') && \
    if ! echo "$EXPECTED" | grep -qE '^[a-f0-9]{128}$'; then \
        echo "ERROR: Malformed sha512 integrity: ${INTEGRITY}" >&2; exit 1; \
    fi && \
    curl -fsSL -o /tmp/opencode.tgz "$TARBALL" && \
    ACTUAL=$(sha512sum /tmp/opencode.tgz | cut -d' ' -f1) && \
    if [ "$ACTUAL" != "$EXPECTED" ]; then \
        echo "ERROR: Checksum mismatch" >&2; \
        echo "  Expected: $EXPECTED" >&2; \
        echo "  Actual:   $ACTUAL" >&2; \
        rm -f /tmp/opencode.tgz; exit 1; \
    fi && \
    echo "Checksum verified: $ACTUAL" && \
    mkdir -p /tmp/opencode-extract && \
    tar -xzf /tmp/opencode.tgz -C /tmp/opencode-extract && \
    install -m 755 /tmp/opencode-extract/package/bin/opencode /usr/local/bin/opencode && \
    rm -rf /tmp/opencode.tgz /tmp/opencode-extract && \
    /usr/local/bin/opencode --version`
}

// v1InstallSnippet downloads the OpenCode 1.x GitHub release tarball and
// verifies it against the release asset digest.
func v1InstallSnippet(in agent.PrepareBuildInput) string {
	return `# Install OpenCode via direct GitHub release download with SHA-256 verification.
# No "curl | bash" — fetches tarball, verifies digest from GitHub API, extracts.
# Note: base image must include curl, jq, sha256sum.
# For CI rate limits, pass GITHUB_TOKEN as build-arg or env to reduce 403s.
ARG OPENCODE_VERSION
ARG GITHUB_TOKEN
RUN set -e && \
    ` + in.InlineInstall("curl", "jq") + ` && \
    case "$(uname -m)" in \
        x86_64|amd64) OC_ARCH="x64" ;; \
        aarch64|arm64) OC_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    ASSET="opencode-linux-${OC_ARCH}.tar.gz" && \
    echo "Installing OpenCode v${OPENCODE_VERSION} (${ASSET})..." && \
    if [ -n "${GITHUB_TOKEN:-}" ]; then \
        META=$(curl -fsSL -H "Authorization: Bearer ${GITHUB_TOKEN}" \
            "https://api.github.com/repos/anomalyco/opencode/releases/tags/v${OPENCODE_VERSION}"); \
    else \
        META=$(curl -fsSL \
            "https://api.github.com/repos/anomalyco/opencode/releases/tags/v${OPENCODE_VERSION}"); \
    fi && \
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
        OC_MATCHES=$(find /tmp/opencode-extract -maxdepth 2 -type f -name opencode -print) && \
        OC_COUNT=$(printf '%s\n' "$OC_MATCHES" | sed '/^$/d' | wc -l) && \
        if [ "$OC_COUNT" -ne 1 ]; then \
            echo "ERROR: Expected exactly one opencode binary, found $OC_COUNT" >&2; exit 1; \
        fi && \
        OC_BIN="$OC_MATCHES"; \
    fi && \
    install -m 755 "$OC_BIN" /usr/local/bin/opencode && \
    rm -rf /tmp/opencode.tar.gz /tmp/opencode-extract && \
    /usr/local/bin/opencode --version`
}

// GetFullDockerfile returns the complete Dockerfile for OpenCode.
func (o *OpenCode) GetFullDockerfile(version string) (string, error) {
	return o.fullDockerfile(agent.PrepareBuildInput{Version: version})
}

func (o *OpenCode) fullDockerfile(in agent.PrepareBuildInput) (string, error) {
	install, err := o.installSnippet(in)
	if err != nil {
		return "", err
	}
	df := "FROM " + in.Base() + "\n\n"
	if in.Version != "" {
		df += fmt.Sprintf("ARG OPENCODE_VERSION=%s\n", in.Version)
	}
	df += install
	// The binary is root-owned and baked into the image; updates come from rebuilds.
	df += "\n\nENV OPENCODE_DISABLE_AUTOUPDATE=1\n"
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
		source := "GitHub release tarball, SHA-256 verified"
		if IsV2(version) {
			source = "npm platform package, SHA-512 verified"
		}
		in.Logf("Building OpenCode image with version %s (%s)", version, source)
	}
	in.Version = version
	df, err := o.fullDockerfile(in)
	if err != nil {
		return fmt.Errorf("failed to get OpenCode install instructions: %w", err)
	}
	if err := os.WriteFile(in.DockerfilePath, []byte(df), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	return nil
}
