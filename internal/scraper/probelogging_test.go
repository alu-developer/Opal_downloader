package scraper

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// Live probe tests in this package run a real crawl and print their own
// summary, and for weeks that summary was the *only* thing they printed. The
// logging package's default is console-only and non-verbose, which shows
// user-facing records plus every error and drops the diagnostic ones - a
// sensible default for a CLI, and the wrong one for an instrument.
//
// The cost was concrete. On 2026-07-29 a cold section-cache run returned files
// for 1 of 6 courses, and orchestrator.go had logged exactly the right thing
// five times over - "course %q crawled successfully but found 0 files" - at
// Warn with a diagnostic audience. Every one was suppressed. The run printed
// "Discovered 39 remote files", exited 0, and PASSed. Isolating the cause took
// a day of comparing cache files, for information the tool had already
// produced and thrown away.
//
// Note it was never Errors that were hidden: consoleHandler.shows returns true
// for anything at Error or above regardless of audience. Only the diagnostic
// Warn/Detail records vanish, which is precisely the band a crawl reports
// partial failure in - loud enough to matter, not fatal enough to abort.
// probeLogOptions is shared with the test below on purpose. When the helper
// built its own Options inline, deleting `Verbose: true` from it left the test
// passing - the test asserted the logging package's behaviour under those
// settings, never that a probe run actually uses them. One source, so the
// regression has somewhere to fail.
func probeLogOptions(console io.Writer) logging.Options {
	return logging.Options{Console: console, Verbose: true}
}

// beginLiveProbe is the single entry point every live probe in this package
// calls first. It does two things a live probe must not be able to forget:
// turns on verbose diagnostic logging (the reason documented above), and
// takes the crawl overlap lock so this probe cannot run while another
// opal-downloader process is crawling the same account (requireQuietAccount,
// probeoverlap_test.go - see there for the two incidents that cost).
//
// It was called captureProbeLogs until 2026-08-10, when the overlap guard
// was added. Renamed rather than left alone: a helper named for logging is a
// helper a new probe will copy without the guard, and "the probe forgot the
// safety step" is how both collisions happened.
func beginLiveProbe(t *testing.T) {
	t.Helper()

	requireQuietAccount(t)

	closeLogs, err := logging.Setup(probeLogOptions(testLogWriter{t: t}))
	if err != nil {
		// No FilePath is set, so there is nothing that can fail to open. If
		// that ever changes, a probe run must not die over its own logging.
		t.Logf("logging setup reported: %v", err)
	}

	t.Cleanup(func() {
		if closeLogs != nil {
			closeLogs()
		}
		// Setup is process-wide, so leaving it verbose would leak into every
		// later test in this package. Restore the package default rather than
		// the previous value, which Setup gives no way to read back.
		if restore, err := logging.Setup(logging.Options{}); err == nil && restore != nil {
			defer restore()
		}
	})
}

// testLogWriter routes log output through t.Logf so it interleaves with the
// test's own output under -v, instead of racing it on a bare os.Stdout.
type testLogWriter struct{ t *testing.T }

func (w testLogWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Logf("%s", bytes.TrimRight(p, "\r\n"))
	return len(p), nil
}

// TestBeginLiveProbeShowsDiagnosticWarnings is the reason to trust the helper
// above: it proves the exact record the 2026-07-29 run lost now arrives.
//
// Deliberately asserts both directions. Without the fix a Warn is dropped, so a
// test that only checked "it appears afterwards" would also pass against a
// logging package that shows everything always, and would not notice the
// default being loosened for every CLI user at the same time.
func TestBeginLiveProbeShowsDiagnosticWarnings(t *testing.T) {
	const msg = "course \"Softwaretechnologie\" crawled successfully but found 0 files"

	var quiet bytes.Buffer
	restore, err := logging.Setup(logging.Options{Console: &quiet})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	logging.Warn("%s", msg)
	restore()
	if strings.Contains(quiet.String(), msg) {
		t.Fatalf("a diagnostic Warn reached a non-verbose console; this test can no longer detect the suppression it exists for:\n%s", quiet.String())
	}

	var loud bytes.Buffer
	restoreLoud, err := logging.Setup(probeLogOptions(&loud))
	if err != nil {
		t.Fatalf("setup verbose: %v", err)
	}
	logging.Warn("%s", msg)
	restoreLoud()
	if !strings.Contains(loud.String(), msg) {
		t.Fatalf("the zero-files warning is still invisible under the settings probe runs use; got:\n%s", loud.String())
	}

	// And restore the package default for whatever runs next.
	if done, err := logging.Setup(logging.Options{}); err == nil && done != nil {
		defer done()
	}
}
