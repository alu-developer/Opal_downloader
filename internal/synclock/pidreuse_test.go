package synclock

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLockFile(t *testing.T, path string, info lockInfo) {
	t.Helper()
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal lock: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write lock: %v", err)
	}
}

// The PID-reuse hole: a lock file naming a PID that is alive but belongs to
// some *other*, later-started process must be reclaimed, not treated as a
// live sync. Before this was fixed, such a lock jammed every future sync
// with "a sync is already running (PID NNNNN)" and could only be cleared by
// deleting the file by hand.
//
// This test uses the current process as the "recycled" PID - it is
// definitely alive, and it definitely started after a lock stamped days ago,
// which is exactly the shape of a reused PID.
func TestAcquireReclaimsLockWhosePIDWasRecycled(t *testing.T) {
	if _, ok := processStartedAt(os.Getpid()); !ok {
		t.Skip("process start times unavailable on this platform; the plain liveness check is the documented fallback")
	}

	path := filepath.Join(t.TempDir(), "sync.lock")
	writeLockFile(t, path, lockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Add(-72 * time.Hour).Format(time.RFC3339),
	})

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("expected the recycled-PID lock to be reclaimed, got: %v", err)
	}
	defer release()
}

// The other side of the same coin: a lock written by a genuinely running
// process must still be honoured. Stamping it with "now" matches the real
// case, where the lock is written immediately after the process starts.
func TestAcquireHonoursLockFromGenuinelyRunningHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.lock")
	writeLockFile(t, path, lockInfo{
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC().Format(time.RFC3339),
	})

	if _, err := Acquire(path); err == nil {
		t.Fatal("expected a live holder's lock to be honoured, but it was reclaimed")
	}
}

// An unparseable timestamp must fail safe (keep the lock held) rather than
// reclaim a lock that might belong to a running sync.
func TestLockHolderStillRunningFailsSafeOnBadTimestamp(t *testing.T) {
	if !lockHolderStillRunning(lockInfo{PID: os.Getpid(), StartedAt: "not-a-timestamp"}) {
		t.Fatal("expected an unparseable lock timestamp to keep the lock held, not reclaim it")
	}
}
