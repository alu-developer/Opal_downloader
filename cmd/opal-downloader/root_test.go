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

// alreadySucceededToday backs errAlreadySucceededToday (root.go), the guard
// that keeps the new LogonTrigger (internal/scheduler, Finding 1) from
// resyncing on every logon/unlock once today's fixed-time or catch-up run
// has already succeeded.
func withFakeScheduledStatus(t *testing.T, status statuslog.Status, ok bool) {
	t.Helper()
	original := readScheduledStatusForDedup
	readScheduledStatusForDedup = func() (statuslog.Status, bool) { return status, ok }
	t.Cleanup(func() { readScheduledStatusForDedup = original })
}

func TestAlreadySucceededToday_TrueForSameDaySuccessEarlierToday(t *testing.T) {
	now := time.Date(2026, 8, 11, 18, 0, 0, 0, time.Local)
	withFakeScheduledStatus(t, statuslog.Status{
		Timestamp: time.Date(2026, 8, 11, 6, 0, 0, 0, time.Local),
		Outcome:   statuslog.OutcomeSuccess,
	}, true)

	if !alreadySucceededToday(now) {
		t.Fatal("expected alreadySucceededToday to be true for a success recorded earlier the same day")
	}
}

func TestAlreadySucceededToday_FalseForSuccessOnAnEarlierDay(t *testing.T) {
	now := time.Date(2026, 8, 11, 6, 0, 0, 0, time.Local)
	withFakeScheduledStatus(t, statuslog.Status{
		Timestamp: time.Date(2026, 8, 10, 6, 0, 0, 0, time.Local),
		Outcome:   statuslog.OutcomeSuccess,
	}, true)

	if alreadySucceededToday(now) {
		t.Fatal("expected alreadySucceededToday to be false for yesterday's success - today's trigger must still run")
	}
}

// TestAlreadySucceededToday_FalseForFailureEarlierToday covers the case the
// guard exists to NOT block: a failed (or partial) run earlier today must
// not stop a later trigger (fixed-time or logon) from trying again - only a
// recorded success should suppress a resync.
func TestAlreadySucceededToday_FalseForFailureEarlierToday(t *testing.T) {
	now := time.Date(2026, 8, 11, 8, 0, 0, 0, time.Local)
	withFakeScheduledStatus(t, statuslog.Status{
		Timestamp: time.Date(2026, 8, 11, 6, 0, 0, 0, time.Local),
		Outcome:   statuslog.OutcomeFailure,
	}, true)

	if alreadySucceededToday(now) {
		t.Fatal("expected alreadySucceededToday to be false after an earlier failure today")
	}
}

func TestAlreadySucceededToday_FalseWhenNoStatusFileExists(t *testing.T) {
	withFakeScheduledStatus(t, statuslog.Status{}, false)

	if alreadySucceededToday(time.Now()) {
		t.Fatal("expected alreadySucceededToday to be false when there is no status to read")
	}
}

func TestExitCodeForError_AlreadySucceededToday(t *testing.T) {
	if got := exitCodeForError(errAlreadySucceededToday); got != exitCodeAlreadySucceededToday {
		t.Fatalf("expected exit code %d, got %d", exitCodeAlreadySucceededToday, got)
	}
}

// TestSyncScheduledSkipsWhenAlreadySucceededToday is the end-to-end version
// of the alreadySucceededToday unit tests above: with the new LogonTrigger
// (internal/scheduler, Finding 1) able to fire `sync --scheduled` on every
// logon/unlock, this guard has to sit ahead of every other scheduled-run
// step - TU-Fast presence, config load, network wait - or a machine that
// gets locked and unlocked repeatedly would pay for all of that on every
// unlock just to then also try, and fail to improve on, an already-succeeded
// day. Mirrors TestListRefusesWhileAnotherRunHoldsTheOverlapLock's "never
// touches the browser" style: this finishes in milliseconds.
func TestSyncScheduledSkipsWhenAlreadySucceededToday(t *testing.T) {
	withFakeScheduledStatus(t, statuslog.Status{
		Timestamp: time.Now().Add(-time.Hour),
		Outcome:   statuslog.OutcomeSuccess,
	}, true)

	err := runSync([]string{"--config", writeMinimalConfig(t), "--scheduled"})
	if !errors.Is(err, errAlreadySucceededToday) {
		t.Fatalf("expected errAlreadySucceededToday, got %v", err)
	}
	if got := exitCodeForError(err); got != exitCodeAlreadySucceededToday {
		t.Fatalf("expected exit code %d, got %d", exitCodeAlreadySucceededToday, got)
	}
}

