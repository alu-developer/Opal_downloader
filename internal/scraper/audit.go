package scraper

import (
	"fmt"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// SetDebugClicks turns on the click/wait audit log (see auditLog's doc
// comment). It is wired to the CLI's --debug-clicks flag
// (cmd/opal-downloader/root.go) and is off by default, matching
// SetDeveloperMode's pattern - set once before a scrape begins, read (never
// written) afterward, so it needs no locking of its own.
func (s *OpalScraper) SetDebugClicks(enabled bool) {
	s.debugClicks = enabled
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
// This is meant to stay in the codebase as an always-available diagnostic
// flag, not a temporary patch - see SetDebugClicks and the --debug-clicks
// CLI flag. When debugClicks is false (the default), this is a single bool
// check and a no-op; no other code path's behavior changes based on it.
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
	fmt.Printf("[audit] t=%s kind=%s page=%s selector=%q reason=%q\n", time.Now().Format(time.RFC3339Nano), kind, pageURL, selector, reason)
}
