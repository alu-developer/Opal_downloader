// Package synclock implements a cross-process, single-instance lock used to
// stop two opal-downloader "sync" runs from racing each other - e.g. a
// Task Scheduler-triggered `sync --scheduled` run overlapping with a manual
// CLI `sync` or a GUI-triggered sync started around the same time (see
// docs/scheduled-sync-plan.md's "Overlap guard" section, and the
// add-scheduled-daily-sync-task-scheduler-registration queue task this was
// built for).
//
// Deliberately a plain PID-stamped lock *file* under ~/.opal-downloader/,
// not the Win32 named mutex internal/scraper's session_lock_windows.go uses
// for a different, narrower purpose (serializing only ensureSession's own
// session-establishment phase). That mutex is scoped to a specific
// profileDir+stateFile pair and is Windows-only; this lock instead needs to
// cover an entire sync run (discovery + every file download) and work the
// same way (file-based) on every platform this package might someday run
// on, even though only isProcessAlive itself is platform-specific.
package synclock

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrHeld is returned by Acquire when another process already holds the
// lock and is confirmed still alive. Wrapped (via fmt.Errorf's %w) into a
// message that already states which PID and when it started, so the error
// is self-explanatory printed as-is; errors.Is(err, ErrHeld) additionally
// lets a caller (cmd/opal-downloader's Execute) recognize this specific
// failure mode for a distinguishable exit code without parsing free-text
// output - see the queue task's "must be consumable ... without needing to
// parse free-text stdout" acceptance criterion.
var ErrHeld = errors.New("a sync is already running")

// lockFileName is the file Acquire/DefaultPath write under
// ~/.opal-downloader/ to serialize sync runs across processes.
const lockFileName = "sync.lock"

// lockInfo is the JSON body written into the lock file: just enough to
// explain who's holding it and let a later Acquire call tell a genuinely
// live holder apart from a stale lock left behind by a process that
// crashed or was force-killed before it could release the lock itself.
// Deliberately carries no session/credential data whatsoever (CLAUDE.md:
// "credentials and session data never leave the machine unscrubbed" -
// this file is local-only anyway, but there is nothing sensitive in it to
// begin with).
type lockInfo struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

// DefaultPath returns the real, single lock-file path used in production:
// ~/.opal-downloader/sync.lock.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the sync lock: %w", err)
	}
	return filepath.Join(home, ".opal-downloader", lockFileName), nil
}

// AcquireDefault acquires the lock at DefaultPath(). This is what
// production code calls (see internal/syncer's acquireSyncLock
// indirection); tests exercise Acquire(path) directly against a temp
// directory instead.
func AcquireDefault() (release func(), err error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return Acquire(path)
}

// Acquire takes a single-instance lock at path, writing this process's PID
// and start time so a later Acquire call - possibly from a different
// process - can tell a genuinely-still-running holder apart from a stale
// lock a crashed/force-killed process left behind (checked via
// isProcessAlive, platform-specific: see synclock_windows.go/
// synclock_other.go).
//
// Returns a release func that must be called (typically via defer) once
// the caller's protected work is done, to remove the lock file on clean
// exit. A process that never calls it (crash, force-kill, `taskkill /F`)
// simply leaves a stale lock behind; the next Acquire call detects that via
// isProcessAlive and reclaims it rather than blocking forever.
func Acquire(path string) (release func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	// Two attempts: the first either succeeds outright (no existing lock)
	// or finds one to inspect; if that inspection concludes the lock is
	// stale (reclaimed and removed), the second attempt performs the actual
	// create. This intentionally does not loop indefinitely - a lock that's
	// stale on inspection but reappears immediately after removal (another
	// process winning a narrow race) is rare enough on a single-user desktop
	// tool that surfacing an error is preferable to spinning.
	for attempt := 0; attempt < 2; attempt++ {
		f, openErr := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if openErr == nil {
			info := lockInfo{PID: os.Getpid(), StartedAt: time.Now().UTC().Format(time.RFC3339)}
			encErr := json.NewEncoder(f).Encode(info)
			closeErr := f.Close()
			if encErr != nil || closeErr != nil {
				_ = os.Remove(path)
				if encErr != nil {
					return nil, fmt.Errorf("writing sync lock at %s: %w", path, encErr)
				}
				return nil, fmt.Errorf("writing sync lock at %s: %w", path, closeErr)
			}

			released := false
			return func() {
				if released {
					return
				}
				released = true
				_ = os.Remove(path)
			}, nil
		}
		if !os.IsExist(openErr) {
			return nil, fmt.Errorf("creating sync lock at %s: %w", path, openErr)
		}

		existing, readErr := readLock(path)
		if readErr != nil {
			// Unreadable/corrupt lock file (e.g. truncated by a crash
			// mid-write) - treat it the same as a stale lock and reclaim it
			// rather than refusing to ever sync again over a broken file.
			_ = os.Remove(path)
			continue
		}
		if lockHolderStillRunning(existing) {
			return nil, fmt.Errorf("%w (PID %d, started at %s) - likely today's scheduled sync or another opal-downloader command; wait for it to finish and try again", ErrHeld, existing.PID, existing.StartedAt)
		}
		// Stale lock: the recorded PID is no longer running, so whatever
		// process wrote it crashed or was force-killed without releasing it.
		// Reclaim it and retry the create.
		_ = os.Remove(path)
	}

	return nil, fmt.Errorf("could not acquire sync lock at %s", path)
}

// pidReuseTolerance is how much later than the lock's own timestamp a live
// process may have started while still being accepted as the genuine lock
// holder. The lock file is written immediately after the process starts, but
// the two timestamps come from different clocks/roundings (the recorded one
// is RFC3339, second-granularity), so they are never exactly equal. Anything
// beyond this window started long after the lock was written and therefore
// cannot be the process that wrote it.
const pidReuseTolerance = 2 * time.Minute

// lockHolderStillRunning reports whether the process recorded in info is
// genuinely still holding the lock.
//
// Liveness alone is not enough. Windows recycles PIDs, so once the process
// that wrote the lock exits without releasing it (a crash, a force-kill, or
// the GUI's own sync-cancel path killing the browser and process), an
// unrelated process can inherit its number - and from then on every sync
// refuses to start with "a sync is already running (PID NNNNN)", naming a
// process that has nothing to do with opal-downloader. There is no recovery
// short of deleting the lock file by hand, which no user would know to do.
//
// So when the recorded PID *is* alive, cross-check that it started no later
// than the lock itself (plus a tolerance): a process that began well after
// the lock was written cannot be the one that wrote it, and the lock is
// stale despite the PID being in use.
//
// Fails safe in both directions. If the start time cannot be read at all
// (processStartedAt reports ok=false, which is every non-Windows platform),
// this falls back to the plain liveness check - the exact behaviour that
// shipped before. And a parse failure on the recorded timestamp likewise
// keeps the lock held rather than reclaiming one that might be live.
func lockHolderStillRunning(info lockInfo) bool {
	if !isProcessAlive(info.PID) {
		return false
	}

	startedAt, ok := processStartedAt(info.PID)
	if !ok {
		return true
	}
	lockedAt, err := time.Parse(time.RFC3339, info.StartedAt)
	if err != nil {
		return true
	}

	return !startedAt.After(lockedAt.Add(pidReuseTolerance))
}

func readLock(path string) (lockInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return lockInfo{}, err
	}
	var info lockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return lockInfo{}, err
	}
	if info.PID <= 0 {
		return lockInfo{}, fmt.Errorf("lock file %s has no valid PID", path)
	}
	return info, nil
}
