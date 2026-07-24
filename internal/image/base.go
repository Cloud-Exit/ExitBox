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

package image

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/cloud-exit/exitbox/internal/config"
	"github.com/cloud-exit/exitbox/internal/container"
	"github.com/cloud-exit/exitbox/internal/platform"
	"github.com/cloud-exit/exitbox/internal/ui"
	"github.com/cloud-exit/exitbox/static"
)

const (
	// BaseImageRegistry is the GHCR URL for the published base image.
	BaseImageRegistry = "ghcr.io/cloud-exit/exitbox-base"

	// SquidImageRegistry is the GHCR URL for the published squid image.
	SquidImageRegistry = "ghcr.io/cloud-exit/exitbox-squid"
)

// exitboxAllowWrapper is a Python script that tries the Go binary first and
// falls back to native Python IPC. Uses #!/usr/bin/env python3 so the kernel
// invokes python3 directly — critical for Codex, whose seccomp sandbox blocks
// Unix socket connect() for all /bin/sh child processes but allows python3.
const exitboxAllowWrapper = `#!/usr/bin/env python3
"""exitbox-allow — domain allow request wrapper.

Tries the native Go binary first; falls back to Python IPC if blocked.
"""

import json
import os
import secrets
import socket
import subprocess
import sys


def try_go_binary(args):
    """Try the Go binary. Returns exit code or None if not found."""
    try:
        result = subprocess.run(
            ["exitbox-allow-bin"] + args,
            capture_output=True, text=True,
        )
        if result.returncode == 0:
            if result.stdout:
                print(result.stdout, end="")
            return 0
        # Check if it failed due to sandbox EPERM vs a real error.
        stderr = result.stderr.lower()
        for marker in ("connect failed", "not available", "operation not permitted"):
            if marker in stderr:
                return None  # sandbox block — fall through to Python IPC
        # Real error — relay it.
        if result.stderr:
            print(result.stderr, end="", file=sys.stderr)
        return result.returncode
    except FileNotFoundError:
        return None


def request_allow(sock_path, domain):
    """Send an allow_domain IPC request. Returns (approved, error)."""
    sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
    try:
        sock.connect(sock_path)
    except OSError as exc:
        return False, (
            f"IPC socket not available ({exc}). "
            "Domain allow requests require firewall mode"
        )

    req = json.dumps({
        "type": "allow_domain",
        "id": secrets.token_hex(8),
        "payload": {"domain": domain},
    }) + "\n"
    sock.sendall(req.encode())

    buf = b""
    while b"\n" not in buf:
        chunk = sock.recv(4096)
        if not chunk:
            break
        buf += chunk
    sock.close()

    if not buf.strip():
        return False, "no response from host"

    resp = json.loads(buf)
    payload = resp.get("payload", {})
    if isinstance(payload, str):
        payload = json.loads(payload)

    err = payload.get("error", "")
    if err:
        return False, err

    return payload.get("approved", False), None


def main():
    if len(sys.argv) < 2:
        print("Usage: exitbox-allow <domain> [domain ...]", file=sys.stderr)
        sys.exit(1)

    # Fast path: try Go binary.
    rc = try_go_binary(sys.argv[1:])
    if rc is not None:
        sys.exit(rc)

    # Fallback: Python IPC.
    sock_path = os.environ.get("EXITBOX_IPC_SOCKET", "/run/exitbox/host.sock")
    failed = False

    for domain in sys.argv[1:]:
        approved, err = request_allow(sock_path, domain)
        if err:
            print(f"Error: {domain}: {err}", file=sys.stderr)
            failed = True
            continue
        if approved:
            print(f"Approved: {domain}")
        else:
            print(f"Denied: {domain}")
            failed = True

    sys.exit(1 if failed else 0)


if __name__ == "__main__":
    main()
`

// Version is set from cmd package.
var Version = "3.2.0"

// SessionTools are extra packages requested via --tools for this build only.
var SessionTools []string

// ForceRebuild forces image rebuilds (from --update flag only).
var ForceRebuild bool

// AutoUpdate enables checking for new agent versions on launch.
var AutoUpdate bool

// AgentVersion is a pinned agent version (e.g. "1.0.123"). Empty means latest.
var AgentVersion string

// isReleaseVersion returns true if the version string looks like a release
// (starts with "v", e.g. "v3.2.0").
func isReleaseVersion(v string) bool {
	return strings.HasPrefix(v, "v")
}

