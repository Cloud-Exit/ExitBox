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
	"fmt"
	"net"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/web"
)

// Rows of the Web access page.
const (
	webRowListen = iota
	webRowUser
	webRowPassword
	webRowConfirm
	webRowSave
	webRowDisable
)

// webForm is the state of the Web access page.
type webForm struct {
	cursor   int
	editing  bool
	listen   string
	username string
	password string
	confirm  string
	authOn   bool // a login is saved
	hash     string
	msg      string
	err      string
}

// The page reaches the config through these, replaced in tests.
var (
	loadSavedConfig = config.LoadConfig
	saveWebConfig   = func(w config.WebConfig) error { return saveWebConfigTo(config.ConfigFile(), w) }
	hashWebPassword = web.HashPassword
)

// saveWebConfigTo sets the web settings in the config at path, never resetting an unreadable one.
func saveWebConfigTo(path string, w config.WebConfig) error {
	cfg, err := config.LoadConfigFrom(path)
	if errors.Is(err, os.ErrNotExist) {
		cfg, err = config.DefaultConfig(), nil
	}
	if err != nil {
		return err
	}
	// This page does not edit it; the web UI does.
	w.RecentDays = cfg.Web.RecentDays
	cfg.Web = w
	return config.SaveConfigTo(cfg, path)
}

func (m Model) openWebAccess() Model {
	var w config.WebConfig
	if cfg, err := loadSavedConfig(); err == nil {
		w = cfg.Web
	}
	m.web = webForm{
		listen:   w.ListenOrDefault(),
		username: w.Username,
		authOn:   w.AuthEnabled(),
		hash:     w.PasswordHash,
	}
	m.step = stepWebAccess
	return m
}

func (f webForm) rows() int {
	if f.authOn {
		return webRowDisable + 1
	}
	return webRowSave + 1
}

func (f *webForm) field(row int) *string {
	switch row {
	case webRowListen:
		return &f.listen
	case webRowUser:
		return &f.username
	case webRowPassword:
		return &f.password
	case webRowConfirm:
		return &f.confirm
	}
	return nil
}

// webConfigFromForm validates the form and returns the config to save.
func webConfigFromForm(f webForm, disable bool) (config.WebConfig, error) {
	listen := strings.TrimSpace(f.listen)
	if listen == "" {
		listen = config.DefaultWebListen
	}
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return config.WebConfig{}, fmt.Errorf("invalid listen address %q (e.g. 127.0.0.1:4848)", listen)
	}
	out := config.WebConfig{Listen: listen}
	if listen == config.DefaultWebListen {
		out.Listen = ""
	}
	loopback := host == "localhost"
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		loopback = true
	}
	if disable {
		if !loopback {
			return config.WebConfig{}, errors.New("a login is required to listen beyond this machine; change the listen address first")
		}
		return out, nil
	}
	user := strings.TrimSpace(f.username)
	if !f.authOn && user == "" && f.password == "" && f.confirm == "" {
		// Only the listen address changed.
		if !loopback {
			return config.WebConfig{}, errors.New("a login is required to listen beyond this machine; set a username and password")
		}
		return out, nil
	}
	if user == "" || len(user) > 64 || strings.ContainsAny(user, ":\x00\n\r") {
		return config.WebConfig{}, errors.New("a username is required (up to 64 characters, no colon)")
	}
	out.Username = user
	switch {
	case f.password != "":
		if f.password != f.confirm {
			return config.WebConfig{}, errors.New("the passwords do not match")
		}
		hash, err := hashWebPassword(f.password)
		if err != nil {
			return config.WebConfig{}, err
		}
		out.PasswordHash = hash
	case f.hash != "":
		out.PasswordHash = f.hash
	default:
		return config.WebConfig{}, errors.New("a password is required")
	}
	return out, nil
}

