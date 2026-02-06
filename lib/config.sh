#!/usr/bin/env bash
# Configuration management - Global and per-project settings
# ============================================================================

# ============================================================================
# DIRECTORY INITIALIZATION
# ============================================================================

# Initialize the agentbox configuration directory structure
init_agentbox_config() {
    # Create main config directory
    mkdir -p "$AGENTBOX_HOME"

    # Create per-agent directories (~/.config/agentbox/<agent>/)
    mkdir -p "$AGENTBOX_HOME/claude"
    mkdir -p "$AGENTBOX_HOME/codex"
    mkdir -p "$AGENTBOX_HOME/opencode"

    # Create projects directory
    mkdir -p "$AGENTBOX_HOME/projects"

    # Create cache directory
    mkdir -p "$AGENTBOX_CACHE"

    # Create data directory
    mkdir -p "$AGENTBOX_DATA"

    # Create default config if it doesn't exist
    if [[ ! -f "$AGENTBOX_HOME/config.ini" ]]; then
        cat > "$AGENTBOX_HOME/config.ini" << 'EOF'
# Agentbox Configuration
[agents]
# Enabled agents will be listed here
# claude=enabled
# codex=enabled
# opencode=enabled

[settings]
# Default settings
auto_update=false
EOF
    fi
}

# Initialize per-project configuration
init_project_config() {
    local project_dir="${1:-$PROJECT_DIR}"
    local project_agentbox_dir="${project_dir}/.agentbox"

    mkdir -p "$project_agentbox_dir"

    # Create per-agent directories
    local agent
    for agent in claude codex opencode; do
        mkdir -p "$project_agentbox_dir/$agent"
    done
}

# ============================================================================
# CONFIG FILE OPERATIONS
# ============================================================================

# Get a config value from an INI file
get_config_value() {
    local file="$1"
    local section="$2"
    local key="$3"
    local default="${4:-}"

    if [[ ! -f "$file" ]]; then
        printf '%s' "$default"
        return
    fi

    local in_section=false
    local value=""

    while IFS= read -r line; do
        # Skip empty lines and comments
        if [[ -z "$line" ]] || [[ "$line" =~ ^[[:space:]]*# ]]; then
            continue
        fi

        # Check for section header
        if [[ "$line" =~ ^\[([^]]+)\]$ ]]; then
            if [[ "${BASH_REMATCH[1]}" == "$section" ]]; then
                in_section=true
            else
                in_section=false
            fi
            continue
        fi

        # Look for key=value in target section
        if [[ "$in_section" == true ]]; then
            if [[ "$line" =~ ^[[:space:]]*${key}[[:space:]]*=[[:space:]]*(.*)$ ]]; then
                value="${BASH_REMATCH[1]}"
                break
            fi
        fi
    done < "$file"

    if [[ -n "$value" ]]; then
        printf '%s' "$value"
    else
        printf '%s' "$default"
    fi
}

# Set a config value in an INI file
set_config_value() {
    local file="$1"
    local section="$2"
    local key="$3"
    local value="$4"

    # Create file if it doesn't exist
    if [[ ! -f "$file" ]]; then
        printf '[%s]\n%s=%s\n' "$section" "$key" "$value" > "$file"
        return
    fi

    local temp_file
    temp_file=$(mktemp)
    local in_section=false
    local key_found=false
    local section_found=false

    while IFS= read -r line; do
        # Check for section header
        if [[ "$line" =~ ^\[([^]]+)\]$ ]]; then
            # If we were in target section and didn't find key, add it
            if [[ "$in_section" == true ]] && [[ "$key_found" == false ]]; then
                printf '%s=%s\n' "$key" "$value" >> "$temp_file"
                key_found=true
            fi

            if [[ "${BASH_REMATCH[1]}" == "$section" ]]; then
                in_section=true
                section_found=true
            else
                in_section=false
            fi
        fi

        # Update or skip existing key in target section
        if [[ "$in_section" == true ]] && [[ "$line" =~ ^[[:space:]]*${key}[[:space:]]*= ]]; then
            printf '%s=%s\n' "$key" "$value" >> "$temp_file"
            key_found=true
            continue
        fi

        printf '%s\n' "$line" >> "$temp_file"
    done < "$file"

    # Handle cases where section or key wasn't found
    if [[ "$section_found" == false ]]; then
        printf '\n[%s]\n%s=%s\n' "$section" "$key" "$value" >> "$temp_file"
    elif [[ "$key_found" == false ]]; then
        # Section exists but key doesn't - already handled above
        if [[ "$in_section" == true ]]; then
            printf '%s=%s\n' "$key" "$value" >> "$temp_file"
        fi
    fi

    mv "$temp_file" "$file"
}

