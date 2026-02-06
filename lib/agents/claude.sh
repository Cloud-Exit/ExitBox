#!/usr/bin/env bash
# Claude agent module - Claude Code CLI specific configuration
# ============================================================================
# Installation: Direct binary download with SHA-256 verification from Anthropic GCS
# Source: Anthropic (storage.googleapis.com)
#
# Supply-chain hardening:
# - Binary checksum verified against manifest.json before execution
# - GCS bucket URL has a hardcoded default but is auto-discovered from the
#   official installer (claude.ai/install.sh) if the hardcoded URL breaks.

# ============================================================================
# CLAUDE AGENT CONFIGURATION
# ============================================================================

# Hardcoded GCS bucket (fast path). If Anthropic changes the bucket UUID,
# the fallback below will scrape the current URL from claude.ai/install.sh.
CLAUDE_GCS_BUCKET_DEFAULT="https://storage.googleapis.com/claude-code-dist-86c565f3-f756-42ad-8dfa-d59b1c096819/claude-code-releases"
CLAUDE_INSTALL_SH_URL="https://claude.ai/install.sh"

# ============================================================================
# GCS BUCKET DISCOVERY
# ============================================================================

# Resolve the GCS bucket URL. Tries the hardcoded default first; if it fails,
# scrapes the current URL from the official installer script.
claude_resolve_gcs_bucket() {
    local bucket="$CLAUDE_GCS_BUCKET_DEFAULT"

    # Quick probe: can we reach the hardcoded bucket?
    if curl -fsSL --head "$bucket/latest" >/dev/null 2>&1; then
        printf '%s' "$bucket"
        return 0
    fi

    # Fallback: fetch the installer script and extract GCS_BUCKET=...
    local install_script
    install_script=$(curl -fsSL "$CLAUDE_INSTALL_SH_URL" 2>/dev/null || true)

    if [[ -n "$install_script" ]]; then
        local discovered
        discovered=$(printf '%s\n' "$install_script" | sed -n 's/^GCS_BUCKET="\(.*\)"/\1/p' | head -1)
        if [[ -n "$discovered" ]] && curl -fsSL --head "$discovered/latest" >/dev/null 2>&1; then
            printf '%s' "$discovered"
            return 0
        fi
    fi

    # Last resort: return default and let caller handle the failure
    printf '%s' "$bucket"
}

# ============================================================================
# VERSION MANAGEMENT
# ============================================================================

# Get the installed Claude version from a Docker image
claude_get_installed_version() {
    local image_name="${1:-agentbox-claude-core}"
    local cmd
    cmd=$(container_cmd 2>/dev/null || printf 'podman')

    if ! $cmd image inspect "$image_name" >/dev/null 2>&1; then
        printf ''
        return 1
    fi

    # Run claude --version in the image
    local version
    version=$($cmd run --rm --entrypoint="" "$image_name" claude --version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || true)
    printf '%s' "$version"
}

# Get the latest available Claude version from Anthropic GCS
claude_get_latest_version() {
    local bucket
    bucket=$(claude_resolve_gcs_bucket)
    local version
    version=$(curl -fsSL "$bucket/latest" 2>/dev/null || true)

    if [[ -n "$version" ]] && [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]]; then
        printf '%s' "$version"
        return 0
    fi

    # Fall back to installed version
    claude_get_installed_version
}

# Check if Claude update is available
claude_update_available() {
    local installed
    local latest

    installed=$(claude_get_installed_version) || return 1
    latest=$(claude_get_latest_version) || return 1

    if [[ -z "$installed" ]] || [[ -z "$latest" ]]; then
        return 1
    fi

    if [[ "$installed" != "$latest" ]]; then
        return 0
    fi

    return 1
}

# ============================================================================
# DOCKERFILE GENERATION
# ============================================================================

