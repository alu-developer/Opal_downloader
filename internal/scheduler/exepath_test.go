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

// findGitWorkingTreeRoot backs CheckExecutableStable's newest rejection
// class (Finding 2, docs/BACKLOG.md, 2026-08-11): a plain `go build .`
// output sitting in a git checkout is not caught by the go-build-cache or
// system-temp-dir checks above, but `git clean -xfd` deletes it exactly the
// same way - live on the maintainer's own machine, a 19-day-stale gitignored
// main.exe kept being what the schedule pointed at. Tested directly against
// t.TempDir() rather than through CheckExecutableStable, since any path
// under a real temp directory is already (correctly) rejected by the
// earlier system-temp-dir check, which would make a full-stack test unable
// to isolate this specific rejection reason.
func TestFindGitWorkingTreeRootFindsAMarkerAboveTheGivenDir(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root, "cmd", "opal-downloader")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findGitWorkingTreeRoot(nested); got != filepath.Clean(root) {
		t.Fatalf("expected to find the git root at %q, got %q", root, got)
	}
}

// A linked worktree's `.git` is a file (pointing at the real repo's
// worktree admin dir), not a directory - findGitWorkingTreeRoot must treat
// that as a working tree too, since this project's own CLAUDE.md has every
// autopilot session build and run from inside exactly such a worktree.
func TestFindGitWorkingTreeRootTreatsAGitFileAsAWorkingTreeToo(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: ../../.git/worktrees/example\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := findGitWorkingTreeRoot(root); got != filepath.Clean(root) {
		t.Fatalf("expected a .git file to count as a working tree root, got %q", got)
	}
}

func TestFindGitWorkingTreeRootReturnsEmptyWhenNoGitAncestorExists(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "no", "git", "here")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findGitWorkingTreeRoot(dir); got != "" {
		t.Fatalf("expected no git working tree to be found, got %q", got)
	}
}

// TestCheckExecutableStableRejectsAPathInsideThisProjectsOwnCheckout is the
// full-stack version of the three findGitWorkingTreeRoot tests above,
// proving the new check actually fires through the public API - using this
// package's own real, always-git working directory (wherever `go test`
// happens to run from) rather than a hardcoded path, so it stays portable.
func TestCheckExecutableStableRejectsAPathInsideThisProjectsOwnCheckout(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(wd, "opal-downloader.exe")

	gitErr := CheckExecutableStable(candidate)
	if gitErr == nil {
		t.Fatalf("expected %q (inside this package's own git checkout) to be rejected", candidate)
	}
	if !errors.Is(gitErr, ErrEphemeralExecutable) {
		t.Fatalf("expected ErrEphemeralExecutable, got %v", gitErr)
	}
	if !strings.Contains(gitErr.Error(), "git working directory") {
		t.Fatalf("expected the error to name the git working directory as the cause, got %q", gitErr)
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
