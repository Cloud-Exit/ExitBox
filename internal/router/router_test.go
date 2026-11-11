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
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// seen is one request as a fake server received it.
type seen struct {
	Method string
	URI    string
	Host   string
	Header http.Header
	Body   []byte
}

// recorder is a fake API server that records every request and answers
// through reply.
type recorder struct {
	mu    sync.Mutex
	reqs  []seen
	reply func(w http.ResponseWriter, r *http.Request, body []byte)
	*httptest.Server
}

func newRecorder(t *testing.T, reply func(w http.ResponseWriter, r *http.Request, body []byte)) *recorder {
	t.Helper()
	rec := &recorder{reply: reply}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.reqs = append(rec.reqs, seen{Method: r.Method, URI: r.URL.RequestURI(), Host: r.Host, Header: r.Header.Clone(), Body: body})
		rec.mu.Unlock()
		if rec.reply != nil {
			rec.reply(w, r, body)
		}
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (rec *recorder) requests() []seen {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return append([]seen(nil), rec.reqs...)
}

// syncBuffer is a log sink that can be read while the router writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// waitLines returns the log once it holds n lines.
func (s *syncBuffer) waitLines(t *testing.T, n int) []string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		text := s.String()
		lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
		if text != "" && len(lines) >= n {
			return lines
		}
		if time.Now().After(deadline) {
			t.Fatalf("log has %d lines, want %d:\n%s", len(lines), n, s.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// direct is a transport that ignores the proxy environment, so the tests
// behave the same inside and outside a sandbox.
func direct() *http.Transport {
	return &http.Transport{Proxy: nil, DisableCompression: true}
}

type fixture struct {
	anthropic *recorder
	router    *httptest.Server
	log       *syncBuffer
	client    *http.Client
}

// start runs a router in front of a fake Anthropic API.
func start(t *testing.T, routes string, tokens map[string]string, sanitize bool, anthropicReply func(http.ResponseWriter, *http.Request, []byte)) *fixture {
	t.Helper()
	f := &fixture{log: &syncBuffer{}, client: &http.Client{Transport: direct()}}
	f.anthropic = newRecorder(t, anthropicReply)
	parsed, err := ParseRoutes(routes)
	if err != nil {
		t.Fatal(err)
	}
	rt, err := New(Config{Routes: parsed, Tokens: tokens, Upstream: f.anthropic.URL, Sanitize: sanitize, Log: f.log, Transport: direct()})
	if err != nil {
		t.Fatal(err)
	}
	f.router = httptest.NewServer(rt)
	t.Cleanup(f.router.Close)
	return f
}

func (f *fixture) do(t *testing.T, method, path string, header map[string]string, body io.Reader) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, f.router.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, out
}

var claudeHeaders = map[string]string{
	"Authorization":            "Bearer oauth-access-token",
	"X-Api-Key":                "sk-ant-api-key",
	"Anthropic-Beta":           "oauth-2025-04-20,claude-code-20250219",
	"Anthropic-Version":        "2023-06-01",
	"Content-Type":             "application/json",
	"User-Agent":               "claude-cli/2.1.319 (external, cli)",
	"X-Claude-Code-Session-Id": "session-1",
	"X-Stainless-Lang":         "js",
}

// The body is deliberately not what a JSON encoder would produce: spacing
// and key order have to survive.
const claudeBody = `{ "stream" : true, "model":"claude-opus-5",  "max_tokens":32000,"messages":[{"role":"user","content":"SECRET-PROMPT-TEXT"}],"context_management":{"edits":[]} }`

func TestAnthropicLegForwardsTheRequestUnchanged(t *testing.T) {
	f := start(t, "qwen=http://127.0.0.1:1", nil, true, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Request-Id", "req_123")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"msg_1","usage":{"input_tokens":7,"output_tokens":3}}`)
	})
	resp, body := f.do(t, http.MethodPost, "/v1/messages?beta=true", claudeHeaders, strings.NewReader(claudeBody))
	if resp.StatusCode != http.StatusOK || string(body) != `{"id":"msg_1","usage":{"input_tokens":7,"output_tokens":3}}` {
		t.Errorf("response = %d %s", resp.StatusCode, body)
	}
	if got := resp.Header.Get("Request-Id"); got != "req_123" {
		t.Errorf("response header Request-Id = %q", got)
	}

	reqs := f.anthropic.requests()
	if len(reqs) != 1 {
		t.Fatalf("upstream got %d requests, want 1", len(reqs))
	}
	got := reqs[0]
	if got.Method != http.MethodPost || got.URI != "/v1/messages?beta=true" {
		t.Errorf("upstream request line = %s %s", got.Method, got.URI)
	}
	if string(got.Body) != claudeBody {
		t.Errorf("upstream body differs\n got %s\nwant %s", got.Body, claudeBody)
	}
	for k, v := range claudeHeaders {
		if got.Header.Get(k) != v {
			t.Errorf("upstream header %s = %q, want %q", k, got.Header.Get(k), v)
		}
	}
	for _, k := range []string{"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "Via"} {
		if v := got.Header.Get(k); v != "" {
			t.Errorf("upstream got %s: %q; the router must not announce itself", k, v)
		}
	}
	if want := strings.TrimPrefix(f.anthropic.URL, "http://"); got.Host != want {
		t.Errorf("upstream Host = %q, want %q", got.Host, want)
	}
}

func TestAnthropicLegForwardsAChunkedBodyUnchanged(t *testing.T) {
	f := start(t, "", nil, true, nil)
	// A reader of unknown length makes the client send chunked encoding.
	resp, _ := f.do(t, http.MethodPost, "/v1/messages", claudeHeaders, io.MultiReader(strings.NewReader(claudeBody)))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	reqs := f.anthropic.requests()
	if len(reqs) != 1 || string(reqs[0].Body) != claudeBody {
		t.Errorf("upstream body differs: %+v", reqs)
	}
}

func TestUnroutedModelsAndBrokenBodiesGoToAnthropic(t *testing.T) {
	f := start(t, "qwen=http://127.0.0.1:1", nil, true, nil)
	for _, body := range []string{
		`{"model":"gpt-oss-120b","messages":[]}`, // not a route: Anthropic answers with its own error
		`{"model":"QWEN","messages":[]}`,         // routes match exactly
		`{"model":`,                              // not JSON
		``,
	} {
		f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(body))
	}
	reqs := f.anthropic.requests()
	if len(reqs) != 4 {
		t.Fatalf("upstream got %d requests, want 4", len(reqs))
	}
	if string(reqs[2].Body) != `{"model":` {
		t.Errorf("a broken body was changed: %q", reqs[2].Body)
	}
}

