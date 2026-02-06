#!/usr/bin/env bash
# Runtime management - Running containers
# ============================================================================

# Count running agent containers (excluding squid proxy).
count_running_agent_containers() {
    local cmd
    cmd=$(container_cmd)

    local count=0
    local name
    while IFS= read -r name; do
        [[ -z "$name" ]] && continue
        [[ "$name" == "$AGENTBOX_SQUID_CONTAINER" ]] && continue
        if [[ "$name" == agentbox-* ]]; then
            ((count++))
        fi
    done < <($cmd ps --format '{{.Names}}' 2>/dev/null || true)

    printf '%s' "$count"
}

# Stop/remove squid proxy if no agent containers are running.
cleanup_squid_if_unused() {
    local cmd
    cmd=$(container_cmd)

    local running_agents_count
    running_agents_count=$(count_running_agent_containers)

    if [[ "$running_agents_count" == "0" ]]; then
        if $cmd ps --filter "name=^/${AGENTBOX_SQUID_CONTAINER}$" --format '{{.Names}}' | grep -q "^${AGENTBOX_SQUID_CONTAINER}$"; then
            info "Stopping Squid proxy (no running agents)..."
            $cmd rm -f "$AGENTBOX_SQUID_CONTAINER" >/dev/null 2>&1 || true
        fi
    fi
}

# Return 0 if a directory has at least one entry.
_dir_has_entries() {
    local dir="$1"
    [[ -d "$dir" ]] || return 1
    find "$dir" -mindepth 1 -maxdepth 1 -print -quit 2>/dev/null | grep -q .
}

# Seed managed directory from host directory once (only when managed is empty).
seed_dir_from_host_once() {
    local host_dir="$1"
    local managed_dir="$2"

    [[ -d "$host_dir" ]] || return 0
    mkdir -p "$managed_dir"

    if _dir_has_entries "$managed_dir"; then
        return 0
    fi

    if ! cp -R "$host_dir"/. "$managed_dir"/ 2>/dev/null; then
        warn "Failed to seed config from $host_dir to $managed_dir"
    fi
}

# Seed managed file from host file once (only when managed file does not exist).
seed_file_from_host_once() {
    local host_file="$1"
    local managed_file="$2"

    [[ -f "$host_file" ]] || return 0
    mkdir -p "$(dirname "$managed_file")"

    if [[ -f "$managed_file" ]]; then
        return 0
    fi

    if ! cp "$host_file" "$managed_file" 2>/dev/null; then
        warn "Failed to seed config from $host_file to $managed_file"
    fi
}

# Use managed config mounts on non-Linux hosts, where container mounts are VM-backed.
should_use_managed_config_mounts() {
    if [[ "${AGENTBOX_FORCE_MANAGED_CONFIG_MOUNTS:-false}" == "true" ]]; then
        return 0
    fi
    if [[ "${AGENTBOX_FORCE_HOST_CONFIG_MOUNTS:-false}" == "true" ]]; then
        return 1
    fi

    local os
    os=$(detect_os)
    [[ "$os" != "linux" ]]
}

# Resolve source directory for mounting into container.
resolve_dir_mount_source() {
    local host_dir="$1"
    local managed_dir="$2"
    local use_managed="$3"

    if [[ "$use_managed" == "true" ]]; then
        seed_dir_from_host_once "$host_dir" "$managed_dir"
        mkdir -p "$managed_dir"
        printf '%s' "$managed_dir"
        return 0
    fi

    if [[ -d "$host_dir" ]]; then
        printf '%s' "$host_dir"
    else
        mkdir -p "$managed_dir"
        printf '%s' "$managed_dir"
    fi
}