// BuildBase builds the exitbox-base image.
func BuildBase(ctx context.Context, rt container.Runtime, force bool) error {
	imageName := "exitbox-base"
	cmd := container.Cmd(rt)

	if !force && !ForceRebuild && rt.ImageExists(imageName) {
		v, _ := rt.ImageInspect(imageName, `{{index .Config.Labels "exitbox.version"}}`)
		if v == Version {
			return nil
		}
		ui.Infof("Base image version mismatch (%s != %s). Rebuilding...", v, Version)
	}

	// For release versions, try pulling the pre-built base image from GHCR
	// and building only the thin local intermediary layer.
	if isReleaseVersion(Version) {
		remoteRef := BaseImageRegistry + ":" + Version
		if err := pullImage(rt, remoteRef, "Pulling base image..."); err == nil {
			if err := buildLocalIntermediary(ctx, rt, cmd, remoteRef, imageName); err == nil {
				ui.Success("Base image ready (from registry)")
				return nil
			}
			ui.Warnf("Local intermediary build failed, falling back to full build")
		} else {
			ui.Warnf("Could not pull %s, building locally", remoteRef)
		}
	}

	// Full local build (dev versions or when pull/intermediary fails).
	return buildBaseFull(ctx, rt, cmd, imageName)
}

// buildLocalIntermediary builds the thin local layer (user creation + exitbox-allow)
// on top of the pulled base image.
func buildLocalIntermediary(ctx context.Context, rt container.Runtime, cmd, baseRef, imageName string) error {
	buildCtx := filepath.Join(config.Cache, "build-local")
	if err := os.MkdirAll(buildCtx, 0755); err != nil {
		return fmt.Errorf("failed to create build context dir: %w", err)
	}

	dockerfilePath := filepath.Join(buildCtx, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, static.DockerfileLocal, 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile.local: %w", err)
	}

	// Write pre-built exitbox-allow binary for the container's architecture.
	if extra, err := writeExitboxAllow(buildCtx); err == nil && extra != "" {
		if err := appendToFile(dockerfilePath, extra); err != nil {
			ui.Warnf("Failed to append exitbox-allow to Dockerfile: %v", err)
		}
	}

	// Write pre-built exitbox-vault binary for the container's architecture.
	if extra, err := writeExitboxVault(buildCtx); err == nil && extra != "" {
		if err := appendToFile(dockerfilePath, extra); err != nil {
			ui.Warnf("Failed to append exitbox-vault to Dockerfile: %v", err)
		}
	}

	// Write pre-built exitbox-kv binary for the container's architecture.
	if extra, err := writeExitboxKV(buildCtx); err == nil && extra != "" {
		if err := appendToFile(dockerfilePath, extra); err != nil {
			ui.Warnf("Failed to append exitbox-kv to Dockerfile: %v", err)
		}
	}

	uid, gid := platform.HostUIDGID()
	args := buildArgs(cmd)
	args = append(args,
		"--build-arg", fmt.Sprintf("BASE_IMAGE=%s", baseRef),
		"--build-arg", fmt.Sprintf("USER_ID=%d", uid),
		"--build-arg", fmt.Sprintf("GROUP_ID=%d", gid),
		"--build-arg", "USERNAME=user",
		"-t", imageName,
		"-f", dockerfilePath,
		buildCtx,
	)

	return buildImage(rt, args, "Building local intermediary...")
}

// buildBaseFull performs a full local build of the base image from scratch.
// It first builds the published base image locally, then layers the local
// intermediary (user creation + exitbox-allow) on top.
func buildBaseFull(ctx context.Context, rt container.Runtime, cmd, imageName string) error {
	ui.Infof("Building base image locally with %s...", cmd)

	// Step 1: Build the published base image locally.
	publishedName := "exitbox-base-published"
	buildCtx := filepath.Join(config.Cache, "build")
	if err := os.MkdirAll(buildCtx, 0755); err != nil {
		return fmt.Errorf("failed to create build context dir: %w", err)
	}

	dockerfilePath := filepath.Join(buildCtx, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, static.DockerfileBase, 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}
	if err := os.WriteFile(filepath.Join(buildCtx, "docker-entrypoint"), static.DockerEntrypoint, 0755); err != nil {
		return fmt.Errorf("failed to write entrypoint: %w", err)
	}
	if err := os.WriteFile(filepath.Join(buildCtx, ".dockerignore"), static.Dockerignore, 0644); err != nil {
		return fmt.Errorf("failed to write .dockerignore: %w", err)
	}

	cfg := config.LoadOrDefault()

	args := buildArgs(cmd)
	args = append(args,
		"--build-arg", fmt.Sprintf("EXITBOX_VERSION=%s", Version),
		"--build-arg", fmt.Sprintf("INSTALL_RTK=%v", cfg.Settings.RTK),
		"-t", publishedName,
		"-f", dockerfilePath,
		buildCtx,
	)

	if err := buildImage(rt, args, "Building base image..."); err != nil {
		return fmt.Errorf("failed to build base image: %w", err)
	}

	// Step 2: Build the local intermediary on top.
	if err := buildLocalIntermediary(ctx, rt, cmd, publishedName, imageName); err != nil {
		return fmt.Errorf("failed to build local intermediary: %w", err)
	}

	ui.Success("Base image built")
	return nil
}

