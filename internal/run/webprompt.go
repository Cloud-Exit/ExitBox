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

package run

import (
	"errors"

	"github.com/cloud-exit/exitbox/internal/ipc"
	"github.com/cloud-exit/exitbox/internal/webprompt"
)

// webApprove asks in the browser first and falls back to popup when no
// browser can answer.
func webApprove(c *webprompt.Client, kind string, popup func(string) (bool, error)) func(string) (bool, error) {
	if c == nil {
		return popup
	}
	return func(subject string) (bool, error) {
		r, err := c.Ask(kind, subject)
		if errors.Is(err, webprompt.ErrUnavailable) {
			return popup(subject)
		}
		if err != nil {
			return false, err
		}
		return r.Allow, nil
	}
}

// webPassword asks for the vault password in the browser first. The
// password stays in memory, as with the popup.
func webPassword(c *webprompt.Client, popup func() (string, error)) func() (string, error) {
	if c == nil {
		return popup
	}
	return func() (string, error) {
		r, err := c.Ask(webprompt.KindVaultPassword, "")
		if errors.Is(err, webprompt.ErrUnavailable) {
			return popup()
		}
		if err != nil {
			return "", err
		}
		if !r.Allow {
			return "", errors.New("password entry cancelled")
		}
		if r.Password == "" {
			return "", errors.New("empty password")
		}
		return r.Password, nil
	}
}

// webVaultConfig routes the vault prompts of cfg through c, if any.
func webVaultConfig(cfg ipc.VaultHandlerConfig, c *webprompt.Client) ipc.VaultHandlerConfig {
	if c == nil {
		return cfg
	}
	p := ipc.Popups{Runtime: cfg.Runtime, ContainerName: cfg.ContainerName}
	cfg.PromptApproveFunc = webApprove(c, webprompt.KindVaultRead, p.VaultRead)
	cfg.PromptApproveSetFunc = webApprove(c, webprompt.KindVaultWrite, p.VaultWrite)
	list := webApprove(c, webprompt.KindVaultList, func(string) (bool, error) { return p.VaultList() })
	cfg.PromptApproveListFunc = func() (bool, error) { return list("") }
	cfg.PromptPasswordFunc = webPassword(c, p.VaultPassword)
	return cfg
}
