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

package router

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// probeServer answers the given paths with fixed bodies and 404 otherwise.
func probeServer(t *testing.T, wantToken string, pages map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantToken != "" && r.Header.Get("Authorization") != "Bearer "+wantToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func probe(t *testing.T, srv *httptest.Server, served, token string) (int, error) {
	t.Helper()
	return ProbeContext(context.Background(), &http.Client{Transport: direct()}, Route{ID: "m", URL: srv.URL, Served: served}, token)
}

func TestProbeContextLlamaCpp(t *testing.T) {
	// With --parallel 4 and -c 131072, each slot gets 32768: that is what
	// default_generation_settings reports and what a request can use.
	srv := probeServer(t, "", map[string]string{
		"/props": `{"default_generation_settings":{"id":0,"n_ctx":32768,"params":{"n_predict":-1}},"total_slots":4,"model_path":"m.gguf","chat_template":"..."}`,
	})
	if n, err := probe(t, srv, "", ""); err != nil || n != 32768 {
		t.Errorf("ProbeContext = %d, %v, want 32768", n, err)
	}
}

func TestProbeContextVLLM(t *testing.T) {
	// vLLM has no /props; /v1/models carries max_model_len.
	srv := probeServer(t, "", map[string]string{
		"/v1/models": `{"object":"list","data":[{"id":"qwen3-coder","object":"model","owned_by":"vllm","max_model_len":262144}]}`,
	})
	if n, err := probe(t, srv, "qwen3-coder", ""); err != nil || n != 262144 {
		t.Errorf("ProbeContext = %d, %v, want 262144", n, err)
	}
	// One model: its name does not have to match the route.
	if n, err := probe(t, srv, "", ""); err != nil || n != 262144 {
		t.Errorf("ProbeContext with another name = %d, %v, want 262144", n, err)
	}
}

func TestProbeContextPicksTheServedModel(t *testing.T) {
	srv := probeServer(t, "", map[string]string{
		"/v1/models": `{"data":[{"id":"small","max_model_len":8192},{"id":"big","max_model_len":131072}]}`,
	})
	if n, err := probe(t, srv, "big", ""); err != nil || n != 131072 {
		t.Errorf("ProbeContext(big) = %d, %v, want 131072", n, err)
	}
	if _, err := probe(t, srv, "other", ""); err == nil || !strings.Contains(err.Error(), "several models") {
		t.Errorf("ProbeContext(other) = %v, want an error about several models", err)
	}
}

func TestProbeContextToken(t *testing.T) {
	srv := probeServer(t, "backend-token", map[string]string{"/props": `{"default_generation_settings":{"n_ctx":4096}}`})
	if _, err := probe(t, srv, "", ""); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("without a token: %v, want ErrUnauthorized", err)
	}
	if _, err := probe(t, srv, "", "wrong"); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("with a wrong token: %v, want ErrUnauthorized", err)
	}
	if n, err := probe(t, srv, "", "backend-token"); err != nil || n != 4096 {
		t.Errorf("with the token: %d, %v, want 4096", n, err)
	}
}

func TestProbeContextWithoutASize(t *testing.T) {
	for name, pages := range map[string]map[string]string{
		"no endpoints":       {},
		"props without size": {"/props": `{"total_slots":1}`},
		"props not JSON":     {"/props": `<html>`},
		"models without len": {"/v1/models": `{"data":[{"id":"m"}]}`},
	} {
		srv := probeServer(t, "", pages)
		if n, err := probe(t, srv, "", ""); err == nil {
			t.Errorf("%s: ProbeContext = %d, want an error", name, n)
		}
	}
	// Nothing listens.
	srv := probeServer(t, "", nil)
	srv.Close()
	if _, err := probe(t, srv, "", ""); err == nil {
		t.Error("an unreachable backend reported a context size")
	}
}

// The container reaches the host's loopback as host.docker.internal; the
// probe runs on the host, where that name may not resolve.
func TestProbeContextAsksTheHostGatewayOnLoopback(t *testing.T) {
	srv := probeServer(t, "", map[string]string{"/props": `{"default_generation_settings":{"n_ctx":2048}}`})
	port := srv.URL[strings.LastIndex(srv.URL, ":")+1:]
	r := Route{ID: "m", URL: "http://" + HostGateway + ":" + port}
	n, err := ProbeContext(context.Background(), &http.Client{Transport: direct()}, r, "")
	if err != nil || n != 2048 {
		t.Errorf("ProbeContext = %d, %v, want 2048", n, err)
	}
}