// writeExitboxAllow writes the exitbox-allow binary into the build context
// and returns the Dockerfile snippet to COPY it. Returns empty string if
// the binary could not be written.
func writeExitboxAllow(buildCtx string) (string, error) {
	var allowBin []byte
	switch runtime.GOARCH {
	case "arm64":
		allowBin = static.ExitboxAllowArm64
	default:
		allowBin = static.ExitboxAllowAmd64
	}
	if err := os.WriteFile(filepath.Join(buildCtx, "exitbox-allow-bin"), allowBin, 0755); err != nil {
		ui.Warnf("Failed to write exitbox-allow: %v", err)
		return "", err
	}
	// Write the shell wrapper that tries the Go binary first and falls back
	// to a Python IPC client when the binary is blocked (e.g. by Codex's
	// seccomp sandbox which returns EPERM on Go's connect() syscall).
	if err := os.WriteFile(filepath.Join(buildCtx, "exitbox-allow"), []byte(exitboxAllowWrapper), 0755); err != nil {
		ui.Warnf("Failed to write exitbox-allow wrapper: %v", err)
		return "", err
	}
	// Write the standalone Python IPC script. Codex's sandbox blocks all
	// /bin/sh children from using Unix sockets, so the shell wrapper's
	// Python fallback also fails. Agents must invoke this script directly
	// as: python3 /usr/local/bin/exitbox-allow-ipc.py <domain>
	if err := os.WriteFile(filepath.Join(buildCtx, "exitbox-allow-ipc.py"), static.ExitboxAllowIPC, 0755); err != nil {
		ui.Warnf("Failed to write exitbox-allow-ipc.py: %v", err)
		return "", err
	}
	return "\n# IPC client (Go binary + shell/Python fallback + standalone Python)\nCOPY exitbox-allow-bin /usr/local/bin/exitbox-allow-bin\nCOPY exitbox-allow /usr/local/bin/exitbox-allow\nCOPY exitbox-allow-ipc.py /usr/local/bin/exitbox-allow-ipc.py\n", nil
}

// writeExitboxVault writes the exitbox-vault binary into the build context
// and returns the Dockerfile snippet to COPY it. Returns empty string if
// the binary could not be written.
func writeExitboxVault(buildCtx string) (string, error) {
	var vaultBin []byte
	switch runtime.GOARCH {
	case "arm64":
		vaultBin = static.ExitboxVaultArm64
	default:
		vaultBin = static.ExitboxVaultAmd64
	}
	if err := os.WriteFile(filepath.Join(buildCtx, "exitbox-vault"), vaultBin, 0755); err != nil {
		ui.Warnf("Failed to write exitbox-vault: %v", err)
		return "", err
	}
	return "\n# Vault IPC client\nCOPY exitbox-vault /usr/local/bin/exitbox-vault\n", nil
}

// writeExitboxKV writes the exitbox-kv binary into the build context
// and returns the Dockerfile snippet to COPY it. Returns empty string if
// the binary could not be written.
func writeExitboxKV(buildCtx string) (string, error) {
	var kvBin []byte
	switch runtime.GOARCH {
	case "arm64":
		kvBin = static.ExitboxKVArm64
	default:
		kvBin = static.ExitboxKVAmd64
	}
	if err := os.WriteFile(filepath.Join(buildCtx, "exitbox-kv"), kvBin, 0755); err != nil {
		ui.Warnf("Failed to write exitbox-kv: %v", err)
		return "", err
	}
	return "\n# KV IPC client\nCOPY exitbox-kv /usr/local/bin/exitbox-kv\n", nil
}

