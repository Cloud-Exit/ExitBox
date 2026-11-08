package opencode

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/cloud-exit/exitbox/internal/container"
)

// v2RegistryPackage is the npm package whose latest dist-tag tracks OpenCode
// 2.x. Per-platform binaries ship as @opencode/cli-<target> at the same version.
const v2RegistryPackage = "@opencode/cli"

// IsV2 reports whether version selects the OpenCode 2.x line. An empty
// version means latest, which is 2.x.
func IsV2(version string) bool {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" {
		return true
	}
	major, err := strconv.Atoi(strings.SplitN(v, ".", 2)[0])
	if err != nil {
		return false
	}
	return major >= 2
}

// GetLatestVersion queries npm for the latest OpenCode 2.x version.
// Pin agents.opencode.version to a 1.x release to stay on OpenCode 1.
func (o *OpenCode) GetLatestVersion() (string, error) {
	if o.NpmPackageName() == "" {
		return "", fmt.Errorf("unsupported architecture for OpenCode")
	}
	out, err := exec.Command("curl", "-fsSL",
		"https://registry.npmjs.org/"+strings.Replace(v2RegistryPackage, "/", "%2f", 1)+"/latest").Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch OpenCode latest version: %w", err)
	}
	return parseRegistryVersion(out)
}

func parseRegistryVersion(out []byte) (string, error) {
	var release struct {
		Version string `json:"version"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(out, &release); err != nil {
		return "", err
	}
	if release.Error != "" && release.Version == "" {
		return "", fmt.Errorf("npm registry error: %s", release.Error)
	}
	if release.Version == "" {
		return "", fmt.Errorf("empty version")
	}
	return release.Version, nil
}

func (o *OpenCode) GetInstalledVersion(rt container.Runtime, img string) (string, error) {
	if rt == nil || !rt.ImageExists(img) {
		return "", fmt.Errorf("image %s not found", img)
	}
	out, err := rt.ImageInspect(img, `{{index .Config.Labels "exitbox.agent.version"}}`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
