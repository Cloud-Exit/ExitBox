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

package subagents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// tomlDoc is the top level of a TOML document.
type tomlDoc struct {
	keys    []string          // top-level keys and tables, first segment only, in file order
	strings map[string]string // top-level string values
}

// readTOML reads the top level of a TOML document, which is all a Codex
// agent file needs checked. It decodes string values and skips over every
// other value, so it accepts some documents a full TOML parser rejects.
func readTOML(data []byte) (*tomlDoc, error) {
	s := &tomlScanner{src: strings.ReplaceAll(strings.TrimPrefix(string(data), "\uFEFF"), "\r\n", "\n")}
	doc := &tomlDoc{strings: map[string]string{}}
	seen := map[string]bool{}
	add := func(key string) {
		if !seen[key] {
			seen[key] = true
			doc.keys = append(doc.keys, key)
		}
	}
	inTable := false
	for {
		s.skipBlank()
		if s.eof() {
			return doc, nil
		}
		if s.src[s.i] == '[' {
			name, err := s.tableHeader()
			if err != nil {
				return nil, err
			}
			add(name)
			inTable = true
			continue
		}
		key, err := s.key()
		if err != nil {
			return nil, err
		}
		s.skipSpaces()
		if s.eof() || s.src[s.i] != '=' {
			return nil, s.errorf("expected = after key %q", strings.Join(key, "."))
		}
		s.i++
		s.skipSpaces()
		value, isString, err := s.value()
		if err != nil {
			return nil, err
		}
		if err := s.endOfLine(); err != nil {
			return nil, err
		}
		if !inTable {
			add(key[0])
			if isString && len(key) == 1 {
				doc.strings[key[0]] = value
			}
		}
	}
}

type tomlScanner struct {
	src string
	i   int
}

func (s *tomlScanner) eof() bool { return s.i >= len(s.src) }

func (s *tomlScanner) errorf(format string, args ...any) error {
	line := 1 + strings.Count(s.src[:min(s.i, len(s.src))], "\n")
	return fmt.Errorf("line %d: %s", line, fmt.Sprintf(format, args...))
}

func (s *tomlScanner) skipSpaces() {
	for !s.eof() && (s.src[s.i] == ' ' || s.src[s.i] == '\t') {
		s.i++
	}
}

func (s *tomlScanner) skipComment() {
	for !s.eof() && s.src[s.i] != '\n' {
		s.i++
	}
}

// skipBlank skips whitespace, newlines and comments.
func (s *tomlScanner) skipBlank() {
	for !s.eof() {
		switch s.src[s.i] {
		case ' ', '\t', '\n':
			s.i++
		case '#':
			s.skipComment()
		default:
			return
		}
	}
}

// endOfLine requires that nothing but a comment follows a value.
func (s *tomlScanner) endOfLine() error {
	s.skipSpaces()
	if !s.eof() && s.src[s.i] == '#' {
		s.skipComment()
	}
	if !s.eof() && s.src[s.i] != '\n' {
		return s.errorf("unexpected text after a value")
	}
	return nil
}

// tableHeader reads [name] or [[name]] and returns the first key segment.
func (s *tomlScanner) tableHeader() (string, error) {
	for !s.eof() && s.src[s.i] == '[' {
		s.i++
	}
	s.skipSpaces()
	key, err := s.key()
	if err != nil {
		return "", err
	}
	s.skipSpaces()
	if s.eof() || s.src[s.i] != ']' {
		return "", s.errorf("table header is not closed")
	}
	for !s.eof() && s.src[s.i] == ']' {
		s.i++
	}
	return key[0], s.endOfLine()
}

// key reads a bare, quoted or dotted key and returns its segments.
func (s *tomlScanner) key() ([]string, error) {
	var segs []string
	for {
		s.skipSpaces()
		if s.eof() {
			return nil, s.errorf("expected a key")
		}
		var seg string
		if c := s.src[s.i]; c == '"' || c == '\'' {
			v, err := s.quoted()
			if err != nil {
				return nil, err
			}
			seg = v
		} else {
			start := s.i
			for !s.eof() && isBareKeyChar(s.src[s.i]) {
				s.i++
			}
			if s.i == start {
				return nil, s.errorf("expected key = value")
			}
			seg = s.src[start:s.i]
		}
		segs = append(segs, seg)
		s.skipSpaces()
		if s.eof() || s.src[s.i] != '.' {
			return segs, nil
		}
		s.i++
	}
}

func isBareKeyChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-'
}

// value reads one value. Only strings are decoded.
func (s *tomlScanner) value() (string, bool, error) {
	if s.eof() {
		return "", false, s.errorf("expected a value")
	}
	switch c := s.src[s.i]; {
	case c == '"' || c == '\'':
		v, err := s.quoted()
		return v, true, err
	case c == '[' || c == '{':
		return "", false, s.skipCompound()
	}
	start := s.i
	for !s.eof() && s.src[s.i] != '\n' && s.src[s.i] != '#' {
		s.i++
	}
	if strings.TrimSpace(s.src[start:s.i]) == "" {
		return "", false, s.errorf("expected a value")
	}
	return "", false, nil
}

// quoted reads a basic, literal or multi-line string starting at a quote.
func (s *tomlScanner) quoted() (string, error) {
	q := s.src[s.i]
	if triple := strings.Repeat(string(q), 3); strings.HasPrefix(s.src[s.i:], triple) {
		start := s.i + 3
		for j := start; j < len(s.src); j++ {
			if q == '"' && s.src[j] == '\\' {
				j++
				continue
			}
			if strings.HasPrefix(s.src[j:], triple) {
				s.i = j + 3
				// Up to two more quotes belong to the string itself.
				for n := 0; n < 2 && !s.eof() && s.src[s.i] == q; n++ {
					s.i++
				}
				return strings.TrimPrefix(s.src[start:j], "\n"), nil
			}
		}
		return "", s.errorf("multi-line string is not closed")
	}
	start := s.i + 1
	for j := start; j < len(s.src) && s.src[j] != '\n'; j++ {
		if q == '"' && s.src[j] == '\\' {
			j++
			continue
		}
		if s.src[j] == q {
			raw := s.src[start:j]
			s.i = j + 1
			if q == '"' {
				if v, err := strconv.Unquote(`"` + raw + `"`); err == nil {
					return v, nil
				}
			}
			return raw, nil
		}
	}
	return "", s.errorf("string is not closed")
}

// skipCompound skips an array or inline table, which may span lines.
func (s *tomlScanner) skipCompound() error {
	depth := 0
	for !s.eof() {
		switch c := s.src[s.i]; c {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				s.i++
				return nil
			}
		case '"', '\'':
			if _, err := s.quoted(); err != nil {
				return err
			}
			continue
		case '#':
			s.skipComment()
			continue
		}
		s.i++
	}
	return s.errorf("array or inline table is not closed")
}

// tomlQuote renders a TOML basic string. JSON string escapes are valid TOML.
func tomlQuote(v string) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return strconv.Quote(v)
	}
	return strings.TrimSuffix(b.String(), "\n")
}
