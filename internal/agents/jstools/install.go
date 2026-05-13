package jstools

import "strings"

// InstallDependencies generates a Dockerfile RUN step for apk-installed
// packages and globally installed npm packages.
// Callers must pass trusted package specifiers only (shell-injection-prone).
func InstallDependencies(apkPackages, npmPackages []string) string {
	var parts []string

	if len(apkPackages) > 0 {
		parts = append(parts, "apk add --no-cache "+strings.Join(apkPackages, " "))
	}
	if len(npmPackages) > 0 {
		parts = append(parts, "npm install -g "+strings.Join(npmPackages, " "))
	}
	if len(parts) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("RUN ")
	for i, part := range parts {
		if i > 0 {
			b.WriteString(" && \\\n    ")
		}
		b.WriteString(part)
	}
	return b.String()
}
