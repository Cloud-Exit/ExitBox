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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultUpstream is the Anthropic API.
const DefaultUpstream = "https://api.anthropic.com"

// HealthPath answers 200 once the router is listening. It is outside every
// path the Anthropic API uses.
const HealthPath = "/exitbox-router/health"

const (
	messagesPath    = "/v1/messages"
	countTokensPath = "/v1/messages/count_tokens"
	helloPath       = "/api/hello"

	// maxRequestBody bounds a Messages API request the router has to read
	// to find its model. The Anthropic API itself stops at 32 MB.
	maxRequestBody = 64 << 20
	// maxErrorBody bounds an error response held back to decide on a retry.
	maxErrorBody = 1 << 20
	// statusClientClosed is logged when the client hung up before an answer.
	statusClientClosed = 499
)

// Config is what a Router needs.
type Config struct {
	Routes []Route
	// Tokens maps a vault key to the bearer token stored under it. A route
	// whose key is missing here is sent without a token.
	Tokens map[string]string
	// Upstream is the Anthropic API base URL. Empty means DefaultUpstream.
	Upstream string
	// Sanitize drops what a local backend does not understand.
	Sanitize bool
	// Log receives one line per request. Nil discards them.
	Log io.Writer
	// Transport carries every outgoing request. Nil builds one that goes
	// through the proxy named in the environment and never times out a
	// response, since a local model can take minutes to answer.
	Transport http.RoundTripper
}

// Router is an http.Handler that splits Claude Code's API traffic by model.
type Router struct {
	routes    map[string]*backend
	upstream  *url.URL
	sanitize  bool
	transport http.RoundTripper
	proxy     *httputil.ReverseProxy
	logMu     sync.Mutex
	log       io.Writer
	now       func() time.Time
}

type backend struct {
	route Route
	token string
	// noCacheControl is set once the backend has rejected cache_control;
	// from then on the field is removed before a request is sent.
	noCacheControl atomic.Bool
}

type contextKey struct{}

// entry collects what one request's log line reports.
type entry struct {
	start   time.Time
	method  string
	path    string
	model   string
	route   string
	dropped []string
}

// New builds a Router.
func New(cfg Config) (*Router, error) {
	raw := cfg.Upstream
	if raw == "" {
		raw = DefaultUpstream
	}
	up, err := url.Parse(raw)
	if err != nil || up.Host == "" || (up.Scheme != "http" && up.Scheme != "https") {
		return nil, fmt.Errorf("upstream %q is not an http(s) URL", raw)
	}
	rt := &Router{
		routes:    map[string]*backend{},
		upstream:  up,
		sanitize:  cfg.Sanitize,
		transport: cfg.Transport,
		log:       cfg.Log,
		now:       time.Now,
	}
	if rt.transport == nil {
		rt.transport = NewTransport()
	}
	for _, r := range cfg.Routes {
		rt.routes[r.ID] = &backend{route: r, token: cfg.Tokens[r.VaultKey]}
	}
	rt.proxy = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			// Only the destination changes. Rewrite, unlike Director,
			// adds no X-Forwarded headers, so Anthropic sees the request
			// exactly as Claude Code wrote it.
			pr.Out.URL.Scheme = up.Scheme
			pr.Out.URL.Host = up.Host
			pr.Out.URL.Path = strings.TrimRight(up.Path, "/") + pr.In.URL.Path
			pr.Out.URL.RawPath = ""
			pr.Out.Host = up.Host
		},
		Transport: rt.transport,
		// Flush after every write: Claude Code reads the stream event by
		// event and aborts one that stays silent.
		FlushInterval: -1,
		ModifyResponse: func(resp *http.Response) error {
			if e, ok := resp.Request.Context().Value(contextKey{}).(*entry); ok {
				resp.Body = &meteredBody{ReadCloser: resp.Body, meter: newUsageMeter(resp.Header), done: func(m *usageMeter) {
					rt.write(e, resp.StatusCode, m)
				}}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			status := http.StatusBadGateway
			if errors.Is(err, context.Canceled) {
				// Claude Code went away mid-request; nobody reads the answer.
				status = statusClientClosed
			}
			if e, ok := r.Context().Value(contextKey{}).(*entry); ok {
				rt.write(e, status, nil)
			}
			writeError(w, http.StatusBadGateway, "the Anthropic API could not be reached: "+err.Error())
		},
	}
	return rt, nil
}

// NewTransport returns the transport the router uses by default. It honors
// the proxy environment (the container's only way out is the firewall
// proxy), sets no response timeout, and leaves compression to the client so
// responses are relayed as they arrive.
func NewTransport() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DisableCompression = true
	t.ResponseHeaderTimeout = 0
	return t
}

func (rt *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e := &entry{start: rt.now(), method: r.Method, path: r.URL.Path, route: "anthropic"}
	switch {
	case r.URL.Path == HealthPath:
		_, _ = io.WriteString(w, "ok\n")
		return
	case r.Method == http.MethodHead && r.URL.Path == helloPath:
		// Claude Code's connectivity check. Answering here keeps a start
		// without network from blocking on it.
		w.WriteHeader(http.StatusOK)
		e.route = "local"
		rt.write(e, http.StatusOK, nil)
		return
	case r.Method == http.MethodPost && (r.URL.Path == messagesPath || r.URL.Path == countTokensPath):
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
		if err != nil {
			status := http.StatusBadRequest
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				status = http.StatusRequestEntityTooLarge
			}
			rt.write(e, status, nil)
			writeError(w, status, "the request body could not be read: "+err.Error())
			return
		}
		e.model = peekModel(body)
		if b, ok := rt.routes[e.model]; ok && e.model != "" {
			rt.serveLocal(w, r, b, body, e)
			return
		}
		// The body was read to find the model; hand the same bytes on.
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
		r.TransferEncoding = nil
	}
	rt.proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, e)))
}

