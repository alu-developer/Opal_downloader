package scraper

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/synclock"
)

// requireQuietAccount refuses to start a live probe while another
// opal-downloader process is already crawling this account, and holds the
// same ~/.opal-downloader/sync.lock a `sync` run holds for the probe's whole
// duration.
//
// Why a *test helper* takes a production lock: the probe tests in this
// package are the thing that actually collided, twice. docs/BACKLOG.md
// records both incidents. On 2026-08-02 a scheduled sync and a manually-run
// probe overlapped: the sync died with a raw 180s Playwright launch timeout
// and the probe hung for 22 minutes until its own `go test -timeout` killed
// it, leaving two orphaned chrome.exe processes. On 2026-08-06 two agent
// sessions crawled at once and a 4-run batch had runs 2, 3 and 4 report
// **0 files for both courses with no error at all** - a silent, total
// collapse that persisted past the collision window.
//
// Neither incident produced ErrProfileLocked, and that is not a bug in
// acquireSessionLock: it covers session *establishment* only, on the
// documented assumption that the crawl afterwards shares nothing (see
// session_lock_windows.go). Locally that is true - each crawl gets its own
// throwaway browser context. Server-side it is not: every context is seeded
// from the same storage-state cookie file, so concurrent crawls present one
// authenticated OPAL session identity to a Wicket backend that is stateful
// per session. That is candidate (D) in docs/BACKLOG.md, and it explains the
// symptom the other candidates cannot - silent, total, and not self-healing
// on restart.
//
// Fatal rather than Skip on purpose. A skipped probe produces no data and
// prints something easy to scroll past, and "the run silently did nothing"
// is the exact failure mode this package keeps paying for (see
// probelogging_test.go). A refusal that names the holder's PID is cheap to
// read and impossible to mistake for a result.
func requireQuietAccount(t *testing.T) {
	t.Helper()

	release, err := synclock.AcquireDefault()
	if err != nil {
		if errors.Is(err, synclock.ErrHeld) {
			t.Fatalf("refusing to start a live probe: %v\n"+
				"Another opal-downloader run (scheduled sync, CLI, GUI, or another agent session) is crawling this account right now. "+
				"Two concurrent crawls share one OPAL server-side session and have twice produced silent file loss - wait for it to finish and re-run.", err)
		}
		t.Fatalf("acquiring the crawl overlap lock: %v", err)
	}
	t.Cleanup(release)
}

// unitTestsTouchingSessionCode are the files that reach session-establishment
// code without ever driving the real account, so the guard above would be
// meaningless in them. Everything else that touches the account has to take
// it - see the enforcement test below.
var unitTestsTouchingSessionCode = map[string]bool{
	"session_test.go":              true,
	"session_lock_windows_test.go": true,
}

// The guard is only worth anything if every live probe actually takes it, and
// "remember to add the call" is exactly the discipline that failed here
// before: the probes that collided in 2026-08-02 and 2026-08-06 were written
// without any overlap guard because nothing required one.
//
// So this asserts the invariant instead of trusting a sweep: a test file that
// crawls or establishes a session against the real account must reference
// either beginLiveProbe (which takes the guard centrally) or
// requireQuietAccount (for the few probes that set up their own logging).
// A new probe that forgets fails here, on a normal `go test ./...`, without
// needing the account.
func TestEveryLiveProbeTakesTheOverlapGuard(t *testing.T) {
	entries, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("glob test files: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("found no test files to check; this test can no longer detect an unguarded probe")
	}

	checked := 0
	for _, path := range entries {
		name := filepath.Base(path)
		if unitTestsTouchingSessionCode[name] {
			continue
		}
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %s: %v", name, readErr)
		}
		src := string(body)

		touchesAccount := strings.Contains(src, "ScrapeWithSavedSession(") ||
			strings.Contains(src, "ensureSession(") ||
			strings.Contains(src, "LoginWithBrowser(")
		if !touchesAccount {
			continue
		}
		checked++

		if !strings.Contains(src, "beginLiveProbe(t)") && !strings.Contains(src, "requireQuietAccount(t)") {
			t.Errorf("%s drives the real account but never takes the crawl overlap guard - call beginLiveProbe(t) (or requireQuietAccount(t) if it sets up its own logging). See probeoverlap_test.go for why.", name)
		}
	}

	if checked == 0 {
		t.Fatal("no real-account probe files matched; the detection above has drifted from the code and would pass silently")
	}
}
