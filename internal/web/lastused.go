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
	"os"
	"sync"
	"time"
)

// lastUsedTTL keeps /api/state cheap with hundreds of saved sessions, polled every few seconds.
const lastUsedTTL = 30 * time.Second

// lastUsedCache remembers when each saved session dir was last used.
type lastUsedCache struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]lastUsedEntry
	// gen drops a result read before an invalidate, so it never lands after it.
	gen uint64
}

type lastUsedEntry struct {
	t  time.Time
	at time.Time
}

func newLastUsedCache() *lastUsedCache {
	return &lastUsedCache{now: time.Now, m: make(map[string]lastUsedEntry)}
}

// get returns the newest last use across dirs, zero when none is known.
func (c *lastUsedCache) get(dirs []string) time.Time {
	var newest time.Time
	for _, d := range dirs {
		if t := c.dir(d); t.After(newest) {
			newest = t
		}
	}
	return newest
}

func (c *lastUsedCache) dir(d string) time.Time {
	c.mu.Lock()
	e, ok := c.m[d]
	gen := c.gen
	now := c.now()
	c.mu.Unlock()
	if ok && now.Sub(e.at) < lastUsedTTL {
		return e.t
	}
	t := dirMTime(d)
	c.mu.Lock()
	if c.gen == gen {
		c.m[d] = lastUsedEntry{t: t, at: now}
	}
	c.mu.Unlock()
	return t
}

// invalidate forgets every entry: a session just ended.
func (c *lastUsedCache) invalidate() {
	c.mu.Lock()
	c.gen++
	c.m = make(map[string]lastUsedEntry)
	c.mu.Unlock()
}

// dirMTime is the newest mtime of dir and the entries directly in it; the
// entrypoint rewrites .name and .resume-token there at each start and exit.
func dirMTime(dir string) time.Time {
	fi, err := os.Stat(dir)
	if err != nil {
		return time.Time{}
	}
	newest := fi.ModTime()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return newest
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if t := info.ModTime(); t.After(newest) {
			newest = t
		}
	}
	return newest
}
