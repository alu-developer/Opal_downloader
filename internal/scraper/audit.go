package scraper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/mxschmitt/playwright-go"
)

// SetDebugClicks turns on the click/wait audit log (see auditLog's doc
// comment). It is wired to the CLI's --debug-clicks flag
// (cmd/opal-downloader/root.go) and is off by default, matching
// SetDeveloperMode's pattern - set once before a scrape begins, read (never
// written) afterward, so it needs no locking of its own.
//
// This deliberately does not also open the persistent log file (see
// EnableDebugLogFile) - keeping the two separate means existing tests that
// only care about the in-memory bool/stdout behavior (audit_test.go) never
// touch a real filesystem, and any caller that only wants the stdout trace
// (e.g. a quick manual check) doesn't get a file it didn't ask for.
func (s *OpalScraper) SetDebugClicks(enabled bool) {
	s.debugClicks = enabled
}

// DebugLogDir returns the directory EnableDebugLogFile writes into by
// default: ~/.opal-downloader/debug-logs. Deliberately global (not under a
// config's DownloadPath, next to the sync manifest/visit log) because
// concurrent-crawl investigations routinely point config at a scratch/throwaway
// DownloadPath for the live test itself (see queue task
// close-residual-concurrent-crawl-ajax-race) - a log that followed
// DownloadPath would get thrown away with that scratch config instead of
// accumulating somewhere stable across investigations.
func DebugLogDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for the debug log: %w", err)
	}
	return filepath.Join(home, ".opal-downloader", "debug-logs"), nil
}

// EnableDebugLogFile opens a new timestamped JSONL file (one JSON object per
// line: time/kind/page/selector/reason, the same fields auditLog already
// prints to stdout) under dir and makes auditLog append every line into it
// too. Pass "" for dir to use DebugLogDir()'s default. Returns the resolved
// file path so the caller can tell the user/log where to find it afterward.
//
// This exists because auditLog's stdout output was, before this, the *only*
// place --debug-clicks data went - it only ever lived in a single terminal's
// scrollback, gone once that session closed. Every past investigation into
// the concurrent-crawl race (fix-concurrent-crawl-ajax-race-and-raise-
// concurrency, investigate-size-aware-course-scheduling-for-concurrency,
// close-residual-concurrent-crawl-ajax-race) had to re-run a full live crawl
// from scratch to get fresh raw data, and even then only ever wrote aggregate
// summaries (poll counts, hard-cap frequency) into the PR/task file rather
// than the actual per-poll/per-wait trace - see
// fix-candidate-stability-poll-concurrent-crawl-race.md's Step 0 for why
// that gap mattered: it left "was the count still climbing when the poll
// gave up, or flat from the start" and "did this resolve faster than the
// fixed wait it replaced" both unanswered, for every run so far.
//
// One file per call (timestamped name), not one ever-growing shared file -
// see docs/OPERATIONS.md for where the resulting directory is documented;
// cleanup is manual (delete old files), matching visitlog's/the login
// profile's precedent of leaving local diagnostic/state data for the user to
// manage rather than auto-pruning it.
//
// Persistence here is a bonus on top of the stdout trace, not a replacement
// for it or a requirement for --debug-clicks to work - a failure to open this
// file (e.g. permissions) is returned to the caller to decide how to report,
// but does not itself disable or degrade auditLog's existing stdout output.
func (s *OpalScraper) EnableDebugLogFile(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = DebugLogDir()
		if err != nil {
			return "", err
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating debug log directory %s: %w", dir, err)
	}
	name := fmt.Sprintf("debug-%s.jsonl", time.Now().Format("20060102-150405.000"))
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", fmt.Errorf("opening debug log file %s: %w", path, err)
	}

	s.debugLogMu.Lock()
	s.debugLogFile = f
	s.debugLogMu.Unlock()

	return path, nil
}

// CloseDebugLogFile closes the file opened by EnableDebugLogFile, if any - a
// no-op when none is open. Wired into OpalScraper.Close() so a run's debug
// log file doesn't stay open (or its content unflushed on some platforms)
// past the run that wrote it.
func (s *OpalScraper) CloseDebugLogFile() error {
	s.debugLogMu.Lock()
	f := s.debugLogFile
	s.debugLogFile = nil
	s.debugLogMu.Unlock()

	if f == nil {
		return nil
	}
	return f.Close()
}

