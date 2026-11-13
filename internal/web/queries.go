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

package web

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

// answerWindow is how long a terminal query waits for its answer.
const answerWindow = 30 * time.Second

// maxPendingSeq bounds an unfinished escape sequence carried to the next
// output chunk.
const maxPendingSeq = 256

// Output nobody answers must not grow the tracker: these bound the queries
// kept per kind, the kinds, and the length of a kind.
const (
	maxQueriesPerKind = 32
	maxQueryKinds     = 64
	maxQueryKindLen   = 32
)

// queryTracker passes one browser answer per terminal query. tmux asks the
// terminal for device attributes, its version and mode 2026 when a client
// starts and takes the first answer; every further answer reaches the agent
// as typed text. Each browser on a session answers, so the rest are dropped.
type queryTracker struct {
	mu      sync.Mutex
	pending map[string][]time.Time
	tail    []byte
	now     func() time.Time
}

func (q *queryTracker) clock() time.Time {
	if q.now != nil {
		return q.now()
	}
	return time.Now()
}

// sawOutput records the queries in a chunk of live output.
func (q *queryTracker) sawOutput(p []byte) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.tail) > 0 {
		p = append(q.tail, p...)
		q.tail = nil
	}
	now := q.clock()
	q.pruneLocked(now)
	for i := 0; i < len(p); {
		j := bytes.IndexByte(p[i:], 0x1b)
		if j < 0 {
			return
		}
		i += j
		seq, n, done := escape(p[i:])
		if !done {
			if len(p)-i <= maxPendingSeq {
				q.tail = append([]byte(nil), p[i:]...)
			}
			return
		}
		if k := queryKind(seq); k != "" {
			q.addLocked(k, now)
		}
		i += n
	}
}

// addLocked records a query of kind k within the caps; q.mu must be held.
func (q *queryTracker) addLocked(k string, now time.Time) {
	if len(k) > maxQueryKindLen {
		return
	}
	if q.pending == nil {
		q.pending = map[string][]time.Time{}
	}
	list, ok := q.pending[k]
	if !ok && len(q.pending) >= maxQueryKinds {
		return
	}
	if len(list) >= maxQueriesPerKind {
		list = list[1:]
	}
	q.pending[k] = append(list, now)
}

// pruneLocked drops the queries past answerWindow; q.mu must be held.
func (q *queryTracker) pruneLocked(now time.Time) {
	for k, list := range q.pending {
		for len(list) > 0 && now.Sub(list[0]) > answerWindow {
			list = list[1:]
		}
		if len(list) == 0 {
			delete(q.pending, k)
		} else {
			q.pending[k] = list
		}
	}
}

// filterInput drops answers nobody asked for from browser input.
func (q *queryTracker) filterInput(p []byte) []byte {
	if bytes.IndexByte(p, 0x1b) < 0 {
		return p
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	now := q.clock()
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); {
		j := bytes.IndexByte(p[i:], 0x1b)
		if j < 0 {
			out = append(out, p[i:]...)
			break
		}
		out = append(out, p[i:i+j]...)
		i += j
		seq, n, done := escape(p[i:])
		if !done {
			out = append(out, p[i:]...)
			break
		}
		if k := answerKind(seq); k != "" && !q.take(k, now) {
			i += n
			continue
		}
		out = append(out, p[i:i+n]...)
		i += n
	}
	return out
}

// take consumes the oldest unexpired query of kind k.
func (q *queryTracker) take(k string, now time.Time) bool {
	list := q.pending[k]
	for len(list) > 0 && now.Sub(list[0]) > answerWindow {
		list = list[1:]
	}
	if len(list) == 0 {
		delete(q.pending, k)
		return false
	}
	if len(list) == 1 {
		delete(q.pending, k)
	} else {
		q.pending[k] = list[1:]
	}
	return true
}