// The companion negative case: a fixed-time or logon trigger firing on a day
// with no recorded success yet must run normally (here it fails fast on
// scraper.ErrTUFastNotReady, since USERPROFILE is redirected to an empty
// temp dir with no login profile - but critically NOT with
// errAlreadySucceededToday, proving the guard did not fire). USERPROFILE is
// redirected (not just the config's own paths) because past this guard the
// scheduled path also writes a real statuslog entry on return - t.Setenv
// keeps that write, and scraper.EnsureTUFastPresent's LoginProfileDir()
// lookup, off the real ~/.opal-downloader/ this machine actually uses.
func TestSyncScheduledRunsWhenNotYetSucceededToday(t *testing.T) {
	withFakeScheduledStatus(t, statuslog.Status{}, false)
	t.Setenv("USERPROFILE", t.TempDir())

	err := runSync([]string{"--config", writeMinimalConfig(t), "--scheduled"})
	if errors.Is(err, errAlreadySucceededToday) {
		t.Fatal("expected the scheduled run to proceed past the dedup guard when nothing succeeded today")
	}
	if !errors.Is(err, scraper.ErrTUFastNotReady) {
		t.Fatalf("expected the run to fail fast on the redirected (empty) login profile, got %v", err)
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

// runStatusConfig writes a minimal config.yaml pointing at stateFile and
// returns its path - shared by the login-status tests below.
func runStatusConfig(t *testing.T, dir, stateFile string) string {
	t.Helper()
	configPath := filepath.Join(dir, "config.yaml")
	configYAML := "download_path: " + filepath.Join(dir, "downloads") +
		"\nsession_state_file: " + stateFile + "\ncourses:\n  - \"*\"\n"
	if err := os.WriteFile(configPath, []byte(configYAML), 0o644); err != nil {
		t.Fatalf("writing config.yaml: %v", err)
	}
	return configPath
}

// writeSessionState writes a Playwright-storage-state-shaped JSON file with
// a single authenticated-marker cookie for bildungsportal.sachsen.de,
// expiring at expiresAt (unix seconds) - the same cookie
// internal/sessionstate.Inspect reads.
func writeSessionState(t *testing.T, path string, expiresAt int64) {
	t.Helper()
	body := fmt.Sprintf(`{"cookies":[{"domain":"bildungsportal.sachsen.de","expires":%d,"name":"authenticated-marker","path":"/opal"}]}`, expiresAt)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing session state: %v", err)
	}
}

// `status` used to report only whether the session state file existed and
// was non-empty ("session state file present") - unable to tell an hour-old
// session from a weeks-expired one. These four cover the states
// internal/sessionstate.Inspect can report, matching the GUI's landing page
// wording (internal/gui/gui.go) so both front ends read as one product.
func TestRunStatus_NotLoggedInWhenNoStateFile(t *testing.T) {
	dir := t.TempDir()
	configPath := runStatusConfig(t, dir, filepath.Join(dir, "state.json"))

	out := captureStdout(t, func() {
		if err := runStatus([]string{"--config", configPath}); err != nil {
			t.Errorf("runStatus returned error: %v", err)
		}
	})

	if !strings.Contains(out, "Not logged in yet. Run: opal-downloader login") {
		t.Fatalf("expected the not-logged-in message, got:\n%s", out)
	}
}

func TestRunStatus_ReportsExpiredSession(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	writeSessionState(t, stateFile, time.Now().Add(-2*time.Hour).Unix())
	configPath := runStatusConfig(t, dir, stateFile)

	out := captureStdout(t, func() {
		if err := runStatus([]string{"--config", configPath}); err != nil {
			t.Errorf("runStatus returned error: %v", err)
		}
	})

	if !strings.Contains(out, "saved session expired on") {
		t.Fatalf("expected an expired-session message, got:\n%s", out)
	}
	if strings.Contains(out, "session state file present") {
		t.Fatalf("expired session must not be reported as merely present, got:\n%s", out)
	}
}

func TestRunStatus_ReportsValidSessionWithTimeRemaining(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	writeSessionState(t, stateFile, time.Now().Add(47*time.Hour).Unix())
	configPath := runStatusConfig(t, dir, stateFile)

	out := captureStdout(t, func() {
		if err := runStatus([]string{"--config", configPath}); err != nil {
			t.Errorf("runStatus returned error: %v", err)
		}
	})

	if !strings.Contains(out, "Logged in: valid until") || !strings.Contains(out, "left)") {
		t.Fatalf("expected a valid-until message with time remaining, got:\n%s", out)
	}
}

func TestRunStatus_ReportsUnknownExpiryWhenMarkerMissing(t *testing.T) {
	dir := t.TempDir()
	stateFile := filepath.Join(dir, "state.json")
	// A state file with cookies but no authenticated-marker - the shape
	// sessionstate.Inspect treats as KnownExpiry=false, not "not logged in".
	if err := os.WriteFile(stateFile, []byte(`{"cookies":[{"domain":"bildungsportal.sachsen.de","expires":-1,"name":"JSESSIONID","path":"/opal"}]}`), 0o644); err != nil {
		t.Fatalf("writing session state: %v", err)
	}
	configPath := runStatusConfig(t, dir, stateFile)

	out := captureStdout(t, func() {
		if err := runStatus([]string{"--config", configPath}); err != nil {
			t.Errorf("runStatus returned error: %v", err)
		}
	})

	if !strings.Contains(out, "could not be read from the saved session") {
		t.Fatalf("expected an unknown-expiry message, got:\n%s", out)
	}
}

func TestHumanizeDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "under a minute"},
		{5 * time.Minute, "5 minutes"},
		{90 * time.Minute, "1 hour"},
		{5 * time.Hour, "5 hours"},
		{72 * time.Hour, "3 days"},
	}
	for _, c := range cases {
		if got := humanizeDuration(c.d); got != c.want {
			t.Errorf("humanizeDuration(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}
