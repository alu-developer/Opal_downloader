package scraper

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// contentFallbackWaitMs bounds waitForInteractiveLinks (below).
// perf-04 (2026-07-08) assumed WaitForSelector "resolves near-instantly on
// the happy path" and left the selector-wait timeout at 2500ms unverified
// against a live run. Queue task click-wait-audit-and-speedup's
// --debug-clicks audit (2026-07-10, live `list --dev --debug-clicks
// --profile`, 6 courses, 205 files) disproved that assumption: the selector
// wait timed out 261/261 times (100%), and was lowered to 400ms rather than
// removed outright, on the theory it was still a useful bounded fallback.
//
// Queue task click-audit-analysis-and-cleanup (2026-07-12) re-ran the same
// live audit (7 courses, 333 files) and found the selector wait still timed
// out 316/316 times (100%) - the "small margin" theory was wrong twice, not
// once. Two fixes were tried, live, before landing on this one:
//
//  1. Dropping WaitForSelectorState's default (Visible: laid out, non-zero
//     bounding box, no visibility:hidden) in favor of State: Attached
//     (present in the DOM regardless of visibility) seemed obviously
//     correct, since extractSectionContentCandidates/
//     extractCourseCardsFromCurrentPage both read the DOM via
//     page.Evaluate()->querySelectorAll(), which is visibility-agnostic - so
//     Visible was gating on a condition the caller never needed. Live-tested
//     alone: the wait dropped to ~20-50ms, but file counts *regressed*
//     (333 -> 288; e.g. Softwaretechnologie 198 -> 155). The broad selector
//     ("a[href], [onclick], [data-href], [data-url]") matches document-wide,
//     so Attached resolves the instant *any* such element exists anywhere on
//     the page (e.g. header/nav boilerplate present at first paint) - long
//     before the section's actual content has finished rendering. Provably
//     unsafe: do not reintroduce Attached here without re-verifying file
//     counts live.
//  2. Dropping the WaitForSelector attempt entirely but *keeping the fixed
//     wait at 700ms* looked like the obvious "keep only the part that
//     works" fix, since the selector attempt never once succeeded in two
//     independent audits. Live-tested alone: wall-clock dropped sharply
//     (323.8s -> ~200s) but file counts also regressed by a small,
//     reproducible amount across two separate clean runs (Algorithmen und
//     Datenstrukturen: 34 -> 32, both times). The reason: even though the
//     selector match itself never succeeds, WaitForSelector(400ms) still
//     *blocks* for the full 400ms before giving up - so the original code's
//     real, total elapsed content-render time on every visit was ~1100ms
//     (400 + 700), not 700ms. Some content genuinely needs close to that
//     full 1100ms to finish rendering; cutting the budget to 700ms measurably
//     lost content. Don't reintroduce a 700ms-only wait here without
//     re-verifying file counts live.
//
// What shipped: skip the WaitForSelector call (it never succeeds, so it is a
// pure extra Playwright round-trip with no early-exit benefit) but keep the
// *total* wait duration unchanged at 1100ms - the fixed value below is 1100,
// not 700. This can't regress content-render time versus the unmodified
// code (every visit there also blocked for exactly ~1100ms before
// extraction, 100% of the time), while removing one wasted IPC round-trip
// per section visit (316 of them in the click-audit-analysis-and-cleanup
// live run). The collectCourseFiles retry-on-empty-candidates logic
// (crawl.go) remains the safety net for any section that still needs more
// than this to render.
const contentGotoTimeoutMs = 15000.0
const contentFallbackWaitMs = 1100.0

var sectionTitleWhitespaceRe = regexp.MustCompile(`\s+`)

func deriveSectionTitle(title, text, rootText string) string {
	for _, raw := range []string{title, text, rootText} {
		cleaned := strings.TrimSpace(raw)
		if cleaned == "" {
			continue
		}
		cleaned = sectionTitleWhitespaceRe.ReplaceAllString(cleaned, " ")
		cleaned = strings.Trim(cleaned, " -,")
		if isValidSectionTitle(cleaned) {
			return cleaned
		}
	}
	return ""
}

