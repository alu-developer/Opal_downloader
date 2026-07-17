//go:build windows

// Regression test for the cross-process session-establishment race described
// in queue task fix-login-window-stays-open-during-crawl (2026-07-16): two
// opal-downloader processes pointed at the same real
// browser_user_data_dir/session_state_file could both fall through
// ensureSession's checks concurrently, one plausible explanation for a
// browser window becoming visible during what should have been a headless
// sync/list run triggered by a separate, concurrent process. See
// session_lock_windows.go's doc comment for the full root-cause writeup.
//
// This uses the same self-exec helper-process pattern as
// internal/procguard/procguard_windows_test.go: acquireSessionLock's
// cross-process behavior genuinely requires a second OS process (a Win32
// named mutex is owned per-thread, so two goroutines in one process - even
// with runtime.LockOSThread - are not a faithful stand-in for "two separate
// opal-downloader.exe processes").
package scraper

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// sessionLockHelperEnv is the marker environment variable that switches this
// test binary into "helper process" mode when re-invoked via os.Args[0].
const sessionLockHelperEnv = "OPAL_SESSION_LOCK_HELPER_PROCESS"

// TestSessionLockHelperProcess is not a real test; see TestHelperProcess in
// procguard_windows_test.go for the pattern this follows. When invoked as a
// helper (env var set), it acquires the session lock for the profileDir/
// stateFile pair given via OPAL_SESSION_LOCK_PROFILE_DIR/
// OPAL_SESSION_LOCK_STATE_FILE, prints ACQUIRED once it has the lock, holds
// it for OPAL_SESSION_LOCK_HOLD_MS milliseconds, then releases and exits.
func TestSessionLockHelperProcess(t *testing.T) {
	if os.Getenv(sessionLockHelperEnv) != "1" {
		return
	}

	profileDir := os.Getenv("OPAL_SESSION_LOCK_PROFILE_DIR")
	stateFile := os.Getenv("OPAL_SESSION_LOCK_STATE_FILE")
	holdMs := 2000
	if v := os.Getenv("OPAL_SESSION_LOCK_HOLD_MS"); v != "" {
		fmt.Sscanf(v, "%d", &holdMs)
	}

	release, err := acquireSessionLock(profileDir, stateFile, 10*time.Second)
	if err != nil {
		fmt.Println("ACQUIRE_ERROR:", err)
		os.Exit(1)
	}
	fmt.Println("ACQUIRED")
	os.Stdout.Sync()

	time.Sleep(time.Duration(holdMs) * time.Millisecond)
	release()
	fmt.Println("RELEASED")
	os.Stdout.Sync()
}

// runSessionLockHelper starts the helper process for the given key and
// returns it once it has reported ACQUIRED (or fails the test on error/
// timeout).
func runSessionLockHelper(t *testing.T, profileDir, stateFile string, holdMs int) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	cmd := exec.Command(exe, "-test.run=TestSessionLockHelperProcess", "-test.v")
	cmd.Env = append(os.Environ(),
		sessionLockHelperEnv+"=1",
		"OPAL_SESSION_LOCK_PROFILE_DIR="+profileDir,
		"OPAL_SESSION_LOCK_STATE_FILE="+stateFile,
		fmt.Sprintf("OPAL_SESSION_LOCK_HOLD_MS=%d", holdMs),
	)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("failed to start session-lock helper process: %v", err)
	}

	found := make(chan error, 1)
	scanner := bufio.NewScanner(stdout)
	go func() {
		// Keep draining stdout for the helper's entire lifetime, even after
		// the line we're waiting for shows up: `go test -v` (and the
		// helper's own later Println("RELEASED")) keeps writing to this pipe
		// long after ACQUIRED is seen, and os/exec.Cmd docs are explicit that
		// stopping reads before the child exits can make the child block on
		// a full pipe buffer - which would in turn make the caller's
		// cmd.Wait() hang, exactly the kind of self-inflicted deadlock this
		// test must not have.
		reported := false
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if reported {
				continue
			}
			if line == "ACQUIRED" {
				reported = true
				found <- nil
				continue
			}
			if strings.HasPrefix(line, "ACQUIRE_ERROR:") {
				reported = true
				found <- fmt.Errorf("helper failed to acquire lock: %s", line)
			}
		}
		if !reported {
			found <- fmt.Errorf("helper exited/closed stdout before reporting ACQUIRED: %v", scanner.Err())
		}
	}()

	select {
	case err := <-found:
		if err != nil {
			t.Fatalf("%v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for session-lock helper process to acquire the lock")
	}

	return cmd
}

// TestAcquireSessionLockSerializesAcrossProcesses reproduces the actual race:
// a second, separate opal-downloader process (the helper) holding the lock
// for the same real profileDir+stateFile pair must block this process's own
// acquireSessionLock call until the helper releases it.
func TestAcquireSessionLockSerializesAcrossProcesses(t *testing.T) {
	profileDir := `C:\fake\opal-downloader-test\login-profile`
	stateFile := `C:\fake\opal-downloader-test\storage-state.json`

	helper := runSessionLockHelper(t, profileDir, stateFile, 2000)
	defer func() { _ = helper.Wait() }()

	start := time.Now()
	release, err := acquireSessionLock(profileDir, stateFile, 500*time.Millisecond)
	if err == nil {
		release()
		t.Fatalf("expected acquireSessionLock to time out while the helper process held the lock, but it succeeded after %s", time.Since(start))
	}
	if !strings.Contains(err.Error(), "another opal-downloader process") {
		t.Fatalf("expected a clear 'another opal-downloader process' timeout error, got: %v", err)
	}

	// The helper releases ~2s after it acquired; wait it out, then confirm
	// the lock is acquirable again - i.e. the timeout above didn't leave
	// anything wedged.
	release, err = acquireSessionLock(profileDir, stateFile, 5*time.Second)
	if err != nil {
		t.Fatalf("expected acquireSessionLock to succeed once the helper process released the lock, got: %v", err)
	}
	release()
}

// TestAcquireSessionLockDoesNotSerializeDifferentProfiles confirms two
// processes targeting different profileDir/stateFile pairs (e.g. two
// isolated test configs, or genuinely different real profiles) never contend
// with each other - only processes sharing the exact same real paths should
// serialize.
func TestAcquireSessionLockDoesNotSerializeDifferentProfiles(t *testing.T) {
	helperProfileDir := `C:\fake\opal-downloader-test\login-profile-A`
	helperStateFile := `C:\fake\opal-downloader-test\storage-state-A.json`
	otherProfileDir := `C:\fake\opal-downloader-test\login-profile-B`
	otherStateFile := `C:\fake\opal-downloader-test\storage-state-B.json`

	helper := runSessionLockHelper(t, helperProfileDir, helperStateFile, 3000)
	defer func() { _ = helper.Wait() }()

	// Should succeed immediately (well within the helper's 3s hold) since it
	// targets a different profile/state-file pair.
	release, err := acquireSessionLock(otherProfileDir, otherStateFile, 1*time.Second)
	if err != nil {
		t.Fatalf("expected acquireSessionLock for a different profile/state-file pair to succeed while an unrelated helper holds its own lock, got: %v", err)
	}
	release()
}