func TestOtherPathsPassThrough(t *testing.T) {
	f := start(t, "qwen=http://127.0.0.1:1", nil, true, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.WriteHeader(http.StatusTeapot)
	})
	resp, _ := f.do(t, http.MethodGet, "/v1/models?limit=5", claudeHeaders, nil)
	if resp.StatusCode != http.StatusTeapot {
		t.Errorf("status = %d, want the upstream's", resp.StatusCode)
	}
	// A body on another path is not read for a model, even if it has one.
	f.do(t, http.MethodPost, "/api/event_logging/batch", nil, strings.NewReader(`{"model":"qwen"}`))
	// GET on the hello path is not the connectivity check.
	f.do(t, http.MethodGet, "/api/hello", nil, nil)

	reqs := f.anthropic.requests()
	if len(reqs) != 3 {
		t.Fatalf("upstream got %d requests, want 3", len(reqs))
	}
	if reqs[0].URI != "/v1/models?limit=5" || reqs[0].Header.Get("Authorization") != claudeHeaders["Authorization"] {
		t.Errorf("first request = %+v", reqs[0])
	}
	if reqs[1].URI != "/api/event_logging/batch" || string(reqs[1].Body) != `{"model":"qwen"}` {
		t.Errorf("second request = %+v", reqs[1])
	}
}

