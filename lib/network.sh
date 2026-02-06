#!/usr/bin/env bash
# Network management - Firewalling and Connectivity
# ============================================================================

# Network names for agentbox
readonly AGENTBOX_INTERNAL_NETWORK="agentbox-int"
readonly AGENTBOX_EGRESS_NETWORK="agentbox-egress"
readonly AGENTBOX_SQUID_CONTAINER="agentbox-squid"

# Get the allowlist file path
get_allowlist_path() {
    local config_allowlist="${AGENTBOX_HOME}/allowlist.txt"
    local default_allowlist="${SCRIPT_DIR}/config/allowlist.txt"

    # User config takes precedence
    if [[ -f "$config_allowlist" ]]; then
        printf '%s' "$config_allowlist"
    elif [[ -f "$default_allowlist" ]]; then
        printf '%s' "$default_allowlist"
    else
        printf ''
    fi
}

# Normalize an allowlist entry for Squid dstdomain ACL matching.
# Supported input:
# - host/IP: example.com, api.example.com, 1.2.3.4, 2606:4700:4700::1111
# - wildcard subdomains: *.example.com (same as example.com in Squid ACL terms)
# - optional protocol/path/port are stripped
normalize_allowlist_entry() {
    local value="$1"

    # Trim whitespace
    value=$(printf '%s' "$value" | xargs)
    [[ -z "$value" ]] && return 1

    # Strip protocol and path
    value="${value#*://}"
    value="${value%%/*}"

    if [[ "$value" == \*.* ]]; then
        value="${value#*.}"
    elif [[ "$value" == .* ]]; then
        value="${value#.}"
    fi

    # Accept bracketed IPv6 literals like [2001:db8::1] or [2001:db8::1]:443
    if [[ "$value" =~ ^\[([0-9A-Fa-f:]+)\](:[0-9]+)?$ ]]; then
        value="${BASH_REMATCH[1]}"
    fi

    # Strip trailing DNS dot from hostnames.
    value="${value%.}"

    # Detect IPv6 literals (simple form check).
    local is_ipv6=false
    if [[ "$value" =~ : ]] && [[ "$value" =~ ^[0-9A-Fa-f:]+$ ]]; then
        is_ipv6=true
    fi

    # Strip port from hostnames only.
    if [[ "$value" =~ : ]] && [[ ! "$value" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] && [[ "$is_ipv6" != "true" ]]; then
        value="${value%%:*}"
    fi

    # Basic validation for hostname or IPv4
    if [[ "$value" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        printf '%s' "$value"
        return 0
    fi

    if [[ "$is_ipv6" == "true" ]]; then
        printf '%s' "$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
        return 0
    fi

    if [[ ! "$value" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)+$ ]]; then
        return 1
    fi

    value=$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')

    # Squid dstdomain with leading dot matches both host and subdomains.
    # We avoid root-domain collapsing and only match the caller-provided host scope.
    printf '.%s' "$value"
}

# Get the subnet for a network
get_network_subnet() {
    local network_name="${1:-$AGENTBOX_INTERNAL_NETWORK}"
    local cmd
    local inspect_json=""
    local subnet=""
    cmd=$(container_cmd)

    # Ensure networks exist first, but keep informational logs out of stdout
    # because callers capture this function output as the subnet value.
    ensure_networks >/dev/null

    if ! inspect_json=$($cmd network inspect "$network_name" 2>/dev/null); then
        return 1
    fi

    # Prefer structured parsing when jq is available.
    if command -v jq >/dev/null 2>&1; then
        subnet=$(printf '%s' "$inspect_json" | jq -r '.[0] | (.IPAM.Config[]?.Subnet), (.subnets[]?.subnet)' 2>/dev/null | head -n 1)
    fi

    # Fallback parser for environments without jq.
    if [[ -z "$subnet" ]]; then
        subnet=$(printf '%s\n' "$inspect_json" | sed -nE 's/.*"Subnet"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' | head -n 1)
    fi
    if [[ -z "$subnet" ]]; then
        subnet=$(printf '%s\n' "$inspect_json" | sed -nE 's/.*"subnet"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' | head -n 1)
    fi

    if [[ -n "$subnet" ]]; then
        printf '%s\n' "$subnet"
        return 0
    fi

    return 1
}

# Generate Squid configuration
# Arguments: extra URLs to allow (optional)
generate_squid_config() {
    local extra_urls=("$@")
    local allowlist_path
    allowlist_path=$(get_allowlist_path)
    local config_file="$AGENTBOX_CACHE/squid.conf"
    local squid_dns_servers
    
    # Get network subnet for locking down access
    local internal_subnet
    if ! internal_subnet=$(get_network_subnet "$AGENTBOX_INTERNAL_NETWORK"); then
        error "Could not detect internal network subnet for firewall enforcement."
    fi
    if [[ -z "$internal_subnet" ]]; then
        error "Could not detect internal network subnet for firewall enforcement."
    fi

    mkdir -p "$(dirname "$config_file")"

    cat > "$config_file" << EOF
# Squid Configuration for Agentbox
http_port 3128
shutdown_lifetime 1 seconds

# Access Control Lists
acl SSL_ports port 443
acl Safe_ports port 80		# http
acl Safe_ports port 21		# ftp
acl Safe_ports port 443		# https
acl Safe_ports port 70		# gopher
acl Safe_ports port 210		# wais
acl Safe_ports port 1025-65535	# unregistered ports
acl Safe_ports port 280		# http-mgmt
acl Safe_ports port 488		# gss-http
acl Safe_ports port 591		# filemaker
acl Safe_ports port 777		# multiling http
acl CONNECT method CONNECT

# Deny requests to certain unsafe ports
http_access deny !Safe_ports
http_access deny CONNECT !SSL_ports

# Localhost access
acl localhost src 127.0.0.1/32

# Only allow proxy clients from the internal agent network
acl agent_sources src ${internal_subnet}

# Allowlist
EOF

    if [[ -n "$allowlist_path" ]] && [[ -f "$allowlist_path" ]]; then
        local added_domains=""
        local allowlist_entry_count=0
        
        # Process allowlist
        while IFS= read -r line; do
            # Skip empty lines and comments
            if [[ -z "$line" ]] || [[ "$line" =~ ^[[:space:]]*# ]]; then
                continue
            fi

            local domain
            domain=$(printf '%s' "$line" | xargs) # Trim

            if [[ -n "$domain" ]]; then
                local normalized_domain
                if ! normalized_domain=$(normalize_allowlist_entry "$domain"); then
                    warn "Skipping invalid allowlist entry: $domain"
                    continue
                fi

                # Deduplicate
                if [[ " $added_domains " == *" $normalized_domain "* ]]; then
                    continue
                fi
                added_domains="$added_domains $normalized_domain"
                printf 'acl allowed_domains dstdomain %s\n' "$normalized_domain" >> "$config_file"
                ((allowlist_entry_count++))
            fi
        done < "$allowlist_path"

        if (( allowlist_entry_count == 0 )); then
            warn "Allowlist is empty or invalid. Blocking all outbound destinations."
            printf 'acl allowed_domains dstdomain .__agentbox_block_all__.invalid\n' >> "$config_file"
        fi
    else
        warn "Allowlist file not found. Blocking all outbound destinations."
        printf 'acl allowed_domains dstdomain .__agentbox_block_all__.invalid\n' >> "$config_file"
    fi

    # Process extra URLs (from --allow-urls / -a)
    if [[ ${#extra_urls[@]} -gt 0 ]]; then
        local url
        for url in "${extra_urls[@]}"; do
            [[ -z "$url" ]] && continue
            local normalized_url
            if ! normalized_url=$(normalize_allowlist_entry "$url"); then
                warn "Skipping invalid --allow-urls entry: $url"
                continue
            fi
            # Deduplicate against already-added domains
            if [[ " ${added_domains:-} " == *" $normalized_url "* ]]; then
                continue
            fi
            added_domains="${added_domains:-} $normalized_url"
            printf 'acl allowed_domains dstdomain %s\n' "$normalized_url" >> "$config_file"
        done
    fi

    cat >> "$config_file" << 'EOF'

# Enforce Access Control
# Only allow access from localhost and our network
http_access allow localhost
http_access allow agent_sources allowed_domains

# Deny everything else
http_access deny all

# Hide proxy info
forwarded_for off
via off
EOF

    squid_dns_servers=$(get_squid_dns_servers)
    if [[ -n "$squid_dns_servers" ]]; then
        # Force Squid to use stable upstream resolvers directly.
        printf 'dns_nameservers %s\n' "$squid_dns_servers" >> "$config_file"
    fi

    echo "$config_file"
}

# Ensure the shared networks exist
ensure_networks() {
    local cmd
    cmd=$(container_cmd)
    
    if ! $cmd network ls --format '{{.Name}}' | grep -q "^${AGENTBOX_INTERNAL_NETWORK}$"; then
        info "Creating internal network $AGENTBOX_INTERNAL_NETWORK..."
        $cmd network create --internal "$AGENTBOX_INTERNAL_NETWORK" >/dev/null
    fi

    if ! $cmd network ls --format '{{.Name}}' | grep -q "^${AGENTBOX_EGRESS_NETWORK}$"; then
        info "Creating egress network $AGENTBOX_EGRESS_NETWORK..."
        $cmd network create "$AGENTBOX_EGRESS_NETWORK" >/dev/null
    fi
}

# Backward compatibility
ensure_network() {
    ensure_networks
}

# Resolve configured DNS servers for Squid as a normalized, space-separated
# list. If AGENTBOX_SQUID_DNS is unset, defaults are used. If explicitly set
# to an empty value, no override is applied and runtime defaults are inherited.
get_squid_dns_servers() {
    local raw_dns=""
    local dns
    local -a dns_servers=()

    if [[ -n "${AGENTBOX_SQUID_DNS+x}" ]]; then
        raw_dns="${AGENTBOX_SQUID_DNS}"
    else
        raw_dns="1.1.1.1,8.8.8.8"
    fi

    # Allow comma or whitespace separators.
    raw_dns="${raw_dns//,/ }"
    for dns in $raw_dns; do
        [[ -z "$dns" ]] && continue
        dns_servers+=("$dns")
    done

    if [[ ${#dns_servers[@]} -eq 0 ]]; then
        return 0
    fi

    printf '%s' "${dns_servers[*]}"
}

# Get runtime DNS flags for Squid.
# Defaults to public resolvers to avoid stale Podman-internal DNS forwarders
# after host sleep/resume events. Set AGENTBOX_SQUID_DNS to a comma/space-
# separated list, or to an empty value to inherit runtime defaults.
get_squid_dns_run_args() {
    local dns
    local dns_servers

    dns_servers=$(get_squid_dns_servers)
    [[ -z "$dns_servers" ]] && return 0

    # shellcheck disable=SC2086
    for dns in $dns_servers; do
        printf -- '--dns %s ' "$dns"
    done
}

# Get DNS search-domain flags for Squid.
# Default to "." to disable inherited host/runtime search domains (for example
# tailscale MagicDNS search suffixes) inside the Squid container.
# Set AGENTBOX_SQUID_DNS_SEARCH to empty to inherit runtime defaults.
get_squid_dns_search_run_args() {
    local raw_search=""
    local search
    local -a search_domains=()

    if [[ -n "${AGENTBOX_SQUID_DNS_SEARCH+x}" ]]; then
        raw_search="${AGENTBOX_SQUID_DNS_SEARCH}"
    else
        raw_search="."
    fi

    raw_search="${raw_search//,/ }"
    for search in $raw_search; do
        [[ -z "$search" ]] && continue
        search_domains+=("$search")
    done

    if [[ ${#search_domains[@]} -eq 0 ]]; then
        return 0
    fi

    for search in "${search_domains[@]}"; do
        printf -- '--dns-search %s ' "$search"
    done
}

# Return 0 when Codex callback port should be exposed on the Squid container.
should_publish_codex_callback_port() {
    if [[ "${AGENTBOX_ENABLE_CODEX_CALLBACK_PORT:-auto}" == "false" ]]; then
        return 1
    fi

    if [[ "${AGENTBOX_ENABLE_CODEX_CALLBACK_PORT:-auto}" == "true" ]]; then
        return 0
    fi

    if declare -F agent_is_enabled >/dev/null 2>&1; then
        if agent_is_enabled "codex"; then
            return 0
        fi
    fi

    return 1
}

# Return 0 when Squid currently publishes the callback port on host.
squid_has_published_callback_port() {
    local cmd
    cmd=$(container_cmd)

    $cmd port "$AGENTBOX_SQUID_CONTAINER" 1455/tcp 2>/dev/null | grep -q .
}

# Reload Squid configuration without restarting the container
reload_squid_config() {
    local cmd; cmd=$(container_cmd)
    $cmd exec "$AGENTBOX_SQUID_CONTAINER" squid -k reconfigure 2>/dev/null
}

# Start the Squid proxy container
start_squid_proxy() {
    local cmd
    local need_codex_callback_port="false"
    cmd=$(container_cmd)

    if should_publish_codex_callback_port; then
        need_codex_callback_port="true"
    fi
    
    # Check if running
    if $cmd ps --filter "name=^/${AGENTBOX_SQUID_CONTAINER}$" --format '{{.Names}}' | grep -q "^${AGENTBOX_SQUID_CONTAINER}$"; then
        if [[ "$need_codex_callback_port" == "true" ]] && ! squid_has_published_callback_port; then
            info "Restarting Squid proxy to publish Codex callback port..."
            $cmd rm -f "$AGENTBOX_SQUID_CONTAINER" >/dev/null 2>&1 || true
        elif [[ ${#CLI_ALLOW_URLS[@]} -gt 0 ]]; then
            # Regenerate config with extra URLs and hot-reload
            info "Updating Squid config with extra allowed domains..."
            generate_squid_config "${CLI_ALLOW_URLS[@]}" >/dev/null
            reload_squid_config || warn "Failed to reload Squid config"
            return 0
        else
            return 0
        fi
    fi

    # Remove if stopped
    $cmd rm -f "$AGENTBOX_SQUID_CONTAINER" >/dev/null 2>&1 || true

    # Ensure networks exist first (needed for subnet detection)
    ensure_networks

    # Build image if needed
    build_squid_image

    # Generate config (include extra URLs from CLI if any)
    local config_file
    if [[ ${#CLI_ALLOW_URLS[@]} -gt 0 ]]; then
        if ! config_file=$(generate_squid_config "${CLI_ALLOW_URLS[@]}"); then
            error "Failed to generate Squid configuration."
        fi
    else
        if ! config_file=$(generate_squid_config); then
            error "Failed to generate Squid configuration."
        fi
    fi
    if [[ -z "$config_file" ]] || [[ ! -f "$config_file" ]]; then
        error "Generated Squid configuration path is invalid."
    fi

    local run_args=(
        -d
        --name "$AGENTBOX_SQUID_CONTAINER"
        --network "$AGENTBOX_EGRESS_NETWORK"
        -v "$config_file":/etc/squid/squid.conf
        --restart=unless-stopped
    )

    if [[ "$need_codex_callback_port" == "true" ]]; then
        run_args+=(-p "1455:1455")
    fi

    local squid_dns_flags
    squid_dns_flags=$(get_squid_dns_run_args)
    if [[ -n "$squid_dns_flags" ]]; then
        local -a squid_dns_flags_array
        read -r -a squid_dns_flags_array <<< "$squid_dns_flags"
        run_args+=("${squid_dns_flags_array[@]}")
    fi

    local squid_dns_search_flags
    squid_dns_search_flags=$(get_squid_dns_search_run_args)
    if [[ -n "$squid_dns_search_flags" ]]; then
        local -a squid_dns_search_flags_array
        read -r -a squid_dns_search_flags_array <<< "$squid_dns_search_flags"
        run_args+=("${squid_dns_search_flags_array[@]}")
    fi

    run_args+=(agentbox-squid)

    info "Starting Squid proxy..."
    if ! $cmd run "${run_args[@]}" >/dev/null; then
        error "Failed to start Squid proxy container."
    fi

    # Connect Squid to the internal network used by agent containers.
    if ! $cmd network connect "$AGENTBOX_INTERNAL_NETWORK" "$AGENTBOX_SQUID_CONTAINER" >/dev/null 2>&1; then
        $cmd rm -f "$AGENTBOX_SQUID_CONTAINER" >/dev/null 2>&1 || true
        error "Failed to connect Squid to internal network."
    fi

    # Wait briefly for IP allocation
    local retries=10
    while [[ $retries -gt 0 ]]; do
        if $cmd inspect "$AGENTBOX_SQUID_CONTAINER" --format "{{with index .NetworkSettings.Networks \"$AGENTBOX_INTERNAL_NETWORK\"}}{{.IPAddress}}{{end}}" 2>/dev/null | grep -q .; then
             break
        fi
        sleep 0.1
        ((retries--))
    done
}

# Configure callback relay inside the shared Squid container.
# Forwards squid:1455 -> <codex_container_name>:2455.
configure_codex_callback_relay() {
    local codex_container_name="$1"
    local cmd
    cmd=$(container_cmd)

    if [[ -z "$codex_container_name" ]]; then
        error "Codex callback relay requires a container name."
    fi

    if ! should_publish_codex_callback_port; then
        return 0
    fi

    start_squid_proxy

    if ! $cmd exec -e "CODEX_RELAY_TARGET=${codex_container_name}" "$AGENTBOX_SQUID_CONTAINER" sh -c '
        set -e
        killall socat >/dev/null 2>&1 || true
        socat "TCP-LISTEN:1455,bind=0.0.0.0,reuseaddr,fork" "TCP:${CODEX_RELAY_TARGET}:2455" >/tmp/codex-callback-relay.log 2>&1 &
        echo $! >/tmp/codex-callback-relay.pid
    '; then
        error "Failed to configure Codex callback relay via Squid."
    fi
}

# Get proxy environment variables
get_proxy_env_vars() {
    if [[ "${AGENTBOX_NO_FIREWALL:-false}" == "true" ]]; then
        return 0
    fi
    
    # We assume the proxy is reachable via hostname 'agentbox-squid' on the network
    # However, to be robust against DNS issues in rootless containers, we try to resolve IP first
    local proxy_host="$AGENTBOX_SQUID_CONTAINER"
    local cmd
    cmd=$(container_cmd)
    
    # Try to get IP address of squid container on agentbox network
    local squid_ip
    squid_ip=$($cmd inspect "$AGENTBOX_SQUID_CONTAINER" --format "{{with index .NetworkSettings.Networks \"$AGENTBOX_INTERNAL_NETWORK\"}}{{.IPAddress}}{{end}}" 2>/dev/null | head -1)

    if [[ -n "$squid_ip" ]]; then
        proxy_host="$squid_ip"
    fi
    
    local proxy_url="http://${proxy_host}:3128"
    
    printf -- '-e http_proxy=%s ' "$proxy_url"
    printf -- '-e https_proxy=%s ' "$proxy_url"
    printf -- '-e HTTP_PROXY=%s ' "$proxy_url"
    printf -- '-e HTTPS_PROXY=%s ' "$proxy_url"
    # Keep local loopback out of proxy routing.
    printf -- '-e no_proxy=localhost,127.0.0.1,.local '
    printf -- '-e NO_PROXY=localhost,127.0.0.1,.local '
}

export -f ensure_network ensure_networks get_squid_dns_servers get_squid_dns_run_args get_squid_dns_search_run_args
export -f should_publish_codex_callback_port squid_has_published_callback_port start_squid_proxy configure_codex_callback_relay
export -f get_proxy_env_vars generate_squid_config normalize_allowlist_entry get_network_subnet reload_squid_config
