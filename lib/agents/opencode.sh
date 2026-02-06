#!/usr/bin/env bash
# OpenCode agent module - OpenCode CLI specific configuration
# ============================================================================
# Installation: Official container image from ghcr.io/anomalyco/opencode

# ============================================================================
# OPENCODE AGENT CONFIGURATION
# ============================================================================

OPENCODE_GITHUB_REPO="opencode-ai/opencode"

# ============================================================================
# ARCHITECTURE DETECTION
# ============================================================================

# Get the OpenCode binary name for the current architecture
opencode_get_binary_name() {
    local arch
    arch=$(uname -m)

    case "$arch" in
        x86_64)
            printf 'opencode_Linux_x86_64.tar.gz'
            ;;
        aarch64|arm64)
            printf 'opencode_Linux_arm64.tar.gz'
            ;;
        *)
            printf '' >&2
            return 1
            ;;
    esac
}

# ============================================================================
# VERSION MANAGEMENT
# ============================================================================

# Get the installed OpenCode version from a Docker image
opencode_get_installed_version() {
    local image_name="${1:-agentbox-opencode-core}"

    if ! docker image inspect "$image_name" >/dev/null 2>&1; then
        printf ''
        return 1
    fi

    # Run opencode --version in the image
    local version
    version=$(docker run --rm "$image_name" opencode --version 2>/dev/null | head -1 | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' || true)
    printf '%s' "$version"
}

# Get the latest available OpenCode version from GitHub
opencode_get_latest_version() {
    # Fetch from GitHub API
    local version
    version=$(curl -s "https://api.github.com/repos/${OPENCODE_GITHUB_REPO}/releases/latest" 2>/dev/null | \
        grep '"tag_name"' | sed -E 's/.*"tag_name": *"v?([^"]+)".*/\1/' || true)

    if [[ -n "$version" ]]; then
        printf '%s' "$version"
        return 0
    fi

    return 1
}

# Check if OpenCode update is available
opencode_update_available() {
    local installed
    local latest

    installed=$(opencode_get_installed_version) || return 1
    latest=$(opencode_get_latest_version) || return 1

    if [[ -z "$installed" ]] || [[ -z "$latest" ]]; then
        return 1
    fi

    if [[ "$installed" != "$latest" ]]; then
        return 0
    fi

    return 1
}

# Get the download URL for the latest OpenCode release
opencode_get_download_url() {
    local version="${1:-}"
    local binary_name

    binary_name=$(opencode_get_binary_name) || return 1

    if [[ -z "$version" ]]; then
        version=$(opencode_get_latest_version) || return 1
    fi

    printf 'https://github.com/%s/releases/download/v%s/%s' "$OPENCODE_GITHUB_REPO" "$version" "$binary_name"
}

# ============================================================================
# DOCKERFILE GENERATION
# ============================================================================

# Generate the full OpenCode Dockerfile (uses official image as base)
opencode_get_dockerfile() {
    cat << 'DOCKERFILE'
# Use official OpenCode image as base
FROM ghcr.io/anomalyco/opencode:latest

# add user "user" with UID 1000
# This is required for rootless operation and to ensure config files are owned by non-root user
RUN adduser --disabled-password --gecos "" --home "/home/user" user && \
    chown -R user:user /home/user

ENV LANG=C.UTF-8 \
    LC_ALL=C.UTF-8 \
    HOME=/home/user \
    PATH="/home/user/.local/bin:/usr/local/bin:${PATH}"

# Install base packages from tools.txt
COPY tools.txt /tmp/tools.txt
RUN apk add --no-cache $(grep -v '^\s*#' /tmp/tools.txt | grep -v '^\s*$' | tr '\n' ' ') && rm /tmp/tools.txt

# Stay rootless-only: no package-manager installs and no privilege escalation.
COPY docker-entrypoint-opencode /usr/local/bin/docker-entrypoint

USER user

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/docker-entrypoint"]
DOCKERFILE
}

# Legacy function for compatibility
opencode_get_dockerfile_install() {
    opencode_get_dockerfile
}

# ============================================================================
# ENTRYPOINT LOGIC
# ============================================================================

# Get the OpenCode-specific entrypoint command
opencode_get_entrypoint_command() {
    printf 'opencode'
}

# Get OpenCode-specific environment variables for container
opencode_get_env_vars() {
    # No environment variables needed - OpenCode uses ~/.opencode/ for auth
    :
}

# ============================================================================
# CONFIG PATHS
# ============================================================================

# Get the OpenCode credentials path inside container
opencode_get_credentials_path() {
    printf '/home/user/.opencode'
}

# Get the OpenCode config path inside container
opencode_get_config_path() {
    printf '/home/user/.config/opencode'
}

# ============================================================================
# HOST CONFIG DETECTION
# ============================================================================

# Detect existing OpenCode installation on host
opencode_detect_host_config() {
    local host_opencode_dir="$HOME/.opencode"
    local host_config_dir="$HOME/.config/opencode"

    if [[ -d "$host_opencode_dir" ]]; then
        printf '%s' "$host_opencode_dir"
        return 0
    fi

    if [[ -d "$host_config_dir" ]]; then
        printf '%s' "$host_config_dir"
        return 0
    fi

    return 1
}

# Get what OpenCode config files to import
opencode_get_importable_files() {
    cat << 'EOF'
config.toml:Configuration:settings
auth.json:Auth tokens:API keys
EOF
}

# ============================================================================
# EXPORTS
# ============================================================================

export -f opencode_get_binary_name
export -f opencode_get_installed_version opencode_get_latest_version opencode_update_available opencode_get_download_url
export -f opencode_get_dockerfile opencode_get_dockerfile_install opencode_get_entrypoint_command opencode_get_env_vars
export -f opencode_get_credentials_path opencode_get_config_path
export -f opencode_detect_host_config opencode_get_importable_files
