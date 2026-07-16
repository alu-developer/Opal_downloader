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
// What shipped (2026-07-12): skip the WaitForSelector call (it never
// succeeds, so it is a pure extra Playwright round-trip with no early-exit
// benefit) but keep the *total* wait duration unchanged at 1100ms - the
// fixed value below is 1100, not 700. This can't regress content-render
// time versus the unmodified code (every visit there also blocked for
// exactly ~1100ms before extraction, 100% of the time), while removing one
// wasted IPC round-trip per section visit (316 of them in the
// click-audit-analysis-and-cleanup live run). The collectCourseFiles
// retry-on-empty-candidates logic (crawl.go) remains the safety net for any
// section that still needs more than this to render.
//
// SUPERSEDED (2026-07-16, queue task add-mutationobserver-load-detection):
// the fixed 1100ms wait above is no longer the primary content-settle
// signal - see waitForInteractiveLinks's doc comment below for the
// MutationObserver-based replacement that queue task
// investigate-load-completion-detection live-proved as a strict
// improvement (zero file-count regression across all 9 real courses,
// 35-45% less wall-clock per course). contentFallbackWaitMs is kept as the
// duration used only if the MutationObserver injection itself fails (e.g.
// page.Evaluate erroring on a crashing/navigating page) - the value is
// unchanged from what was live-verified safe as a full-wait duration, so it
// remains a safe fallback duration too.
const contentGotoTimeoutMs = 15000.0
const contentFallbackWaitMs = 1100.0

// mutationObserverDebounceMs/mutationObserverHardCapMs are the
// course_concurrency=1 (serial crawl) budget for the MutationObserver-based
// content-settle wait waitForInteractiveLinks now uses as its primary
// detection technique: wait until the content root stops mutating for
// mutationObserverDebounceMs, or give up and proceed anyway after
// mutationObserverHardCapMs total, whichever comes first.
//
// Live-tested (queue task investigate-load-completion-detection,
// 2026-07-16, `list --dev --debug-clicks --profile` against all 9 real
// discovered TU Dresden courses, 344 files) at these exact values: every
// course's file count matched the fixed-1100ms-wait baseline exactly
// (344/344), wait resolution consistently ~330-440ms (vs. the fixed wait's
// flat 1100ms), 35-45% less wall-clock per course, 38.6% faster on the
// largest course (230.1s vs 375.0s for the 198-file Softwaretechnologie
// course). That live test used course_concurrency=1 only - see
// mutationObserverConcurrentDebounceMs/mutationObserverConcurrentHardCapMs
// below for the wider budget used at course_concurrency>1.
const mutationObserverDebounceMs = 300.0
const mutationObserverHardCapMs = 4000.0