func isValidSectionTitle(value string) bool {
	cleaned := strings.ToLower(strings.TrimSpace(value))
	if len(cleaned) < 2 {
		return false
	}
	if strings.HasPrefix(cleaned, "http://") || strings.HasPrefix(cleaned, "https://") {
		return false
	}
	if containsAny(cleaned, []string{"gehe zu", "aktuelle seite", "vorherige seite", "nächste seite", "als favorit markieren", "verantwortliche", "zuletzt angesehen", "aufrufe"}) {
		return false
	}
	return true
}

func looksLikeSectionLink(href, title string) bool {
	hrefL := strings.ToLower(strings.TrimSpace(href))
	titleL := strings.ToLower(strings.TrimSpace(title))
	if titleL == "" {
		return false
	}
	if containsAny(titleL, []string{"forum", "kalender", "neuigkeiten", "ankündigungen", "mitglieder", "teilnehmer", "bewertung", "statistik", "übersicht", "meine kurse", "katalog"}) {
		return false
	}
	if containsAny(hrefL, []string{"/login", "shibboleth", "logout", "mycourses", "membership", "/auth/home", "resource/courses", "resource/resources", "cmd=edit", "cmd=delete", "baseclass=ildashboardgui", "baseclass=ilmembershipoverviewgui", "-pager-", "downloadtablecontainer", "/auth/repository/catalog"}) {
		return false
	}
	return containsAny(hrefL, []string{"target=fold_", "target=grp_", "target=crs_", "goto.php?target=fold_", "goto.php?target=grp_", "goto.php?target=crs_", "/coursenode/", "/repositoryentry/"})
}

func isSectionURLAllowedForCourse(absURL, repoID string) bool {
	if strings.TrimSpace(absURL) == "" || strings.TrimSpace(repoID) == "" {
		return false
	}
	relatedRepoID := extractRepositoryEntryID(absURL)
	if relatedRepoID != "" && relatedRepoID != repoID {
		return false
	}
	urlLower := strings.ToLower(absURL)
	if containsAny(urlLower, []string{"/auth/home", "/auth/repository/catalog", "mycourses", "membership", "baseclass=ildashboardgui", "baseclass=ilmembershipoverviewgui", "resource/courses", "resource/resources"}) {
		return false
	}
	return true
}

// waitForInteractiveLinks waits for the content extraction that follows it
// to have something to read on page. page is taken as an explicit parameter
// (rather than defaulting to s.getPage()) so this also works correctly
// against one of the per-worker tabs opened by
// collectCourseFilesConcurrently for parallel course crawling, not just the
// single shared s.page.
//
// This intentionally does not attempt a WaitForSelector early-exit before
// the fixed fallbackWaitMs wait - see the contentFallbackWaitMs doc comment
// above for the live-audit history of why that was removed (it never
// resolved early, twice, and the obvious fix - waiting for Attached instead
// of Playwright's default Visible - was live-verified to cause real file
// count regressions instead).
//
// This fixed wait is deliberately still followed by condition-based polling
// (waitForStableSectionContent in crawl.go, built on candidateStabilityPoll
// below) rather than being trusted alone - see candidateStabilityPoll's doc
// comment for why: this fixed duration was tuned against serial
// (course_concurrency=1) crawling, where it is enough; under concurrent
// crawling it measurably is not, for the same class of reason
// showAllExpansionPollIntervalMs/waitForStableExpandedCandidates already had
// to account for.
func (s *OpalScraper) waitForInteractiveLinks(page playwright.Page, fallbackWaitMs float64) {
	if page == nil {
		return
	}
	const selector = "a[href], [onclick], [data-href], [data-url]"
	start := time.Now()
	page.WaitForTimeout(fallbackWaitMs)
	s.auditLog("wait-fixed", page, selector, fmt.Sprintf("fixed wait took %s (no selector early-exit attempted - see contentFallbackWaitMs doc comment)", time.Since(start)))
}

