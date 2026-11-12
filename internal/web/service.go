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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

const (
	systemdUnitName = "exitbox-web.service"
	launchdLabel    = "com.cloud-exit.exitbox-web"
)

// ServiceSpec is the command a service runs.
type ServiceSpec struct {
	Exe  string   // absolute path of the exitbox binary
	Args []string // e.g. ["web"]
	Path string   // PATH for the service, so podman or docker is found
	Home string
}

// runCommand runs a service manager command; tests replace it.
var runCommand = func(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// systemdQuote quotes one word of an ExecStart line.
func systemdQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$%;") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", "$$", "%", "%%")
	return `"` + r.Replace(s) + `"`
}

// SystemdUnit renders the user unit.
func SystemdUnit(spec ServiceSpec) string {
	words := []string{systemdQuote(spec.Exe)}
	for _, a := range spec.Args {
		words = append(words, systemdQuote(a))
	}
	var b strings.Builder
	b.WriteString("[Unit]\n")
	b.WriteString("Description=ExitBox web\n")
	b.WriteString("After=network-online.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString("ExecStart=" + strings.Join(words, " ") + "\n")
	if spec.Path != "" {
		b.WriteString("Environment=" + systemdQuote("PATH="+spec.Path) + "\n")
	}
	b.WriteString("Restart=on-failure\n")
	b.WriteString("RestartSec=3\n")
	// Stopping the service hangs up the sessions; give their containers time.
	b.WriteString("TimeoutStopSec=30\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

// LaunchdPlist renders the launchd agent.
func LaunchdPlist(spec ServiceSpec, logFile string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + launchdLabel + `</string>
  <key>ProgramArguments</key>
  <array>
`)
	for _, a := range append([]string{spec.Exe}, spec.Args...) {
		b.WriteString("    <string>" + xmlEscape(a) + "</string>\n")
	}
	b.WriteString("  </array>\n")
	if spec.Path != "" {
		b.WriteString("  <key>EnvironmentVariables</key>\n  <dict>\n    <key>PATH</key>\n    <string>" + xmlEscape(spec.Path) + "</string>\n  </dict>\n")
	}
	if spec.Home != "" {
		b.WriteString("  <key>WorkingDirectory</key>\n  <string>" + xmlEscape(spec.Home) + "</string>\n")
	}
	b.WriteString(`  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>StandardOutPath</key>
  <string>` + xmlEscape(logFile) + `</string>
  <key>StandardErrorPath</key>
  <string>` + xmlEscape(logFile) + `</string>
</dict>
</plist>
`)
	return b.String()
}

func systemdUnitPath(home string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "systemd", "user", systemdUnitName)
}

func launchdPaths(home string) (plist, logFile string) {
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"),
		filepath.Join(home, "Library", "Logs", "exitbox-web.log")
}

// InstallService writes the service for this OS and starts it. It returns
// lines that tell the user what happened.
func InstallService(spec ServiceSpec) ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		path := systemdUnitPath(spec.Home)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(SystemdUnit(spec)), 0o644); err != nil {
			return nil, err
		}
		for _, args := range [][]string{
			{"--user", "daemon-reload"},
			{"--user", "enable", systemdUnitName},
			{"--user", "restart", systemdUnitName},
		} {
			if err := runCommand("systemctl", args...); err != nil {
				return nil, err
			}
		}
		return []string{
			"Installed " + path,
			"Started with: systemctl --user restart " + systemdUnitName,
			"Logs: journalctl --user -u " + systemdUnitName + " -f",
			"It starts when you log in. To keep it running after you log out: loginctl enable-linger",
		}, nil
	case "darwin":
		plist, logFile := launchdPaths(spec.Home)
		if err := os.MkdirAll(filepath.Dir(plist), 0o755); err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(plist, []byte(LaunchdPlist(spec, logFile)), 0o644); err != nil {
			return nil, err
		}
		domain := "gui/" + strconv.Itoa(os.Getuid())
		// Not loaded yet is fine.
		_ = runCommand("launchctl", "bootout", domain+"/"+launchdLabel)
		if err := runCommand("launchctl", "bootstrap", domain, plist); err != nil {
			return nil, err
		}
		return []string{
			"Installed " + plist,
			"It starts when you log in.",
			"Logs: " + logFile,
		}, nil
	}
	return nil, errors.New("exitbox web --service supports Linux (systemd) and macOS (launchd)")
}

// RemoveService stops and deletes the service.
func RemoveService(home string) ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		path := systemdUnitPath(home)
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return []string{"No service installed (" + path + " does not exist)"}, nil
		}
		if err := runCommand("systemctl", "--user", "disable", "--now", systemdUnitName); err != nil {
			return nil, err
		}
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		if err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
			return nil, err
		}
		return []string{"Stopped and removed " + path}, nil
	case "darwin":
		plist, _ := launchdPaths(home)
		if _, err := os.Stat(plist); errors.Is(err, os.ErrNotExist) {
			return []string{"No service installed (" + plist + " does not exist)"}, nil
		}
		_ = runCommand("launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid())+"/"+launchdLabel)
		if err := os.Remove(plist); err != nil {
			return nil, err
		}
		return []string{"Stopped and removed " + plist}, nil
	}
	return nil, errors.New("exitbox web --service supports Linux (systemd) and macOS (launchd)")
}