# Resolve source file for mounting into container.
resolve_file_mount_source() {
    local host_file="$1"
    local managed_file="$2"
    local use_managed="$3"

    if [[ "$use_managed" == "true" ]]; then
        seed_file_from_host_once "$host_file" "$managed_file"
        mkdir -p "$(dirname "$managed_file")"
        if [[ ! -f "$managed_file" ]]; then
            touch "$managed_file"
        fi
        printf '%s' "$managed_file"
        return 0
    fi

    if [[ -f "$host_file" ]]; then
        printf '%s' "$host_file"
    else
        mkdir -p "$(dirname "$managed_file")"
        if [[ ! -f "$managed_file" ]]; then
            touch "$managed_file"
        fi
        printf '%s' "$managed_file"
    fi
}

# Run an agent container
# Usage: run_agent_container <agent> <container_name> <mode> [args...]
run_agent_container() {
    local agent="$1"
    local container_name="$2"
    local run_mode="$3"  # "interactive", "detached", "pipe"
    shift 3
    local container_args=("$@")

    local cmd
    cmd=$(container_cmd)

    local run_args=()

    # Set run mode
    case "$run_mode" in
        "interactive")
            if [[ -t 0 ]] && [[ -t 1 ]]; then
                run_args+=("-it")
            fi
            run_args+=("--rm")
            if [[ -n "$container_name" ]]; then
                run_args+=("--name" "$container_name")
            fi
            run_args+=("--init")
            ;;
        "detached")
            run_args+=("-d")
            if [[ -n "$container_name" ]]; then
                run_args+=("--name" "$container_name")
            fi
            ;;
        "pipe")
            run_args+=("--rm" "--init")
            ;;
    esac

    # Podman-specific: use keep-id for rootless uid mapping
    if [[ "$cmd" == "podman" ]]; then
        run_args+=("--userns=keep-id")
        run_args+=("--security-opt=no-new-privileges")
    fi

    # Network Setup: agents are attached only to the internal network.
    ensure_networks
    run_args+=("--network" "$AGENTBOX_INTERNAL_NETWORK")
    
    # Firewall / Proxy Setup
    if [[ "${AGENTBOX_NO_FIREWALL:-false}" != "true" ]]; then
        if ! start_squid_proxy; then
            error "Failed to start firewall (Squid proxy). Aborting for security."
        fi
        # Inject proxy vars
        # We read the output of get_proxy_env_vars which returns a string of flags
        local proxy_flags
        proxy_flags=$(get_proxy_env_vars)
        # We can just split it by space if we trust it doesn't contain spaces in values (urls don't).
        read -r -a proxy_flags_array <<< "$proxy_flags"
        run_args+=("${proxy_flags_array[@]}")
    fi

    # Get the image name for this agent and project
    local image_name
    image_name=$(get_agent_image_name "$agent")

    # Resource Limits (Sane defaults)
    run_args+=(
        "--memory=8g"
        "--cpus=4"
    )

    # Mount workspace
    local workspace_mount_mode=""
    if [[ "${CLI_READ_ONLY:-false}" == "true" ]]; then
        workspace_mount_mode=":ro"
    fi

    run_args+=(
        -w /workspace
        -v "${PROJECT_DIR}:/workspace${workspace_mount_mode}"
    )

    # Enforce non-root execution for all agent containers.
    run_args+=("--user" "$(id -u):$(id -g)")


    # Mount additional include directories inside /workspace
    if [[ ${#CLI_INCLUDE_DIRS[@]} -gt 0 ]]; then
        local include_src
        for include_src in "${CLI_INCLUDE_DIRS[@]}"; do
            if [[ -z "$include_src" ]]; then
                continue
            fi

            # Expand ~/
            if [[ "$include_src" == "~/"* ]]; then
                include_src="$HOME/${include_src#~/}"
            fi

            # Resolve relative paths against project dir
            if [[ "$include_src" != /* ]]; then
                include_src="$PROJECT_DIR/$include_src"
            fi

            # Trim trailing slash
            include_src="${include_src%/}"

            if [[ ! -d "$include_src" ]]; then
                warn "Include dir not found: $include_src"
                continue
            fi

            local rel=""
            if [[ "$include_src" == /tmp/* ]]; then
                rel="${include_src#/tmp/}"
            elif [[ "$include_src" == /tmp ]]; then
                warn "Skipping include dir /tmp (would map to /workspace)"
                continue
            else
                rel="${include_src#/}"
            fi

            if [[ -z "$rel" ]]; then
                warn "Skipping include dir $include_src (invalid target)"
                continue
            fi

            local dest="/workspace/${rel}"
            run_args+=(-v "$include_src:$dest")
        done
    fi


    # Mount agent-specific config directories
    local agent_config_dir
    agent_config_dir=$(get_agent_config_dir "$agent")

    # Ensure config directories exist
    mkdir -p "$agent_config_dir"

    local use_managed_mounts="false"
    if should_use_managed_config_mounts; then
        use_managed_mounts="true"
    fi

    # Mount credentials based on agent type
    case "$agent" in
        claude)
            if [[ "$use_managed_mounts" == "true" ]]; then
                local claude_dir_src
                claude_dir_src=$(resolve_dir_mount_source "$HOME/.claude" "$agent_config_dir/.claude" "$use_managed_mounts")
                run_args+=(-v "$claude_dir_src":/home/user/.claude)

                local claude_json_src
                claude_json_src=$(resolve_file_mount_source "$HOME/.claude.json" "$agent_config_dir/.claude.json" "$use_managed_mounts")
                run_args+=(-v "$claude_json_src":/home/user/.claude.json)

                local claude_share_dir
                claude_share_dir=$(resolve_dir_mount_source "$HOME/.local/share/claude" "$agent_config_dir/.local/share/claude" "$use_managed_mounts")
                run_args+=(-v "$claude_share_dir":/home/user/.local/share/claude)
            else
                # Linux behavior: keep native host mounts unchanged.
                if [[ -d "$HOME/.claude" ]]; then
                    run_args+=(-v "$HOME/.claude":/home/user/.claude)
                else
                    mkdir -p "$agent_config_dir/.claude"
                    run_args+=(-v "$agent_config_dir/.claude":/home/user/.claude)
                fi
                if [[ -f "$HOME/.claude.json" ]]; then
                    run_args+=(-v "$HOME/.claude.json":/home/user/.claude.json)
                fi
            fi

            # Claude binary is installed in the container via the build process
            mkdir -p "$agent_config_dir/.config"
            run_args+=(-v "$agent_config_dir/.config":/home/user/.config)
            ;;
        codex)
            if [[ "$use_managed_mounts" == "true" ]]; then
                local codex_dir_src
                codex_dir_src=$(resolve_dir_mount_source "$HOME/.codex" "$agent_config_dir/.codex" "$use_managed_mounts")
                run_args+=(-v "$codex_dir_src":/home/user/.codex)

                local codex_config_dir="$agent_config_dir/.config/codex"
                seed_dir_from_host_once "$HOME/.config/codex" "$codex_config_dir"
                mkdir -p "$codex_config_dir"
                run_args+=(-v "$codex_config_dir":/home/user/.config/codex)
            else
                # Linux behavior: keep native host mounts unchanged.
                if [[ -d "$HOME/.codex" ]]; then
                    run_args+=(-v "$HOME/.codex":/home/user/.codex)
                else
                    mkdir -p "$agent_config_dir/.codex"
                    run_args+=(-v "$agent_config_dir/.codex":/home/user/.codex)
                fi

                mkdir -p "$agent_config_dir/.config/codex"
                run_args+=(-v "$agent_config_dir/.config/codex":/home/user/.config/codex)
            fi

            # Configure shared Squid callback relay target before Codex starts.
            configure_codex_callback_relay "$container_name"
            ;;
        opencode)
            if [[ "$use_managed_mounts" == "true" ]]; then
                local opencode_dir_src
                opencode_dir_src=$(resolve_dir_mount_source "$HOME/.opencode" "$agent_config_dir/.opencode" "$use_managed_mounts")
                if [[ ! -w "$opencode_dir_src" ]]; then
                    error "OpenCode config dir not writable: $opencode_dir_src (fix ownership, e.g. sudo chown -R $USER:$USER \"$opencode_dir_src\")"
                fi
                run_args+=(-v "$opencode_dir_src":/home/user/.opencode)

                local opencode_config_dir="$agent_config_dir/.config/opencode"
                seed_dir_from_host_once "$HOME/.config/opencode" "$opencode_config_dir"
                mkdir -p "$opencode_config_dir"
                if [[ ! -w "$opencode_config_dir" ]]; then
                    error "OpenCode config dir not writable: $opencode_config_dir (fix ownership, e.g. sudo chown -R $USER:$USER \"$opencode_config_dir\")"
                fi
                run_args+=(-v "$opencode_config_dir":/home/user/.config/opencode)

                # Mount local share for OpenCode auth/state
                local opencode_share_dir
                opencode_share_dir=$(resolve_dir_mount_source "$HOME/.local/share/opencode" "$agent_config_dir/.local/share/opencode" "$use_managed_mounts")
                if [[ ! -w "$opencode_share_dir" ]]; then
                    error "OpenCode share dir not writable: $opencode_share_dir (fix ownership, e.g. sudo chown -R $USER:$USER \"$opencode_share_dir\")"
                fi
                run_args+=(-v "$opencode_share_dir":/home/user/.local/share/opencode)
            else
                # Linux behavior: keep native host mounts unchanged.
                if [[ -d "$HOME/.opencode" ]]; then
                    if [[ ! -w "$HOME/.opencode" ]]; then
                        error "OpenCode config dir not writable: $HOME/.opencode (fix ownership, e.g. sudo chown -R $USER:$USER \"$HOME/.opencode\")"
                    fi
                    run_args+=(-v "$HOME/.opencode":/home/user/.opencode)
                else
                    mkdir -p "$agent_config_dir/.opencode"
                    if [[ ! -w "$agent_config_dir/.opencode" ]]; then
                        error "OpenCode config dir not writable: $agent_config_dir/.opencode (fix ownership, e.g. sudo chown -R $USER:$USER \"$agent_config_dir/.opencode\")"
                    fi
                    run_args+=(-v "$agent_config_dir/.opencode":/home/user/.opencode)
                fi
                mkdir -p "$agent_config_dir/.config/opencode"
                if [[ ! -w "$agent_config_dir/.config/opencode" ]]; then
                    error "OpenCode config dir not writable: $agent_config_dir/.config/opencode (fix ownership, e.g. sudo chown -R $USER:$USER \"$agent_config_dir/.config/opencode\")"
                fi
                if [[ -f "$HOME/.config/opencode/opencode.json" ]] && [[ ! -f "$agent_config_dir/.config/opencode/opencode.json" ]]; then
                    cp "$HOME/.config/opencode/opencode.json" "$agent_config_dir/.config/opencode/opencode.json"
                fi
                run_args+=(-v "$agent_config_dir/.config/opencode":/home/user/.config/opencode)
                local opencode_share_dir="$HOME/.local/share/opencode"
                mkdir -p "$opencode_share_dir"
                if [[ ! -w "$opencode_share_dir" ]]; then
                    error "OpenCode share dir not writable: $opencode_share_dir (fix ownership, e.g. sudo chown -R $USER:$USER \"$opencode_share_dir\")"
                fi
                run_args+=(-v "$opencode_share_dir":/home/user/.local/share/opencode)
            fi
            # Mount local state for OpenCode (prevents root-owned /home/user/.local)
            local opencode_state_dir="$agent_config_dir/.local/state"
            mkdir -p "$opencode_state_dir"
            if [[ ! -w "$opencode_state_dir" ]]; then
                error "OpenCode state dir not writable: $opencode_state_dir (fix ownership, e.g. sudo chown -R $USER:$USER \"$opencode_state_dir\")"
            fi
            run_args+=(-v "$opencode_state_dir":/home/user/.local/state)
            ;;
    esac

    # SECURITY FIX: REMOVED DEFAULT SSH MOUNT
    # Previous code mounted ~/.ssh read-only. This is unsafe.
    # Users must provide secrets explicitly via other means if needed.

    # Mount tmux socket if available
    if [[ -n "${TMUX:-}" ]]; then
        local tmux_socket="${TMUX%%,*}"
        local tmux_socket_dir
        tmux_socket_dir=$(dirname "$tmux_socket")
        if [[ -d "$tmux_socket_dir" ]]; then
            run_args+=(-v "$tmux_socket_dir:$tmux_socket_dir")
            run_args+=(-e "TMUX=$TMUX")
        fi
    fi

    # Add environment variables
    local project_name
    project_name=$(basename "$PROJECT_DIR")

    run_args+=(
        -e "NODE_ENV=${NODE_ENV:-production}"
        -e "TERM=${TERM:-xterm-256color}"
        -e "VERBOSE=${VERBOSE:-false}"
        -e "AGENTBOX_AGENT=$agent"
        -e "AGENTBOX_PROJECT_NAME=$project_name"
    )

    # Security options
    run_args+=(
        --security-opt=no-new-privileges:true
        --cap-drop=ALL
    )

    # Add image name
    run_args+=("$image_name")

    # Add container arguments
    if [[ ${#container_args[@]} -gt 0 ]]; then
        run_args+=("${container_args[@]}")
    fi

    # Run the container
    if [[ "${VERBOSE:-false}" == "true" ]]; then
        printf '[DEBUG] Container run: %s run %s\n' "$cmd" "${run_args[*]}" >&2
    fi

    # Always capture container exit code and continue to proxy cleanup even
    # when the agent exits non-zero (e.g., SIGINT/SIGTERM/user abort).
    local exit_code=0
    set +e
    $cmd run "${run_args[@]}"
    exit_code=$?
    set -e

    cleanup_squid_if_unused

    return $exit_code
}

# Check if a container exists
check_container_exists() {
    local container_name="$1"
    local cmd
    cmd=$(container_cmd)

    if $cmd ps -a --filter "name=^${container_name}$" --format "{{.Names}}" | grep -q "^${container_name}$"; then
        if $cmd ps --filter "name=^${container_name}$" --format "{{.Names}}" | grep -q "^${container_name}$"; then
            printf 'running'
        else
            printf 'stopped'
        fi
    else
        printf 'none'
    fi
}

# Clean up container resources
clean_container_resources() {
    local mode="${1:-unused}"
    local cmd
    cmd=$(container_cmd)

    case "$mode" in
        unused)
            info "Removing unused agentbox images..."
            $cmd images --filter "reference=agentbox-*" --format "{{.Repository}}:{{.Tag}}" | \
                while read -r image; do
                    if ! $cmd ps -a --filter "ancestor=$image" --format "{{.ID}}" | grep -q .; then
                        $cmd rmi "$image" 2>/dev/null || true
                    fi
                done

            # Podman-specific: prune build cache
            if [[ "$cmd" == "podman" ]]; then
                $cmd system prune -f 2>/dev/null || true
            fi
            ;;
        all)
            info "Removing all agentbox images..."
            $cmd images --filter "reference=agentbox-*" --format "{{.Repository}}:{{.Tag}}" | \
                xargs -r $cmd rmi -f 2>/dev/null || true
            ;;
        containers)
            info "Stopping all agentbox containers..."
            $cmd ps --filter "name=agentbox-*" --format "{{.ID}}" | \
                xargs -r $cmd stop 2>/dev/null || true
            cleanup_squid_if_unused
            ;;
    esac
}

# Alias for backwards compatibility
clean_docker_resources() {
    clean_container_resources "$@"
}


export -f count_running_agent_containers cleanup_squid_if_unused
export -f run_agent_container check_container_exists clean_container_resources clean_docker_resources