// sequence is one parsed escape sequence.
type sequence struct {
	intro  byte   // '[' CSI, ']' OSC, 'P' DCS, 0 anything else
	prefix byte   // CSI private marker: '?', '>', '=', '<' or 0
	params string // CSI parameters after the marker; OSC and DCS content
	inter  string // CSI intermediate bytes
	final  byte   // CSI final byte
}

// escape parses the escape sequence at the start of p (p[0] is ESC). It
// returns the sequence, its length, and false when p ends inside it.
func escape(p []byte) (sequence, int, bool) {
	if len(p) < 2 {
		return sequence{}, 0, false
	}
	switch p[1] {
	case '[':
		s := sequence{intro: '['}
		i := 2
		if i < len(p) && p[i] >= '<' && p[i] <= '?' {
			s.prefix = p[i]
			i++
		}
		start := i
		for i < len(p) && p[i] >= 0x30 && p[i] <= 0x3f {
			i++
		}
		s.params = string(p[start:i])
		start = i
		for i < len(p) && p[i] >= 0x20 && p[i] <= 0x2f {
			i++
		}
		s.inter = string(p[start:i])
		if i >= len(p) {
			return s, 0, false
		}
		if p[i] < 0x40 || p[i] > 0x7e {
			// Not a CSI sequence after all: leave the rest alone.
			return sequence{}, i, true
		}
		s.final = p[i]
		return s, i + 1, true
	case ']', 'P':
		// String sequences end with BEL (OSC only) or ST (ESC \).
		for i := 2; i < len(p); i++ {
			if p[i] == 0x07 && p[1] == ']' {
				return sequence{intro: p[1], params: string(p[2:i])}, i + 1, true
			}
			if p[i] == 0x1b {
				if i+1 >= len(p) {
					return sequence{}, 0, false
				}
				if p[i+1] == '\\' {
					return sequence{intro: p[1], params: string(p[2:i])}, i + 2, true
				}
			}
		}
		return sequence{}, 0, false
	}
	return sequence{}, 2, true
}

func isDefault(params string) bool { return params == "" || params == "0" }

// queryKind names the answer a query in the output expects, or "".
func queryKind(s sequence) string {
	switch s.intro {
	case '[':
		switch {
		case s.final == 'c' && s.inter == "" && isDefault(s.params):
			switch s.prefix {
			case 0:
				return "da1"
			case '>':
				return "da2"
			case '=':
				return "da3"
			}
		case s.final == 'q' && s.inter == "" && s.prefix == '>' && isDefault(s.params):
			return "xtversion"
		case s.final == 'p' && s.inter == "$" && (s.prefix == 0 || s.prefix == '?'):
			return "decrqm" + prefixText(s.prefix) + s.params
		}
	case ']':
		if n, rest, ok := oscNumber(s.params); ok && rest == "?" && (n == "10" || n == "11" || n == "12") {
			return "osc" + n
		}
	}
	return ""
}

// answerKind names the query an answer from the browser belongs to, or ""
// for anything else. Keys never produce these sequences.
func answerKind(s sequence) string {
	switch s.intro {
	case '[':
		switch {
		case s.final == 'c' && s.inter == "" && s.prefix == '?':
			return "da1"
		case s.final == 'c' && s.inter == "" && s.prefix == '>':
			return "da2"
		case s.final == 'y' && s.inter == "$" && (s.prefix == 0 || s.prefix == '?'):
			mode, _, _ := strings.Cut(s.params, ";")
			return "decrqm" + prefixText(s.prefix) + mode
		}
	case 'P':
		switch {
		case strings.HasPrefix(s.params, ">|"):
			return "xtversion"
		case strings.HasPrefix(s.params, "!|"):
			return "da3"
		}
	case ']':
		if n, _, ok := oscNumber(s.params); ok && (n == "10" || n == "11" || n == "12") {
			return "osc" + n
		}
	}
	return ""
}

func prefixText(b byte) string {
	if b == 0 {
		return ""
	}
	return string(b)
}

func oscNumber(content string) (string, string, bool) {
	n, rest, ok := strings.Cut(content, ";")
	return n, rest, ok && n != ""
}
