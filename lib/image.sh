#!/usr/bin/env bash
# Image management - Building and managing container images
# ============================================================================

# Get the image name for an agent in the current project
get_agent_image_name() {
    local agent="$1"
    local project_hash
    project_hash=$(generate_project_folder_name "$PROJECT_DIR")
    printf 'agentbox-%s-%s' "$agent" "$project_hash"
}

# Build the base image
build_base_image() {
    local force="${1:-false}"
    local image_name="agentbox-base"
    local cmd
    cmd=$(container_cmd)

    # Skip if image exists and not forcing rebuild
    if [[ "$force" != "true" ]] && container_image_exists "$image_name"; then
        # Check if version matches
        local image_version
        image_version=$($cmd inspect "$image_name" --format '{{index .Config.Labels "agentbox.version"}}' 2>/dev/null || printf '')
        
        if [[ "$image_version" == "$AGENTBOX_VERSION" ]]; then
            return 0
        fi
        info "Base image version mismatch ($image_version != $AGENTBOX_VERSION). Rebuilding..."
    fi

    info "Building base image with $cmd..."

    local build_context="${AGENTBOX_CACHE}/build"
    mkdir -p "$build_context"

    # Copy build files
    cp "${BUILD_DIR}/Dockerfile.base" "$build_context/Dockerfile"
    cp "${BUILD_DIR}/docker-entrypoint" "$build_context/docker-entrypoint"
    cp "${BUILD_DIR}/dockerignore" "$build_context/.dockerignore"
    chmod +x "$build_context/docker-entrypoint"

    local build_args=()

    # Podman-specific build optimizations
    if [[ "$cmd" == "podman" ]]; then
        build_args+=("--layers")
        build_args+=("--pull=newer")
    else
        # Docker BuildKit
        export DOCKER_BUILDKIT=1
        build_args+=("--progress=auto")
    fi

    local target_arch
    target_arch=$(detect_arch)

    build_args+=(
        --build-arg "USER_ID=$(id -u)"
        --build-arg "GROUP_ID=$(id -g)"
        --build-arg "USERNAME=user"
        --build-arg "TARGETARCH=$target_arch"
        --build-arg "AGENTBOX_VERSION=$AGENTBOX_VERSION"
        -t "$image_name"
        -f "$build_context/Dockerfile"
        "$build_context"
    )

    $cmd build "${build_args[@]}" || error "Failed to build base image"

    success "Base image built"
}

# Build the Squid proxy image
build_squid_image() {
    local force="${1:-false}"
    local image_name="agentbox-squid"
    local cmd
    cmd=$(container_cmd)

    if [[ "$force" != "true" ]] && container_image_exists "$image_name"; then
        local image_version
        image_version=$($cmd inspect "$image_name" --format '{{index .Config.Labels "agentbox.version"}}' 2>/dev/null || printf '')
        if [[ "$image_version" == "$AGENTBOX_VERSION" ]]; then
            return 0
        fi
        info "Squid image version mismatch ($image_version != $AGENTBOX_VERSION). Rebuilding..."
    fi

    info "Building Squid proxy image..."

    local build_context="${AGENTBOX_CACHE}/build-squid"
    mkdir -p "$build_context"

    cat > "$build_context/Dockerfile" << 'EOF'
FROM alpine:latest
ARG AGENTBOX_VERSION
RUN apk add --no-cache squid socat
RUN mkdir -p /etc/squid
LABEL agentbox.version="${AGENTBOX_VERSION}"
# Default config will be mounted
CMD ["squid", "-N", "-d", "1", "-f", "/etc/squid/squid.conf"]
EOF

    local build_args=()
    if [[ "$cmd" == "podman" ]]; then
        build_args+=("--layers")
    else
        export DOCKER_BUILDKIT=1
    fi

    build_args+=(
        --build-arg "AGENTBOX_VERSION=$AGENTBOX_VERSION"
        -t "$image_name"
        "$build_context"
    )

    $cmd build "${build_args[@]}" || error "Failed to build Squid image"
}

