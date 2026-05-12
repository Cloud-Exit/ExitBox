package opencode

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/cloud-exit/exitbox/internal/container"
)

func (o *OpenCode) GetLatestVersion() (string, error) {
	pkg := o.NpmPackageName()
	if pkg == "" {
		return "", fmt.Errorf("unsupported architecture for OpenCode")
	}
	out, err := exec.Command("curl", "-fsSL",
		fmt.Sprintf("https://registry.npmjs.org/%s/latest", pkg)).Output()
	if err != nil {
		return "", fmt.Errorf("failed to fetch OpenCode latest version: %w", err)
	}
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