// mutationObserverConcurrentDebounceMs/mutationObserverConcurrentHardCapMs
// are the course_concurrency>1 budget for the same MutationObserver wait -
// wider than the serial budget above for the same reason
// sectionContentRequiredStableReads/showAllExpansionRequiredStableReads
// (crawl.go) are only applied at course_concurrency>1: OPAL's client-side
// render competes for the browser's rendering pipeline (CPU/event loop)
// with however many other courses' tabs are rendering at the same time
// under concurrent crawling, so a debounce/cap tuned against a serial
// crawl (where the renderer has the machine to itself) is not guaranteed
// to be enough once that assumption no longer holds - the same class of
// risk documented on requiredStableReads's doc comment (crawl.go).
//
// Live-tested (same queue task, follow-up run) at course_concurrency=2
// against the real account: the serial budget above (300ms/4000ms)
// resolved via the hard cap far more often under concurrent contention
// (multiple tabs mutating DOM at once rarely let the debounce window go
// quiet), which is safe (candidateStabilityPoll in crawl.go still catches
// any content that hasn't finished rendering by then) but forfeits most of
// the wall-clock win from the debounce actually settling early. Widening
// both values measurably reduced hard-cap fallback frequency at
// concurrency=2 while keeping serial crawls on the tighter budget above
// (this only applies when effectiveCourseConcurrency() > 1 - see
// contentSettleWaitBudget below).
const mutationObserverConcurrentDebounceMs = 500.0
const mutationObserverConcurrentHardCapMs = 6000.0

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
// the content-settle wait below - see the contentFallbackWaitMs doc comment
// above for the live-audit history of why a *selector-based* early-exit was
// removed (it never resolved early, twice, and the obvious fix - waiting for
// Attached instead of Playwright's default Visible - was live-verified to
// cause real file count regressions instead).
//
// Primary technique (2026-07-16, queue task
// add-mutationobserver-load-detection, productionizing queue task
// investigate-load-completion-detection's research): a MutationObserver
// injected via page.Evaluate onto the page's main-content root, resolving
// once that root goes mutationObserverDebounceMs/mutationObserverConcurrentDebounceMs
// without a mutation (or mutationObserverHardCapMs/mutationObserverConcurrentHardCapMs
// elapses, whichever is first) - see waitForContentSettled below. This
// replaces trusting a single fixed duration to always be "enough" with an
// actual completion signal: OPAL/Wicket's section content is constructed
// client-side by JS running *after* the document itself has loaded (network
// trace confirmed no separate "populate content" AJAX request exists to hook
// a response-based wait on instead - see investigate-load-completion-
// detection's Result section), so watching the DOM itself for when that
// construction stops is a direct signal, not a proxy for one - unlike:
//   - `page.WaitForLoadState(LoadStateNetworkidle)` (also live-tested by the
//     same research task): a *network*-based signal for content that isn't
//     gated on network activity - it resolved instantly after any click that
//     itself involved a round-trip (e.g. the "show all" pagination click),
//     giving zero protection for the AJAX-rendered rows that click was
//     supposed to wait for, and lost 32 files in the largest course when
//     used as a full replacement. Do not reintroduce it as a waiting
//     strategy without re-verifying file counts live - see that research
//     task's Result section for the full failure analysis.
//   - the selector-based WaitForSelector attempts above: a *static DOM
//     shape* signal (does element X exist/is it visible) that either never
//     resolves early (Visible) or resolves on page-wide boilerplate that
//     exists long before the section's own content does (Attached) -
//     neither actually tracks *this section's* render completing.
//
// If the MutationObserver injection itself errors, this retries the
// injection once (after a short contextRecoveryWaitMs pause) when the error
// looks like a recoverable "execution context was destroyed" condition
// (isExecutionContextDestroyedError below) rather than falling back
// immediately - live-confirmed necessary at course_concurrency>1 (see that
// function's doc comment for the specific failure this recovers from). If
// the injection still fails after the retry (or the error isn't
// recoverable, e.g. a real page crash), this falls back to a fixed wait
// sized to hardCapMs - not the shorter fallbackWaitMs parameter - since
// reaching this path already means something unusual disrupted the page's
// JS around the same time content was supposed to be settling, which is
// reason for more patience, not less.
//
// This wait is deliberately still followed by condition-based polling
// (waitForStableSectionContent in crawl.go, built on candidateStabilityPoll
// below) rather than being trusted alone, regardless of which technique
// produced it - see candidateStabilityPoll's doc comment for why: OPAL can
// render a section's content in stages (e.g. the file/row list appearing
// before a later-arriving pagination control), and a content-settle wait
// that resolves between those stages cannot tell the difference from
// looking at DOM mutation timing alone. Keeping this safety net independent
// of the wait technique above it is intentional per
// investigate-load-completion-detection's own recommendation - this task
// does not touch it.
func (s *OpalScraper) waitForInteractiveLinks(page playwright.Page, fallbackWaitMs float64) {
	if page == nil {
		return
	}
	debounceMs, hardCapMs := s.contentSettleWaitBudget()
	start := time.Now()
	reason, err := s.waitForContentSettled(page, debounceMs, hardCapMs)
	retried := false
	if err != nil && isExecutionContextDestroyedError(err) {
		retried = true
		page.WaitForTimeout(contextRecoveryWaitMs)
		reason, err = s.waitForContentSettled(page, debounceMs, hardCapMs)
	}
	if err != nil {
		// Either a non-recoverable injection error (e.g. a real page crash -
		// isPageCrashError below still applies to the *next* Goto/extraction
		// attempt as always) or the retry above also failed - fall back to a
		// fixed wait so this section visit still gets *some* content-render
		// wait rather than none. Sized to hardCapMs (not the shorter
		// fallbackWaitMs parameter) - see the doc comment above for why.
		page.WaitForTimeout(hardCapMs)
		s.auditLog("wait-fixed-fallback", page, "", fmt.Sprintf("MutationObserver wait injection failed (%v, retried=%v); used fixed fallback wait of %s (hardCapMs=%.0fms) instead", err, retried, time.Since(start), hardCapMs))
		return
	}
	s.auditLog("wait-mutationobserver", page, "", fmt.Sprintf("MutationObserver wait resolved (%s) after %s (debounce=%.0fms hardCap=%.0fms, retried=%v)", reason, time.Since(start), debounceMs, hardCapMs, retried))
}

