#!/usr/bin/env bash
# CLI parsing - Command line argument handling
# ============================================================================
# Architecture: Four-bucket parsing system
# 1. Host flags: --verbose, --help, --version
# 2. Control flags: --update, --no-firewall
# 3. Script commands: list, enable, disable, rebuild, aliases, profile, info, clean
# 4. Pass-through: Everything after agent name goes to the agent CLI

# ============================================================================
# CLI STATE
# ============================================================================

# Parsed CLI values
CLI_COMMAND=""
CLI_SUBCOMMAND=""
CLI_AGENT=""
CLI_ARGS=()
CLI_PASSTHROUGH=()

# Host flags
CLI_VERBOSE=false
CLI_HELP=false
CLI_VERSION=false

# Control flags
CLI_UPDATE=false
CLI_NO_FIREWALL=false
CLI_READ_ONLY=false
CLI_NO_ENV=false

# User-specified environment variables (-e KEY=VALUE)
CLI_ENV_VARS=()

# Include directories to mount inside /workspace
CLI_INCLUDE_DIRS=()

# Extra tools (Alpine packages) to add to the image
CLI_TOOLS=()

# Extra domains to allow for this session
CLI_ALLOW_URLS=()

# Known agent names (populated from agents/base.sh)
AGENTBOX_AGENT_NAMES=(claude codex opencode)

# ============================================================================
# ARGUMENT PARSING
# ============================================================================

# Check if a value is a known agent name
is_agent_name() {
    local name="$1"
    local agent

    for agent in "${AGENTBOX_AGENT_NAMES[@]}"; do
        if [[ "$agent" == "$name" ]]; then
            return 0
        fi
    done

    return 1
}

