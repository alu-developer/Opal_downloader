package synclock

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync.lock")

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("expected lock file to exist after Acquire, stat error: %v", statErr)
	}

	release()
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected lock file to be removed after release, stat error: %v", statErr)
	}

	// release must be safe to call more than once (defer + explicit call is
	// a real pattern callers might use).
	release()
}

func TestAcquireFailsWhileHeldBySelf(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync.lock")

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	defer release()

	_, err = Acquire(path)
	if err == nil {
		t.Fatal("expected second Acquire to fail while this process (still alive) holds the lock")
	}
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("expected errors.Is(err, ErrHeld), got: %v", err)
	}
}

func TestAcquireSucceedsAfterRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync.lock")

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	release()

	release2, err := Acquire(path)
	if err != nil {
		t.Fatalf("expected Acquire to succeed once the lock was released, got: %v", err)
	}
	release2()
}

// TestAcquireReclaimsStaleLockFromDeadProcess seeds a lock file recording a
// real PID that has genuinely exited (a throwaway helper process, waited
// out to completion), then confirms Acquire treats it as stale and reclaims
// it rather than reporting ErrHeld - this is the "process crashed without
// cleaning up" case the overlap guard must handle per
// docs/scheduled-sync-plan.md.
func TestAcquireReclaimsStaleLockFromDeadProcess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync.lock")

	deadPID := spawnAndWaitForExit(t)

	seedLock(t, path, deadPID)

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("expected Acquire to reclaim a stale lock left by a dead PID (%d), got: %v", deadPID, err)
	}
	release()
}

// TestAcquireReclaimsCorruptLockFile confirms an unreadable/truncated lock
// file (e.g. left behind by a crash mid-write) is treated as reclaimable
// rather than permanently wedging every future sync.
func TestAcquireReclaimsCorruptLockFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync.lock")

	if err := os.WriteFile(path, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("seeding corrupt lock file: %v", err)
	}

	release, err := Acquire(path)
	if err != nil {
		t.Fatalf("expected Acquire to reclaim a corrupt lock file, got: %v", err)
	}
	release()
}

func seedLock(t *testing.T, path string, pid int) {
	t.Helper()
	data, err := json.Marshal(lockInfo{PID: pid, StartedAt: time.Now().UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatalf("marshaling seeded lock: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating lock dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("seeding lock file: %v", err)
	}
}

// spawnAndWaitForExit starts a trivial short-lived child process and waits
// for it to fully exit, returning its PID - a real PID guaranteed to be
// dead (barring an extremely unlikely immediate OS PID-reuse race), so the
// stale-lock-reclaim path is exercised against a real dead process rather
// than a made-up number that might coincidentally collide with something
// alive on the machine running the test.
func spawnAndWaitForExit(t *testing.T) int {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/C", "exit 0")
	} else {
		cmd = exec.Command("true")
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting throwaway helper process: %v", err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Wait(); err != nil {
		t.Fatalf("waiting for throwaway helper process to exit: %v", err)
	}
	return pid
}
