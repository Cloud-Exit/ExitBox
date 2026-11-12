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
	"encoding/json"
	"fmt"
	"io"
)

// keptFields are the top-level Messages API fields a local backend is sent.
// Claude Code adds request fields every few releases; anything not listed
// here is dropped, so a new field cannot break a backend that rejects
// unknown input.
var keptFields = map[string]bool{
	"model":          true,
	"messages":       true,
	"system":         true,
	"max_tokens":     true,
	"stream":         true,
	"tools":          true,
	"tool_choice":    true,
	"stop_sequences": true,
	"temperature":    true,
	"top_p":          true,
	"top_k":          true,
	"metadata":       true,
	"thinking":       true, // kept unless adaptive, see sanitizeTop
}

// keptToolFields are the tool definition fields a local backend is sent.
var keptToolFields = map[string]bool{
	"name":          true,
	"description":   true,
	"input_schema":  true,
	"type":          true,
	"cache_control": true,
}

// member is one key of a JSON object. Objects are kept as ordered members
// with raw values, so everything the router does not change reaches the
// backend byte for byte and in the order Claude Code wrote it. That keeps
// the prompt prefix stable between turns, which the backend's prefix cache
// depends on.
type member struct {
	key string
	val json.RawMessage
}

type object []member

func parseObject(raw []byte) (object, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("not a JSON object")
	}
	var obj object
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("not a JSON object")
		}
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		obj = append(obj, member{key: key, val: val})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("data after the JSON object")
	}
	return obj, nil
}

func (o object) get(key string) (json.RawMessage, bool) {
	for _, m := range o {
		if m.key == key {
			return m.val, true
		}
	}
	return nil, false
}

// str returns the string value of a key, or "" when it is missing or not a
// string.
func (o object) str(key string) string {
	raw, ok := o.get(key)
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}

func (o object) set(key string, val json.RawMessage) object {
	for i := range o {
		if o[i].key == key {
			o[i].val = val
			return o
		}
	}
	return append(o, member{key: key, val: val})
}

func (o object) without(key string) object {
	out := o[:0:0]
	for _, m := range o {
		if m.key != key {
			out = append(out, m)
		}
	}
	return out
}

func (o object) encode() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(encodeString(m.key))
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// encodeString renders a JSON string without escaping <, > and &, so a
// <system-reminder> tag stays readable on the wire.
func encodeString(s string) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// A string always encodes.
	_ = enc.Encode(s)
	return bytes.TrimRight(b.Bytes(), "\n")
}

func parseArray(raw []byte) ([]json.RawMessage, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	return items, nil
}

func encodeArray(items []json.RawMessage) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('[')
	for i, it := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(it)
	}
	b.WriteByte(']')
	return b.Bytes()
}

// rewriteOptions selects what rewrite does to a request for a local backend.
type rewriteOptions struct {
	Model             string // model name the backend serves
	Sanitize          bool   // keep only fields a local backend understands
	StripCacheControl bool   // the backend rejected cache_control once
}

// rewrite turns a Messages API request from Claude Code into one a local
// backend accepts. It returns the names of the server tools it dropped.
func rewrite(body []byte, opts rewriteOptions) ([]byte, []string, error) {
	req, err := parseObject(body)
	if err != nil {
		return nil, nil, fmt.Errorf("request body is not a JSON object: %w", err)
	}
	if opts.Model != "" {
		if _, ok := req.get("model"); ok {
			req = req.set("model", encodeString(opts.Model))
		}
	}
	var dropped []string
	if opts.Sanitize {
		req, dropped = sanitizeTop(req)
	}
	if raw, ok := req.get("messages"); ok {
		if out, changed := foldSystemMessages(raw); changed {
			req = req.set("messages", out)
		}
	}
	if opts.StripCacheControl {
		req = stripCacheControl(req)
	}
	return req.encode(), dropped, nil
}

// sanitizeTop applies the top-level allowlist and cleans the tool list.
func sanitizeTop(req object) (object, []string) {
	out := req[:0:0]
	for _, m := range req {
		if !keptFields[m.key] {
			continue
		}
		if m.key == "thinking" && isAdaptiveThinking(m.val) {
			continue
		}
		out = append(out, m)
	}

	raw, ok := out.get("tools")
	if !ok {
		return out, nil
	}
	tools, err := parseArray(raw)
	if err != nil {
		// Not a list: leave it for the backend to reject.
		return out, nil
	}
	var kept []json.RawMessage
	var keptNames, dropped []string
	for _, t := range tools {
		tool, err := parseObject(t)
		if err != nil {
			kept = append(kept, t)
			continue
		}
		// A server tool runs on Anthropic's side; a local backend cannot
		// run it and most reject its definition.
		if typ := tool.str("type"); typ != "" && typ != "custom" {
			name := tool.str("name")
			if name == "" {
				name = typ
			}
			dropped = append(dropped, name)
			continue
		}
		clean := tool[:0:0]
		for _, m := range tool {
			if keptToolFields[m.key] {
				clean = append(clean, m)
			}
		}
		kept = append(kept, clean.encode())
		keptNames = append(keptNames, tool.str("name"))
	}
	if len(kept) == 0 {
		// tool_choice without tools is an error on every backend.
		return out.without("tools").without("tool_choice"), dropped
	}
	out = out.set("tools", encodeArray(kept))
	if choice, ok := out.get("tool_choice"); ok {
		if c, err := parseObject(choice); err == nil && c.str("type") == "tool" && !containsString(keptNames, c.str("name")) {
			out = out.without("tool_choice")
		}
	}
	return out, dropped
}