func (m Model) updateWebAccess(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	f := &m.web
	if f.editing {
		p := f.field(f.cursor)
		switch key.Type {
		case tea.KeyEnter, tea.KeyEsc, tea.KeyTab:
			f.editing = false
		case tea.KeyBackspace:
			if r := []rune(*p); len(r) > 0 {
				*p = string(r[:len(r)-1])
			}
		case tea.KeyRunes:
			*p += string(key.Runes)
		case tea.KeySpace:
			*p += " "
		}
		return m, nil
	}
	switch key.String() {
	case "up", "k":
		if f.cursor > 0 {
			f.cursor--
		}
	case "down", "j", "tab":
		if f.cursor < f.rows()-1 {
			f.cursor++
		}
	case "enter", " ":
		f.msg, f.err = "", ""
		switch f.cursor {
		case webRowSave, webRowDisable:
			w, err := webConfigFromForm(*f, f.cursor == webRowDisable)
			if err != nil {
				f.err = err.Error()
				break
			}
			if err := saveWebConfig(w); err != nil {
				f.err = "saving config.yaml: " + err.Error()
				break
			}
			f.authOn, f.hash = w.AuthEnabled(), w.PasswordHash
			f.username = w.Username
			f.password, f.confirm = "", ""
			if w.AuthEnabled() {
				f.msg = "Saved. exitbox web asks " + w.Username + " to sign in."
			} else {
				f.msg = "Saved. exitbox web has no login."
			}
			f.msg += " A running exitbox web picks up the listen address when it restarts."
			if f.cursor >= f.rows() {
				f.cursor = f.rows() - 1
			}
		default:
			f.editing = true
		}
	case "esc", "q":
		m.step = stepTopMenu
	}
	return m, nil
}

func (m Model) viewWebAccess() string {
	f := m.web
	var b strings.Builder
	b.WriteString(titleStyle.Render("Web access"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("exitbox web runs agent sessions in the browser. Sessions mount host folders, so"))
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("set a login before it listens beyond this machine."))
	b.WriteString("\n\n")
	if f.authOn {
		b.WriteString(successStyle.Render("Login is on for " + f.username))
	} else {
		b.WriteString(warnStyle.Render("No login"))
	}
	b.WriteString("\n\n")

	mask := func(s string) string { return strings.Repeat("*", len([]rune(s))) }
	pwHint := "at least 8 characters"
	if f.authOn {
		pwHint = "empty keeps the current password"
	}
	rows := []struct{ label, value, hint string }{
		{"Listen address", f.listen, "e.g. 127.0.0.1:4848"},
		{"Username", f.username, ""},
		{"Password", mask(f.password), pwHint},
		{"Confirm password", mask(f.confirm), ""},
	}
	for i, r := range rows {
		cursor := "  "
		label := r.label
		if f.cursor == i {
			cursor = cursorStyle.Render("> ")
			label = selectedStyle.Render(label)
		}
		value := r.value
		if f.editing && f.cursor == i {
			value += "█"
		}
		line := fmt.Sprintf("%s%-18s %s", cursor, label, value)
		if r.hint != "" && !(f.editing && f.cursor == i) {
			line += "  " + dimStyle.Render(r.hint)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")
	actions := []string{"Save login"}
	if f.authOn {
		actions = append(actions, "Turn off login")
	}
	for i, a := range actions {
		row := webRowSave + i
		if f.cursor == row {
			b.WriteString(cursorStyle.Render("> ") + selectedStyle.Render("["+a+"]") + "\n")
		} else {
			b.WriteString("  [" + a + "]\n")
		}
	}
	if f.err != "" {
		b.WriteString("\n" + warnStyle.Render(f.err) + "\n")
	}
	if f.msg != "" {
		b.WriteString("\n" + successStyle.Render(f.msg) + "\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("Run it with: exitbox web    As a login service: exitbox web --service"))
	b.WriteString("\n\n")
	if f.editing {
		b.WriteString(helpStyle.Render("Type, Enter to finish"))
	} else {
		b.WriteString(helpStyle.Render("Up/Down to move, Enter to edit or press, Esc to go back"))
	}
	return b.String()
}