# ============================================================================
# PROJECT PROFILES
# ============================================================================

# Get profiles for an agent in the current project
get_project_profiles() {
    local agent="${1:-$AGENTBOX_CURRENT_AGENT}"
    local profiles_file

    # Check project-local config first
    profiles_file="${PROJECT_AGENTBOX_DIR}/${agent}/profiles.ini"
    if [[ -f "$profiles_file" ]]; then
        read_profile_section "$profiles_file" "profiles"
        return
    fi

    # Fall back to project parent dir
    local project_parent
    project_parent=$(get_project_parent_dir)
    profiles_file="${project_parent}/${agent}/profiles.ini"

    if [[ -f "$profiles_file" ]]; then
        read_profile_section "$profiles_file" "profiles"
    fi
}

# Add a profile for an agent in the current project
add_project_profile() {
    local agent="${1:-$AGENTBOX_CURRENT_AGENT}"
    local profile="$2"
    local profiles_dir="${PROJECT_AGENTBOX_DIR}/${agent}"
    local profiles_file="${profiles_dir}/profiles.ini"

    mkdir -p "$profiles_dir"

    # Check if profile already exists
    if [[ -f "$profiles_file" ]]; then
        if grep -q "^${profile}$" "$profiles_file" 2>/dev/null; then
            return 0  # Already exists
        fi
    fi

    # Add profile to file
    if [[ ! -f "$profiles_file" ]]; then
        printf '[profiles]\n' > "$profiles_file"
    fi

    printf '%s\n' "$profile" >> "$profiles_file"
}

# Remove a profile for an agent in the current project
remove_project_profile() {
    local agent="${1:-$AGENTBOX_CURRENT_AGENT}"
    local profile="$2"
    local profiles_file="${PROJECT_AGENTBOX_DIR}/${agent}/profiles.ini"

    if [[ ! -f "$profiles_file" ]]; then
        return 0
    fi

    local temp_file
    temp_file=$(mktemp)

    grep -v "^${profile}$" "$profiles_file" > "$temp_file" 2>/dev/null || true
    mv "$temp_file" "$profiles_file"
}

# ============================================================================
# DEFAULT FLAGS
# ============================================================================

# Get saved default flags for an agent
get_default_flags() {
    local agent="${1:-$AGENTBOX_CURRENT_AGENT}"
    local flags_file="${AGENTBOX_HOME}/agents/${agent}/default-flags"

    if [[ -f "$flags_file" ]]; then
        cat "$flags_file"
    fi
}

# Save default flags for an agent
save_default_flags() {
    local agent="${1:-$AGENTBOX_CURRENT_AGENT}"
    local flags="$2"
    local flags_dir="${AGENTBOX_HOME}/agents/${agent}"
    local flags_file="${flags_dir}/default-flags"

    mkdir -p "$flags_dir"
    printf '%s\n' "$flags" > "$flags_file"
}

# ============================================================================
# PROFILE PACKAGES AND DESCRIPTIONS
# ============================================================================

# Get packages for a profile
get_profile_packages() {
    case "$1" in
        core) printf 'gcc g++ make git pkgconf openssl-dev libffi-dev zlib-dev tmux' ;;
        build-tools) printf 'cmake samurai autoconf automake libtool' ;;
        shell) printf 'rsync openssh-client mandoc gnupg file' ;;
        networking) printf 'iptables ipset iproute2 bind-tools' ;;
        c) printf 'gdb valgrind clang clang-extra-tools cppcheck doxygen boost-dev ncurses-dev' ;;
        rust) printf '' ;;
        python) printf '' ;;
        go) printf '' ;;
        flutter) printf '' ;;
        javascript) printf '' ;;
        java) printf '' ;;
        ruby) printf 'ruby ruby-dev readline-dev yaml-dev sqlite-dev sqlite libxml2-dev libxslt-dev curl-dev' ;;
        php) printf 'php83 php83-cli php83-fpm php83-mysqli php83-pgsql php83-sqlite3 php83-curl php83-gd php83-mbstring php83-xml php83-zip composer' ;;
        database) printf 'postgresql16-client mariadb-client sqlite redis' ;;
        devops) printf 'docker docker-cli-compose kubectl helm terraform ansible' ;;
        web) printf 'nginx apache2-utils httpie' ;;
        embedded) printf '' ;;
        datascience) printf 'R' ;;
        security) printf 'nmap tcpdump netcat-openbsd' ;;
        ml) printf '' ;;
        *) printf '' ;;
    esac
}

