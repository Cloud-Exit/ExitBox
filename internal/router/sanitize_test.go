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
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// decode parses a rewritten body for inspection.
func decode(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("rewritten body is not JSON: %v\n%s", err, body)
	}
	return m
}

func mustRewrite(t *testing.T, body string, opts rewriteOptions) ([]byte, []string) {
	t.Helper()
	out, dropped, err := rewrite([]byte(body), opts)
	if err != nil {
		t.Fatalf("rewrite: %v", err)
	}
	return out, dropped
}

func TestRewriteSetsServedModel(t *testing.T) {
	out, _ := mustRewrite(t, `{"model":"qwen3.6-27b","max_tokens":10,"messages":[]}`, rewriteOptions{Model: "qwen3-coder"})
	if got := decode(t, out)["model"]; got != "qwen3-coder" {
		t.Errorf("model = %v, want the served name", got)
	}
}

func TestSanitizeKeepsOnlyKnownTopLevelFields(t *testing.T) {
	const body = `{
		"model":"m","max_tokens":10,"stream":true,"system":"s","messages":[],
		"temperature":0.5,"top_p":0.9,"top_k":40,"stop_sequences":["x"],"metadata":{"user_id":"u"},
		"context_management":{"edits":[]},
		"output_config":{"effort":"high"},
		"service_tier":"auto",
		"some_field_from_a_future_release":{"a":1}
	}`
	out, _ := mustRewrite(t, body, rewriteOptions{Model: "m", Sanitize: true})
	got := decode(t, out)
	for _, k := range []string{"model", "max_tokens", "stream", "system", "messages", "temperature", "top_p", "top_k", "stop_sequences", "metadata"} {
		if _, ok := got[k]; !ok {
			t.Errorf("%s was dropped", k)
		}
	}
	for _, k := range []string{"context_management", "output_config", "service_tier", "some_field_from_a_future_release"} {
		if _, ok := got[k]; ok {
			t.Errorf("%s was kept", k)
		}
	}
}

func TestSanitizeOffKeepsEveryField(t *testing.T) {
	const body = `{"model":"m","context_management":{"edits":[]},"output_config":{"effort":"high"},"thinking":{"type":"adaptive"},"tools":[{"type":"web_search_20250305","name":"web_search"},{"name":"t","strict":true,"input_schema":{}}],"messages":[]}`
	out, dropped := mustRewrite(t, body, rewriteOptions{Model: "m"})
	if string(out) != body {
		t.Errorf("with sanitize off the body must not change\n got %s\nwant %s", out, body)
	}
	if len(dropped) != 0 {
		t.Errorf("dropped = %v, want none", dropped)
	}
}

func TestSanitizeThinking(t *testing.T) {
	out, _ := mustRewrite(t, `{"model":"m","thinking":{"type":"adaptive"},"messages":[]}`, rewriteOptions{Sanitize: true})
	if _, ok := decode(t, out)["thinking"]; ok {
		t.Error("adaptive thinking was kept")
	}
	out, _ = mustRewrite(t, `{"model":"m","thinking":{"type":"enabled","budget_tokens":2048},"messages":[]}`, rewriteOptions{Sanitize: true})
	th, ok := decode(t, out)["thinking"].(map[string]any)
	if !ok || th["type"] != "enabled" || th["budget_tokens"] != float64(2048) {
		t.Errorf("thinking with a budget must be kept, got %v", decode(t, out)["thinking"])
	}
}