// debugLogLine is one JSON line written by writeDebugLogLine - deliberately
// the same fields auditLog already prints to stdout (see its doc comment),
// just structured instead of formatted, so the persistent log and the
// interactive stdout trace never drift into reporting different things.
type debugLogLine struct {
	Time     string `json:"time"`
	Kind     string `json:"kind"`
	Page     string `json:"page"`
	Selector string `json:"selector"`
	Reason   string `json:"reason"`
}

// writeDebugLogLine appends one line to the currently-open debug log file,
// if any. Holds debugLogMu for the whole marshal+write (not just the file
// lookup) so concurrent callers - auditLog can be invoked from several
// courses' crawl goroutines at once during course_concurrency>1 - can never
// interleave partial writes into the same file. A marshal or write failure
// is silently dropped: this is a diagnostic bonus (see EnableDebugLogFile's
// doc comment), not something that should ever fail or slow down the actual
// crawl it's observing.
func (s *OpalScraper) writeDebugLogLine(t time.Time, kind, page, selector, reason string) {
	s.debugLogMu.Lock()
	defer s.debugLogMu.Unlock()
	if s.debugLogFile == nil {
		return
	}
	line, err := json.Marshal(debugLogLine{
		Time:     t.Format(time.RFC3339Nano),
		Kind:     kind,
		Page:     page,
		Selector: selector,
		Reason:   reason,
	})
	if err != nil {
		return
	}
	line = append(line, '\n')
	_, _ = s.debugLogFile.Write(line)
}

// auditLog prints one diagnostic line for every .Click() call site
// (crawl.go's "show all" expansion, download.go's browser-fallback download
// link click) and every significant wait call in the crawl/discovery/login
// path, when debugClicks is enabled. It exists to answer recurring questions
// from queue tasks click-wait-audit-and-speedup and
// click-audit-analysis-and-cleanup: "why does the crawler click on X" and
// "how long do the fixed waits actually take on a real OPAL instance" - each
// line carries a timestamp, the page's current URL, the selector/text that
// was matched (or attempted), and a short reason string identifying which
// candidate/file/section triggered the call.
//
// Coverage as of click-audit-analysis-and-cleanup (2026-07-12, confirmed by
// grepping internal/scraper for every .Click()/WaitFor*() call site): all
// three .Click() sites (crawl.go's show-all expansion, download.go's two
// browser-fallback attempts) and all Wait*() calls in the crawl/discovery
// path are logged - navigation.go's waitForInteractiveLinks (crawl.go's and
// download.go's per-section content wait), discovery.go's
// waitForCourseEntries (previously a real gap - added in this task), and
// session.go's waitForLoggedInCourseLink (previously a gap too; low-impact
// since it is a one-time per-login wait, added anyway for completeness).
// discovery.go's extractCourseCardsFromCurrentPage and files.go's
// extractSectionContentCandidates run entirely inside page.Evaluate() (pure
// JS DOM reads, no Playwright Click/Wait calls at all), so there is nothing
// to instrument there.
//
// Extended (2026-07-13, queue task fix-concurrent-crawl-ajax-race-and-
// raise-concurrency) with a "section-content-poll" kind logged from
// crawl.go's waitForStableSectionContent, one line per poll iteration with
// that read's candidate count - this is what let that task tell a
// stuck-at-N-forever plateau apart from "the poll loop never even ran",
// and is kept as a permanent diagnostic for the same reason.
//
// This is meant to stay in the codebase as an always-available diagnostic
// flag, not a temporary patch - see SetDebugClicks and the --debug-clicks
// CLI flag. When debugClicks is false (the default), this is a single bool
// check and a no-op; no other code path's behavior changes based on it.
//
// Also writes the same line to the persistent debug log file, if
// EnableDebugLogFile was called (see its doc comment) - a no-op if it
// wasn't, so this stays exactly as cheap as before for any caller that only
// wants the stdout trace.
func (s *OpalScraper) auditLog(kind string, page playwright.Page, selector, reason string) {
	if !s.debugClicks {
		return
	}
	pageURL := "?"
	if page != nil {
		// page.URL() is a synchronous local read of Playwright's cached page
		// state, not a round-trip to the browser, so this is cheap enough to
		// call unconditionally here even though the outer debugClicks check
		// already guards it.
		pageURL = page.URL()
	}
	now := time.Now()
	logging.Detail("[audit] t=%s kind=%s page=%s selector=%q reason=%q", now.Format(time.RFC3339Nano), kind, pageURL, selector, reason)
	s.writeDebugLogLine(now, kind, pageURL, selector, reason)
}