func TestHelloAndHealthAreAnsweredLocally(t *testing.T) {
	f := start(t, "", nil, true, nil)
	resp, _ := f.do(t, http.MethodHead, "/api/hello", nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("HEAD /api/hello = %d", resp.StatusCode)
	}
	resp, body := f.do(t, http.MethodGet, HealthPath, nil, nil)
	if resp.StatusCode != http.StatusOK || string(body) != "ok\n" {
		t.Errorf("health = %d %q", resp.StatusCode, body)
	}
	if n := len(f.anthropic.requests()); n != 0 {
		t.Errorf("upstream got %d requests, want none", n)
	}
}

// readEvent reads one server-sent event (up to its blank line) or fails
// after a timeout, so a relay that buffers shows up as a failure and not as
// a hung test.
func readEvent(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	type result struct {
		ev  string
		err error
	}
	ch := make(chan result, 1)
	go func() {
		var b strings.Builder
		for {
			line, err := r.ReadString('\n')
			b.WriteString(line)
			if err != nil || line == "\n" {
				ch <- result{b.String(), err}
				return
			}
		}
	}()
	select {
	case res := <-ch:
		if res.err != nil && res.ev == "" {
			t.Fatalf("stream ended: %v", res.err)
		}
		return res.ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event arrived: the router is holding the stream back")
		return ""
	}
}

// streamRequest builds a streaming Messages API request that gives up after
// a few seconds, so a router that holds a stream back fails the test instead
// of hanging it.
func streamRequest(t *testing.T, base, body string) *http.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages?beta=true", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

// gate returns a channel a fake server waits on and the function that
// opens it. Register the function with t.Cleanup after the servers are
// started: cleanups run last in, first out, so the gate then opens before a
// server waits for its handler, and a failed test cannot hang.
func gate() (<-chan struct{}, func()) {
	ch := make(chan struct{})
	var once sync.Once
	return ch, func() { once.Do(func() { close(ch) }) }
}

// streamReply writes one event, waits for release, then sends a ping and
// the final event.
func streamReply(release <-chan struct{}) func(http.ResponseWriter, *http.Request, []byte) {
	return func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":1}}}\n\n")
		w.(http.Flusher).Flush()
		<-release
		_, _ = io.WriteString(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":34}}\n\n")
	}
}

