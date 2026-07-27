package scheduler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrEphemeralExecutable is returned by Enable when the executable it was
// asked to register lives somewhere that will not survive - most commonly
// Go's build cache, which is what os.Executable() resolves to during a
// `go run .` session.
//
// This is not hypothetical tidiness. Found live on the maintainer's machine
// (2026-07-22): the registered task's action was
//
//	C:\Users\alois\AppData\Local\go-build\1e\1ec79afd...-d\opal-downloader.exe sync --scheduled
//
// Entries under go-build are content-addressed temporaries; `go clean
// -cache`, the cache's own size-based trimming, or simply building changed
// code all invalidate that path. When it goes, the scheduled sync stops
// running and says nothing: Task Scheduler records a launch failure the user
// never looks at, and opal-downloader's own scheduled-status file is only
// written *by a run that happened*, so a task that never launches leaves the
// GUI showing the last successful run forever.
//
// Registering a doomed path is worse than refusing to register, because the
// user walks away believing automatic sync is set up.
var ErrEphemeralExecutable = fmt.Errorf("this program is running from a temporary location")

// ephemeralPathMarkers are path segments that mean "this binary will not be
// here later". go-build is Go's build cache (a `go run` session); the temp
// directories cover the common "downloaded and ran it from Downloads/Temp"
// and installer-staging cases.
// goBuildCacheMarker is handled ahead of ephemeralPathMarkers so it can carry
// its own wording, which is why it is not in that list.
const goBuildCacheMarker = "go-build"

var ephemeralPathMarkers = []string{
	string(filepath.Separator) + "Temp" + string(filepath.Separator),
	string(filepath.Separator) + "tmp" + string(filepath.Separator),
}

// CheckExecutableStable reports whether exePath is somewhere a scheduled
// task can reasonably point at for months. It returns ErrEphemeralExecutable
// (wrapped with the offending path and what to do instead) when it is not.
//
// Deliberately a path-shape check rather than anything cleverer: the failure
// it prevents is silent and long-delayed, so a cheap conservative test that
// occasionally nags about an unusual-but-fine location is a far better trade
// than one that lets a doomed registration through.
func CheckExecutableStable(exePath string) error {
	normalized := filepath.Clean(exePath)

	// A `go run` build gets its own sentence. The generic "temporary
	// location" wording is accurate but reads as a fault, when it is simply
	// how `go run` works - reported by the maintainer (2026-07-27) after
	// seeing it while running `go run . gui`, where it is the expected state
	// and there is nothing wrong to fix.
	if strings.Contains(normalized, goBuildCacheMarker) {
		return fmt.Errorf(
			"%w: this is a `go run` build, which lives in Go's build cache and is deleted whenever that cache is trimmed. "+
				"Automatic sync needs an installed binary - run `go build .` and start opal-downloader from the built file",
			ErrEphemeralExecutable)
	}

	for _, marker := range ephemeralPathMarkers {
		if strings.Contains(normalized, marker) {
			return fmt.Errorf(
				"%w (%s), so a scheduled task pointing at it would silently stop working once that file is cleaned up. "+
					"Build or install opal-downloader to a permanent location and enable the schedule from there",
				ErrEphemeralExecutable, normalized)
		}
	}
	if tempDir := filepath.Clean(os.TempDir()); tempDir != "." && strings.HasPrefix(normalized, tempDir) {
		return fmt.Errorf(
			"%w (%s is inside the system temp directory), so a scheduled task pointing at it would silently stop working. "+
				"Build or install opal-downloader to a permanent location and enable the schedule from there",
			ErrEphemeralExecutable, normalized)
	}
	return nil
}

// withExecutableState fills Info's ExecutablePath/ExecutableMissing. An
// empty exePath (the task XML had no Exec Command, or could not be parsed)
// leaves both untouched, so an unknown path is never reported as a missing
// one.
//
// Lives here rather than beside its only caller in scheduler_windows.go
// because nothing about it is Windows-specific, and a build-tagged helper
// cannot be exercised by the platform-neutral tests in this package - which
// is exactly how it first shipped, and CI caught it.
func withExecutableState(info Info, exePath string) Info {
	if strings.TrimSpace(exePath) == "" {
		return info
	}
	info.ExecutablePath = exePath
	if _, err := os.Stat(exePath); err != nil {
		info.ExecutableMissing = true
	}
	return info
}
