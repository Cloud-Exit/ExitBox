package ipc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
)

func TestVaultGetApproved(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	state := &VaultState{}
	defer state.Cleanup()

	store := map[string]string{"API_KEY": "secret123", "TOKEN": "abc"}

	srv.Handle("vault_get", NewVaultGetHandler(VaultHandlerConfig{
		PromptApproveFunc: func(key string) (bool, error) {
			return true, nil
		},
		PromptPasswordFunc: func() (string, error) {
			return "testpass", nil
		},
		OpenFunc: func(workspace, password string) (map[string]string, error) {
			return store, nil
		},
		WorkspaceName: "test",
	}, state))
	srv.Start()

	resp := sendVaultGet(t, srv, "API_KEY")
	if !resp.Approved {
		t.Error("expected approved=true")
	}
	if resp.Value != "secret123" {
		t.Errorf("value = %q, want %q", resp.Value, "secret123")
	}
	if resp.Error != "" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

func TestVaultGetDenied(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	state := &VaultState{}
	defer state.Cleanup()

	srv.Handle("vault_get", NewVaultGetHandler(VaultHandlerConfig{
		PromptApproveFunc: func(key string) (bool, error) {
			return false, nil
		},
		PromptPasswordFunc: func() (string, error) {
			t.Error("password should not be prompted when denied")
			return "", nil
		},
		OpenFunc: func(workspace, password string) (map[string]string, error) {
			t.Error("open should not be called when denied")
			return nil, nil
		},
		WorkspaceName: "test",
	}, state))
	srv.Start()

	resp := sendVaultGet(t, srv, "API_KEY")
	if resp.Approved {
		t.Error("expected approved=false")
	}
}

func TestVaultGetEmptyKey(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	state := &VaultState{}
	defer state.Cleanup()

	srv.Handle("vault_get", NewVaultGetHandler(VaultHandlerConfig{
		PromptApproveFunc: func(key string) (bool, error) {
			t.Error("approve should not be called for empty key")
			return false, nil
		},
		PromptPasswordFunc: func() (string, error) { return "", nil },
		OpenFunc:           func(w, p string) (map[string]string, error) { return nil, nil },
		WorkspaceName:      "test",
	}, state))
	srv.Start()

	resp := sendVaultGet(t, srv, "")
	if resp.Error == "" {
		t.Error("expected error for empty key")
	}
}

func TestVaultGetUnlockFailure(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	state := &VaultState{}
	defer state.Cleanup()

	srv.Handle("vault_get", NewVaultGetHandler(VaultHandlerConfig{
		PromptApproveFunc: func(key string) (bool, error) {
			return true, nil
		},
		PromptPasswordFunc: func() (string, error) {
			return "wrongpass", nil
		},
		OpenFunc: func(workspace, password string) (map[string]string, error) {
			return nil, fmt.Errorf("wrong password")
		},
		WorkspaceName: "test",
	}, state))
	srv.Start()

	resp := sendVaultGet(t, srv, "API_KEY")
	if resp.Error == "" {
		t.Error("expected error for unlock failure")
	}
}

func TestVaultGetCachedUnlock(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	state := &VaultState{}
	defer state.Cleanup()

	var openCalls int32
	store := map[string]string{"KEY1": "v1", "KEY2": "v2"}

	srv.Handle("vault_get", NewVaultGetHandler(VaultHandlerConfig{
		PromptApproveFunc: func(key string) (bool, error) {
			return true, nil
		},
		PromptPasswordFunc: func() (string, error) {
			return "testpass", nil
		},
		OpenFunc: func(workspace, password string) (map[string]string, error) {
			atomic.AddInt32(&openCalls, 1)
			return store, nil
		},
		WorkspaceName: "test",
	}, state))
	srv.Start()

	// First request: triggers unlock.
	resp := sendVaultGet(t, srv, "KEY1")
	if !resp.Approved {
		t.Error("expected approved=true for first request")
	}

	// Second request: should reuse cached store.
	resp = sendVaultGet(t, srv, "KEY2")
	if !resp.Approved {
		t.Error("expected approved=true for second request")
	}

	if n := atomic.LoadInt32(&openCalls); n != 1 {
		t.Errorf("OpenFunc called %d times, expected 1", n)
	}
}

func TestVaultGetKeyNotFound(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	state := &VaultState{}
	defer state.Cleanup()

	srv.Handle("vault_get", NewVaultGetHandler(VaultHandlerConfig{
		PromptApproveFunc:  func(key string) (bool, error) { return true, nil },
		PromptPasswordFunc: func() (string, error) { return "pass", nil },
		OpenFunc: func(w, p string) (map[string]string, error) {
			return map[string]string{"OTHER": "val"}, nil
		},
		WorkspaceName: "test",
	}, state))
	srv.Start()

	resp := sendVaultGet(t, srv, "MISSING")
	if resp.Error == "" {
		t.Error("expected error for missing key")
	}
}

func TestVaultListApproved(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	state := &VaultState{}
	defer state.Cleanup()

	srv.Handle("vault_list", NewVaultListHandler(VaultHandlerConfig{
		PromptApproveFunc: func(key string) (bool, error) {
			return true, nil
		},
		PromptPasswordFunc: func() (string, error) {
			return "testpass", nil
		},
		OpenFunc: func(workspace, password string) (map[string]string, error) {
			return map[string]string{"API_KEY": "secret", "TOKEN": "abc"}, nil
		},
		WorkspaceName: "test",
	}, state))
	srv.Start()

	resp := sendVaultList(t, srv)
	if !resp.Approved {
		t.Error("expected approved=true")
	}
	if len(resp.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(resp.Keys))
	}
}

func TestVaultListDenied(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	state := &VaultState{}
	defer state.Cleanup()

	srv.Handle("vault_list", NewVaultListHandler(VaultHandlerConfig{
		PromptApproveFunc: func(key string) (bool, error) {
			return false, nil
		},
		PromptPasswordFunc: func() (string, error) { return "", nil },
		OpenFunc:           func(w, p string) (map[string]string, error) { return nil, nil },
		WorkspaceName:      "test",
	}, state))
	srv.Start()

	resp := sendVaultList(t, srv)
	if resp.Approved {
		t.Error("expected approved=false")
	}
}

func sendVaultGet(t *testing.T, srv *Server, key string) VaultGetResponse {
	t.Helper()

	conn, err := net.Dial("unix", srv.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	payload, err := json.Marshal(VaultGetRequest{Key: key})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := Request{
		Type:    "vault_get",
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
	var result VaultGetResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return result
}

func sendVaultList(t *testing.T, srv *Server) VaultListResponse {
	t.Helper()

	conn, err := net.Dial("unix", srv.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	payload, err := json.Marshal(VaultListRequest{})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := Request{
		Type:    "vault_list",
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
	var result VaultListResponse
	if err := json.Unmarshal(raw, &result); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return result
}
