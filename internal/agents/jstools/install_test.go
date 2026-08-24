package jstools

import (
	"strings"
	"testing"
)

func TestInstallDependencies_APKAndNPM(t *testing.T) {
	got := InstallDependencies([]string{"nodejs", "npm"}, []string{"bun", "foo@1.2.3"})
	want := "RUN apk add --no-cache nodejs npm && \\\n    npm_config_optional=true npm_config_omit= npm install -g --include=optional bun foo@1.2.3"
	if got != want {
		t.Fatalf("InstallDependencies() = %q, want %q", got, want)
	}
}

func TestInstallDependencies_OnlyNPMIncludesOptionalDeps(t *testing.T) {
	got := InstallDependencies(nil, []string{"bun"})
	want := "RUN npm_config_optional=true npm_config_omit= npm install -g --include=optional bun"
	if got != want {
		t.Fatalf("InstallDependencies() = %q, want %q", got, want)
	}
}

func TestInstallBunUsesAlpineMuslPackages(t *testing.T) {
	got := InstallBun()
	for _, want := range []string{
		`@oven/bun-linux-x64-musl`,
		`@oven/bun-linux-aarch64-musl`,
		`npm_config_optional=true npm_config_omit= npm install -g --include=optional "${BUN_PACKAGE}"`,
		`install -m 755 "$(npm root -g)/${BUN_PACKAGE}/bin/bun" /usr/local/bin/bun`,
		`ln -sf /usr/local/bin/bun /usr/local/bin/bunx`,
		`bun --version`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("InstallBun() missing %q in:\n%s", want, got)
		}
	}
}

func TestInstallDependencies_OnlyAPK(t *testing.T) {
	got := InstallDependencies([]string{"nodejs", "npm"}, nil)
	want := "RUN apk add --no-cache nodejs npm"
	if got != want {
		t.Fatalf("InstallDependencies() = %q, want %q", got, want)
	}
}

func TestInstallDependencies_Empty(t *testing.T) {
	if got := InstallDependencies(nil, nil); got != "" {
		t.Fatalf("InstallDependencies() = %q, want empty string", got)
	}
}