# Parse command line arguments
parse_cli() {
    local args=("$@")
    local i=0
    local parsing_passthrough=false
    local explicit_passthrough=false

    while [[ $i -lt ${#args[@]} ]]; do
        local arg="${args[$i]}"

        # Once we hit --, everything is passthrough
        if [[ "$arg" == "--" ]]; then
            parsing_passthrough=true
            explicit_passthrough=true
            i=$((i + 1))
            continue
        fi

        # If parsing passthrough, collect all remaining args
        if [[ "$parsing_passthrough" == true ]]; then
            if [[ "$explicit_passthrough" == false ]]; then
                case "$arg" in
                    -i|--include-dir)
                        if [[ $((i + 1)) -ge ${#args[@]} ]]; then
                            error "--include-dir requires a path"
                        fi
                        i=$((i + 1))
                        CLI_INCLUDE_DIRS+=("${args[$i]}")
                        i=$((i + 1))
                        continue
                        ;;
                    -i=*|--include-dir=*)
                        CLI_INCLUDE_DIRS+=("${arg#*=}")
                        i=$((i + 1))
                        continue
                        ;;
                    -n|--no-env)
                        CLI_NO_ENV=true
                        i=$((i + 1))
                        continue
                        ;;
                    -u|--update)
                        CLI_UPDATE=true
                        i=$((i + 1))
                        continue
                        ;;
                    -f|--no-firewall)
                        CLI_NO_FIREWALL=true
                        export AGENTBOX_NO_FIREWALL=true
                        i=$((i + 1))
                        continue
                        ;;
                    -r|--read-only)
                        CLI_READ_ONLY=true
                        i=$((i + 1))
                        continue
                        ;;
                    -t|--tools)
                        if [[ $((i + 1)) -ge ${#args[@]} ]]; then
                            error "--tools requires a package list"
                        fi
                        i=$((i + 1))
                        IFS=',' read -ra items <<< "${args[$i]}"
                        CLI_TOOLS+=("${items[@]}")
                        i=$((i + 1))
                        continue
                        ;;
                    -a|--allow-urls)
                        if [[ $((i + 1)) -ge ${#args[@]} ]]; then
                            error "--allow-urls requires a domain list"
                        fi
                        i=$((i + 1))
                        IFS=',' read -ra items <<< "${args[$i]}"
                        CLI_ALLOW_URLS+=("${items[@]}")
                        i=$((i + 1))
                        continue
                        ;;
                    -e)
                        if [[ $((i + 1)) -ge ${#args[@]} ]]; then
                            error "-e requires a KEY=VALUE argument"
                        fi
                        i=$((i + 1))
                        CLI_ENV_VARS+=("${args[$i]}")
                        i=$((i + 1))
                        continue
                        ;;
                    -e=*|-e*)
                        local env_val="${arg#-e}"
                        env_val="${env_val#=}"
                        if [[ -z "$env_val" ]]; then
                            error "-e requires a KEY=VALUE argument"
                        fi
                        CLI_ENV_VARS+=("$env_val")
                        i=$((i + 1))
                        continue
                        ;;
                esac
            fi
            CLI_PASSTHROUGH+=("$arg")
            i=$((i + 1))
            continue
        fi

        # Parse host flags
        case "$arg" in
            -v|--verbose)
                CLI_VERBOSE=true
                VERBOSE=true
                export VERBOSE
                ;;
            -h|--help)
                CLI_HELP=true
                ;;
            -V|--version)
                CLI_VERSION=true
                ;;
            -u|--update)
                CLI_UPDATE=true
                ;;
            -f|--no-firewall)
                CLI_NO_FIREWALL=true
                export AGENTBOX_NO_FIREWALL=true
                ;;
            -r|--read-only)
                CLI_READ_ONLY=true
                ;;
            -n|--no-env)
                CLI_NO_ENV=true
                ;;
            -t|--tools)
                if [[ $((i + 1)) -ge ${#args[@]} ]]; then
                    error "--tools requires a package list"
                fi
                i=$((i + 1))
                IFS=',' read -ra items <<< "${args[$i]}"
                CLI_TOOLS+=("${items[@]}")
                ;;
            -a|--allow-urls)
                if [[ $((i + 1)) -ge ${#args[@]} ]]; then
                    error "--allow-urls requires a domain list"
                fi
                i=$((i + 1))
                IFS=',' read -ra items <<< "${args[$i]}"
                CLI_ALLOW_URLS+=("${items[@]}")
                ;;
            -e)
                if [[ $((i + 1)) -ge ${#args[@]} ]]; then
                    error "-e requires a KEY=VALUE argument"
                fi
                i=$((i + 1))
                CLI_ENV_VARS+=("${args[$i]}")
                ;;
            -e=*|-e*)
                local env_val="${arg#-e}"
                env_val="${env_val#=}"
                if [[ -z "$env_val" ]]; then
                    error "-e requires a KEY=VALUE argument"
                fi
                CLI_ENV_VARS+=("$env_val")
                ;;
            -i|--include-dir)
                if [[ $((i + 1)) -ge ${#args[@]} ]]; then
                    error "--include-dir requires a path"
                fi
                i=$((i + 1))
                CLI_INCLUDE_DIRS+=("${args[$i]}")
                ;;
            -i=*|--include-dir=*)
                CLI_INCLUDE_DIRS+=("${arg#*=}")
                ;;
            -*)
                # Unknown flag - could be for agent, save to passthrough
                CLI_PASSTHROUGH+=("$arg")
                ;;
            *)
                # Non-flag argument
                if [[ -z "$CLI_COMMAND" ]]; then
                    # First positional is the command
                    CLI_COMMAND="$arg"

                    # Check if it's an agent name
                    if is_agent_name "$arg"; then
                        CLI_AGENT="$arg"
                        # Everything after agent name is passthrough
                        parsing_passthrough=true
                    fi
                elif [[ -z "$CLI_SUBCOMMAND" ]]; then
                    # Second positional is subcommand
                    CLI_SUBCOMMAND="$arg"
                else
                    # Additional args
                    CLI_ARGS+=("$arg")
                fi
                ;;
        esac

        i=$((i + 1))
    done

    export CLI_COMMAND CLI_SUBCOMMAND CLI_AGENT CLI_VERBOSE CLI_HELP CLI_VERSION
    export CLI_UPDATE
}

# ============================================================================
# HELP TEXT
# ============================================================================

# Print main usage
print_usage() {
    cat << EOF
Usage: agentbox [options] <command> [args...]

Multi-Agent Docker Sandbox - Run AI coding assistants in isolated containers

Options:
  -v, --verbose           Enable verbose output
  -h, --help              Show this help message
  -V, --version           Show version information
  -u, --update            Check for and apply agent updates
  -f, --no-firewall       Disable network firewall *DANGEROUS*
  -r, --read-only         Mount workspace as read-only
  -n, --no-env            Don't pass host environment variables to container
  -e KEY=VALUE            Pass an environment variable to the container (repeatable)
  -i, --include-dir PATH  Mount a host dir inside /workspace (repeatable)
  -t, --tools PKG         Add Alpine packages to the image (comma-separated, persisted)
  -a, --allow-urls DOMAIN Allow extra domains for this session (comma-separated)

Agent Commands:
  agentbox <agent> [args...]           Run an agent (claude, codex, opencode)
  agentbox <agent> profile list        List available profiles
  agentbox <agent> profile add <name>  Add a profile
  agentbox <agent> profile remove <n>  Remove a profile
  agentbox <agent> profile status      Show current profiles

Management Commands:
  list               List available agents
  enable <agent>     Enable an agent
  disable <agent>    Disable an agent
  rebuild <agent>     Force rebuild of agent image
  import <agent>     Import agent config from host
  uninstall [agent]   Uninstall agentbox or specific agent
  aliases             Print shell aliases

Utility Commands:
  logs <agent>      Show latest agent log file
  info             Show system and project information
  clean            Clean up unused Docker resources
  projects         List known projects

Examples:
  agentbox claude                 # Run Claude (builds if needed)
  agentbox rebuild claude         # Force rebuild Claude image
  agentbox claude profile add go  # Add Go profile to Claude
  agentbox aliases >> ~/.zshrc    # Add agent aliases to shell

EOF
}

# Print version
print_version() {
    printf 'agentbox version %s\n' "${AGENTBOX_VERSION:-3.0.0}"
}

# ============================================================================
# EXPORTS
# ============================================================================

export CLI_COMMAND CLI_SUBCOMMAND CLI_AGENT
export CLI_VERBOSE CLI_HELP CLI_VERSION CLI_UPDATE CLI_NO_FIREWALL CLI_READ_ONLY CLI_NO_ENV
export CLI_TOOLS CLI_ALLOW_URLS
export -f is_agent_name parse_cli print_usage print_version