// pullImage pulls a container image, using a spinner in quiet mode or
// full output in verbose mode.
func pullImage(rt container.Runtime, ref, label string) error {
	if ui.Verbose {
		start := time.Now()
		err := container.PullInteractive(rt, ref)
		ui.Infof("Pull took %s", formatDuration(time.Since(start)))
		return err
	}
	spin := ui.NewSpinner(label)
	spin.Start()
	output, err := container.PullQuiet(rt, ref)
	elapsed := spin.Stop()
	if err != nil {
		ui.Debugf("Pull output: %s", output)
		return err
	}
	ui.Infof("Pull took %s", formatDuration(elapsed))
	return nil
}

// cacheExportErrRe matches the "Cache export is not supported for the docker
// driver" error emitted by buildx when --cache-to is passed to a driver that
// cannot export cache.
var cacheExportErrRe = regexp.MustCompile(`(?i)cache export is not supported`)

// buildImage runs a container build, using a spinner in quiet mode or
// full output in verbose mode. On failure in quiet mode, the captured
// build output is printed to stderr.
//
// If the build fails specifically because the active buildx driver cannot
// export cache (--cache-to on the default docker driver), the cache-export
// flags are stripped and the build is retried once. This turns a hard
// "Cache export is not supported" failure into a successful no-export build
// rather than aborting the entire image pipeline.
func buildImage(rt container.Runtime, args []string, label string) error {
	if ui.Verbose {
		start := time.Now()
		err := container.BuildInteractive(rt, args)
		ui.Infof("Build took %s", formatDuration(time.Since(start)))
		if err == nil {
			return nil
		}
		if isCacheExportError(err, "") {
			ui.Warnf("Active buildx driver cannot export cache; retrying without --cache-to...")
			return container.BuildInteractive(rt, stripCacheExportArgs(args))
		}
		return err
	}

	spin := ui.NewSpinner(label)
	spin.Start()
	output, err := container.BuildQuiet(rt, args)
	elapsed := spin.Stop()
	if err != nil {
		if isCacheExportError(err, output) {
			spin = ui.NewSpinner(label + " (no cache export)")
			spin.Start()
			output2, err2 := container.BuildQuiet(rt, stripCacheExportArgs(args))
			elapsed = spin.Stop()
			if err2 != nil {
				fmt.Fprint(os.Stderr, output2)
				return err2
			}
			ui.Warnf("Build succeeded after disabling cache export")
			ui.Infof("Build took %s", formatDuration(elapsed))
			return nil
		}
		fmt.Fprint(os.Stderr, output)
		return err
	}
	ui.Infof("Build took %s", formatDuration(elapsed))
	return nil
}

// isCacheExportError reports whether err/output indicate the build failed
// because --cache-to was rejected by the active driver.
func isCacheExportError(err error, output string) bool {
	if err == nil {
		return false
	}
	if cacheExportErrRe.MatchString(err.Error()) {
		return true
	}
	return cacheExportErrRe.MatchString(output)
}

// stripCacheExportArgs returns args with any --cache-to flag pair removed so a
// build can proceed against a driver that cannot export cache (the default
// docker driver). --cache-from is left in place since cache import is always
// supported.
func stripCacheExportArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--cache-to" {
			i++ // skip the value
			continue
		}
		// Also tolerate the "--cache-to=..." single-token form.
		if strings.HasPrefix(args[i], "--cache-to=") {
			continue
		}
		out = append(out, args[i])
	}
	return out
}

// formatDuration formats a duration as a human-friendly string (e.g., "12s", "1m 23s").
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	m := int(d.Minutes())
	s := int(d.Seconds()) % 60
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// cacheExportDrivers are the buildx drivers that support exporting build
// cache (the "--cache-to" flag). The default "docker" driver does NOT —
// attempting to export cache fails with "Cache export is not supported for
// the docker driver" and aborts the whole build.
//
// These non-default drivers also do NOT load the built image into the local
// image store by default (the build result stays inside the buildx cache), so
// any FROM <local-image> in a follow-up build would fail to resolve. When one
// of these drivers is active we therefore also pass --load so the result is
// pulled back into the local docker/podman store, matching the default
// driver's behavior.
var cacheExportDrivers = map[string]bool{
	"docker-container": true,
	"kubernetes":       true,
	"remote":            true,
	"cloud":             true,
}

var (
	builderDriverOnce  sync.Once
	builderDriverValue string

	// detectBuilderDriver shells out to discover the active buildx driver.
	// It is a variable so tests can inject a stub without spawning docker.
	detectBuilderDriver = defaultDetectBuilderDriver
)