# Get all available profile names
get_all_profile_names() {
    printf 'core build-tools shell networking c rust python go flutter javascript java ruby php database devops web embedded datascience security ml'
}

# Check if a profile exists
profile_exists() {
    local profile="$1"
    local p
    for p in $(get_all_profile_names); do
        if [[ "$p" == "$profile" ]]; then
            return 0
        fi
    done
    return 1
}

# ============================================================================
# PROFILE INSTALLATION FUNCTIONS (for Dockerfile generation)
# ============================================================================

get_profile_core() {
    local packages
    packages=$(get_profile_packages "core")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_build_tools() {
    local packages
    packages=$(get_profile_packages "build-tools")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_shell() {
    local packages
    packages=$(get_profile_packages "shell")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_networking() {
    local packages
    packages=$(get_profile_packages "networking")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_c() {
    local packages
    packages=$(get_profile_packages "c")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_rust() {
    cat << 'EOF'
USER user
RUN curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y
ENV PATH="/home/user/.cargo/bin:$PATH"
USER root
EOF
}

get_profile_python() {
    cat << 'EOF'
# Python profile - uses system Python with pip
USER user
RUN python3 -m pip install --user --upgrade pip setuptools wheel
USER root
EOF
}

get_profile_go() {
    cat << 'EOF'
RUN wget -O go.tar.gz https://golang.org/dl/go1.22.0.linux-amd64.tar.gz && \
    tar -C /usr/local -xzf go.tar.gz && \
    rm go.tar.gz
ENV PATH="/usr/local/go/bin:$PATH"
EOF
}

get_profile_flutter() {
    cat << 'EOF'
USER user
RUN curl -fsSL https://fvm.app/install.sh | bash
ENV PATH="/usr/local/bin:$PATH"
RUN fvm install stable && fvm global stable
ENV PATH="/home/user/fvm/default/bin:$PATH"
USER root
EOF
}

get_profile_javascript() {
    cat << 'EOF'
# JavaScript profile - uses NVM from base image
USER user
RUN bash -c "source $NVM_DIR/nvm.sh && npm install -g typescript eslint prettier yarn pnpm"
USER root
EOF
}

get_profile_java() {
    cat << 'EOF'
USER user
RUN curl -s "https://get.sdkman.io?ci=true" | bash
RUN bash -c "source $HOME/.sdkman/bin/sdkman-init.sh && sdk install java && sdk install maven && sdk install gradle"
USER root
RUN for tool in java javac jar; do \
        ln -sf /home/user/.sdkman/candidates/java/current/bin/$tool /usr/local/bin/$tool; \
    done && \
    ln -sf /home/user/.sdkman/candidates/maven/current/bin/mvn /usr/local/bin/mvn && \
    ln -sf /home/user/.sdkman/candidates/gradle/current/bin/gradle /usr/local/bin/gradle
ENV JAVA_HOME="/home/user/.sdkman/candidates/java/current"
EOF
}

get_profile_ruby() {
    local packages
    packages=$(get_profile_packages "ruby")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_php() {
    local packages
    packages=$(get_profile_packages "php")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_database() {
    local packages
    packages=$(get_profile_packages "database")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_devops() {
    local packages
    packages=$(get_profile_packages "devops")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_web() {
    local packages
    packages=$(get_profile_packages "web")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_embedded() {
    local packages
    packages=$(get_profile_packages "embedded")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_datascience() {
    local packages
    packages=$(get_profile_packages "datascience")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_security() {
    local packages
    packages=$(get_profile_packages "security")
    if [[ -n "$packages" ]]; then
        printf 'RUN apk add --no-cache %s\n' "$packages"
    fi
}

get_profile_ml() {
    printf '# ML profile uses build-tools for compilation\n'
}

# ============================================================================
# EXPORTS
# ============================================================================

export -f init_agentbox_config init_project_config
export -f get_config_value set_config_value
export -f get_project_profiles add_project_profile remove_project_profile
export -f get_default_flags save_default_flags
export -f get_profile_packages get_all_profile_names profile_exists
export -f get_profile_core get_profile_build_tools get_profile_shell get_profile_networking
export -f get_profile_c get_profile_rust get_profile_python get_profile_go
export -f get_profile_flutter get_profile_javascript get_profile_java get_profile_ruby
export -f get_profile_php get_profile_database get_profile_devops get_profile_web
export -f get_profile_embedded get_profile_datascience get_profile_security get_profile_ml