func isAdaptiveThinking(raw json.RawMessage) bool {
	t, err := parseObject(raw)
	return err == nil && t.str("type") == "adaptive"
}

// foldSystemMessages turns every "system" entry inside messages into a
// <system-reminder> text block in the user turn next to it, so the roles
// keep alternating. The top-level system prompt is left alone on purpose:
// appending there would change the prompt prefix on every turn and defeat
// the backend's prefix cache.
func foldSystemMessages(raw json.RawMessage) (json.RawMessage, bool) {
	items, err := parseArray(raw)
	if err != nil {
		return raw, false
	}
	msgs := make([]object, len(items))
	found := false
	for i, it := range items {
		m, err := parseObject(it)
		if err != nil {
			return raw, false
		}
		msgs[i] = m
		if m.str("role") == "system" {
			found = true
		}
	}
	if !found {
		return raw, false
	}

	var out []object
	var pending []json.RawMessage
	for _, m := range msgs {
		if m.str("role") == "system" {
			blocks := reminderBlocks(m)
			if len(blocks) == 0 {
				continue
			}
			if n := len(out); n > 0 && out[n-1].str("role") == "user" {
				out[n-1] = withBlocks(out[n-1], blocks, false)
			} else {
				pending = append(pending, blocks...)
			}
			continue
		}
		if len(pending) > 0 {
			if m.str("role") == "user" {
				m = withBlocks(m, pending, true)
			} else {
				out = append(out, userMessage(pending))
			}
			pending = nil
		}
		out = append(out, m)
	}
	if len(pending) > 0 {
		out = append(out, userMessage(pending))
	}

	encoded := make([]json.RawMessage, len(out))
	for i, m := range out {
		encoded[i] = m.encode()
	}
	return encodeArray(encoded), true
}

// reminderBlocks renders the content of a system message as content blocks:
// text is wrapped in <system-reminder> tags, anything else is kept as is.
func reminderBlocks(m object) []json.RawMessage {
	raw, ok := m.get("content")
	if !ok {
		return nil
	}
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		if text == "" {
			return nil
		}
		return []json.RawMessage{textBlock(reminder(text))}
	}
	items, err := parseArray(raw)
	if err != nil {
		return nil
	}
	var blocks []json.RawMessage
	for _, it := range items {
		b, err := parseObject(it)
		if err != nil || b.str("type") != "text" {
			blocks = append(blocks, it)
			continue
		}
		if t := b.str("text"); t != "" {
			blocks = append(blocks, b.set("text", encodeString(reminder(t))).encode())
		}
	}
	return blocks
}

func reminder(text string) string {
	return "<system-reminder>\n" + text + "\n</system-reminder>"
}

func textBlock(text string) json.RawMessage {
	return object{
		{key: "type", val: json.RawMessage(`"text"`)},
		{key: "text", val: encodeString(text)},
	}.encode()
}

func userMessage(blocks []json.RawMessage) object {
	return object{
		{key: "role", val: json.RawMessage(`"user"`)},
		{key: "content", val: encodeArray(blocks)},
	}
}

// withBlocks adds content blocks to a message, before or after what it has.
func withBlocks(m object, blocks []json.RawMessage, before bool) object {
	var own []json.RawMessage
	if raw, ok := m.get("content"); ok {
		var text string
		if err := json.Unmarshal(raw, &text); err == nil {
			if text != "" {
				own = []json.RawMessage{textBlock(text)}
			}
		} else if items, err := parseArray(raw); err == nil {
			own = items
		} else {
			own = []json.RawMessage{raw}
		}
	}
	var all []json.RawMessage
	if before {
		all = append(append(all, blocks...), own...)
	} else {
		all = append(append(all, own...), blocks...)
	}
	// Copy before changing: m may share its backing array with the caller.
	return append(object(nil), m...).set("content", encodeArray(all))
}

// stripCacheControl removes cache_control from every place the Messages API
// accepts it: the request itself, system blocks, tool definitions, and the
// content blocks of messages, including those nested in a tool result.
func stripCacheControl(req object) object {
	req = req.without("cache_control")
	for _, key := range []string{"system", "tools"} {
		if raw, ok := req.get(key); ok {
			req = req.set(key, stripFromBlocks(raw))
		}
	}
	if raw, ok := req.get("messages"); ok {
		if items, err := parseArray(raw); err == nil {
			for i, it := range items {
				m, err := parseObject(it)
				if err != nil {
					continue
				}
				if content, ok := m.get("content"); ok {
					items[i] = m.set("content", stripFromBlocks(content)).encode()
				}
			}
			req = req.set("messages", encodeArray(items))
		}
	}
	return req
}

// stripFromBlocks removes cache_control from a list of blocks. A value that
// is not a list of objects (a plain string, say) is returned unchanged.
func stripFromBlocks(raw json.RawMessage) json.RawMessage {
	items, err := parseArray(raw)
	if err != nil {
		return raw
	}
	for i, it := range items {
		b, err := parseObject(it)
		if err != nil {
			continue
		}
		b = b.without("cache_control")
		if content, ok := b.get("content"); ok {
			b = b.set("content", stripFromBlocks(content))
		}
		items[i] = b.encode()
	}
	return encodeArray(items)
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