# Build an agent core image
# Usage: build_agent_core_image <agent> [force]
build_agent_core_image() {
    local agent="$1"
    local force="${2:-false}"
    local image_name="agentbox-${agent}-core"
    local cmd
    cmd=$(container_cmd)

    # Skip if image exists and not forcing rebuild
    if [[ "$force" != "true" ]] && container_image_exists "$image_name"; then
        # Check if version matches
        local image_version
        image_version=$($cmd inspect "$image_name" --format '{{index .Config.Labels "agentbox.version"}}' 2>/dev/null || printf '')
        
        # If version matches (and for agents that use base, we rely on base check or recursive check logic? 
        # Actually base check happens below. But if core exists, we typically return.
        # We should check version here to force update.)
        if [[ "$image_version" == "$AGENTBOX_VERSION" ]]; then
            # Still ensure base exists (except for agents with custom base)
            if [[ "$agent" != "opencode" ]]; then
                build_base_image "false"
            fi
            return 0
        fi
        info "Agent core image version mismatch ($image_version != $AGENTBOX_VERSION). Rebuilding..."
    fi

    # Build base image (force if we're forcing) - except for agents with custom base
    if [[ "$agent" != "opencode" ]]; then
        build_base_image "$force"
    fi

    # Ensure Squid is rebuilt when any agent is rebuilt
    build_squid_image "true"

    info "Building $agent core image with $cmd..."

    local build_context="${AGENTBOX_CACHE}/build-${agent}"
    mkdir -p "$build_context"

    # Create agent-specific Dockerfile
    local dockerfile="$build_context/Dockerfile"

    # Add agent-specific installation
    case "$agent" in
        claude)
            # Start from agentbox base
            printf 'FROM agentbox-base\n\n' > "$dockerfile"
            claude_get_dockerfile_install >> "$dockerfile"
            ;;
        codex)
            # Start from agentbox base
            printf 'FROM agentbox-base\n\n' > "$dockerfile"
            local version
            version=$(codex_get_latest_version) || version="latest"
            printf 'ARG CODEX_VERSION=%s\n' "$version" >> "$dockerfile"
            codex_get_dockerfile_install >> "$dockerfile"
            ;;
        opencode)
            # OpenCode uses official image as base
            opencode_get_dockerfile >> "$dockerfile"
            # Copy entrypoint to build context
            cp "${SCRIPT_DIR}/build/docker-entrypoint-opencode" "$build_context/"
            chmod +x "$build_context/docker-entrypoint-opencode"
            ;;
    esac

    # Add label
    printf '\nLABEL agentbox.agent="%s"\n' "$agent" >> "$dockerfile"
    printf 'LABEL agentbox.version="%s"\n' "$AGENTBOX_VERSION" >> "$dockerfile"

    local build_args=()

    if [[ "$cmd" == "podman" ]]; then
        build_args+=("--layers")
    else
        export DOCKER_BUILDKIT=1
        build_args+=("--progress=auto")
    fi

    build_args+=(
        -t "$image_name"
        -f "$dockerfile"
        "$build_context"
    )

    $cmd build "${build_args[@]}" || error "Failed to build $agent core image"

    # Save the installed version
    local version_file
    version_file=$(get_agent_config_dir "$agent")/installed_version
    local version="unknown"
    case "$agent" in
        claude)
            version=$(claude_get_latest_version 2>/dev/null) || version="unknown"
            ;;
        codex)
            version=$(codex_get_latest_version 2>/dev/null) || version="unknown"
            ;;
        opencode)
            version=$(opencode_get_latest_version 2>/dev/null) || version="unknown"
            ;;
    esac
    mkdir -p "$(dirname "$version_file")"
    printf '%s' "$version" > "$version_file"

    success "$agent core image built (version: $version)"
}

