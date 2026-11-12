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
	"encoding/binary"
	"errors"
	"io"
)

// Frames between exitbox web and a session host: one type byte, a 4-byte
// big-endian length, then the payload.
const (
	// web -> host
	frameInput  = 'i' // keys for the terminal
	frameResize = 'r' // cols, rows as two uint32
	frameStop   = 'k' // hang up the process, kill it after a grace period
	frameQuit   = 'q' // the window is closed: stop if needed, clean up, exit
	// host -> web
	frameHello  = 'h' // first frame after connecting, no payload
	frameOutput = 'o' // terminal output; the replay arrives as output too
	frameExit   = 'x' // the process ended; int32 exit code
)

const maxFrame = 4 << 20

func writeFrame(w io.Writer, kind byte, payload []byte) error {
	if len(payload) > maxFrame {
		return errors.New("frame too large")
	}
	hdr := [5]byte{kind}
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	// One write per frame keeps frames whole when writers share a lock.
	buf := make([]byte, 0, len(hdr)+len(payload))
	buf = append(append(buf, hdr[:]...), payload...)
	_, err := w.Write(buf)
	return err
}

func readFrame(r io.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > maxFrame {
		return 0, nil, errors.New("frame too large")
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return 0, nil, err
	}
	return hdr[0], payload, nil
}
