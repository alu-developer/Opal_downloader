//go:build windows

package scraper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

// acquireSessionLock serializes ensureSession's session-establishment phase
// (checking/reading the saved session-state file, and - if that's missing or
// expired - launching the persistent-context login browser against the
// shared login profile, waiting for interactive login, and saving the fresh
// session state) across every opal-downloader process on this machine that
// targets the same real profileDir+stateFile pair.
//
// Root cause this closes (queue task
// fix-login-window-stays-open-during-crawl, 2026-07-16): before this lock
// existed, ensureSession's "check isUserDataDirLocked, then
// LaunchPersistentContext" sequence in launchBrowser (profile.go/session.go)
// was a TOCTOU race - two processes could both observe "not locked" before
// either had actually launched Chromium, then both call
// LaunchPersistentContext against the same login-profile userDataDir.
// Chromium's own ProcessSingleton would then raise/focus whichever instance
// won that race instead of cleanly erroring, which is a very plausible
// explanation for a browser window becoming visible/foregrounded during what
// should have been a fully headless sync/list run triggered by a *different*
// concurrent process. Separately, one process's saveState() (session.go)
// writing the shared session_state_file while another process's
// isAuthenticated()/StorageStatePath read it mid-write could produce a torn
// read, wrongly convincing the reading process its saved session was invalid
// and sending it down the visible interactive-login path even though a
// perfectly good session existed. Both races share the same fix: only one
// process may be inside ensureSession's session-establishment phase, for a
// given real profileDir+stateFile pair, at a time.
//
// This uses a named Win32 mutex rather than a lock file so a process that
// crashes (or is force-killed) while holding the lock can never deadlock a
// later process: Windows automatically releases a mutex whose owning process
// terminated, surfaced here as WAIT_ABANDONED, which this treats as a
// successful acquisition (the crashed process's ensureSession state - a
// half-written session file, an unfinished login - is exactly the kind of
// thing ensureSession already has to tolerate and recover from on its own,
// same as if it were the very first run).
//
// Deliberately scoped to only the session-establishment phase, not the crawl
// that follows: once ensureSession returns, each process's crawl runs
// against its own throwaway headless browser/context (or, for the standalone
// `login` command, has nothing left to do) with no further *local*
// shared-resource access. See docs/OPERATIONS.md for the resulting
// documented concurrency model.
//
// CORRECTION 2026-08-10: this comment used to end "...so multiple crawls are
// safe to run fully in parallel". That is only true locally, and the
// distinction cost real file loss twice. Every crawl's context is seeded from
// the same session_state_file cookie jar (ctxOpts.StorageStatePath,
// session.go) - so two local Chromium processes present one *identical*
// authenticated OPAL session identity to a Wicket backend that is stateful
// server-side per session. Both collision incidents in docs/BACKLOG.md
// (2026-08-02, 2026-08-06) are consistent with that and with nothing else:
// no ErrProfileLocked on either side, and on 2026-08-06 a silent collapse to
// 0 files for both courses that persisted across three further runs - which
// a purely local lock contention cannot produce, because a restarted process
// would have self-healed.
//
// So the lock this file provides is correct for what it covers, and it was
// never the thing that should have caught either incident. Overlapping
// *crawls* are guarded one level up instead, by the coarse
// ~/.opal-downloader/sync.lock (internal/synclock): held by `sync` for its
// whole duration, by `list` since 2026-08-10, and by every live probe test in
// this package since the same day (requireQuietAccount,
// probeoverlap_test.go). Do not reintroduce the "safe to run fully in
// parallel" claim without evidence that the *server-side* session tolerates
// it, which nothing has established.
func acquireSessionLock(profileDir, stateFile string, timeout time.Duration) (release func(), err error) {
	name, err := sessionMutexName(profileDir, stateFile)
	if err != nil {
		return nil, err
	}
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, fmt.Errorf("preparing cross-process session lock name: %w", err)
	}

	// windows.CreateMutex sets a non-nil err whenever the underlying Win32
	// CreateMutexW call reports ERROR_ALREADY_EXISTS - which is the normal,
	// expected outcome every time a second (or later) process opens a mutex
	// a first process already created, not a failure. The returned handle is
	// still perfectly valid in that case (Win32 CreateMutexW always returns
	// a usable handle to the existing object alongside that "error"). Only
	// a zero handle is a genuine failure.
	handle, err := windows.CreateMutex(nil, false, namePtr)
	if handle == 0 {
		return nil, fmt.Errorf("creating cross-process session lock: %w", err)
	}

	waitMs := uint32(timeout / time.Millisecond)
	event, waitErr := windows.WaitForSingleObject(handle, waitMs)
	switch {
	case waitErr != nil:
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("waiting for cross-process session lock: %w", waitErr)
	case event == windows.WAIT_OBJECT_0, event == windows.WAIT_ABANDONED:
		released := false
		return func() {
			if released {
				return
			}
			released = true
			_ = windows.ReleaseMutex(handle)
			_ = windows.CloseHandle(handle)
		}, nil
	case event == uint32(windows.WAIT_TIMEOUT):
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("%w: another opal-downloader process appears to be logging in or checking its session against the same browser profile/session file right now - wait for it to finish and try again", ErrProfileLocked)
	default:
		_ = windows.CloseHandle(handle)
		return nil, fmt.Errorf("waiting for cross-process session lock: unexpected wait result %d", event)
	}
}

// sessionMutexName derives a stable, filesystem-and-case-insensitive Win32
// kernel object name from profileDir+stateFile, so two opal-downloader
// processes/worktrees whose config.yaml both resolve to the exact same real
// paths (as can happen today - see this fix's queue task for a real example
// of two worktrees sharing session_state_file/browser_user_data_dir) contend
// on the same mutex, while processes configured against different
// paths (e.g. a test fixture) never do. Hashed rather than used verbatim
// because Win32 object names reject backslashes outside of namespace
// prefixes, and paths routinely contain them. "Local\" scopes the mutex to
// this login session, which is the right scope for a single-user desktop
// tool - it does not need to serialize across different Windows user
// sessions on the same machine.
func sessionMutexName(profileDir, stateFile string) (string, error) {
	key := strings.ToLower(profileDir) + "|" + strings.ToLower(stateFile)
	sum := sha256.Sum256([]byte(key))
	return `Local\opal-downloader-session-` + hex.EncodeToString(sum[:16]), nil
}