# Check if an agent needs to be updated
# Returns 0 if update needed, 1 if up to date
check_agent_update() {
    local agent="$1"
    local version_file
    version_file=$(get_agent_config_dir "$agent")/installed_version

    # If no version file, can't check
    if [[ ! -f "$version_file" ]]; then
        return 1
    fi

    local installed_version
    installed_version=$(cat "$version_file")

    local latest_version
    case "$agent" in
        claude)
            latest_version=$(claude_get_latest_version 2>/dev/null) || return 1
            ;;
        codex)
            latest_version=$(codex_get_latest_version 2>/dev/null) || return 1
            ;;
        opencode)
            latest_version=$(opencode_get_latest_version 2>/dev/null) || return 1
            ;;
        *)
            return 1
            ;;
    esac

    if [[ "$installed_version" != "$latest_version" ]]; then
        printf '%s' "$latest_version"
        return 0
    fi

    return 1
}

# Build an agent project image (with profiles)
build_agent_project_image() {
    local agent="$1"
    local image_name
    image_name=$(get_agent_image_name "$agent")
    local core_image="agentbox-${agent}-core"
    local cmd
    cmd=$(container_cmd)

    # Ensure core image exists
    build_agent_core_image "$agent"

    # Check if profiles have changed
    local profiles_file="${PROJECT_AGENTBOX_DIR}/${agent}/profiles.ini"
    local current_hash=""

    if [[ -f "$profiles_file" ]]; then
        current_hash=$(cksum "$profiles_file" | cut -d' ' -f1)
    fi

    # Check existing image
    if container_image_exists "$image_name"; then
        # Check if core image is newer than project image
        local core_created project_created
        core_created=$($cmd inspect "$core_image" --format '{{.Created}}' 2>/dev/null || printf '')
        project_created=$($cmd inspect "$image_name" --format '{{.Created}}' 2>/dev/null || printf '')

        # If core is newer, rebuild project image
        if [[ -n "$core_created" ]] && [[ -n "$project_created" ]]; then
            if [[ "$core_created" > "$project_created" ]]; then
                info "Core image updated, rebuilding project image..."
            else
                # Check profiles hash only if core hasn't changed
                local image_hash
                image_hash=$($cmd inspect "$image_name" --format '{{index .Config.Labels "agentbox.profiles.hash"}}' 2>/dev/null || printf '')

                if [[ "$current_hash" == "$image_hash" ]]; then
                    return 0
                fi
            fi
        fi
    fi

    info "Building $agent project image with $cmd..."

    local build_context="${AGENTBOX_CACHE}/build-${agent}-project"
    mkdir -p "$build_context"

    local dockerfile="$build_context/Dockerfile"
    printf 'FROM %s\n\n' "$core_image" > "$dockerfile"

    # Add profile installations
    if [[ -f "$profiles_file" ]]; then
        local profile
        while IFS= read -r profile; do
            if [[ -n "$profile" ]] && [[ ! "$profile" =~ ^# ]]; then
                local profile_fn="get_profile_${profile//-/_}"
                if type -t "$profile_fn" >/dev/null 2>&1; then
                    "$profile_fn" >> "$dockerfile"
                fi
            fi
        done < <(read_profile_section "$profiles_file" "profiles")
    fi

    # Add labels
    printf '\nLABEL agentbox.profiles.hash="%s"\n' "$current_hash" >> "$dockerfile"

    local build_args=()

    if [[ "$cmd" == "podman" ]]; then
        build_args+=("--layers")
    else
        export DOCKER_BUILDKIT=1
        build_args+=("--progress=auto")
    fi

    build_args+=(
        -t "$image_name"
        -f "$dockerfile"
        "$build_context"
    )

    $cmd build "${build_args[@]}" || error "Failed to build $agent project image"

    success "$agent project image built"
}

export -f get_agent_image_name build_base_image build_agent_core_image build_agent_project_image check_agent_update build_squid_image
