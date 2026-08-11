package opaldownloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/statuslog"
	"github.com/alu-developer/opal-downloader/internal/syncer"
	"github.com/alu-developer/opal-downloader/internal/synclock"
	"github.com/alu-developer/opal-downloader/internal/updater"
	"github.com/alu-developer/opal-downloader/internal/visitlog"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading pipe: %v", err)
	}
	return string(out)
}

func TestPrintUpdateFooter_NewerVersionAvailable(t *testing.T) {
	origFn := updaterCheckLatest
	origVersion := buildVersion
	defer func() {
		updaterCheckLatest = origFn
		buildVersion = origVersion
	}()

	buildVersion = "0.1.0"
	updaterCheckLatest = func(ctx context.Context, currentVersion string) (updater.Release, error) {
		if currentVersion != "0.1.0" {
			t.Errorf("expected currentVersion %q, got %q", "0.1.0", currentVersion)
		}
		return updater.Release{
			TagName: "v0.2.0",
			Version: "0.2.0",
			HTMLURL: "https://github.example/releases/tag/v0.2.0",
			IsNewer: true,
		}, nil
	}

	out := captureStdout(t, printUpdateFooter)

	if !strings.Contains(out, "v0.2.0") {
		t.Errorf("expected output to mention v0.2.0, got %q", out)
	}
	if !strings.Contains(out, "https://github.example/releases/tag/v0.2.0") {
		t.Errorf("expected output to contain release URL, got %q", out)
	}
}

func TestPrintUpdateFooter_NoNewerVersion(t *testing.T) {
	origFn := updaterCheckLatest
	defer func() { updaterCheckLatest = origFn }()

	updaterCheckLatest = func(ctx context.Context, currentVersion string) (updater.Release, error) {
		return updater.Release{Version: "0.1.0", IsNewer: false}, nil
	}

	out := captureStdout(t, printUpdateFooter)

	if out != "" {
		t.Errorf("expected no output when no newer version is available, got %q", out)
	}
}

func TestPrintUpdateFooter_ErrorIsSilent(t *testing.T) {
	origFn := updaterCheckLatest
	defer func() { updaterCheckLatest = origFn }()

	updaterCheckLatest = func(ctx context.Context, currentVersion string) (updater.Release, error) {
		return updater.Release{}, errors.New("network unreachable")
	}

	out := captureStdout(t, printUpdateFooter)

	if out != "" {
		t.Errorf("expected no output on error, got %q", out)
	}
}

func TestPrintUpdateFooter_DoesNotHangOnSlowCheck(t *testing.T) {
	origFn := updaterCheckLatest
	defer func() { updaterCheckLatest = origFn }()

	// Simulate an unreachable/slow API: block until the context deadline
	// fires, then return its error - this exercises the same code path a
	// real offline/slow network hit would take, without an actual network
	// dependency in the test.
	updaterCheckLatest = func(ctx context.Context, currentVersion string) (updater.Release, error) {
		<-ctx.Done()
		return updater.Release{}, ctx.Err()
	}

	done := make(chan string, 1)
	go func() {
		done <- captureStdout(t, printUpdateFooter)
	}()

	select {
	case out := <-done:
		if out != "" {
			t.Errorf("expected no output when check times out, got %q", out)
		}
	case <-time.After(updateCheckTimeout + 2*time.Second):
		t.Fatal("printUpdateFooter did not return within the expected timeout window")
	}
}

// TestPersistVisitLogNoopWhenNoRecords exercises persistVisitLog's early-out
// for a scraper that recorded nothing (e.g. a scrape that failed before
// visiting any section) - it must not create a log file at all.
func TestPersistVisitLogNoopWhenNoRecords(t *testing.T) {
	dir := t.TempDir()
	sc := scraper.New("", "")

	if err := persistVisitLog(sc, dir); err != nil {
		t.Fatalf("persistVisitLog with no records returned error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, visitlog.FileName)); !os.IsNotExist(err) {
		t.Fatalf("expected no visit log file to be created, stat err: %v", err)
	}
}

// TestRunListVisitReportReadsExistingLogWithoutScraping exercises the
// `list --visit-report` CLI path end-to-end against a real config.yaml and a
// pre-seeded visit log: it must print the aggregate report and return
// without ever constructing a scraper (i.e. no browser/login attempted) -
// confirmed here by the command succeeding entirely offline in a unit test.
func TestRunListVisitReportReadsExistingLogWithoutScraping(t *testing.T) {
	dir := t.TempDir()
	downloadPath := filepath.Join(dir, "downloads")
	if err := os.MkdirAll(downloadPath, 0o755); err != nil {
		t.Fatalf("MkdirAll downloadPath: %v", err)
	}

	logPath := filepath.Join(downloadPath, visitlog.FileName)
	records := []visitlog.Record{
		{Course: "Analysis", SectionTitle: "Forum", SectionURL: "https://opal/forum", FilesFound: 0, Timestamp: time.Now()},
		{Course: "Analysis", SectionTitle: "Forum", SectionURL: "https://opal/forum", FilesFound: 0, Timestamp: time.Now()},
	}
	if err := visitlog.Append(logPath, records); err != nil {
		t.Fatalf("seeding visit log: %v", err)
	}

	configPath := filepath.Join(dir, "config.yaml")
	configYAML := "download_path: " + downloadPath + "\ncourses:\n  - \"*\"\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("writing config.yaml: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runList([]string{"--config", configPath, "--visit-report"}); err != nil {
			t.Errorf("runList --visit-report returned error: %v", err)
		}
	})

	if !strings.Contains(out, "Forum") {
		t.Fatalf("expected report to mention the seeded Forum section, got:\n%s", out)
	}
	if !strings.Contains(out, "empty on all 2 visit(s)") {
		t.Fatalf("expected report to flag Forum as always-empty, got:\n%s", out)
	}
}

