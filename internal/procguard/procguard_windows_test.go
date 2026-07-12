//go:build windows

// Regression test for the "browser window survives a force-killed
// opal-downloader" bug fixed in PR #53 (commit 30f5fd0). See
// procguard_windows.go's doc comment for the full background.
//
// The property under test - "every descendant of this process dies the
// instant this process is killed, not just on graceful exit" - cannot be
// exercised by the test binary itself: if the test process taskkills
// itself, it stops running before it can observe the outcome. Instead this
// uses the standard Go self-exec test pattern (as used throughout the Go
// standard library, e.g. os/exec's TestHelperProcess): the test binary
// re-invokes itself as a subprocess with a marker environment variable set,
// so `go test` selects a special code path instead of running the normal
// test suite. That "helper process" calls
// EnsureChildProcessesDieWithParent(), spawns a real long-lived child
// (`cmd.exe /c timeout /t 30`), prints the child's PID, and then blocks
// forever (so the outer test can taskkill /F it while it's still alive).
// The outer test then force-kills the helper process (simulating
// `taskkill /F` on opal-downloader itself, or a crash) and polls whether
// the grandchild PID is still alive, asserting it disappears within a few
// seconds - which only happens if the Job Object's
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE is actually wired up correctly.
package procguard

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// helperProcessEnv is the marker environment variable that switches this
// test binary into "helper process" mode when re-invoked via os.Args[0].
const helperProcessEnv = "OPAL_PROCGUARD_HELPER_PROCESS"

// TestHelperProcess is not a real test. It is invoked by re-executing the
// test binary with helperProcessEnv set, following the standard Go
// self-exec pattern (see os/exec's TestHelperProcess for prior art). When
// helperProcessEnv is unset, it does nothing so the normal `go test` run
// is unaffected.
func TestHelperProcess(t *testing.T) {
	if os.Getenv(helperProcessEnv) != "1" {
		return
	}

	EnsureChildProcessesDieWithParent()

	cmd := exec.Command("cmd.exe", "/c", "timeout", "/t", "30", "/nobreak")
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		fmt.Println("START_ERROR:", err)
		os.Exit(1)
	}

	fmt.Println("CHILD_PID:", cmd.Process.Pid)
	os.Stdout.Sync()

	// Block "forever" (bounded so a stuck test doesn't hang CI
	// indefinitely if the outer test fails to kill us) so the outer test
	// can taskkill /F this process while the child is still alive.
	time.Sleep(60 * time.Second)
}

// processExists reports whether a process with the given PID is currently
// running, using OpenProcess rather than shelling out to tasklist.
func processExists(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var exitCode uint32
	if err := windows.GetExitCodeProcess(h, &exitCode); err != nil {
		return false
	}
	// STILL_ACTIVE == 259.
	return exitCode == 259
}

// TestEnsureChildProcessesDieWithParent_KillsGrandchildOnForceKill
// reproduces the exact scenario PR #53 fixed: a process that called
// EnsureChildProcessesDieWithParent() and then launched a long-lived child
// is force-killed (taskkill /F, simulating a crash or a queue-run
// force-stop) instead of exiting gracefully. The child must not survive
// that - a regression here (e.g. EnsureChildProcessesDieWithParent moving
// to run after the browser launches, or JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
// getting dropped from the job's limit flags) would leave the child process
// (in production, the Chromium/Brave window) orphaned and still running.
func TestEnsureChildProcessesDieWithParent_KillsGrandchildOnForceKill(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}

	helper := exec.Command(exe, "-test.run=TestHelperProcess", "-test.v")
	helper.Env = append(os.Environ(), helperProcessEnv+"=1")
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	if err := helper.Start(); err != nil {
		t.Fatalf("failed to start helper process: %v", err)
	}
	helperPID := helper.Process.Pid

	// Make sure the helper (and anything it spawns) is cleaned up even if
	// an assertion below fails partway through.
	defer func() {
		_ = exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(helperPID)).Run()
	}()

	// Read the helper's stdout until it reports the grandchild PID it
	// spawned, or the START_ERROR marker.
	var childPID int
	scanner := bufio.NewScanner(stdout)
	found := make(chan error, 1)
	go func() {
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "CHILD_PID:") {
				pidStr := strings.TrimSpace(strings.TrimPrefix(line, "CHILD_PID:"))
				pid, err := strconv.Atoi(pidStr)
				if err != nil {
					found <- fmt.Errorf("unparseable child PID %q: %w", pidStr, err)
					return
				}
				childPID = pid
				found <- nil
				return
			}
			if strings.HasPrefix(line, "START_ERROR:") {
				found <- fmt.Errorf("helper process failed to start its child: %s", line)
				return
			}
		}
		found <- fmt.Errorf("helper process exited/closed stdout before reporting a child PID: %v", scanner.Err())
	}()

	select {
	case err := <-found:
		if err != nil {
			t.Fatalf("%v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for helper process to report its child PID")
	}

	if !processExists(childPID) {
		t.Fatalf("grandchild process (PID %d) is not running right after helper reported it", childPID)
	}

	// Simulate opal-downloader being force-killed: taskkill /F the helper
	// process only (not /T - we want to confirm the Job Object mechanism
	// kills the grandchild, not taskkill's own tree-kill).
	killCmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(helperPID))
	if out, err := killCmd.CombinedOutput(); err != nil {
		t.Fatalf("taskkill /F on helper process (PID %d) failed: %v (%s)", helperPID, err, out)
	}

	// Poll for the grandchild to disappear. Job Object cleanup is
	// effectively immediate, but give it a few seconds of margin.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if !processExists(childPID) {
			// Success: reap the helper's exit and return.
			_ = helper.Wait()
			return
		}
		time.Sleep(200 * time.Millisecond)
	}

	_ = helper.Wait()
	t.Fatalf("grandchild process (PID %d) is still running %s after its parent was force-killed - "+
		"EnsureChildProcessesDieWithParent's Job Object is not killing descendants on force-kill", childPID, 10*time.Second)
}
