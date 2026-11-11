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

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/plugins"
	"github.com/cloud-exit/exitbox/internal/plugins/clauderouter"
	"github.com/cloud-exit/exitbox/internal/router"
	"github.com/cloud-exit/exitbox/internal/subagents"
	"github.com/cloud-exit/exitbox/internal/vault"
)

func init() {
	// Subagent validation asks here whether a model that is not a Claude
	// model has a claude-router route in the workspace.
	subagents.RoutesFor = func(workspace string) subagents.Routes {
		routes, enabled := claudeRouterRoutes(workspace)
		return subagents.Routes{Enabled: enabled, IDs: router.IDs(routes)}
	}
}

// claudeRouterRoutes returns the routes of the claude-router plugin in a
// workspace, and whether the plugin is enabled for Claude Code there.
func claudeRouterRoutes(workspace string) ([]router.Route, bool) {
	ws := config.LoadOrDefault().Workspace(workspace)
	p := plugins.Get(clauderouter.Name)
	if ws == nil || p == nil {
		return nil, false
	}
	entry, _ := ws.PluginEntry(subagents.Claude.Agent, p.Name)
	if !entry.Enabled {
		return nil, false
	}
	return clauderouter.Routes(plugins.Settings(ws, subagents.Claude.Agent, p)), true
}

// probeTimeout bounds one request to a backend during validate --probe.
const probeTimeout = 5 * time.Second

// probeContext asks a backend for its context size. A seam for tests.
var probeContext = func(r router.Route, token string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*probeTimeout)
	defer cancel()
	return router.ProbeContext(ctx, &http.Client{Timeout: probeTimeout}, r, token)
}

// vaultReady reports whether a workspace has a vault. A seam for tests.
var vaultReady = vault.IsInitialized

// vaultSecret reads one key from a workspace's vault. A seam for tests.
var vaultSecret = func(workspace, password, key string) (string, error) {
	return vault.QuickGet(workspace, password, key)
}

// vaultPassword asks for a workspace's vault password. A seam for tests.
var vaultPassword = func(workspace string) string {
	return promptPassword(fmt.Sprintf("Vault password for workspace '%s' (to read backend tokens): ", workspace))
}

// routeProber checks the subagents of one workspace against the context
// size their backends really serve. Each backend is asked once.
type routeProber struct {
	workspace string
	routes    []router.Route
	results   map[string]probeResult
	password  string
	asked     bool
}

type probeResult struct {
	ctx int
	err error
}

func newRouteProber(workspace string) *routeProber {
	routes, _ := claudeRouterRoutes(workspace)
	return &routeProber{workspace: workspace, routes: routes, results: map[string]probeResult{}}
}

// context returns the context size of a route's backend. The backend is
// asked without a token first, so a vault password is only needed when the
// backend really wants one.
func (p *routeProber) context(r router.Route) (int, error) {
	if res, ok := p.results[r.ID]; ok {
		return res.ctx, res.err
	}
	n, err := probeContext(r, "")
	if errors.Is(err, router.ErrUnauthorized) && r.VaultKey != "" {
		token, terr := p.token(r.VaultKey)
		if terr != nil {
			err = fmt.Errorf("the backend wants a token and vault key %s could not be read: %w", r.VaultKey, terr)
		} else {
			n, err = probeContext(r, token)
		}
	}
	p.results[r.ID] = probeResult{ctx: n, err: err}
	return n, err
}

func (p *routeProber) token(key string) (string, error) {
	if !vaultReady(p.workspace) {
		return "", fmt.Errorf("workspace '%s' has no vault", p.workspace)
	}
	if !p.asked {
		p.password, p.asked = vaultPassword(p.workspace), true
	}
	return vaultSecret(p.workspace, p.password, key)
}

// findings compares a subagent's autoCompactWindow with the context its
// backend serves. Claude Code sizes a conversation for a Claude model unless
// told otherwise, so a smaller local context overflows before it compacts.
func (p *routeProber) findings(def *subagents.Definition) []subagents.Finding {
	if def == nil || subagents.ClassifyModel(def.Model) != subagents.ModelOther {
		return nil
	}
	r, ok := router.Find(p.routes, def.Model)
	if !ok {
		return nil
	}
	n, err := p.context(r)
	warn := func(format string, a ...any) []subagents.Finding {
		return []subagents.Finding{{Severity: subagents.Warning, Field: "autoCompactWindow", Message: fmt.Sprintf(format, a...)}}
	}
	switch {
	case err != nil:
		return warn("could not read the context size of %s: %v", r.URL, err)
	case n < subagents.MinAutoCompactWindow:
		return warn("%s serves %d tokens of context for %q, less than the smallest autoCompactWindow Claude Code accepts (%d): a long run overflows the backend before Claude Code compacts. Give the backend more context",
			r.URL, n, def.Model, subagents.MinAutoCompactWindow)
	case def.AutoCompactWindow == 0:
		return warn("not set, but %s serves %d tokens of context for %q; set autoCompactWindow: %d or lower, or Claude Code compacts too late",
			r.URL, n, def.Model, n)
	case def.AutoCompactWindow > n:
		return warn("%d is more than the %d tokens of context %s serves for %q; lower it to %d",
			def.AutoCompactWindow, n, r.URL, def.Model, n)
	}
	return nil
}