func TestSanitizeTools(t *testing.T) {
	const body = `{"model":"m","messages":[],"tools":[
		{"name":"Read","description":"read a file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}},"strict":true,"defer_loading":true,"cache_control":{"type":"ephemeral"}},
		{"type":"custom","name":"Grep","description":"search","input_schema":{"type":"object"},"eager_input_streaming":true},
		{"type":"web_search_20250305","name":"web_search","max_uses":5},
		{"type":"bash_20250124","name":"bash"},
		{"type":"code_execution_20250825"}
	]}`
	out, dropped := mustRewrite(t, body, rewriteOptions{Sanitize: true})
	if want := []string{"web_search", "bash", "code_execution_20250825"}; !reflect.DeepEqual(dropped, want) {
		t.Errorf("dropped = %v, want %v", dropped, want)
	}
	tools, _ := decode(t, out)["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("kept %d tools, want 2: %s", len(tools), out)
	}
	read := tools[0].(map[string]any)
	for _, k := range []string{"strict", "defer_loading"} {
		if _, ok := read[k]; ok {
			t.Errorf("%s was kept on a tool definition", k)
		}
	}
	for _, k := range []string{"name", "description", "input_schema", "cache_control"} {
		if _, ok := read[k]; !ok {
			t.Errorf("%s was dropped from a tool definition", k)
		}
	}
	grep := tools[1].(map[string]any)
	if grep["type"] != "custom" || grep["name"] != "Grep" {
		t.Errorf("custom tool = %v", grep)
	}
	if _, ok := grep["eager_input_streaming"]; ok {
		t.Error("an unknown tool field was kept")
	}
}

func TestSanitizeToolChoice(t *testing.T) {
	// The chosen tool is a server tool that gets dropped.
	out, _ := mustRewrite(t, `{"model":"m","messages":[],"tools":[{"name":"Read","input_schema":{}},{"type":"web_search_20250305","name":"web_search"}],"tool_choice":{"type":"tool","name":"web_search"}}`, rewriteOptions{Sanitize: true})
	if _, ok := decode(t, out)["tool_choice"]; ok {
		t.Error("tool_choice naming a dropped tool was kept")
	}
	// The chosen tool survives.
	out, _ = mustRewrite(t, `{"model":"m","messages":[],"tools":[{"name":"Read","input_schema":{}}],"tool_choice":{"type":"tool","name":"Read"}}`, rewriteOptions{Sanitize: true})
	if _, ok := decode(t, out)["tool_choice"]; !ok {
		t.Error("tool_choice naming a kept tool was dropped")
	}
	// "auto" does not name a tool.
	out, _ = mustRewrite(t, `{"model":"m","messages":[],"tools":[{"name":"Read","input_schema":{}}],"tool_choice":{"type":"auto"}}`, rewriteOptions{Sanitize: true})
	if _, ok := decode(t, out)["tool_choice"]; !ok {
		t.Error("tool_choice auto was dropped")
	}
	// Every tool is dropped: tools and tool_choice go with them.
	out, _ = mustRewrite(t, `{"model":"m","messages":[],"tools":[{"type":"web_search_20250305","name":"web_search"}],"tool_choice":{"type":"auto"}}`, rewriteOptions{Sanitize: true})
	got := decode(t, out)
	if _, ok := got["tools"]; ok {
		t.Error("an empty tool list was kept")
	}
	if _, ok := got["tool_choice"]; ok {
		t.Error("tool_choice was kept without tools")
	}
}

// What the router does not change must reach the backend byte for byte and
// in the same order, or the backend's prefix cache misses on every turn.
func TestRewriteKeepsUntouchedBytesAndOrder(t *testing.T) {
	const schema = `{"type":"object",  "required":["path"],"properties":{"path":{"type":"string","description":"<abs> & more"}}}`
	const system = `[{"type":"text","text":"You are   a coder.","cache_control":{"type":"ephemeral"}}]`
	const messages = `[{"role":"user","content":"z before a"},{"role":"assistant","content":[{"type":"text","text":"ok"}]}]`
	body := `{"stream":true,"model":"m","system":` + system + `,"messages":` + messages + `,"output_config":{},"tools":[{"name":"Read","input_schema":` + schema + `,"strict":true}],"max_tokens":32000}`
	out, _ := mustRewrite(t, body, rewriteOptions{Model: "served", Sanitize: true})
	want := `{"stream":true,"model":"served","system":` + system + `,"messages":` + messages + `,"tools":[{"name":"Read","input_schema":` + schema + `}],"max_tokens":32000}`
	if string(out) != want {
		t.Errorf("rewrite changed more than it had to\n got %s\nwant %s", out, want)
	}
}

func roles(t *testing.T, body []byte) []string {
	t.Helper()
	msgs, _ := decode(t, body)["messages"].([]any)
	var rs []string
	for _, m := range msgs {
		rs = append(rs, m.(map[string]any)["role"].(string))
	}
	return rs
}

