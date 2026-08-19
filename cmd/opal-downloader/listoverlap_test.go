package opaldownloader

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/synclock"
)

func writeMinimalConfig(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "download_path: \"" + filepath.ToSlash(t.TempDir()) + "\"\n" +
		"opal_url: \"https://bildungsportal.sachsen.de/opal/\"\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

// `list` runs the same full crawl `sync` does, and until 2026-08-10 it took no
// overlap lock at all - so a scheduled sync and a manual list could crawl the
// same account at the same time. That is how both collision incidents in
// docs/BACKLOG-archive.md happened, and neither produced a clear error: one died with a
// raw 180s Playwright launch timeout, the other silently reported 0 files for
// every course across three consecutive runs.
//
// This asserts the refusal, and asserts it happens *before* any browser or
// network work - the test finishes in milliseconds and never launches Chromium,
// which is only possible if the guard sits ahead of scraper.New.
func TestListRefusesWhileAnotherRunHoldsTheOverlapLock(t *testing.T) {
	held := fmt.Errorf("%w (PID 4321, started at 2026-08-10T09:00:00Z)", synclock.ErrHeld)

	calls := 0
	original := acquireCrawlOverlapLock
	acquireCrawlOverlapLock = func() (func(), error) {
		calls++
		return nil, held
	}
	t.Cleanup(func() { acquireCrawlOverlapLock = original })

	err := runList([]string{"--config", writeMinimalConfig(t)})
	if err == nil {
		t.Fatal("expected list to refuse while another run holds the overlap lock, got nil")
	}
	if !errors.Is(err, synclock.ErrHeld) {
		t.Fatalf("expected an error satisfying errors.Is(err, synclock.ErrHeld), got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected list to ask for the overlap lock exactly once, got %d", calls)
	}

	// The exit code matters as much as the error: a scheduled run has to be
	// able to tell "another run is already going" apart from a real failure
	// without parsing stdout. exitCodeForError already maps synclock.ErrHeld,
	// but nothing previously proved a `list` error reaches that mapping.
	if got := exitCodeForError(err); got != exitCodeSyncAlreadyRunning {
		t.Fatalf("expected exit code %d for an overlap refusal, got %d", exitCodeSyncAlreadyRunning, got)
	}
}

// `login` opens the visible persistent-context browser against the shared
// login profile and - by design, see shouldRelaunchHeadlessAfterInteractiveLogin
// - leaves it open until the process exits, after the session mutex has already
// been released. So it has to contend on the coarse lock too, or it stays a way
// to hand a concurrent process a raw Playwright launch timeout instead of a
// legible error.
func TestLoginRefusesWhileAnotherRunHoldsTheOverlapLock(t *testing.T) {
	held := fmt.Errorf("%w (PID 4321, started at 2026-08-10T09:00:00Z)", synclock.ErrHeld)

	original := acquireCrawlOverlapLock
	acquireCrawlOverlapLock = func() (func(), error) { return nil, held }
	t.Cleanup(func() { acquireCrawlOverlapLock = original })

	err := runLogin([]string{"--config", writeMinimalConfig(t)})
	if !errors.Is(err, synclock.ErrHeld) {
		t.Fatalf("expected login to refuse with synclock.ErrHeld while another run holds the lock, got %v", err)
	}
}

// smoke-check and dump-links took no overlap lock at all until 2026-08-19 -
// docs/BACKLOG.md's friction-campaign walk 10 found the gap by sweeping every
// scraper.New call site against every acquireCrawlOverlapLock call site.
// smoke-check is the higher-risk of the two: a real, documented,
// unattended-safe command anyone (or automation) could run at any time,
// including the exact minute a scheduled sync is already crawling.
//
// Acquired before scraper.EnsureTUFastPresent, so this test never touches
// the real ~/.opal-downloader/login-profile directory and passes regardless
// of whether TU-Fast is set up on the machine running it.
func TestSmokeCheckRefusesWhileAnotherRunHoldsTheOverlapLock(t *testing.T) {
	held := fmt.Errorf("%w (PID 4321, started at 2026-08-19T09:00:00Z)", synclock.ErrHeld)

	calls := 0
	original := acquireCrawlOverlapLock
	acquireCrawlOverlapLock = func() (func(), error) {
		calls++
		return nil, held
	}
	t.Cleanup(func() { acquireCrawlOverlapLock = original })

	err := runSmokeCheck([]string{"--config", writeMinimalConfig(t)})
	if !errors.Is(err, synclock.ErrHeld) {
		t.Fatalf("expected smoke-check to refuse with synclock.ErrHeld while another run holds the lock, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected smoke-check to ask for the overlap lock exactly once, got %d", calls)
	}
}

// dump-links is maintainer-only, but has the identical gap: it opens the same
// shared session against the same account.
func TestDumpLinksRefusesWhileAnotherRunHoldsTheOverlapLock(t *testing.T) {
	held := fmt.Errorf("%w (PID 4321, started at 2026-08-19T09:00:00Z)", synclock.ErrHeld)

	original := acquireCrawlOverlapLock
	acquireCrawlOverlapLock = func() (func(), error) { return nil, held }
	t.Cleanup(func() { acquireCrawlOverlapLock = original })

	err := runDumpLinks([]string{"--config", writeMinimalConfig(t), "--url", "https://bildungsportal.sachsen.de/opal/some-course"})
	if !errors.Is(err, synclock.ErrHeld) {
		t.Fatalf("expected dump-links to refuse with synclock.ErrHeld while another run holds the lock, got %v", err)
	}
}

// The other direction: --visit-report reads a local log file and never opens a
// browser or touches OPAL, so it must not be blocked by a sync running
// elsewhere. Without this, taking the lock earlier in runList (an easy
// "tidy-up" edit) would break offline reporting while a scheduled sync runs.
func TestListVisitReportDoesNotTakeTheOverlapLock(t *testing.T) {
	original := acquireCrawlOverlapLock
	acquireCrawlOverlapLock = func() (func(), error) {
		t.Error("--visit-report asked for the crawl overlap lock; it does no crawl and must work while a sync is running")
		return func() {}, nil
	}
	t.Cleanup(func() { acquireCrawlOverlapLock = original })

	// No visit log exists in the temp download path, so this returns whatever
	// visitlog.Load reports for a missing file. Either outcome is fine - the
	// assertion is in the stub above.
	_ = runList([]string{"--config", writeMinimalConfig(t), "--visit-report"})
}