// candidateStabilityPoll repeatedly calls extractFn (waiting via waitFn
// between attempts) until a read comes back no larger than the previous
// best, or maxPolls extra attempts have been made, and returns the largest
// candidate set observed.
//
// This is the shared engine behind both waitForStableExpandedCandidates
// (crawl.go's "show all" pagination expansion, the original 2026-07-12 use
// of this pattern) and waitForStableSectionContent (crawl.go's main
// per-section content wait, added by the queue task
// fix-concurrent-crawl-ajax-race-and-raise-concurrency). Root cause for why
// this exists at all: OPAL's section content is rendered by client-side
// AJAX (Wicket) after the page's own load event, and how long that
// rendering takes is not fixed - it scales with contention for the
// browser's rendering pipeline (CPU/event-loop), which is exactly what goes
// up when collectCourseFilesConcurrently runs several courses' pages at
// once. A single fixed-duration wait tuned against a serial
// (course_concurrency=1) crawl - where the renderer has the machine to
// itself - was live-confirmed (2026-07-12, task increase-parallel-tab-
// concurrency) to sometimes not be enough once that render has to compete
// with N-1 other courses' pages rendering at the same time: the extraction
// that follows the fixed wait can run before the AJAX update has finished,
// silently returning fewer candidates than actually exist - in the worst
// case (the root section, which gates whether any subfolder links get
// queued at all) zero, dropping an entire course's files with no error.
//
// Polling for a *stable* (no-longer-growing) read, rather than trusting any
// fixed duration to be "enough", adapts automatically to however much
// contention is actually present: under light/no load the very first extra
// poll comes back equal to the first read and this returns almost
// immediately (matching the old fixed-wait behavior's typical cost within
// one poll interval); under heavy load it keeps polling - up to the
// maxPolls budget - for as long as the candidate count keeps growing.
//
// A transient extraction error mid-poll is treated as "no progress this
// round" (so one flaky read can't discard an already-successful render)
// unless isFatal(err) reports true, in which case it is returned
// immediately so the caller's own crash-recovery path can run - mirrors
// waitForStableExpandedCandidates's original error handling.
//
// requiredStableReads is how many *consecutive* non-growing reads are
// needed before the render is considered settled (values < 1 are treated as
// 1). This was added after live A/B testing (queue task
// fix-concurrent-crawl-ajax-race-and-raise-concurrency) showed that a
// single non-growing read is not always a reliable "done" signal: OPAL
// renders a section's content in stages under real contention (e.g. the
// file/row list appears first, with a separate pagination control such as
// the "show all"/pager-showall link - see expandShowAllInSection in
// crawl.go - appearing only in a later stage). A poll that stops as soon as
// one read matches the previous one can catch a coincidental plateau
// between those stages and declare victory before the later stage - most
// consequentially the pagination control itself - has rendered at all,
// silently truncating the section to whatever the first stage contained.
// Confirmed live: an initial version of this fix using requiredStableReads
// effectively 1 still lost exactly the files past OPAL's ~20-item default
// page size in a paginated section, with the "show all" control's click
// never even attempted (findShowAllTarget found nothing, because the
// control had not rendered into the DOM at the moment extraction ran) -
// requiring multiple consecutive confirming reads before trusting a
// "stable" count fixed it. See waitForStableSectionContent's
// sectionContentRequiredStableReads (crawl.go) for the tuned value used for
// the main per-section content wait, where this was found.
//
// Deliberately has no playwright.Page/browser dependency, so it can be unit
// tested with a fake extractFn that simulates slow/concurrent-load,
// staged-render AJAX behavior without a real browser - see
// candidate_stability_test.go.
func candidateStabilityPoll(extractFn func() ([]map[string]string, error), waitFn func(), maxPolls int, requiredStableReads int, isFatal func(error) bool) ([]map[string]string, error) {
	if requiredStableReads < 1 {
		requiredStableReads = 1
	}

	best, err := extractFn()
	if err != nil {
		if isFatal != nil && isFatal(err) {
			return nil, err
		}
		best = nil
	}

	stableStreak := 0
	for i := 0; i < maxPolls; i++ {
		waitFn()
		next, err := extractFn()
		if err != nil {
			if isFatal != nil && isFatal(err) {
				return nil, err
			}
			// A transient error is neither growth nor a confirming stable
			// read - it tells us nothing about whether the render has
			// settled, so it doesn't reset or advance the stable streak
			// either. Just try again next poll.
			continue
		}
		if len(next) > len(best) {
			// Real growth - the render is still in progress. Reset the
			// stable streak: whatever plateau (if any) preceded this no
			// longer counts, since it's now proven not to have been the
			// final state.
			best = next
			stableStreak = 0
			continue
		}
		stableStreak++
		if stableStreak >= requiredStableReads {
			// This round, and requiredStableReads-1 rounds before it, all
			// failed to grow the candidate set - a single plateau read is
			// no longer trusted alone (see the doc comment above), but this
			// many in a row is. best already holds the largest read seen.
			break
		}
	}
	return best, nil
}