# Generate the Claude-specific Dockerfile installation commands
# Uses direct binary download with SHA-256 checksum verification
# to mitigate supply-chain attacks (no pipe-to-bash).
claude_get_dockerfile_install() {
    cat << 'EOF'
# Install Claude Code via verified binary download (supply-chain hardened)
# 1. Try hardcoded GCS bucket; if unreachable, discover URL from claude.ai/install.sh
# 2. Download manifest.json → extract SHA-256 checksum for this platform
# 3. Download binary → verify checksum → run installer
USER user
RUN set -e && \
    GCS_DEFAULT="https://storage.googleapis.com/claude-code-dist-86c565f3-f756-42ad-8dfa-d59b1c096819/claude-code-releases" && \
    INSTALL_SH_URL="https://claude.ai/install.sh" && \
    # --- Resolve GCS bucket URL with fallback ---
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
    # --- Detect platform (always linux + musl on Alpine) ---
    case "$(uname -m)" in \
        x86_64|amd64) CLAUDE_ARCH="x64" ;; \
        aarch64|arm64) CLAUDE_ARCH="arm64" ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    CLAUDE_PLATFORM="linux-${CLAUDE_ARCH}-musl" && \
    # --- Fetch latest version ---
    CLAUDE_VERSION=$(curl -fsSL "$GCS_BUCKET/latest") && \
    echo "Installing Claude Code v${CLAUDE_VERSION} for ${CLAUDE_PLATFORM}..." && \
    # --- Download manifest and extract expected checksum ---
    MANIFEST=$(curl -fsSL "$GCS_BUCKET/$CLAUDE_VERSION/manifest.json") && \
    EXPECTED_CHECKSUM=$(printf '%s' "$MANIFEST" | jq -r ".platforms[\"$CLAUDE_PLATFORM\"].checksum // empty") && \
    if [ -z "$EXPECTED_CHECKSUM" ] || ! echo "$EXPECTED_CHECKSUM" | grep -qE '^[a-f0-9]{64}$'; then \
        echo "ERROR: No valid checksum for $CLAUDE_PLATFORM in manifest" >&2; exit 1; \
    fi && \
    # --- Download binary ---
    mkdir -p "$HOME/.claude/downloads" && \
    BINARY_PATH="$HOME/.claude/downloads/claude-${CLAUDE_VERSION}" && \
    curl -fsSL -o "$BINARY_PATH" "$GCS_BUCKET/$CLAUDE_VERSION/$CLAUDE_PLATFORM/claude" && \
    # --- Verify SHA-256 checksum ---
    ACTUAL_CHECKSUM=$(sha256sum "$BINARY_PATH" | cut -d' ' -f1) && \
    if [ "$ACTUAL_CHECKSUM" != "$EXPECTED_CHECKSUM" ]; then \
        echo "ERROR: Checksum verification failed!" >&2; \
        echo "  Expected: $EXPECTED_CHECKSUM" >&2; \
        echo "  Actual:   $ACTUAL_CHECKSUM" >&2; \
        rm -f "$BINARY_PATH"; exit 1; \
    fi && \
    echo "Checksum verified: $ACTUAL_CHECKSUM" && \
    chmod +x "$BINARY_PATH" && \
    # --- Run installer to set up launcher and shell integration ---
    "$BINARY_PATH" install && \
    rm -f "$BINARY_PATH" && \
    # Ensure claude is on PATH
    if [ -d "$HOME/.local/share/claude/versions" ]; then \
        latest_dir="$(ls -1d "$HOME/.local/share/claude/versions/"* | sort -V | tail -1)"; \
        if [ -x "$latest_dir/bin/claude" ]; then \
            ln -sf "$latest_dir/bin/claude" "$HOME/.local/bin/claude"; \
        fi; \
    fi && \
    command -v claude >/dev/null && \
    echo "Claude Code v${CLAUDE_VERSION} installed successfully"
USER root
EOF
}

# ============================================================================
# ENTRYPOINT LOGIC
# ============================================================================

# Get the Claude-specific entrypoint command
claude_get_entrypoint_command() {
    printf 'claude'
}

# Get Claude-specific environment variables for container
# Note: Claude uses config files, no env vars needed
claude_get_env_vars() {
    # No environment variables needed - Claude uses ~/.claude/ for auth
    :
}

# ============================================================================
# CONFIG PATHS
# ============================================================================

# Get the Claude credentials path inside container
claude_get_credentials_path() {
    printf '/home/user/.claude'
}

# Get the Claude config path inside container
claude_get_config_path() {
    printf '/home/user/.config'
}

# ============================================================================
# HOST CONFIG DETECTION
# ============================================================================

# Detect existing Claude installation on host
claude_detect_host_config() {
    local host_claude_dir="$HOME/.claude"

    if [[ -d "$host_claude_dir" ]]; then
        printf '%s' "$host_claude_dir"
        return 0
    fi

    return 1
}

# Get what Claude config files to import
claude_get_importable_files() {
    cat << 'EOF'
.credentials.json:Credentials:authentication
settings.json:Settings:user preferences
settings.local.json:Local Settings:MCP config
EOF
}

# ============================================================================
# EXPORTS
# ============================================================================

export -f claude_resolve_gcs_bucket claude_get_installed_version claude_get_latest_version claude_update_available
export -f claude_get_dockerfile_install claude_get_entrypoint_command claude_get_env_vars
export -f claude_get_credentials_path claude_get_config_path
export -f claude_detect_host_config claude_get_importable_files