func TestAnthropicLegStreamsEventsAsTheyArrive(t *testing.T) {
	release, done := gate()
	f := start(t, "", nil, true, streamReply(release))
	t.Cleanup(done)
	resp, err := f.client.Do(streamRequest(t, f.router.URL, claudeBody))
	if err != nil {
		t.Fatalf("no response headers arrived while the upstream was still streaming: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	r := bufio.NewReader(resp.Body)
	// The upstream is still holding its response open at this point.
	if ev := readEvent(t, r); !strings.HasPrefix(ev, "event: message_start\n") {
		t.Fatalf("first event = %q", ev)
	}
	done()
	if ev := readEvent(t, r); ev != "event: ping\ndata: {\"type\":\"ping\"}\n\n" {
		t.Errorf("ping was not relayed as sent: %q", ev)
	}
	if ev := readEvent(t, r); !strings.HasPrefix(ev, "event: message_delta\n") {
		t.Errorf("last event = %q", ev)
	}
	line := f.log.waitLines(t, 1)[0]
	if !strings.Contains(line, "model=claude-opus-5 route=anthropic status=200") || !strings.HasSuffix(line, " in=12 out=34") {
		t.Errorf("log line = %q", line)
	}
}

func TestLocalLegStreamsEventsAsTheyArrive(t *testing.T) {
	release, done := gate()
	backend := newRecorder(t, streamReply(release))
	f := start(t, "qwen="+backend.URL, nil, true, nil)
	t.Cleanup(done)
	resp, err := f.client.Do(streamRequest(t, f.router.URL, `{"model":"qwen","stream":true,"messages":[]}`))
	if err != nil {
		t.Fatalf("no response headers arrived while the backend was still streaming: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	r := bufio.NewReader(resp.Body)
	if ev := readEvent(t, r); !strings.HasPrefix(ev, "event: message_start\n") {
		t.Fatalf("first event = %q", ev)
	}
	done()
	if ev := readEvent(t, r); !strings.HasPrefix(ev, "event: ping\n") {
		t.Errorf("second event = %q", ev)
	}
	readEvent(t, r)
	line := f.log.waitLines(t, 1)[0]
	if !strings.Contains(line, "model=qwen route=local:"+backend.URL+" status=200") || !strings.HasSuffix(line, " in=12 out=34") {
		t.Errorf("log line = %q", line)
	}
}

const localBody = `{"model":"qwen","max_tokens":100,"messages":[{"role":"user","content":"SECRET-PROMPT-TEXT"}],"context_management":{"edits":[]},"tools":[{"name":"Read","input_schema":{}},{"type":"web_search_20250305","name":"web_search"}]}`

func TestLocalLegReplacesCredentialsAndModel(t *testing.T) {
	backend := newRecorder(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_local","usage":{"input_tokens":5,"output_tokens":9}}`)
	})
	f := start(t, "qwen="+backend.URL+"/v1=qwen3-coder=vault:LLM_KEY", map[string]string{"LLM_KEY": "backend-token"}, true, nil)
	resp, body := f.do(t, http.MethodPost, "/v1/messages?beta=true", claudeHeaders, strings.NewReader(localBody))
	if resp.StatusCode != http.StatusOK || string(body) != `{"id":"msg_local","usage":{"input_tokens":5,"output_tokens":9}}` {
		t.Errorf("response = %d %s", resp.StatusCode, body)
	}
	if n := len(f.anthropic.requests()); n != 0 {
		t.Fatalf("a routed request reached Anthropic (%d requests)", n)
	}
	reqs := backend.requests()
	if len(reqs) != 1 {
		t.Fatalf("backend got %d requests, want 1", len(reqs))
	}
	got := reqs[0]
	if got.Method != http.MethodPost || got.URI != "/v1/messages" {
		t.Errorf("backend request line = %s %s, want POST /v1/messages without the beta query", got.Method, got.URI)
	}
	if auth := got.Header.Get("Authorization"); auth != "Bearer backend-token" {
		t.Errorf("backend Authorization = %q, want the backend's token", auth)
	}
	if key := got.Header.Get("X-Api-Key"); key != "backend-token" {
		t.Errorf("backend X-Api-Key = %q, want the backend's token", key)
	}
	if v := got.Header.Get("Anthropic-Beta"); v != "" {
		t.Errorf("backend got Anthropic-Beta: %q", v)
	}
	for k, vs := range got.Header {
		for _, v := range vs {
			if strings.Contains(v, "oauth-access-token") || strings.Contains(v, "sk-ant-api-key") {
				t.Errorf("Claude Code's credential reached the backend in %s", k)
			}
		}
	}
	for _, k := range []string{"Anthropic-Version", "Content-Type", "User-Agent"} {
		if got.Header.Get(k) != claudeHeaders[k] {
			t.Errorf("backend header %s = %q, want %q", k, got.Header.Get(k), claudeHeaders[k])
		}
	}
	if strings.Contains(string(got.Body), "oauth-access-token") || strings.Contains(string(got.Body), "sk-ant-api-key") {
		t.Error("Claude Code's credentials reached the backend")
	}
	var sent map[string]any
	if err := json.Unmarshal(got.Body, &sent); err != nil {
		t.Fatalf("backend body is not JSON: %v", err)
	}
	if sent["model"] != "qwen3-coder" {
		t.Errorf("backend model = %v, want the served name", sent["model"])
	}
	if _, ok := sent["context_management"]; ok {
		t.Error("the sanitizer did not run")
	}

	line := f.log.waitLines(t, 1)[0]
	for _, want := range []string{"POST /v1/messages ", "model=qwen ", "route=local:" + backend.URL + " ", "status=200", " in=5 out=9", " dropped_tools=web_search"} {
		if !strings.Contains(line, want) {
			t.Errorf("log line %q lacks %q", line, want)
		}
	}
}

func TestLocalLegSendsNoTokenWhenThereIsNone(t *testing.T) {
	backend := newRecorder(t, nil)
	// One route without a vault key, one whose key came without a token.
	f := start(t, "a="+backend.URL+",b="+backend.URL+"=vault:MISSING", nil, true, nil)
	f.do(t, http.MethodPost, "/v1/messages", claudeHeaders, strings.NewReader(`{"model":"a","messages":[]}`))
	f.do(t, http.MethodPost, "/v1/messages", claudeHeaders, strings.NewReader(`{"model":"b","messages":[]}`))
	reqs := backend.requests()
	if len(reqs) != 2 {
		t.Fatalf("backend got %d requests, want 2", len(reqs))
	}
	for i, r := range reqs {
		for _, k := range []string{"Authorization", "X-Api-Key"} {
			if _, ok := r.Header[k]; ok {
				t.Errorf("request %d carries %s %q", i, k, r.Header.Get(k))
			}
		}
	}
}

func TestLocalLegSanitizeOff(t *testing.T) {
	backend := newRecorder(t, nil)
	f := start(t, "qwen="+backend.URL, nil, false, nil)
	f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(localBody))
	reqs := backend.requests()
	if len(reqs) != 1 || string(reqs[0].Body) != localBody {
		t.Errorf("with sanitize off the body must arrive unchanged: %+v", reqs)
	}
}

func TestLocalLegReturnsErrorsUnmodified(t *testing.T) {
	const errBody = `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}  `
	backend := newRecorder(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(529)
		_, _ = io.WriteString(w, errBody)
	})
	f := start(t, "qwen="+backend.URL, nil, true, nil)
	resp, body := f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(localBody))
	if resp.StatusCode != 529 || string(body) != errBody || resp.Header.Get("Retry-After") != "7" {
		t.Errorf("response = %d %q Retry-After=%q, want the backend's answer untouched", resp.StatusCode, body, resp.Header.Get("Retry-After"))
	}
}

