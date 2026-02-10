package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
)

func TestAllowDomainHandlerApproved(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	srv.Handle("allow_domain", NewAllowDomainHandler(AllowDomainHandlerConfig{
		PromptFunc: func(domain string) (bool, error) {
			return true, nil
		},
		ReloadFunc: func(domain string) error {
			return nil
		},
	}))
	srv.Start()

	resp := sendAllowDomain(t, srv, "example.com")
	if !resp.Approved {
		t.Error("expected approved=true")
	}
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestAllowDomainHandlerDenied(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	srv.Handle("allow_domain", NewAllowDomainHandler(AllowDomainHandlerConfig{
		PromptFunc: func(domain string) (bool, error) {
			return false, nil
		},
		ReloadFunc: func(domain string) error {
			t.Error("reload should not be called when denied")
			return nil
		},
	}))
	srv.Start()

	resp := sendAllowDomain(t, srv, "example.com")
	if resp.Approved {
		t.Error("expected approved=false")
	}
}

func TestAllowDomainHandlerInvalidDomain(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	srv.Handle("allow_domain", NewAllowDomainHandler(AllowDomainHandlerConfig{
		PromptFunc: func(domain string) (bool, error) {
			t.Error("prompt should not be called for invalid domain")
			return false, nil
		},
		ReloadFunc: func(domain string) error {
			t.Error("reload should not be called for invalid domain")
			return nil
		},
	}))
	srv.Start()

	resp := sendAllowDomain(t, srv, "not a valid domain!!!")
	if resp.Approved {
		t.Error("expected approved=false for invalid domain")
	}
	if resp.Error == "" {
		t.Error("expected error for invalid domain")
	}
}

func TestAllowDomainHandlerEmptyDomain(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	srv.Handle("allow_domain", NewAllowDomainHandler(AllowDomainHandlerConfig{
		PromptFunc: func(domain string) (bool, error) {
			t.Error("prompt should not be called for empty domain")
			return false, nil
		},
		ReloadFunc: func(domain string) error {
			t.Error("reload should not be called for empty domain")
			return nil
		},
	}))
	srv.Start()

	resp := sendAllowDomain(t, srv, "")
	if resp.Error == "" {
		t.Error("expected error for empty domain")
	}
}

func sendAllowDomain(t *testing.T, srv *Server, domain string) AllowDomainResponse {
	t.Helper()

	conn, err := net.Dial("unix", srv.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	payload, err := json.Marshal(AllowDomainRequest{Domain: domain})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := Request{
		Type:    "allow_domain",
		ID:      "test",
		Payload: payload,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if _, err = conn.Write(append(data, '\n')); err != nil {
		t.Fatalf("Write: %v", err)
	}

	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response")
	}

	var resp Response
	if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var result AllowDomainResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return result
}
