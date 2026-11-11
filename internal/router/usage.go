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
	"net/http"
	"strings"
)

// maxUsageBuffer bounds what the usage meter holds of one line or body.
const maxUsageBuffer = 1 << 20

// usageMeter reads token counts out of a response while it is relayed. It
// only ever sees a copy of the bytes: nothing it does can change or delay
// what the client receives, and nothing it reads is logged except the two
// numbers.
type usageMeter struct {
	sse     bool
	off     bool // unreadable (compressed) or over the buffer limit
	buf     []byte
	in, out int
	seen    bool
}

// newUsageMeter picks how to read a response from its headers. A compressed
// response is not read at all.
func newUsageMeter(h http.Header) *usageMeter {
	m := &usageMeter{}
	if enc := h.Get("Content-Encoding"); enc != "" && !strings.EqualFold(enc, "identity") {
		m.off = true
	}
	ct := strings.ToLower(h.Get("Content-Type"))
	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		m.sse = true
	case strings.HasPrefix(ct, "application/json"):
	default:
		m.off = true
	}
	return m
}

// Write takes the next chunk of the response body.
func (m *usageMeter) Write(p []byte) {
	if m.off {
		return
	}
	m.buf = append(m.buf, p...)
	if !m.sse {
		if len(m.buf) > maxUsageBuffer {
			m.off, m.buf = true, nil
		}
		return
	}
	for {
		i := bytes.IndexByte(m.buf, '\n')
		if i < 0 {
			break
		}
		m.line(m.buf[:i])
		m.buf = m.buf[i+1:]
	}
	if len(m.buf) > maxUsageBuffer {
		// One event larger than the limit: give up on this line only.
		m.buf = nil
	}
}

// line reads one server-sent event line. Only data lines that mention usage
// are parsed.
func (m *usageMeter) line(l []byte) {
	l = bytes.TrimRight(l, "\r")
	if !bytes.HasPrefix(l, []byte("data:")) || !bytes.Contains(l, []byte(`"usage"`)) {
		return
	}
	var ev struct {
		Message struct {
			Usage *usage `json:"usage"`
		} `json:"message"`
		Usage *usage `json:"usage"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(l[len("data:"):]), &ev); err != nil {
		return
	}
	m.add(ev.Message.Usage)
	m.add(ev.Usage)
}

type usage struct {
	InputTokens  *int `json:"input_tokens"`
	OutputTokens *int `json:"output_tokens"`
}

// add records the counts of one usage object. Later events win: the final
// message_delta carries the total output tokens.
func (m *usageMeter) add(u *usage) {
	if u == nil {
		return
	}
	if u.InputTokens != nil {
		m.in, m.seen = *u.InputTokens, true
	}
	if u.OutputTokens != nil {
		m.out, m.seen = *u.OutputTokens, true
	}
}

// Result returns the token counts, and false when the response had none.
func (m *usageMeter) Result() (in, out int, ok bool) {
	if !m.off && !m.sse && len(m.buf) > 0 {
		var body struct {
			Usage       *usage `json:"usage"`
			InputTokens *int   `json:"input_tokens"` // count_tokens response
		}
		if err := json.Unmarshal(m.buf, &body); err == nil {
			m.add(body.Usage)
			if body.InputTokens != nil {
				m.in, m.seen = *body.InputTokens, true
			}
		}
		m.buf = nil
	}
	return m.in, m.out, m.seen
}
