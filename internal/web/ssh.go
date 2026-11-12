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
	"bufio"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// SSHOptions are the options of an SSH session. The system OpenSSH client
// does the rest (~/.ssh/config, the agent, known_hosts, ProxyJump), and it
// asks for passwords and passphrases in the terminal: exitbox never sees them.
type SSHOptions struct {
	Name     string `json:"name"`
	Host     string `json:"host"` // a hostname, an IP or a ~/.ssh/config alias
	User     string `json:"user"`
	Port     int    `json:"port"`     // 0 is ssh's default
	Identity string `json:"identity"` // a private key file; never read here
}

var (
	// Tight on purpose: nothing here reaches ssh as an option.
	sshHostRe = regexp.MustCompile(`^[A-Za-z0-9._:%\[\]-]+$`)
	sshUserRe = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

func validSSHHost(h string) bool {
	return h != "" && len(h) <= 255 && !strings.HasPrefix(h, "-") && sshHostRe.MatchString(h)
}

func validSSHUser(u string) bool {
	return u != "" && len(u) <= 64 && !strings.HasPrefix(u, "-") && sshUserRe.MatchString(u)
}

// cleaned validates the options and returns them as they run: trimmed,
// the identity file an absolute path, the name defaulted to user@host.
func (o SSHOptions) cleaned(home string) (SSHOptions, error) {
	o.Host = strings.TrimSpace(o.Host)
	o.User = strings.TrimSpace(o.User)
	o.Name = strings.TrimSpace(o.Name)
	o.Identity = strings.TrimSpace(o.Identity)
	if !validSSHHost(o.Host) {
		return SSHOptions{}, fmt.Errorf("invalid host %q: use a hostname, an IP address or a ~/.ssh/config alias", o.Host)
	}
	if o.User != "" && !validSSHUser(o.User) {
		return SSHOptions{}, fmt.Errorf("invalid user %q", o.User)
	}
	if o.Port < 0 || o.Port > 65535 {
		return SSHOptions{}, fmt.Errorf("invalid port %d (1 to 65535)", o.Port)
	}
	if o.Identity != "" {
		p, err := identityPath(o.Identity, home)
		if err != nil {
			return SSHOptions{}, err
		}
		o.Identity = p
	}
	if o.Name == "" {
		o.Name = o.Host
		if o.User != "" {
			o.Name = o.User + "@" + o.Host
		}
	}
	if !validName(o.Name) {
		return SSHOptions{}, fmt.Errorf("invalid session name %q", o.Name)
	}
	return o, nil
}

// identityPath expands ~ and checks that the key file exists, without
// opening it.
func identityPath(p, home string) (string, error) {
	switch {
	case p == "~":
		p = home
	case strings.HasPrefix(p, "~/"), strings.HasPrefix(p, `~\`):
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) || strings.ContainsAny(p, "\x00\n\r") {
		return "", fmt.Errorf("identity file %q is not an absolute path", p)
	}
	p = filepath.Clean(p)
	st, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("identity file %s does not exist", p)
		}
		return "", fmt.Errorf("identity file: %w", err)
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("identity file %s is not a regular file", p)
	}
	return p, nil
}

// sshArgs are the ssh arguments of cleaned options. The host comes after
// "--", so it is never read as an option.
func sshArgs(o SSHOptions) []string {
	args := []string{"-t"}
	if o.Port > 0 {
		args = append(args, "-p", strconv.Itoa(o.Port))
	}
	if o.User != "" {
		args = append(args, "-l", o.User)
	}
	if o.Identity != "" {
		args = append(args, "-i", o.Identity)
	}
	return append(args, "--", o.Host)
}

// sshSession checks options and returns the SSH session they start.
func (s *Server) sshSession(o SSHOptions) (SessionInfo, error) {
	home := s.store.home()
	clean, err := o.cleaned(home)
	if err != nil {
		return SessionInfo{}, err
	}
	if _, err := s.store.sshPath(); err != nil {
		return SessionInfo{}, errors.New("no ssh client found on the machine running exitbox web; install OpenSSH")
	}
	return SessionInfo{Kind: KindSSH, Dir: home, Name: clean.Name, Args: sshArgs(clean), SSH: &clean}, nil
}

// --- ~/.ssh/config hosts ---

// sshHostDoc is a concrete Host alias of ~/.ssh/config, with what the
// config says about it. Best effort: Match blocks are not evaluated.
type sshHostDoc struct {
	Alias    string `json:"alias"`
	HostName string `json:"hostname,omitempty"`
	User     string `json:"user,omitempty"`
	Port     int    `json:"port,omitempty"`
}

const (
	maxSSHIncludeDepth = 8
	maxSSHConfigFiles  = 64
	maxSSHHosts        = 500
	maxSSHConfigBytes  = 1 << 20
)

type sshBlock struct {
	patterns []string // nil: lines before the first Host apply to every host
	match    bool     // a Match block, not evaluated
	opts     map[string]string
}

type sshConfigParser struct {
	home   string
	files  int
	blocks []*sshBlock
}

// sshConfigHosts lists the concrete aliases of home/.ssh/config, following
// Include. Unreadable files are skipped.
func sshConfigHosts(home string) []sshHostDoc {
	p := &sshConfigParser{home: home, blocks: []*sshBlock{{opts: map[string]string{}}}}
	p.parseFile(filepath.Join(home, ".ssh", "config"), 0)
	seen := map[string]bool{}
	out := []sshHostDoc{}
	for _, b := range p.blocks {
		for _, pat := range b.patterns {
			if strings.ContainsAny(pat, "*?!") || !validSSHHost(pat) || seen[strings.ToLower(pat)] {
				continue
			}
			seen[strings.ToLower(pat)] = true
			out = append(out, p.resolve(pat))
			if len(out) >= maxSSHHosts {
				return out
			}
		}
	}
	return out
}

func (p *sshConfigParser) parseFile(path string, depth int) {
	if depth > maxSSHIncludeDepth || p.files >= maxSSHConfigFiles {
		return
	}
	p.files++
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	if st, err := f.Stat(); err != nil || !st.Mode().IsRegular() || st.Size() > maxSSHConfigBytes {
		return
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), 64<<10)
	for sc.Scan() {
		key, args := sshConfigLine(sc.Text())
		if key == "" {
			continue
		}
		switch key {
		case "host":
			p.blocks = append(p.blocks, &sshBlock{patterns: args, opts: map[string]string{}})
		case "match":
			p.blocks = append(p.blocks, &sshBlock{match: true, opts: map[string]string{}})
		case "include":
			for _, a := range args {
				p.include(a, depth)
			}
		case "hostname", "user", "port":
			cur := p.blocks[len(p.blocks)-1]
			if _, ok := cur.opts[key]; !ok && len(args) > 0 {
				cur.opts[key] = args[0]
			}
		}
	}
	// A line too long ends this file only.
}

func (p *sshConfigParser) include(pattern string, depth int) {
	switch {
	case strings.HasPrefix(pattern, "~/"):
		pattern = filepath.Join(p.home, pattern[2:])
	case !filepath.IsAbs(pattern):
		pattern = filepath.Join(p.home, ".ssh", pattern)
	}
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return
	}
	sort.Strings(paths)
	for _, path := range paths {
		p.parseFile(path, depth+1)
	}
}

// resolve applies the blocks that match alias in order; the first value
// of each option wins, as in ssh.
func (p *sshConfigParser) resolve(alias string) sshHostDoc {
	d := sshHostDoc{Alias: alias}
	got := map[string]string{}
	for _, b := range p.blocks {
		if b.match || (b.patterns != nil && !sshHostMatches(b.patterns, alias)) {
			continue
		}
		for k, v := range b.opts {
			if _, ok := got[k]; !ok {
				got[k] = v
			}
		}
	}
	if h := got["hostname"]; h != "" {
		d.HostName = strings.ReplaceAll(h, "%h", alias)
	}
	d.User = got["user"]
	if n, err := strconv.Atoi(got["port"]); err == nil && n > 0 && n <= 65535 {
		d.Port = n
	}
	return d
}

// sshConfigLine splits a config line into its lowercased keyword and its
// arguments ("Key value", "Key=value", quoted arguments).
func sshConfigLine(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}
	end := strings.IndexAny(line, " \t=")
	if end < 0 {
		return strings.ToLower(line), nil
	}
	key := strings.ToLower(line[:end])
	rest := strings.TrimLeft(line[end:], " \t")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), " \t")
	var args []string
	for rest != "" {
		var a string
		if rest[0] == '"' {
			i := strings.IndexByte(rest[1:], '"')
			if i < 0 {
				return key, args
			}
			a, rest = rest[1:i+1], rest[i+2:]
		} else {
			i := strings.IndexAny(rest, " \t")
			if i < 0 {
				i = len(rest)
			}
			a, rest = rest[:i], rest[i:]
		}
		if a != "" {
			args = append(args, a)
		}
		rest = strings.TrimLeft(rest, " \t")
	}
	return key, args
}

// sshHostMatches applies a Host line: any pattern matches and no negated
// one does.
func sshHostMatches(patterns []string, host string) bool {
	host = strings.ToLower(host)
	ok := false
	for _, pat := range patterns {
		neg := strings.HasPrefix(pat, "!")
		if !sshGlob(strings.ToLower(strings.TrimPrefix(pat, "!")), host) {
			continue
		}
		if neg {
			return false
		}
		ok = true
	}
	return ok
}

// sshGlob matches ssh's * and ? wildcards.
func sshGlob(pat, s string) bool {
	for pat != "" {
		switch pat[0] {
		case '*':
			for i := 0; i <= len(s); i++ {
				if sshGlob(pat[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if s == "" {
				return false
			}
		default:
			if s == "" || s[0] != pat[0] {
				return false
			}
		}
		pat, s = pat[1:], s[1:]
	}
	return s == ""
}

// handleSSHHosts lists the Host aliases of ~/.ssh/config for the New
// session dialog.
func (s *Server) handleSSHHosts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"hosts": sshConfigHosts(s.store.home())})
}