// isPageCrashError reports whether err is Playwright's "Target crashed"/
// "Page crashed" class of error, signalling that the underlying Chromium
// renderer process for a page has died. Confirmed live against real OPAL
// courses (queue task fix-playwright-page-crashes-during-crawl, e.g. course
// "TUDMATH SoSe2026 Modul Math-Ba-NM20: Numerische Mathematik -
// Iterationsverfahren"): once this happens, the Playwright Page object is
// permanently unusable - every further call on it (Goto, Evaluate, Click,
// ...) fails immediately with the same crashed error, rather than being a
// one-off transient hiccup like net::ERR_ABORTED. Retrying an operation on
// the same page after this error is pointless and, worse, is what turned one
// real renderer crash into a cascade of "skipping section" warnings across
// every remaining section in that course: collectCourseFiles reuses a single
// page for the whole course crawl, so a crashed page stayed crashed for
// every subsequent Goto/extraction in the loop. Callers that see this error
// must recover onto a fresh page/tab (recoverFromPageCrash below) instead of
// retrying in place.
func isPageCrashError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "target crashed") || strings.Contains(msg, "page crashed")
}

// recoverFromPageCrash opens a fresh replacement page/tab in the same
// browser context as the crashed page and best-effort closes the crashed
// one (its underlying renderer process is already gone, so Close() failing
// here is expected and ignored - there is nothing left to clean up on the
// Playwright side beyond what the browser process teardown already handles).
// It is safe to call concurrently from multiple course-crawl goroutines:
// ctx.NewPage() is already relied on for exactly that concurrent pattern by
// newCourseFileCollector (orchestrator.go), which opens one page per course
// across a worker pool.
//
// The returned page has the same default timeouts applied as any other
// freshly opened crawl page (see launchBrowser/newCourseFileCollector) -
// pages opened during a concurrent crawl don't get this from the ctx.OnPage
// hook, since page-tracking is suspended for the duration of the concurrent
// crawl (see pageTrackingSuspended's doc comment in scraper.go).
func (s *OpalScraper) recoverFromPageCrash(crashedPage playwright.Page) (playwright.Page, error) {
	ctx := s.getContext()
	if ctx == nil {
		return nil, errors.New("no browser context available to open a replacement tab after a page crash")
	}
	newPage, err := ctx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("failed to open replacement browser tab after crash: %w", err)
	}
	newPage.SetDefaultTimeout(15000)
	newPage.SetDefaultNavigationTimeout(20000)
	if crashedPage != nil {
		_ = crashedPage.Close()
	}
	return newPage, nil
}