func TestLocalLegCountTokens(t *testing.T) {
	var supported atomic.Bool
	supported.Store(true)
	backend := newRecorder(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if !supported.Load() {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"input_tokens":4242}`)
	})
	f := start(t, "qwen="+backend.URL+"=served", nil, true, nil)

	resp, body := f.do(t, http.MethodPost, "/v1/messages/count_tokens?beta=true", claudeHeaders, strings.NewReader(localBody))
	if resp.StatusCode != http.StatusOK || string(body) != `{"input_tokens":4242}` {
		t.Errorf("response = %d %s", resp.StatusCode, body)
	}
	reqs := backend.requests()
	if len(reqs) != 1 || reqs[0].URI != "/v1/messages/count_tokens" {
		t.Fatalf("backend requests = %+v", reqs)
	}
	if !strings.Contains(string(reqs[0].Body), `"model":"served"`) {
		t.Errorf("count_tokens body was not rewritten: %s", reqs[0].Body)
	}
	if line := f.log.waitLines(t, 1)[0]; !strings.Contains(line, "POST /v1/messages/count_tokens ") || !strings.HasSuffix(line, " in=4242 out=0 dropped_tools=web_search") {
		t.Errorf("log line = %q", line)
	}

	// A backend without the endpoint: Claude Code needs the 404 to fall
	// back to its own estimate.
	supported.Store(false)
	resp, _ = f.do(t, http.MethodPost, "/v1/messages/count_tokens", nil, strings.NewReader(localBody))
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want the backend's 404", resp.StatusCode)
	}
	if n := len(f.anthropic.requests()); n != 0 {
		t.Errorf("count_tokens for a routed model reached Anthropic (%d requests)", n)
	}
}

const cachedRequest = `{"model":"qwen","system":[{"type":"text","text":"s","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":"hi"}]}`

func TestLocalLegDropsCacheControlOnceABackendRejectsIt(t *testing.T) {
	backend := newRecorder(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
		w.Header().Set("Content-Type", "application/json")
		if bytes.Contains(body, []byte("cache_control")) {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"system.0.cache_control: Extra inputs are not permitted"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	})
	f := start(t, "qwen="+backend.URL, nil, true, nil)

	resp, body := f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(cachedRequest))
	if resp.StatusCode != http.StatusOK || string(body) != `{"id":"ok"}` {
		t.Fatalf("first response = %d %s, want the retried answer", resp.StatusCode, body)
	}
	if n := len(backend.requests()); n != 2 {
		t.Fatalf("backend got %d requests for the first call, want the rejected one and the retry", n)
	}
	// From now on the field is removed up front.
	resp, _ = f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(cachedRequest))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("second response = %d", resp.StatusCode)
	}
	reqs := backend.requests()
	if len(reqs) != 3 {
		t.Fatalf("backend got %d requests, want 3: the backend must not be asked twice again", len(reqs))
	}
	if bytes.Contains(reqs[2].Body, []byte("cache_control")) {
		t.Errorf("cache_control was sent again: %s", reqs[2].Body)
	}
}

func TestLocalLegKeepsCacheControlForABackendThatAcceptsIt(t *testing.T) {
	backend := newRecorder(t, nil)
	f := start(t, "qwen="+backend.URL, nil, true, nil)
	f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(cachedRequest))
	f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(cachedRequest))
	for i, r := range backend.requests() {
		if !bytes.Contains(r.Body, []byte(`"cache_control":{"type":"ephemeral"}`)) {
			t.Errorf("request %d lost cache_control: %s", i, r.Body)
		}
	}
}

func TestLocalLegDoesNotRetryOtherErrors(t *testing.T) {
	const errBody = `{"error":{"message":"max_tokens is too large"}}`
	backend := newRecorder(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, errBody)
	})
	f := start(t, "qwen="+backend.URL, nil, true, nil)
	resp, body := f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(cachedRequest))
	if resp.StatusCode != http.StatusBadRequest || string(body) != errBody {
		t.Errorf("response = %d %s, want the backend's error", resp.StatusCode, body)
	}
	if n := len(backend.requests()); n != 1 {
		t.Errorf("backend got %d requests, want 1", n)
	}
}

func TestLocalLegReportsAnUnreachableBackend(t *testing.T) {
	// Reserve a port and close it again: nothing listens there.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	f := start(t, "qwen=http://"+addr, nil, true, nil)
	resp, body := f.do(t, http.MethodPost, "/v1/messages", nil, strings.NewReader(localBody))
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", resp.StatusCode)
	}
	var e struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("error body is not JSON: %s", body)
	}
	if e.Type != "error" || !strings.Contains(e.Error.Message, "exitbox-router") || !strings.Contains(e.Error.Message, `"qwen"`) {
		t.Errorf("error = %+v, want an Anthropic-shaped error naming the router and the model", e)
	}
	if n := len(f.anthropic.requests()); n != 0 {
		t.Errorf("a failed local request fell back to Anthropic (%d requests)", n)
	}
	if line := f.log.waitLines(t, 1)[0]; !strings.Contains(line, "status=502") {
		t.Errorf("log line = %q", line)
	}
}

// The log names requests, never their content.
func TestLogHoldsNoBodiesHeadersOrTokens(t *testing.T) {
	backend := newRecorder(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"SECRET-ANSWER-TEXT"}],"usage":{"input_tokens":1,"output_tokens":2}}`)
	})
	f := start(t, "qwen="+backend.URL+"=vault:LLM_KEY", map[string]string{"LLM_KEY": "backend-token"}, true, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"content":[{"type":"text","text":"SECRET-ANSWER-TEXT"}],"usage":{"input_tokens":3,"output_tokens":4}}`)
	})
	f.do(t, http.MethodPost, "/v1/messages?beta=true", claudeHeaders, strings.NewReader(claudeBody))
	f.do(t, http.MethodPost, "/v1/messages?beta=true", claudeHeaders, strings.NewReader(localBody))
	f.do(t, http.MethodGet, "/v1/models?after_id=SECRET-QUERY", claudeHeaders, nil)

	lines := f.log.waitLines(t, 3)
	if len(lines) != 3 {
		t.Fatalf("log has %d lines, want one per request:\n%s", len(lines), f.log.String())
	}
	log := f.log.String()
	for _, secret := range []string{
		"SECRET-PROMPT-TEXT", "SECRET-ANSWER-TEXT", "SECRET-QUERY",
		"oauth-access-token", "sk-ant-api-key", "backend-token", "LLM_KEY",
		"session-1", "oauth-2025-04-20",
	} {
		if strings.Contains(log, secret) {
			t.Errorf("the log contains %q:\n%s", secret, log)
		}
	}
	if !strings.Contains(lines[0], "route=anthropic") || !strings.HasSuffix(lines[0], " in=3 out=4") {
		t.Errorf("line 1 = %q", lines[0])
	}
	if !strings.Contains(lines[1], "route=local:") || !strings.Contains(lines[1], " in=1 out=2") {
		t.Errorf("line 2 = %q", lines[1])
	}
	if !strings.Contains(lines[2], "GET /v1/models model=- route=anthropic") {
		t.Errorf("line 3 = %q", lines[2])
	}
}

func TestNewRejectsABadUpstream(t *testing.T) {
	for _, up := range []string{"api.anthropic.com", "ftp://x", "http://"} {
		if _, err := New(Config{Upstream: up}); err == nil {
			t.Errorf("upstream %q accepted", up)
		}
	}
	rt, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	if rt.upstream.String() != DefaultUpstream {
		t.Errorf("default upstream = %s", rt.upstream)
	}
}

func TestUsageMeter(t *testing.T) {
	h := func(ct, enc string) http.Header {
		hd := http.Header{}
		hd.Set("Content-Type", ct)
		if enc != "" {
			hd.Set("Content-Encoding", enc)
		}
		return hd
	}
	// An event split across writes, with CRLF line ends.
	m := newUsageMeter(h("text/event-stream; charset=utf-8", ""))
	m.Write([]byte("event: message_start\r\ndata: {\"type\":\"message_start\",\"message\":{\"usa"))
	m.Write([]byte("ge\":{\"input_tokens\":100,\"output_tokens\":1}}}\r\n\r\nevent: content_block_delta\r\ndata: {\"delta\":{\"text\":\"the word usage, not the field\"}}\r\n\r\n"))
	m.Write([]byte("data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":250}}\n\n"))
	if in, out, ok := m.Result(); !ok || in != 100 || out != 250 {
		t.Errorf("stream usage = %d %d %v, want 100 250 true", in, out, ok)
	}

	m = newUsageMeter(h("application/json", ""))
	m.Write([]byte(`{"usage":{"input_tok`))
	m.Write([]byte(`ens":8,"output_tokens":9}}`))
	if in, out, ok := m.Result(); !ok || in != 8 || out != 9 {
		t.Errorf("JSON usage = %d %d %v, want 8 9 true", in, out, ok)
	}

	// Compressed, of another type, or without usage: nothing to report.
	for _, hd := range []http.Header{h("application/json", "gzip"), h("text/html", ""), h("", "")} {
		m = newUsageMeter(hd)
		m.Write([]byte(`{"usage":{"input_tokens":8,"output_tokens":9}}`))
		if _, _, ok := m.Result(); ok {
			t.Errorf("usage reported for headers %v", hd)
		}
	}
	m = newUsageMeter(h("application/json", ""))
	m.Write([]byte(`{"error":{"message":"no usage here"}}`))
	if _, _, ok := m.Result(); ok {
		t.Error("usage reported for a body without one")
	}
}