// TestBuildScheduledRunStatus_Success covers the common case: no error, no
// per-file errors, headless-only login path (saved session was still
// valid).
func TestBuildScheduledRunStatus_Success(t *testing.T) {
	now := time.Now()
	stats := syncer.Stats{Downloaded: 12, Skipped: 3}

	status := buildScheduledRunStatus(now, nil, stats, true, false)

	if status.Outcome != statuslog.OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %q", status.Outcome)
	}
	if status.LoginPath != statuslog.LoginPathHeadlessOnly {
		t.Fatalf("expected LoginPathHeadlessOnly, got %q", status.LoginPath)
	}
	if status.FilesDownloaded != 12 || status.FilesSkipped != 3 || status.FilesErrored != 0 {
		t.Fatalf("unexpected file counts: %+v", status)
	}
	if !status.Timestamp.Equal(now) {
		t.Fatalf("expected timestamp %v, got %v", now, status.Timestamp)
	}
}

// TestBuildScheduledRunStatus_InteractiveRelogin covers a successful run
// that had to fall through to interactive login first (saved session
// expired) - the LoginPath field this task exists to instrument.
func TestBuildScheduledRunStatus_InteractiveRelogin(t *testing.T) {
	status := buildScheduledRunStatus(time.Now(), nil, syncer.Stats{Downloaded: 5}, true, true)

	if status.LoginPath != statuslog.LoginPathInteractiveRelogin {
		t.Fatalf("expected LoginPathInteractiveRelogin, got %q", status.LoginPath)
	}
	if status.Outcome != statuslog.OutcomeSuccess {
		t.Fatalf("expected OutcomeSuccess, got %q", status.Outcome)
	}
}

// TestBuildScheduledRunStatus_Partial covers a run that completed (no
// returned error) but had per-file download errors.
func TestBuildScheduledRunStatus_Partial(t *testing.T) {
	stats := syncer.Stats{Downloaded: 8, Skipped: 1, Errors: 2}
	status := buildScheduledRunStatus(time.Now(), nil, stats, true, false)

	if status.Outcome != statuslog.OutcomePartial {
		t.Fatalf("expected OutcomePartial, got %q", status.Outcome)
	}
	if !strings.Contains(status.Message, "2 file error") {
		t.Fatalf("expected message to mention the file error count, got %q", status.Message)
	}
}

// TestBuildScheduledRunStatus_Failure covers a hard failure (e.g. network
// unreachable) - the message should be sanitized (see statuslog.SanitizeMessage)
// but otherwise pass through.
func TestBuildScheduledRunStatus_Failure(t *testing.T) {
	runErr := errors.New("could not reach OPAL at https://bildungsportal.sachsen.de/opal/ - check your internet connection and opal_url in config.yaml")
	status := buildScheduledRunStatus(time.Now(), runErr, syncer.Stats{}, true, false)

	if status.Outcome != statuslog.OutcomeFailure {
		t.Fatalf("expected OutcomeFailure, got %q", status.Outcome)
	}
	if !strings.Contains(status.Message, "could not reach OPAL") {
		t.Fatalf("expected message to preserve the safe, already-wrapped error text, got %q", status.Message)
	}
}