// peekModel returns the model a Messages API request asks for, or "".
func peekModel(body []byte) string {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return ""
	}
	return req.Model
}

// serveLocal sends a request to a local backend and relays the answer.
func (rt *Router) serveLocal(w http.ResponseWriter, r *http.Request, b *backend, body []byte, e *entry) {
	e.route = "local:" + b.route.URL
	opts := rewriteOptions{
		Model:             b.route.ModelName(),
		Sanitize:          rt.sanitize,
		StripCacheControl: b.noCacheControl.Load(),
	}
	out, dropped, err := rewrite(body, opts)
	if err != nil {
		rt.write(e, http.StatusBadRequest, nil)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	e.dropped = dropped

	resp, err := rt.sendLocal(r, b, out)
	if err != nil {
		rt.write(e, http.StatusBadGateway, nil)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("backend %s for model %q could not be reached: %v", b.route.URL, b.route.ID, err))
		return
	}

	// Prompt caching markers are kept until a backend refuses them. The
	// refusal is a 4xx naming the field; retry once without it and remember.
	var held []byte
	if !opts.StripCacheControl && (resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity) {
		held, _ = io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		if bytes.Contains(held, []byte("cache_control")) {
			opts.StripCacheControl = true
			if retryBody, _, err := rewrite(body, opts); err == nil {
				if retry, err := rt.sendLocal(r, b, retryBody); err == nil {
					_ = resp.Body.Close()
					b.noCacheControl.Store(true)
					resp, held = retry, nil
				}
			}
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Status and body go back unmodified: Claude Code's retry logic matches
	// on the wording of errors.
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	meter := newUsageMeter(resp.Header)
	relay(w, io.MultiReader(bytes.NewReader(held), resp.Body), meter)
	rt.write(e, resp.StatusCode, meter)
}

// sendLocal posts a rewritten request to a backend. Claude Code's own
// credentials stay behind: the backend gets its token or none.
func (rt *Router) sendLocal(r *http.Request, b *backend, body []byte) (*http.Response, error) {
	// The query (?beta=true) asks for Anthropic beta features; a local
	// backend has none, so it is dropped together with anthropic-beta.
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, b.route.URL+r.URL.Path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyHeader(req.Header, r.Header)
	for _, h := range []string{"Authorization", "X-Api-Key", "Anthropic-Beta", "Accept-Encoding", "Content-Length"} {
		req.Header.Del(h)
	}
	if b.token != "" {
		// Both spellings of the same backend token: vLLM reads the bearer,
		// llama.cpp documents x-api-key for its Messages API.
		req.Header.Set("Authorization", "Bearer "+b.token)
		req.Header.Set("X-Api-Key", b.token)
	}
	return rt.transport.RoundTrip(req)
}

// hopHeaders describe one connection, not the message, and are not relayed.
var hopHeaders = []string{
	"Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization",
	"Proxy-Connection", "Te", "Trailer", "Transfer-Encoding", "Upgrade",
}

func copyHeader(dst, src http.Header) {
	skip := map[string]bool{}
	for _, h := range hopHeaders {
		skip[h] = true
	}
	for _, name := range src.Values("Connection") {
		for _, f := range strings.Split(name, ",") {
			skip[http.CanonicalHeaderKey(strings.TrimSpace(f))] = true
		}
	}
	for k, vs := range src {
		if skip[k] {
			continue
		}
		dst[k] = append([]string(nil), vs...)
	}
}

// relay copies a response body to the client, flushing after every read so
// a stream arrives event by event.
func relay(w http.ResponseWriter, body io.Reader, meter *usageMeter) {
	rc := http.NewResponseController(w)
	buf := make([]byte, 32<<10)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			meter.Write(buf[:n])
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			_ = rc.Flush()
		}
		if err != nil {
			return
		}
	}
}

// meteredBody feeds a response body to a usage meter as it is read and
// reports once when the body is closed.
type meteredBody struct {
	io.ReadCloser
	meter *usageMeter
	done  func(*usageMeter)
	once  sync.Once
}

func (b *meteredBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.meter.Write(p[:n])
	}
	return n, err
}

func (b *meteredBody) Close() error {
	err := b.ReadCloser.Close()
	b.once.Do(func() { b.done(b.meter) })
	return err
}

// write appends one line to the request log: time, request, model, route,
// status, duration and token counts. Bodies and headers are never logged.
func (rt *Router) write(e *entry, status int, meter *usageMeter) {
	if rt.log == nil {
		return
	}
	now := rt.now()
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s model=%s route=%s status=%d dur=%s",
		now.UTC().Format(time.RFC3339), e.method, e.path, orDash(e.model), e.route, status,
		now.Sub(e.start).Round(time.Millisecond))
	if meter != nil {
		if in, out, ok := meter.Result(); ok {
			fmt.Fprintf(&b, " in=%d out=%d", in, out)
		}
	}
	if len(e.dropped) > 0 {
		fmt.Fprintf(&b, " dropped_tools=%s", strings.Join(e.dropped, ","))
	}
	b.WriteByte('\n')
	rt.logMu.Lock()
	defer rt.logMu.Unlock()
	_, _ = io.WriteString(rt.log, b.String())
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// writeError answers in the shape of an Anthropic API error, which is what
// Claude Code knows how to show.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    "api_error",
			"message": "exitbox-router: " + msg,
		},
	})
}
