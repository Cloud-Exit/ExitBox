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

package project

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/cloud-exit/exitbox/internal/config"
)

// SlugifyPath converts a filesystem path to a safe slug.
func SlugifyPath(path string) string {
	// Remove leading /
	path = strings.TrimPrefix(path, "/")
	// Replace / with _
	path = strings.ReplaceAll(path, "/", "_")
	// Remove unsafe characters and lowercase
	var b strings.Builder
	for _, r := range path {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			b.WriteRune(unicode.ToLower(r))
		} else {
			b.WriteRune('_')
		}
	}
	return b.String()
}

// GenerateFolderName creates a project folder name from a path (slug + CRC32 hash).
func GenerateFolderName(path string) string {
	slug := SlugifyPath(path)
	hash := POSIXCksumString(path)
	return fmt.Sprintf("%s_%08x", slug, hash)
}

// ParentDir returns the project's parent directory inside exitbox config.
func ParentDir(path string) string {
	return filepath.Join(config.ProjectsDir(), GenerateFolderName(path))
}

// Init creates the project directory structure.
func Init(path string) error {
	parent := ParentDir(path)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}

	// Store original project path
	if err := os.WriteFile(filepath.Join(parent, ".project_path"), []byte(path+"\n"), 0644); err != nil {
		return err
	}

	// Create per-agent directories
	for _, agent := range []string{"claude", "codex", "opencode"} {
		if err := os.MkdirAll(filepath.Join(parent, agent), 0755); err != nil {
			return err
		}
	}

	// Create local .exitbox directory in project
	_ = os.MkdirAll(filepath.Join(path, ".exitbox"), 0755)
	return nil
}

// ImageName returns the Docker image name for an agent in a project.
// profileHash must encode the active profile configuration so that each
// profile produces a distinct image (no cache sharing between profiles).
func ImageName(agent, projectDir, profileHash string) string {
	return fmt.Sprintf("exitbox-%s-%s-%s", agent, GenerateFolderName(projectDir), profileHash)
}

// ContainerName returns a unique container name for an agent in a project.
func ContainerName(agent, projectDir string) string {
	folder := GenerateFolderName(projectDir)
	// Random suffix from crypto/rand
	suffix := randomHex(4)
	return fmt.Sprintf("exitbox-%s-%s-%s", agent, folder, suffix)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n*2)
	}
	return fmt.Sprintf("%x", b)
}
