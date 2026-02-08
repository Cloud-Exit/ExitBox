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

package ui

import (
	"fmt"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner displays an animated spinner with a message and elapsed time.
type Spinner struct {
	msg       string
	stop      chan struct{}
	done      sync.WaitGroup
	startTime time.Time
}

// NewSpinner creates a new spinner with the given message.
func NewSpinner(msg string) *Spinner {
	return &Spinner{
		msg:  msg,
		stop: make(chan struct{}),
	}
}

// Start begins the spinner animation in a goroutine.
func (s *Spinner) Start() {
	s.startTime = time.Now()
	s.done.Add(1)
	go func() {
		defer s.done.Done()
		i := 0
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-s.stop:
				fmt.Printf("\r\033[K")
				return
			case <-ticker.C:
				frame := spinnerFrames[i%len(spinnerFrames)]
				elapsed := int(time.Since(s.startTime).Seconds())
				fmt.Printf("\r%s%s%s %s %s(%ds)%s", Cyan, frame, NC, s.msg, Dim, elapsed, NC)
				i++
			}
		}
	}()
}

// Stop halts the spinner, clears the line, and returns the elapsed duration.
func (s *Spinner) Stop() time.Duration {
	close(s.stop)
	s.done.Wait()
	return time.Since(s.startTime)
}