func TestSystemMessagesAreFoldedIntoUserTurns(t *testing.T) {
	tests := []struct {
		name     string
		messages string
		roles    []string
		// text of each resulting message, blocks joined by " | "
		text []string
	}{
		{
			"after a user turn: appended to it",
			`[{"role":"user","content":"hi"},{"role":"system","content":"be brief"},{"role":"assistant","content":"ok"}]`,
			[]string{"user", "assistant"},
			[]string{"hi | <system-reminder>\nbe brief\n</system-reminder>", "ok"},
		},
		{
			"before a user turn: prepended to it",
			`[{"role":"user","content":"hi"},{"role":"assistant","content":"ok"},{"role":"system","content":"be brief"},{"role":"user","content":[{"type":"text","text":"next"}]}]`,
			[]string{"user", "assistant", "user"},
			[]string{"hi", "ok", "<system-reminder>\nbe brief\n</system-reminder> | next"},
		},
		{
			"between assistant turns: its own user turn",
			`[{"role":"user","content":"hi"},{"role":"assistant","content":"a"},{"role":"system","content":"note"},{"role":"assistant","content":"b"}]`,
			[]string{"user", "assistant", "user", "assistant"},
			[]string{"hi", "a", "<system-reminder>\nnote\n</system-reminder>", "b"},
		},
		{
			"first message: prepended to the first user turn",
			`[{"role":"system","content":"note"},{"role":"user","content":"hi"}]`,
			[]string{"user"},
			[]string{"<system-reminder>\nnote\n</system-reminder> | hi"},
		},
		{
			"last message after an assistant turn: its own user turn",
			`[{"role":"user","content":"hi"},{"role":"assistant","content":"a"},{"role":"system","content":"note"}]`,
			[]string{"user", "assistant", "user"},
			[]string{"hi", "a", "<system-reminder>\nnote\n</system-reminder>"},
		},
		{
			"two in a row and block content",
			`[{"role":"user","content":"hi"},{"role":"system","content":[{"type":"text","text":"one"},{"type":"text","text":"two"}]},{"role":"system","content":"three"},{"role":"assistant","content":"a"}]`,
			[]string{"user", "assistant"},
			[]string{"hi | <system-reminder>\none\n</system-reminder> | <system-reminder>\ntwo\n</system-reminder> | <system-reminder>\nthree\n</system-reminder>", "a"},
		},
		{
			"empty system message: dropped",
			`[{"role":"user","content":"hi"},{"role":"system","content":""},{"role":"assistant","content":"a"}]`,
			[]string{"user", "assistant"},
			[]string{"hi", "a"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, _ := mustRewrite(t, `{"model":"m","messages":`+tt.messages+`}`, rewriteOptions{})
			if got := roles(t, out); !reflect.DeepEqual(got, tt.roles) {
				t.Fatalf("roles = %v, want %v\n%s", got, tt.roles, out)
			}
			msgs := decode(t, out)["messages"].([]any)
			for i, m := range msgs {
				if got := messageText(m); got != tt.text[i] {
					t.Errorf("message %d = %q, want %q", i, got, tt.text[i])
				}
			}
			if strings.Contains(string(out), "\\u003c") {
				t.Errorf("the reminder tag must stay readable on the wire: %s", out)
			}
		})
	}
}

// messageText flattens a decoded message's content for comparison.
func messageText(m any) string {
	switch c := m.(map[string]any)["content"].(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, b := range c {
			parts = append(parts, b.(map[string]any)["text"].(string))
		}
		return strings.Join(parts, " | ")
	}
	return ""
}

func TestMessagesWithoutSystemEntriesAreNotTouched(t *testing.T) {
	const messages = `[ {"role":"user","content":"hi"} , {"role":"assistant","content":"ok"} ]`
	out, _ := mustRewrite(t, `{"model":"m","messages":`+messages+`}`, rewriteOptions{})
	if want := `{"model":"m","messages":` + messages + `}`; string(out) != want {
		t.Errorf("got %s\nwant %s", out, want)
	}
}

