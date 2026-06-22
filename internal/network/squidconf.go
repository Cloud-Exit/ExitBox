// ExitBox - Multi-Agent Container Sandbox
// Copyright (C) 2026 Cloud Exit B.V.
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package network

import (
	"fmt"
	"net"
	"strings"

	"github.com/cloud-exit/exitbox/internal/ui"
)

// GenerateSquidConfig generates the squid.conf content.
func GenerateSquidConfig(subnet string, domains []string, extraURLs []string) string {
	var b strings.Builder

	b.WriteString(`# Squid Configuration for Agentbox
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
`)
	fmt.Fprintf(&b, "acl agent_sources src %s\n\n# Allowlist\n", subnet)

	// Collect and deduplicate all entries.
	seen := make(map[string]bool)
	var entries []string

	for _, domain := range domains {
		normalized, err := NormalizeAllowlistEntry(domain)
		if err != nil {
			ui.Warnf("Skipping invalid allowlist entry: %s", domain)
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		entries = append(entries, normalized)
	}

	for _, url := range extraURLs {
		if url == "" {
			continue
		}
		normalized, err := NormalizeAllowlistEntry(url)
		if err != nil {
			ui.Warnf("Skipping invalid --allow-urls entry: %s", url)
			continue
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		entries = append(entries, normalized)
	}

	// Remove subdomain entries covered by a parent domain.
	// Squid FATAL-errors when e.g. ".www.icy-veins.com" and ".icy-veins.com"
	// both appear (the subdomain is redundant).
	entries = removeRedundantSubdomains(entries)

	// Split into hostnames (dstdomain) and IP literals (dst). Squid ignores IP
	// addresses in a dstdomain ACL, so an endpoint addressed by IP (e.g. a LAN
	// model server at 10.10.10.185:8088) is only reachable when emitted as a dst
	// ACL. The agent network is egress-isolated, so such traffic must traverse
	// squid and would otherwise be denied.
	var domainEntries, ipEntries []string
	for _, e := range entries {
		if net.ParseIP(e) != nil {
			ipEntries = append(ipEntries, e)
		} else {
			domainEntries = append(domainEntries, e)
		}
	}

	for _, e := range domainEntries {
		fmt.Fprintf(&b, "acl allowed_domains dstdomain %s\n", e)
	}
	for _, e := range ipEntries {
		fmt.Fprintf(&b, "acl allowed_ips dst %s\n", e)
	}

	hasDomains := len(domainEntries) > 0
	if !hasDomains && len(ipEntries) == 0 {
		ui.Warn("Allowlist is empty or invalid. Blocking all outbound destinations.")
		b.WriteString("acl allowed_domains dstdomain .__agentbox_block_all__.invalid\n")
		hasDomains = true
	}

	b.WriteString("\n# Enforce Access Control\n# Only allow access from localhost and our network\nhttp_access allow localhost\n")
	if hasDomains {
		b.WriteString("http_access allow agent_sources allowed_domains\n")
	}
	if len(ipEntries) > 0 {
		b.WriteString("http_access allow agent_sources allowed_ips\n")
	}

	b.WriteString(`
# Deny everything else
http_access deny all

# Hide proxy info
forwarded_for off
via off

# DNS caching — reduce latency for repeated lookups
dns_nameservers 1.1.1.1 8.8.8.8
positive_dns_ttl 5 minutes
negative_dns_ttl 30 seconds
`)

	return b.String()
}

// removeRedundantSubdomains filters out entries that are subdomains of another
// entry. E.g. if ".icy-veins.com" is present, ".www.icy-veins.com" is removed
// since the parent already covers all subdomains in squid's dstdomain matching.
func removeRedundantSubdomains(entries []string) []string {
	var result []string
	for _, e := range entries {
		if !strings.HasPrefix(e, ".") {
			// IPs, localhost — never redundant.
			result = append(result, e)
			continue
		}
		redundant := false
		for _, other := range entries {
			if other == e || !strings.HasPrefix(other, ".") {
				continue
			}
			// e=".www.icy-veins.com" is covered by other=".icy-veins.com"
			if strings.HasSuffix(e, other) && len(e) > len(other) {
				redundant = true
				break
			}
		}
		if !redundant {
			result = append(result, e)
		}
	}
	return result
}
