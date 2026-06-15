package jstools

import "testing"

func TestInstallDependencies_APKAndNPM(t *testing.T) {
	got := InstallDependencies([]string{"nodejs", "npm"}, []string{"bun", "foo@1.2.3"})
	want := "RUN apk add --no-cache nodejs npm && \\\n    npm install -g bun foo@1.2.3"
	if got != want {
		t.Fatalf("InstallDependencies() = %q, want %q", got, want)
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
