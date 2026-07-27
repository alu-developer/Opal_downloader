package scheduler

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The exact shape found live on the maintainer's machine: a scheduled task
// registered against a Go build-cache binary, which vanishes on the next
// `go clean -cache` or cache trim and takes the scheduled sync with it.
func TestCheckExecutableStableRejectsGoBuildCache(t *testing.T) {
	err := CheckExecutableStable(`C:\Users\alois\AppData\Local\go-build\1e\1ec79afd-d\opal-downloader.exe`)
	if err == nil {
		t.Fatal("expected a go-build cache path to be rejected")
	}
	if !errors.Is(err, ErrEphemeralExecutable) {
		t.Fatalf("expected ErrEphemeralExecutable, got %v", err)
	}
	// The message has to tell the user what to do, not just that it failed.
	// A `go run` build gets the concrete instruction rather than the generic
	// "build to a permanent location", because it is a specific, recognisable
	// situation with a specific answer - the maintainer hit exactly this
	// (2026-07-27) running `go run . gui` and read it as a fault.
	if !strings.Contains(err.Error(), "go build .") {
		t.Fatalf("error should name the command that fixes it, got %q", err)
	}
	// It must also not read as breakage. "go run" names the situation; without
	// it the user is left with "temporary location" and no idea why.
	if !strings.Contains(err.Error(), "`go run` build") {
		t.Fatalf("error should say this is a go run build, got %q", err)
	}
}

func TestCheckExecutableStableRejectsSystemTempDir(t *testing.T) {
	candidate := filepath.Join(os.TempDir(), "opal-downloader.exe")
	if err := CheckExecutableStable(candidate); err == nil {
		t.Fatalf("expected a path inside the system temp dir (%s) to be rejected", candidate)
	}
}

func TestCheckExecutableStableAcceptsNormalInstallPaths(t *testing.T) {
	for _, good := range []string{
		`C:\Program Files\opal-downloader\opal-downloader.exe`,
		`C:\Users\alois\Apps\opal-downloader.exe`,
		`/usr/local/bin/opal-downloader`,
		`/home/alois/bin/opal-downloader`,
	} {
		if err := CheckExecutableStable(good); err != nil {
			t.Fatalf("expected %q to be accepted, got %v", good, err)
		}
	}
}

// withExecutableState must never report an unknown path as a missing one -
// that would show a scary "your schedule is broken" banner on nothing.
func TestWithExecutableStateTreatsUnknownPathAsNotMissing(t *testing.T) {
	info := withExecutableState(Info{Registered: true}, "")
	if info.ExecutableMissing {
		t.Fatal("an unknown executable path must not be reported as missing")
	}
	if info.ExecutablePath != "" {
		t.Fatalf("expected an empty path, got %q", info.ExecutablePath)
	}
}

func TestWithExecutableStateDetectsMissingAndPresent(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "gone.exe")
	if info := withExecutableState(Info{Registered: true}, missing); !info.ExecutableMissing {
		t.Fatal("expected a non-existent registered binary to be reported missing")
	}

	present := filepath.Join(t.TempDir(), "here.exe")
	if err := os.WriteFile(present, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if info := withExecutableState(Info{Registered: true}, present); info.ExecutableMissing {
		t.Fatal("expected an existing registered binary not to be reported missing")
	}
}
