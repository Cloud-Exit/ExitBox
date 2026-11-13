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

package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
)

// stubWebConfig replaces the config access of the Web access page.
func stubWebConfig(t *testing.T, start config.WebConfig) *config.WebConfig {
	t.Helper()
	saved := start
	oldLoad, oldSave, oldHash := loadSavedConfig, saveWebConfig, hashWebPassword
	loadSavedConfig = func() (*config.Config, error) {
		c := config.DefaultConfig()
		c.Web = saved
		return c, nil
	}
	saveWebConfig = func(w config.WebConfig) error { saved = w; return nil }
	hashWebPassword = func(pw string) (string, error) {
		if len(pw) < 8 {
			return "", errors.New("the password needs at least 8 characters")
		}
		return "hash:" + pw, nil
	}
	t.Cleanup(func() { loadSavedConfig, saveWebConfig, hashWebPassword = oldLoad, oldSave, oldHash })
	return &saved
}

func keys(m Model, ks ...string) Model {
	for _, k := range ks {
		var msg tea.KeyMsg
		switch k {
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func TestWebAccessMenuItem(t *testing.T) {
	found := false
	for _, o := range topMenuOptions {
		if o.Label == topMenuWeb {
			found = true
		}
	}
	if !found {
		t.Fatal("Web access is not on the main menu")
	}
}

func TestWebAccessTurnsOnLogin(t *testing.T) {
	saved := stubWebConfig(t, config.WebConfig{})
	m := NewModel().openWebAccess()
	if m.step != stepWebAccess || m.web.listen != config.DefaultWebListen {
		t.Fatalf("page not opened: step %v listen %q", m.step, m.web.listen)
	}

	// Saving without a login keeps a loopback address only.
	m.web.listen = "0.0.0.0:4848"
	m.web.cursor = webRowSave
	m = keys(m, "enter")
	if m.web.err == "" || saved.AuthEnabled() || saved.Listen != "" {
		t.Fatal("saved a public address without a login")
	}
	m.web.listen = "127.0.0.1:9000"
	m = keys(m, "enter")
	if m.web.err != "" || saved.Listen != "127.0.0.1:9000" || saved.AuthEnabled() || m.web.authOn {
		t.Fatalf("listen-only save: err %q saved %+v", m.web.err, *saved)
	}
	m.web.listen = config.DefaultWebListen

	// A username without a password fails and says why.
	m.web.username = "me"
	m = keys(m, "enter")
	if m.web.err == "" || saved.AuthEnabled() {
		t.Fatal("saved without a password")
	}
	m.web.username = ""

	m.web.cursor = webRowUser
	m = keys(m, "enter", "m", "e", "enter", "down", "enter", "correct horse", "enter", "down", "enter", "correct horsx", "backspace", "e", "enter", "down", "enter")
	if m.web.err != "" {
		t.Fatalf("save failed: %s", m.web.err)
	}
	if saved.Username != "me" || saved.PasswordHash != "hash:correct horse" || saved.Listen != "" {
		t.Fatalf("saved %+v", *saved)
	}
	if !m.web.authOn || m.web.password != "" {
		t.Fatal("form not updated after saving")
	}
	if !strings.Contains(m.viewWebAccess(), "Login is on for me") {
		t.Fatal("view does not show the login")
	}

	// Turn it off again.
	m.web.cursor = webRowDisable
	m = keys(m, "enter")
	if saved.AuthEnabled() || m.web.authOn {
		t.Fatalf("login still on: %+v", *saved)
	}

	m = keys(m, "esc")
	if m.step != stepTopMenu {
		t.Fatal("esc does not go back to the menu")
	}
}

func TestWebConfigFromForm(t *testing.T) {
	stubWebConfig(t, config.WebConfig{})
	if _, err := webConfigFromForm(webForm{listen: "nope", username: "me", password: "correct horse", confirm: "correct horse"}, false); err == nil {
		t.Error("bad listen address accepted")
	}
	if _, err := webConfigFromForm(webForm{listen: "127.0.0.1:1", username: "me", password: "a", confirm: "b"}, false); err == nil {
		t.Error("different passwords accepted")
	}
	if _, err := webConfigFromForm(webForm{listen: "0.0.0.0:4848"}, true); err == nil {
		t.Error("public listen without login accepted")
	}
	w, err := webConfigFromForm(webForm{listen: "0.0.0.0:4848", username: "me", hash: "old"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if w.Listen != "0.0.0.0:4848" || w.PasswordHash != "old" {
		t.Fatalf("got %+v; an empty password keeps the old hash", w)
	}
}

func TestSaveWebConfigTo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	// No file yet: defaults plus the web settings.
	if err := saveWebConfigTo(path, config.WebConfig{Listen: "127.0.0.1:9000"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfigFrom(path)
	if err != nil || cfg.Web.Listen != "127.0.0.1:9000" {
		t.Fatalf("saved %+v: %v", cfg, err)
	}
	// The web UI's recently ended days survive a save here.
	cfg.Web.RecentDays = 9
	if err := config.SaveConfigTo(cfg, path); err != nil {
		t.Fatal(err)
	}
	if err := saveWebConfigTo(path, config.WebConfig{Username: "me", PasswordHash: "h"}); err != nil {
		t.Fatal(err)
	}
	if cfg, err = config.LoadConfigFrom(path); err != nil || cfg.Web.RecentDays != 9 || cfg.Web.Username != "me" {
		t.Fatalf("saved %+v: %v", cfg.Web, err)
	}
	// A config it cannot parse is left alone.
	bad := []byte("settings: [unclosed\n")
	if err := os.WriteFile(path, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := saveWebConfigTo(path, config.WebConfig{}); err == nil {
		t.Fatal("saved over an unreadable config")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(bad) {
		t.Fatalf("config changed to %q: %v", data, err)
	}
}
