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

// Package statusbar renders a persistent top-line status bar using ANSI
// scroll regions. The bar stays fixed at the top while the container's
// terminal output scrolls below it. A background goroutine re-renders
// the bar when the terminal is resized.
package statusbar

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// ANSI sequences
const (
	clearScreen = "\033[2J"
	clearLine   = "\033[K"
	resetScroll = "\033[r"
	resetAttrs  = "\033[0m"
	bgDarkGrey  = "\033[48;5;236m"
	fgWhite     = "\033[97m"
	saveCur     = "\033[s"
	restoreCur  = "\033[u"
)

var (
	active     bool
	curVersion string
	curAgent   string
	lastWidth  int
	lastHeight int
	stopCh     chan struct{}
	tty        *os.File
	mu         sync.Mutex
)

var displayNames = map[string]string{
	"claude":   "Claude Code",
	"codex":    "OpenAI Codex",
	"opencode": "OpenCode",
}

// barRows is the number of rows reserved for the status bar area.
const barRows = 2 // text + spacer

// Show clears the screen, renders the status bar, sets the scroll region,
// and starts a background watcher that re-renders on terminal resize.
// No-op if stdout is not a TTY.
func Show(version, agent string) {
	fd := int(os.Stdout.Fd())
	if !term.IsTerminal(fd) {
		return
	}

	curVersion = version
	curAgent = agent

	// Open /dev/tty for direct terminal writes (avoids interleaving with
	// the container's stdout).
	var err error
	tty, err = os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		tty = os.Stdout
	}

	w, h, _ := term.GetSize(fd)
	lastWidth = w
	lastHeight = h

	// Clear screen and render bar
	writeStr(clearScreen)
	render(w, h)

	active = true
	stopCh = make(chan struct{})
	go watchSize()
}

// render draws the bar and sets the scroll region for the given dimensions.
func render(width, height int) {
	if height < barRows+2 || width < 10 {
		return
	}

	name := displayNames[curAgent]
	if name == "" {
		name = curAgent
	}
	left := fmt.Sprintf(" ExitBox - %s", name)
	right := fmt.Sprintf("v%s ", curVersion)

	gap := width - len(left) - len(right)
	if gap < 1 {
		gap = 1
	}
	text := left + strings.Repeat(" ", gap) + right

	contentStart := barRows + 1

	// Build entire update as one string to minimise interleaving.
	var b strings.Builder
	b.WriteString(saveCur)
	// Row 1: status text (dark grey bg, white text)
	fmt.Fprintf(&b, "\033[1;1H%s%s%s%s", bgDarkGrey, fgWhite, text, resetAttrs)
	// Row 2: blank spacer
	fmt.Fprintf(&b, "\033[2;1H%s", clearLine)
	// Set scroll region
	fmt.Fprintf(&b, "\033[%d;%dr", contentStart, height)
	b.WriteString(restoreCur)

	writeStr(b.String())
}

// watchSize polls terminal dimensions and re-renders on change.
func watchSize() {
	fd := int(os.Stdout.Fd())
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			mu.Lock()
			if !active {
				mu.Unlock()
				return
			}
			w, h, err := term.GetSize(fd)
			if err != nil {
				mu.Unlock()
				continue
			}
			if w != lastWidth || h != lastHeight {
				lastWidth = w
				lastHeight = h
				render(w, h)
			}
			mu.Unlock()
		}
	}
}

// Hide resets the scroll region, clears the bar, and stops the watcher.
// No-op if Show was never called.
func Hide() {
	mu.Lock()
	defer mu.Unlock()

	if !active {
		return
	}
	active = false
	close(stopCh)

	// Reset scroll region
	writeStr(resetScroll)

	// Clear bar rows
	var b strings.Builder
	for i := 1; i <= barRows; i++ {
		fmt.Fprintf(&b, "\033[%d;1H%s", i, clearLine)
	}
	b.WriteString("\033[1;1H")
	writeStr(b.String())

	if tty != nil && tty != os.Stdout {
		_ = tty.Close()
	}
}

func writeStr(s string) {
	_, _ = fmt.Fprint(tty, s)
}