// contextRecoveryWaitMs is the short pause waitForInteractiveLinks takes
// before retrying a MutationObserver injection that failed with a
// recoverable "execution context was destroyed" error (see
// isExecutionContextDestroyedError) - just long enough for the new
// execution context that replaced the destroyed one to finish initializing
// before the retry's page.Evaluate targets it.
const contextRecoveryWaitMs = 150.0

// isExecutionContextDestroyedError reports whether err is Playwright's
// "Execution context was destroyed, most likely because of a navigation"
// class of error - distinct from isPageCrashError's "Target/Page crashed"
// class (a permanently dead renderer process). This one is transient and
// recoverable: the underlying page is still alive, its JS realm was just
// torn down and replaced (Playwright's own explanation - "most likely
// because of a navigation" - undersells it here: OPAL/Wicket's
// AJAX-driven DOM updates can apparently trigger this too, not just a real
// page navigation, since it was live-observed to fire against a "show all"
// pagination click that stays on the same URL).
//
// CONFIRMED LIVE (2026-07-16, queue task add-mutationobserver-load-
// detection, `list --dev --debug-clicks --profile --course-concurrency 2`
// against the real TU Dresden account): the MutationObserver injection this
// task added hit exactly this error, immediately after a successful "show
// all" click, for the same courses/sections previously documented (crawl.go,
// sectionContentRequiredStableReads's doc comment) as sensitive to
// concurrent-crawl AJAX timing races - Analysis and Algorithmen und
// Datenstrukturen. Before adding the retry this function enables, that
// error fell straight through to a short fixed fallback wait and produced a
// real file-count regression versus master at course_concurrency=2 (332/344
// vs 344/344, live A/B compared against an unmodified-code control run on
// the same account) - the exact same "tail past the show-all expansion
// missing" shape as the historical race, just from a different proximate
// cause (a destroyed JS execution context instead of a slow click).
// Retrying the injection once after contextRecoveryWaitMs, rather than
// giving up immediately, closed the gap (re-verified 344/344 - see this
// task's queue file for the full before/after comparison). A page.Evaluate
// call against a genuinely crashed page (isPageCrashError) does not produce
// this specific message, so the two error classes stay distinguishable and
// isPageCrashError's existing recovery path (recoverFromPageCrash, a new
// tab entirely) is unaffected by this addition.
func isExecutionContextDestroyedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "execution context was destroyed")
}