// defaultDetectBuilderDriver inspects the active buildx builder for the given
// runtime command and returns its driver (e.g. "docker", "docker-container",
// "remote"). Returns the empty string if it cannot be determined.
func defaultDetectBuilderDriver(cmd string) string {
	out, err := exec.Command(cmd, "buildx", "inspect").CombinedOutput()
	if err != nil {
		return ""
	}
	// `docker buildx inspect` prints a line like:
	//   Driver:  docker
	// or:
	//   Driver/type: docker-container
	m := regexp.MustCompile(`(?m)Driver(?:/type)?:\s*(\S+)`).FindStringSubmatch(string(out))
	if len(m) >= 2 {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// activeBuilderDriver returns the buildx driver of the currently active
// builder for the given runtime command ("docker" or "podman"). It caches
// the result for the process lifetime. Returns the empty string if it cannot
// be determined (the caller then treats cache export as unsupported, which is
// the safe default for the plain docker daemon).
//
// exitbox never creates or switches builders itself: changing the global active
// builder is an intrusive side effect for the user's whole machine, and the
// docker-container/remote drivers do not load images into the local store by
// default (which breaks our FROM <local-image> chains). We only adapt to
// whatever driver the user already has active.
func activeBuilderDriver(cmd string) string {
	builderDriverOnce.Do(func() {
		builderDriverValue = detectBuilderDriver(cmd)
	})
	return builderDriverValue
}

// resetBuilderDriverCache clears the cached active driver so the next call to
// activeBuilderDriver re-inspects. Used by tests after swapping the detector.
func resetBuilderDriverCache() {
	builderDriverOnce = sync.Once{}
	builderDriverValue = ""
}

// supportsCacheExport reports whether the active buildx driver for the given
// runtime command can export build cache. Podman always can (its layers cache
// does not need --cache-to). Docker can only when a non-default driver is active.
func supportsCacheExport(cmd string) bool {
	if cmd == "podman" {
		return true
	}
	return cacheExportDrivers[activeBuilderDriver(cmd)]
}

// needsLoad reports whether the active buildx driver keeps build results in
// the buildx cache instead of the local image store, requiring an explicit
// --load. The default "docker" driver auto-loads, so it does not need --load.
// Podman loads by default too. The non-default docker drivers (docker-container,
// remote, etc.) need --load for downstream FROM <local-image> builds to resolve.
func needsLoad(cmd string) bool {
	if cmd == "podman" {
		return false
	}
	driver := activeBuilderDriver(cmd)
	// Unknown driver: play it safe and don't add --load (the default docker
	// behavior), since --load is unnecessary there and harmless only there.
	if driver == "" || driver == "docker" {
		return false
	}
	return true
}

// buildArgs returns the build flags common to every image build. For docker it
// enables BuildKit and adapts to the active buildx driver:
//   - --cache-from is always added (cache import works on every driver).
//   - --cache-to is added only when the active driver supports cache export.
//     The default "docker" driver rejects --cache-to, so omitting it there
//     prevents the "Cache export is not supported for the docker driver" abort.
//   - --load is added for non-default docker drivers (docker-container, remote,
//     ...), which otherwise leave the built image in the buildx cache and break
//     downstream FROM <local-image> builds. The default driver auto-loads.
func buildArgs(cmd string) []string {
	var args []string
	if cmd == "podman" {
		args = append(args, "--layers", "--pull=newer")
		return args
	}

	os.Setenv("DOCKER_BUILDKIT", "1")
	args = append(args, "--progress=auto")

	cacheDir := filepath.Join(config.Cache, "buildx")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		ui.Warnf("Failed to create buildx cache dir: %v", err)
	}

	// Importing cache works on every driver, so always wire up --cache-from.
	args = append(args, "--cache-from", "type=local,src="+cacheDir)

	if supportsCacheExport(cmd) {
		args = append(args, "--cache-to", "type=local,dest="+cacheDir+",mode=max")
	} else {
		ui.Warnf("Active buildx driver %q cannot export cache; using build cache import only. Switch to a docker-container buildx driver to enable cache export.",
			activeBuilderDriver(cmd))
	}

	if needsLoad(cmd) {
		// Non-default drivers keep results in the build cache; --load pulls
		// the built image into the local store so later FROM <image> works.
		args = append(args, "--load")
	}
	return args
}
