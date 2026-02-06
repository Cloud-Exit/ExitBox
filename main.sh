#!/usr/bin/env bash
# ==============================================================================
#  Agentbox – Multi-Agent Docker Sandbox
#
#  Run AI coding assistants (Claude, Codex, OpenCode) in isolated containers
# ==============================================================================

set -euo pipefail

# Version
readonly AGENTBOX_VERSION="3.1.0"

# Add error handler
trap 'exit_code=$?; [[ $exit_code -eq 130 ]] && exit 130 || { printf "Error at line %s: Command failed with exit code %s\n" "$LINENO" "$exit_code" >&2; printf "Failed command: %s\n" "$BASH_COMMAND" >&2; }' ERR INT

# ============================================================================
# SCRIPT PATH RESOLUTION
# ============================================================================

get_script_path() {
    local source="${BASH_SOURCE[0]:-$0}"
    while [[ -L "$source" ]]; do
        local dir
        dir=$(cd -P "$(dirname "$source")" && pwd)
        source=$(readlink "$source")
        if [[ "$source" != /* ]]; then
            source="$dir/$source"
        fi
    done
    printf '%s/%s' "$(cd -P "$(dirname "$source")" && pwd)" "$(basename "$source")"
}

readonly SCRIPT_PATH="$(get_script_path)"
readonly SCRIPT_DIR="$(dirname "$SCRIPT_PATH")"
export SCRIPT_PATH SCRIPT_DIR

# Set PROJECT_DIR early
export PROJECT_DIR="${PROJECT_DIR:-$(pwd)}"

# Initialize VERBOSE
export VERBOSE=false

# ============================================================================
# LOAD LIBRARIES
# ============================================================================

LIB_DIR="${SCRIPT_DIR}/lib"

# Load libraries in dependency order
for lib in common env os state cli config project container image network runtime preflight welcome commands; do
    # shellcheck source=/dev/null
    source "${LIB_DIR}/${lib}.sh"
done

# Load agent modules
source "${LIB_DIR}/agents/base.sh"
source "${LIB_DIR}/agents/claude.sh"
source "${LIB_DIR}/agents/codex.sh"
source "${LIB_DIR}/agents/opencode.sh"

# Load agent commands
source "${LIB_DIR}/commands.agent.sh"

# ============================================================================
# MAIN FUNCTION
# ============================================================================

main() {
    # Save original arguments
    local original_args=("$@")

    # Enable BuildKit for Docker
    export DOCKER_BUILDKIT=1

    # Rootless-only guardrail
    if [[ "$(id -u)" == "0" ]]; then
        error "Refusing to run as root. Agentbox requires a non-root user."
    fi


    # Step 1: Update symlink
    update_symlink

    # Step 2: Initialize config directory
    init_agentbox_config

    # Step 3: Parse CLI arguments
    parse_cli "$@"

    # Step 4: Handle host flags
    if [[ "$CLI_VERBOSE" == "true" ]]; then
        VERBOSE=true
        export VERBOSE
    fi

    if [[ "$CLI_HELP" == "true" ]]; then
        print_usage
        exit 0
    fi

    if [[ "$CLI_VERSION" == "true" ]]; then
        print_version
        exit 0
    fi

    # Step 5: Check if this is first run
    if is_first_install; then
        show_first_time_welcome
        mark_installed
        save_version
        exit 0
    fi

    # Step 6: Determine what to do
    local cmd="${CLI_COMMAND:-}"

    # If no command and no agent, show list
    if [[ -z "$cmd" ]]; then
        _cmd_agent_list
        exit 0
    fi

    # Step 7: Check if command is an agent name
    if is_agent_name "$cmd"; then
        # Running an agent
        local agent="$cmd"
        export AGENTBOX_CURRENT_AGENT="$agent"

        # Check if agent is enabled
        if ! agent_is_enabled "$agent"; then
            error "Agent '$agent' is not enabled. Run 'agentbox enable $agent' first."
        fi

        # Check if subcommand is 'profile'
        if [[ "${CLI_SUBCOMMAND:-}" == "profile" ]]; then
            _cmd_agent_profile "$agent" "${CLI_ARGS[@]:-}"
            exit $?
        fi

        # Get command requirements
        local requirements
        requirements=$(get_command_requirements "$cmd")

        # Run Docker checks if needed
        if [[ "$requirements" == "docker" ]]; then
            local docker_status
            docker_status=$(check_container_runtime; printf '%s' $?)

            case "$docker_status" in
                1) install_container_runtime ;;
                2)
                    warn "Docker is installed but not running."
                    case "$(uname -s)" in
                        Darwin)
                            error "Docker Desktop is not running. Please start it."
                            ;;
                        Linux)
                            warn "Starting Docker..."
                            sudo systemctl start docker
                            if ! docker info >/dev/null 2>&1; then
                                error "Failed to start Docker"
                            fi
                            ;;
                    esac
                    ;;
                3)
                    configure_docker_nonroot
                    ;;
            esac

            # Initialize project
            init_project_dir "$PROJECT_DIR"

            # Build images if needed
            build_agent_project_image "$agent"

            # Run the agent
            _cmd_agent_run "$agent" "${CLI_PASSTHROUGH[@]:-}"
        fi

        exit $?
    fi

    # Logs command (no container/runtime needed)
    if [[ "$cmd" == "logs" ]]; then
        _cmd_agent_logs "${CLI_SUBCOMMAND:-}" "${CLI_ARGS[@]:-}"
        exit $?
    fi

    # Step 8: Handle other commands
    local requirements
    requirements=$(get_command_requirements "$cmd" "${CLI_SUBCOMMAND:-}")

    # Run Docker checks if needed
    if [[ "$requirements" == "docker" ]] || [[ "$requirements" == "image" ]]; then
        local docker_status
        docker_status=$(check_container_runtime; printf '%s' $?)

        case "$docker_status" in
            1) install_container_runtime ;;
            2)
                warn "Docker is installed but not running."
                case "$(uname -s)" in
                    Darwin)
                        error "Docker Desktop is not running. Please start it."
                        ;;
                    Linux)
                        warn "Starting Docker..."
                        sudo systemctl start docker
                        if ! docker info >/dev/null 2>&1; then
                            error "Failed to start Docker"
                        fi
                        ;;
                esac
                ;;
            3)
                configure_docker_nonroot
                ;;
        esac
    fi

    # Dispatch to command handler
    dispatch_command "$cmd" "${CLI_SUBCOMMAND:-}" "${CLI_ARGS[@]:-}"
}

# Run main
main "$@"
