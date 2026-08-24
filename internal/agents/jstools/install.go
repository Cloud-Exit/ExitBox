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
		parts = append(parts, npmGlobalInstallCommand(npmPackages))
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

// InstallBun generates a Dockerfile RUN step that installs Bun on Alpine.
func InstallBun() string {
	return `RUN set -e && \
    case "$(uname -m)" in \
        x86_64|amd64) BUN_PACKAGE="@oven/bun-linux-x64-musl" ;; \
        aarch64|arm64) BUN_PACKAGE="@oven/bun-linux-aarch64-musl" ;; \
        *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;; \
    esac && \
    ` + npmGlobalInstallCommand([]string{`"${BUN_PACKAGE}"`}) + ` && \
    install -m 755 "$(npm root -g)/${BUN_PACKAGE}/bin/bun" /usr/local/bin/bun && \
    ln -sf /usr/local/bin/bun /usr/local/bin/bunx && \
    bun --version`
}

func npmGlobalInstallCommand(npmPackages []string) string {
	// Some npm CLIs ship their platform binary as an optional dependency. Force
	// optional deps on even if user/global npm config omits them.
	return "npm_config_optional=true npm_config_omit= npm install -g --include=optional " + strings.Join(npmPackages, " ")
}