// A system entry in the conversation must not move into the top-level
// system prompt: that would change the prompt prefix on every turn.
func TestTopLevelSystemIsIdenticalAcrossTurns(t *testing.T) {
	const system = `[{"type":"text","text":"You are a coder."}]`
	turn1 := `{"model":"m","system":` + system + `,"messages":[{"role":"user","content":"one"},{"role":"system","content":"reminder A"}]}`
	turn2 := `{"model":"m","system":` + system + `,"messages":[{"role":"user","content":"one"},{"role":"system","content":"reminder A"},{"role":"assistant","content":"done"},{"role":"system","content":"reminder B"},{"role":"user","content":"two"}]}`
	out1, _ := mustRewrite(t, turn1, rewriteOptions{Sanitize: true})
	out2, _ := mustRewrite(t, turn2, rewriteOptions{Sanitize: true})

	sys := func(body []byte) string {
		obj, err := parseObject(body)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := obj.get("system")
		return string(raw)
	}
	if sys(out1) != system || sys(out2) != system {
		t.Errorf("top-level system changed:\n turn 1 %s\n turn 2 %s\n want   %s", sys(out1), sys(out2), system)
	}
	// The first turn of the second request is rendered exactly as it was in
	// the first request, so the backend can reuse its cached prefix.
	first := func(body []byte) string {
		obj, _ := parseObject(body)
		raw, _ := obj.get("messages")
		items, err := parseArray(raw)
		if err != nil {
			t.Fatal(err)
		}
		return string(items[0])
	}
	if first(out1) != first(out2) {
		t.Errorf("an earlier turn changed between requests:\n turn 1 %s\n turn 2 %s", first(out1), first(out2))
	}
}

const cachedBody = `{"model":"m","cache_control":{"type":"ephemeral"},
	"system":[{"type":"text","text":"s","cache_control":{"type":"ephemeral"}}],
	"tools":[{"name":"Read","input_schema":{"type":"object","properties":{"cache_control":{"type":"string"}}},"cache_control":{"type":"ephemeral"}}],
	"messages":[
		{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"t1","name":"Read","input":{"cache_control":"an argument, not a marker"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"t1","content":[{"type":"text","text":"out","cache_control":{"type":"ephemeral"}}],"cache_control":{"type":"ephemeral"}}]},
		{"role":"user","content":"plain"}
	]}`

func TestCacheControlIsKeptByDefault(t *testing.T) {
	out, _ := mustRewrite(t, cachedBody, rewriteOptions{})
	if got := strings.Count(string(out), `"cache_control":{"type":"ephemeral"}`); got != 6 {
		t.Errorf("kept %d cache_control markers, want all 6", got)
	}
}

func TestCacheControlIsStrippedWhenTheBackendRejectsIt(t *testing.T) {
	out, _ := mustRewrite(t, cachedBody, rewriteOptions{StripCacheControl: true})
	if strings.Contains(string(out), `"cache_control":{"type":"ephemeral"}`) {
		t.Errorf("a cache_control marker survived: %s", out)
	}
	// A schema property and a tool argument with that name are data, not
	// markers, and must stay.
	if !strings.Contains(string(out), `"properties":{"cache_control":{"type":"string"}}`) {
		t.Errorf("a schema property named cache_control was removed: %s", out)
	}
	if !strings.Contains(string(out), `"input":{"cache_control":"an argument, not a marker"}`) {
		t.Errorf("a tool argument named cache_control was removed: %s", out)
	}
	got := decode(t, out)
	if len(got["messages"].([]any)) != 4 || got["messages"].([]any)[3].(map[string]any)["content"] != "plain" {
		t.Errorf("messages were damaged: %s", out)
	}
}

func TestRewriteRejectsABodyThatIsNotAnObject(t *testing.T) {
	for _, body := range []string{``, `[]`, `"text"`, `{"model":`, `{"model":"m"} trailing`} {
		if _, _, err := rewrite([]byte(body), rewriteOptions{}); err == nil {
			t.Errorf("rewrite(%q) succeeded", body)
		}
	}
}

func TestSanitizeLeavesAMalformedToolListAlone(t *testing.T) {
	out, dropped := mustRewrite(t, `{"model":"m","tools":"not a list","messages":[]}`, rewriteOptions{Sanitize: true})
	if decode(t, out)["tools"] != "not a list" || len(dropped) != 0 {
		t.Errorf("got %s, dropped %v", out, dropped)
	}
	if n := len(decode(t, out)); n != 3 {
		t.Errorf("%d fields, want model, tools and messages: %s", n, out)
	}
}
