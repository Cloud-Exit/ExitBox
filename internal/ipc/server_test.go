package ipc

import (
	"bufio"
	"encoding/json"
	"net"
	"testing"
)

func TestServerRoundTrip(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()

	srv.Handle("echo", func(req *Request) (interface{}, error) {
		var payload map[string]string
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return nil, err
		}
		return payload, nil
	})
	srv.Start()

	// Connect and send a request.
	conn, err := net.Dial("unix", srv.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req := Request{
		Type: "echo",
		ID:   "test-1",
	}
	payloadBytes, err := json.Marshal(map[string]string{"msg": "hello"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req.Payload = payloadBytes
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

	if resp.Type != "echo" {
		t.Errorf("type = %q, want echo", resp.Type)
	}
	if resp.ID != "test-1" {
		t.Errorf("id = %q, want test-1", resp.ID)
	}

	// Decode payload.
	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var got map[string]string
	if err = json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got["msg"] != "hello" {
		t.Errorf("payload msg = %q, want hello", got["msg"])
	}
}

func TestServerUnknownType(t *testing.T) {
	srv, err := NewServer()
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Stop()
	srv.Start()

	conn, err := net.Dial("unix", srv.socketPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer conn.Close()

	req := Request{Type: "nonexistent", ID: "test-2"}
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
	if err = json.Unmarshal(scanner.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	raw, err := json.Marshal(resp.Payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	var payload ErrorResponse
	if err = json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload.Error == "" {
		t.Error("expected error for unknown type")
	}
}