// contentSettleWaitBudget returns the (debounceMs, hardCapMs) pair
// waitForInteractiveLinks should use for the current crawl: the wider
// mutationObserverConcurrentDebounceMs/mutationObserverConcurrentHardCapMs
// budget when running at course_concurrency>1, the tighter
// mutationObserverDebounceMs/mutationObserverHardCapMs budget otherwise -
// mirrors requiredStableReads's (crawl.go) concurrency-gated pattern for the
// same underlying reason: a budget tuned against a serial crawl (the
// renderer has the machine to itself) is not guaranteed to still be enough
// once it has to compete with other courses' tabs rendering at the same
// time.
func (s *OpalScraper) contentSettleWaitBudget() (debounceMs, hardCapMs float64) {
	if s.effectiveCourseConcurrency() > 1 {
		return mutationObserverConcurrentDebounceMs, mutationObserverConcurrentHardCapMs
	}
	return mutationObserverDebounceMs, mutationObserverHardCapMs
}

// contentSettleWaitScript is injected via page.Evaluate by
// waitForContentSettled below. It locates the page's main-content root using
// the same rootSelectors precedence as extractSectionContentCandidates
// (files.go) - falling back to document.body when none match, so the
// observer always has something to watch even on an unexpected page shape -
// then resolves a Promise once that root goes opts.debounceMs without a
// DOM mutation (childList/subtree/attributes/characterData all watched, since
// OPAL's Wicket-driven rendering can update any of these while constructing
// a section's content), or opts.hardCapMs elapses since the observer
// started, whichever happens first. Resolves with a string reason
// ("settled-no-mutations" or "hard-cap") purely for --debug-clicks
// diagnostics (see the auditLog call in waitForInteractiveLinks) - callers
// don't need to distinguish the two to proceed safely, since
// waitForStableSectionContent's polling in crawl.go is the actual safety
// net regardless of which reason this resolved for.
const contentSettleWaitScript = `(opts) => new Promise((resolve) => {
	const rootSelectors = [
		'main',
		'[role="main"]',
		'#il_center_col',
		'#center_col',
		'#maincontent',
		'#content',
		'.ilContainerWidth',
		'.ilc_page_cont_page',
		'.o_main_content',
		'.o_page_body',
		'.o_content'
	];
	let root = null;
	for (const selector of rootSelectors) {
		root = document.querySelector(selector);
		if (root) {
			break;
		}
	}
	if (!root) {
		root = document.body;
	}

	let settled = false;
	let debounceTimer = null;

	function finish(reason) {
		if (settled) {
			return;
		}
		settled = true;
		if (debounceTimer) {
			clearTimeout(debounceTimer);
		}
		clearTimeout(hardCapTimer);
		try {
			observer.disconnect();
		} catch (e) {
			// Ignore - nothing left to observe once we're resolving anyway.
		}
		resolve(reason);
	}

	const observer = new MutationObserver(() => {
		if (debounceTimer) {
			clearTimeout(debounceTimer);
		}
		debounceTimer = setTimeout(() => finish('settled-no-mutations'), opts.debounceMs);
	});
	observer.observe(root, {
		childList: true,
		subtree: true,
		attributes: true,
		characterData: true
	});

	debounceTimer = setTimeout(() => finish('settled-no-mutations'), opts.debounceMs);
	const hardCapTimer = setTimeout(() => finish('hard-cap'), opts.hardCapMs);
})`

// waitForContentSettled injects contentSettleWaitScript into page and
// blocks until it resolves (bounded by hardCapMs plus normal Playwright
// Evaluate IPC overhead - the Promise itself enforces the hard cap
// JS-side). Returns the resolution reason ("settled-no-mutations" or
// "hard-cap") on success, or a non-nil error if the injection/evaluation
// itself failed (e.g. the page crashed or navigated away mid-evaluate) -
// callers should treat that as "no content-settle signal available" and
// fall back accordingly, not conflate it with a normal hard-cap resolution.
func (s *OpalScraper) waitForContentSettled(page playwright.Page, debounceMs, hardCapMs float64) (string, error) {
	value, err := page.Evaluate(contentSettleWaitScript, map[string]interface{}{
		"debounceMs": debounceMs,
		"hardCapMs":  hardCapMs,
	})
	if err != nil {
		return "", err
	}
	reason, _ := value.(string)
	return reason, nil
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
