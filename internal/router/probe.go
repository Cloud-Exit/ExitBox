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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// ErrUnauthorized is returned by ProbeContext when the backend wants a
// token it was not given, or rejects the one it got.
var ErrUnauthorized = errors.New("the backend rejected the request (401/403)")

// maxProbeBody bounds a probe response. llama.cpp includes the chat
// template in /props, which can be large.
const maxProbeBody = 4 << 20

// ProbeContext asks a backend how many tokens of context it serves for a
// route. It tries the llama.cpp endpoint first (GET /props, where n_ctx is
// the context of one slot, which is what a request gets when the server
// runs with --parallel), then the vLLM one (max_model_len in GET
// /v1/models).
//
// It runs on the host, not in the container, so a backend the container
// reaches as host.docker.internal is asked on 127.0.0.1.
func ProbeContext(ctx context.Context, client *http.Client, r Route, token string) (int, error) {
	base, err := url.Parse(r.URL)
	if err != nil {
		return 0, err
	}
	if base.Hostname() == HostGateway {
		host := "127.0.0.1"
		if p := base.Port(); p != "" {
			host += ":" + p
		}
		base.Host = host
	}

	var props struct {
		NCtx     int `json:"n_ctx"`
		Defaults struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	status, err := probeGet(ctx, client, base.String()+"/props", token, &props)
	switch {
	case err != nil:
		return 0, err
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return 0, ErrUnauthorized
	case status == http.StatusOK && props.Defaults.NCtx > 0:
		return props.Defaults.NCtx, nil
	case status == http.StatusOK && props.NCtx > 0:
		return props.NCtx, nil
	}

	var models struct {
		Data []struct {
			ID          string `json:"id"`
			MaxModelLen int    `json:"max_model_len"`
		} `json:"data"`
	}
	status, err = probeGet(ctx, client, base.String()+"/v1/models", token, &models)
	switch {
	case err != nil:
		return 0, err
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return 0, ErrUnauthorized
	case status != http.StatusOK:
		return 0, fmt.Errorf("neither /props nor /v1/models reports a context size (HTTP %d)", status)
	}
	// Prefer the model the route asks for; a server with one model is
	// unambiguous either way.
	first := 0
	for _, m := range models.Data {
		if m.MaxModelLen <= 0 {
			continue
		}
		if m.ID == r.ModelName() {
			return m.MaxModelLen, nil
		}
		if first == 0 {
			first = m.MaxModelLen
		}
	}
	if first > 0 && len(models.Data) == 1 {
		return first, nil
	}
	if first > 0 {
		return 0, fmt.Errorf("the backend serves several models and none is named %q", r.ModelName())
	}
	return 0, errors.New("neither /props nor /v1/models reports a context size")
}

// probeGet fetches a JSON document. A body that is not JSON leaves into
// untouched: a server that answers /props with something else simply has no
// context size there.
func probeGet(ctx context.Context, client *http.Client, target, token string, into any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxProbeBody))
	if err != nil {
		return 0, err
	}
	if resp.StatusCode == http.StatusOK {
		_ = json.Unmarshal(body, into)
	}
	return resp.StatusCode, nil
}