// TestBuildScheduledRunStatus_TUFastNotReadyIsLoginPathUnknown covers the
// pre-flight failure mode (scraper.EnsureTUFastPresent) that happens before
// any scraper/session is ever created - attemptedLogin is still false in
// this case (runSync returns before reaching syncer.SyncCourses), and the
// sentinel error itself should also force LoginPathUnknown regardless.
func TestBuildScheduledRunStatus_TUFastNotReadyIsLoginPathUnknown(t *testing.T) {
	runErr := fmt.Errorf("%w: TU-Fast extension not detected", scraper.ErrTUFastNotReady)
	status := buildScheduledRunStatus(time.Now(), runErr, syncer.Stats{}, false, false)

	if status.LoginPath != statuslog.LoginPathUnknown {
		t.Fatalf("expected LoginPathUnknown, got %q", status.LoginPath)
	}
	if status.Outcome != statuslog.OutcomeFailure {
		t.Fatalf("expected OutcomeFailure, got %q", status.Outcome)
	}
}

// TestBuildScheduledRunStatus_SyncLockHeldIsLoginPathUnknown covers the
// overlap-guard failure mode (synclock.ErrHeld): even though attemptedLogin
// might be true by the time this error surfaces (SyncCourses was called),
// the lock is acquired before ensureSession ever runs, so LoginPath must
// still be Unknown rather than misreporting headless-only.
func TestBuildScheduledRunStatus_SyncLockHeldIsLoginPathUnknown(t *testing.T) {
	runErr := fmt.Errorf("%w (PID 1234, started at 2026-07-17T06:00:00Z)", synclock.ErrHeld)
	status := buildScheduledRunStatus(time.Now(), runErr, syncer.Stats{}, true, false)

	if status.LoginPath != statuslog.LoginPathUnknown {
		t.Fatalf("expected LoginPathUnknown, got %q", status.LoginPath)
	}
}

// TestBuildScheduledRunStatus_SyncLockHeldIsSkippedNotFailure covers the
// live-reported bug (2026-07-19, docs/BACKLOG.md's "sync already running"
// entry): another opal-downloader process (typically the GUI's own
// in-process "Sync now" job) holding the lock when the scheduled run tries
// to start is routine overlap, not an incident - it must not be classified
// as OutcomeFailure (which would fire the scheduled-failure toast
// notification and the GUI banner over something the user can't act on).
func TestBuildScheduledRunStatus_SyncLockHeldIsSkippedNotFailure(t *testing.T) {
	runErr := fmt.Errorf("%w (PID 1234, started at 2026-07-17T06:00:00Z)", synclock.ErrHeld)
	status := buildScheduledRunStatus(time.Now(), runErr, syncer.Stats{}, true, false)

	if status.Outcome != statuslog.OutcomeSkipped {
		t.Fatalf("expected OutcomeSkipped, got %q", status.Outcome)
	}
	if !strings.Contains(status.Message, "PID 1234") {
		t.Fatalf("expected the message to still name the holding PID for diagnosis, got %q", status.Message)
	}
}

// `status` used to print the download path unchecked, so a broken path (a
// typo'd drive letter, a path under a file) validated fine and only
// surfaced minutes later inside a real sync. Both cases below cover that a
// writable path now says so, and an unwritable one is caught at `status`
// time instead.
func TestRunStatus_ReportsWritableDownloadPath(t *testing.T) {
	dir := t.TempDir()
	downloadPath := filepath.Join(dir, "downloads")
	stateFile := filepath.Join(dir, "state.json")

	configPath := filepath.Join(dir, "config.yaml")
	configYAML := "download_path: " + downloadPath + "\nsession_state_file: " + stateFile + "\ncourses:\n  - \"*\"\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("writing config.yaml: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runStatus([]string{"--config", configPath}); err != nil {
			t.Errorf("runStatus returned error: %v", err)
		}
	})

	wantLine := "Download path: " + downloadPath + " (OK)"
	if !strings.Contains(out, wantLine) {
		t.Fatalf("expected %q, got:\n%s", wantLine, out)
	}
	if info, err := os.Stat(downloadPath); err != nil || !info.IsDir() {
		t.Fatalf("expected download path to have been created, stat: %v", err)
	}
}

func TestRunStatus_FlagsUnwritableDownloadPath(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")

	// A regular file where a directory segment needs to be: MkdirAll fails
	// under it on both Windows and Unix, the same way a typo'd drive letter
	// (the real-world report) fails - a directory simply cannot be created
	// there.
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("writing blocker file: %v", err)
	}
	downloadPath := filepath.Join(blocker, "downloads")

	configPath := filepath.Join(dir, "config.yaml")
	configYAML := "download_path: " + downloadPath + "\nsession_state_file: " + stateFile + "\ncourses:\n  - \"*\"\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("writing config.yaml: %v", err)
	}

	out := captureStdout(t, func() {
		if err := runStatus([]string{"--config", configPath}); err != nil {
			t.Errorf("runStatus returned error: %v", err)
		}
	})

	if !strings.Contains(out, "Download path: "+downloadPath+" (BROKEN:") {
		t.Fatalf("expected status to flag the unwritable download path, got:\n%s", out)
	}
}
