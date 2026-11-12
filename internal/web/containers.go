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
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
)

// containerDoc is an agent container that runs now, wherever it was started.
type containerDoc struct {
	Container string    `json:"container"`
	Agent     string    `json:"agent"`
	Dir       string    `json:"dir"`
	Workspace string    `json:"workspace"`
	Session   string    `json:"session"`
	Started   time.Time `json:"started"`
	// NoTmux sessions have no tmux to attach to.
	NoTmux bool `json:"noTmux"`
	// WebSession is the id of the web session that started it, if any.
	WebSession string `json:"-"`
}

// containerNameRe matches the agent container names of project.ContainerName.
var containerNameRe = regexp.MustCompile(`^exitbox-[a-z0-9]+-[^\s/]+-[0-9a-f]{8}$`)

// inspectEntry is the part of `podman inspect` / `docker inspect` used here.
type inspectEntry struct {
	Name   string
	Config struct {
		Env []string
	}
	Mounts []struct {
		Source      string
		Destination string
	}
	State struct {
		Running   bool
		StartedAt string
	}
}

// parseInspect turns inspect output into agent containers. Containers
// without EXITBOX_AGENT (the squid proxy, other tools) are skipped.
func parseInspect(data []byte) ([]containerDoc, error) {
	var entries []inspectEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	var out []containerDoc
	for _, e := range entries {
		name := strings.TrimPrefix(e.Name, "/")
		if !containerNameRe.MatchString(name) || !e.State.Running {
			continue
		}
		env := map[string]string{}
		for _, kv := range e.Config.Env {
			k, v, _ := strings.Cut(kv, "=")
			env[k] = v
		}
		if env["EXITBOX_AGENT"] == "" {
			continue
		}
		c := containerDoc{
			Container:  name,
			Agent:      env["EXITBOX_AGENT"],
			Workspace:  env["EXITBOX_WORKSPACE_NAME"],
			Session:    env["EXITBOX_SESSION_NAME"],
			NoTmux:     env["EXITBOX_NO_TMUX"] == "1",
			WebSession: env[WebSessionEnv],
		}
		for _, m := range e.Mounts {
			if m.Destination == "/workspace" {
				c.Dir = m.Source
			}
		}
		if t, err := time.Parse(time.RFC3339Nano, e.State.StartedAt); err == nil {
			c.Started = t
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out, nil
}

// matchContainers pairs the web sessions with their containers. A run
// session owns the container that carries its id (WebSessionEnv), which
// holds across relaunches on a workspace or session switch. An attach
// session owns the container it attaches to. Containers without a web
// session id were started in a terminal.
func matchContainers(sessions []SessionInfo, ctrs []containerDoc) map[string]int {
	owner := map[string]int{}
	taken := map[int]bool{}
	for _, s := range sessions {
		if s.Exited {
			continue
		}
		for i, c := range ctrs {
			if taken[i] {
				continue
			}
			mine := (s.Kind == KindRun && c.WebSession == s.ID) ||
				(s.Kind == KindAttach && c.Container == s.Container)
			if mine {
				owner[s.ID] = i
				taken[i] = true
				break
			}
		}
	}
	return owner
}
