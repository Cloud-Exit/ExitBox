package network

import (
	"os"
	"strings"
	"testing"
)

func TestGenerateSquidConfig_BasicStructure(t *testing.T) {
	conf := GenerateSquidConfig("10.89.0.0/24", []string{"example.com"}, nil)

	// Should contain core squid directives
	required := []string{
		"http_port 3128",
		"acl SSL_ports port 443",
		"acl CONNECT method CONNECT",
		"http_access deny !Safe_ports",
		"http_access deny CONNECT !SSL_ports",
		"http_access deny all",
		"forwarded_for off",
		"via off",
	}
	for _, r := range required {
		if !strings.Contains(conf, r) {
			t.Errorf("config missing required directive: %q", r)
		}
	}
}

func TestGenerateSquidConfig_SubnetInACL(t *testing.T) {
	subnet := "10.89.0.0/24"
	conf := GenerateSquidConfig(subnet, []string{"example.com"}, nil)

	if !strings.Contains(conf, "acl agent_sources src "+subnet) {
		t.Error("config should contain agent_sources ACL with subnet")
	}
}

func TestGenerateSquidConfig_Domains(t *testing.T) {
	domains := []string{"github.com", "npmjs.org"}
	conf := GenerateSquidConfig("10.89.0.0/24", domains, nil)

	if !strings.Contains(conf, "acl allowed_domains dstdomain .github.com") {
		t.Error("config should contain .github.com domain ACL")
	}
	if !strings.Contains(conf, "acl allowed_domains dstdomain .npmjs.org") {
		t.Error("config should contain .npmjs.org domain ACL")
	}
}

func TestGenerateSquidConfig_ExtraURLs(t *testing.T) {
	conf := GenerateSquidConfig("10.89.0.0/24", []string{"example.com"}, []string{"extra.io"})

	if !strings.Contains(conf, "acl allowed_domains dstdomain .extra.io") {
		t.Error("config should contain extra URL domain ACL")
	}
}

func TestGenerateSquidConfig_Deduplication(t *testing.T) {
	domains := []string{"example.com", "example.com", "example.com"}
	conf := GenerateSquidConfig("10.89.0.0/24", domains, nil)

	count := strings.Count(conf, ".example.com")
	if count != 1 {
		t.Errorf("expected 1 occurrence of .example.com, got %d", count)
	}
}

func TestGenerateSquidConfig_DeduplicationAcrossLists(t *testing.T) {
	conf := GenerateSquidConfig("10.89.0.0/24", []string{"example.com"}, []string{"example.com"})

	count := strings.Count(conf, ".example.com")
	if count != 1 {
		t.Errorf("expected 1 occurrence of .example.com across domains+extraURLs, got %d", count)
	}
}

func TestGenerateSquidConfig_EmptyAllowlist(t *testing.T) {
	conf := GenerateSquidConfig("10.89.0.0/24", nil, nil)

	if !strings.Contains(conf, "__agentbox_block_all__") {
		t.Error("empty allowlist should produce block-all entry")
	}
}

func TestGenerateSquidConfig_EmptyExtraURLsSkipped(t *testing.T) {
	conf := GenerateSquidConfig("10.89.0.0/24", []string{"example.com"}, []string{"", ""})

	// Should only have the one domain, empty strings skipped
	count := strings.Count(conf, "acl allowed_domains dstdomain")
	if count != 1 {
		t.Errorf("expected 1 domain ACL entry, got %d", count)
	}
}

func TestGenerateSquidConfig_AllowAccess(t *testing.T) {
	conf := GenerateSquidConfig("10.89.0.0/24", []string{"example.com"}, nil)

	if !strings.Contains(conf, "http_access allow agent_sources allowed_domains") {
		t.Error("config should allow agent_sources with allowed_domains")
	}
	if !strings.Contains(conf, "http_access allow localhost") {
		t.Error("config should allow localhost")
	}
}

func TestGetSquidDNSServers_Default(t *testing.T) {
	os.Unsetenv("EXITBOX_SQUID_DNS")
	servers := getSquidDNSServers()
	if len(servers) != 2 {
		t.Fatalf("expected 2 default DNS servers, got %d", len(servers))
	}
	if servers[0] != "1.1.1.1" || servers[1] != "8.8.8.8" {
		t.Errorf("default DNS = %v, want [1.1.1.1 8.8.8.8]", servers)
	}
}

func TestGetSquidDNSServers_CustomComma(t *testing.T) {
	t.Setenv("EXITBOX_SQUID_DNS", "9.9.9.9,8.8.4.4")
	servers := getSquidDNSServers()
	if len(servers) != 2 {
		t.Fatalf("expected 2 custom DNS servers, got %d", len(servers))
	}
	if servers[0] != "9.9.9.9" || servers[1] != "8.8.4.4" {
		t.Errorf("custom DNS = %v, want [9.9.9.9 8.8.4.4]", servers)
	}
}

func TestGetSquidDNSServers_CustomSpaces(t *testing.T) {
	t.Setenv("EXITBOX_SQUID_DNS", "9.9.9.9 8.8.4.4")
	servers := getSquidDNSServers()
	if len(servers) != 2 {
		t.Fatalf("expected 2 servers, got %d", len(servers))
	}
}

func TestSessionURLFileRoundtrip(t *testing.T) {
	dir := t.TempDir()

	// Write a session URL file manually
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(dir+"/test-container.urls", []byte("extra1.com\nextra2.com\n"), 0644)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 session file, got %d", len(entries))
	}
}
