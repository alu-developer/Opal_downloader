package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/mxschmitt/playwright-go"
)

var courseNodeSectionKeyRe = regexp.MustCompile(`(?i)/coursenode/(\d+)(/[^?#]*)?`)

// collectCourseFiles crawls a single course's section/folder tree on page,
// starting from course.URL, and returns every downloadable file found along
// with the downloadCandidates recorded for the browser-fallback download
// path (see appendSectionFiles). page is taken as an explicit parameter
// (rather than defaulting to s.getPage()) so this can be run concurrently
// for multiple courses at once, each on its own Playwright page/tab opened
// against the shared authenticated browser context - see
// collectCourseFilesConcurrently in orchestrator.go. The returned
// downloadCandidates map is local to this call; callers are responsible for
// merging it into any shared state (concurrent writes to a single shared map
// from here would race).
//
// The first return value is always the page the caller should now consider
// "current" for this course - ordinarily the same page passed in, but a
// different (freshly opened) one if a mid-crawl browser crash was recovered
// from (see isPageCrashError/recoverFromPageCrash in navigation.go). Callers
// must close that returned page themselves (not the original one) once
// they're done with it, since a recovered crash already closed the original.
func (s *OpalScraper) collectCourseFiles(page playwright.Page, course CourseRef) (playwright.Page, []FileRef, map[string]downloadCandidate, error) {
	if page == nil {
		return page, nil, nil, errors.New("no page available")
	}
	if course.RepoID == "" {
		return page, nil, nil, errors.New("course repo id is required")
	}

	files := make([]FileRef, 0)
	downloadCandidates := map[string]downloadCandidate{}
	fileSeen := map[string]struct{}{}
	visited := map[string]struct{}{}
	// sectionsVisited/sectionsFailed distinguish a genuinely empty course (every
	// attempted section page loaded and was extracted, just had no file/folder
	// links) from a course whose crawl silently failed outright (queue task
	// fix-course-level-crawl-flakiness, 2026-07-13). Before this, a section that
	// hit the Goto-retry-fails or extraction-retry-fails branches below just
	// logged a "Warning: skipping section" line and `continue`d - if that
	// happened to be the *only* URL ever queued for this course (most commonly
	// the root section, before any subfolder links could be discovered from it),
	// the loop then exited with files still empty and no error, which is
	// indistinguishable downstream from a real 0-file course (see
	// newCourseFileCollector's "crawled successfully but found 0 files" warning
	// in orchestrator.go). sectionsVisited only increments once a section's
	// Goto+extraction actually succeeded (even if it then had zero candidates -
	// that's a legitimate empty-section result, not a failure); sectionsFailed
	// increments on each `continue` below. If every attempted section failed
	// (sectionsVisited stays 0 while sectionsFailed > 0), that's reported as a
	// real error instead of a clean empty result - see the check after the loop.
	//
	// NOTE: live testing 2026-07-12/13 (queue task fix-course-level-crawl-flakiness)
	// ran this exact crawl twice in a row at course_concurrency=1 against the real
	// TU Dresden account and got byte-identical per-course file counts both times,
	// with zero section-level Goto/extraction failures - the originally reported
	// "0 files" flakiness for a course with real content did not reproduce at
	// concurrency=1. That flakiness was root-caused by PR #64/#65 to an AJAX-render
	// race specific to *concurrent* course crawling, not to this per-section
	// retry logic. This hardening covers a latent gap the acceptance criteria
	// still calls for (a genuinely-failed root section reporting the same as a
	// genuinely-empty course), not a live-reproduced bug in this retry logic itself.
	sectionsVisited := 0
	sectionsFailed := 0
	rootKey := sectionKey(course.URL, course.RepoID)
	queue := []string{course.URL}
	queued := map[string]struct{}{rootKey: {}}
	// sectionTitles carries the human-readable folder/section title discovered
	// when a section link was queued (via appendSectionFolderTargets, which
	// already extracts real OPAL folder names like "Übungen"/"Vorlesung" using
	// deriveSectionTitle). Without this, nested course content would only be
	// identifiable by deriveSectionTitleFromURL's coarse URL-shape guess, which
	// cannot recover the actual OPAL-assigned section name.
	sectionTitles := map[string]string{rootKey: course.Title}
	// maxPages is a sanity cap against runaway crawls, not the primary loop
	// bound - the visited/queued dedup above already prevents infinite loops
	// on legitimately distinct sections. It was 16 until a live run against
	// real OPAL courses (queue task fix-list-flaky-missing-files) showed
	// courses with 100+ genuine sections (weekly folders, exercise sheets,
	// etc.) silently losing most of their content to this cap - not
	// randomly, but deterministically for any course past the 16th BFS
	// section, which read as "flaky missing files" because which files
	// landed in the first 16 varied with section-discovery order.
	maxPages := 500

	// The crawl proceeds one BFS *level* at a time: everything currently in
	// the queue is popped together, its members are visited concurrently on
	// their own tabs, and the results are then merged strictly in the order
	// they were popped.
	//
	// WHY LEVELS RATHER THAN A FREE-RUNNING WORKER QUEUE. The merge phase is
	// where every shared structure is touched - fileSeen's dedupe, the visit
	// log, and appendSectionFolderTargets appending the next level's URLs.
	// Doing it serially in pop order means `files`, `queue` and the dedupe
	// outcomes come out identical to a serial crawl, whatever order the pages
	// happened to finish rendering in. The only thing that becomes concurrent
	// is page rendering.
	//
	// That matters here more than it usually would: every previous concurrency
	// change in this crawl that went wrong went wrong *silently*, losing files
	// while looking faster (see config.DefaultCourseConcurrency's note). A
	// design whose output cannot depend on completion order removes an entire
	// class of that.
	sectionWorkers := s.effectiveSectionConcurrency()
	pool := newSectionTabPool(s, page, sectionWorkers)
	defer pool.closeExtras()

	for len(queue) > 0 && len(visited) < maxPages {
		// Pop a whole level. Taking everything currently queued is exactly
		// what a serial FIFO would have drained before reaching any child
		// discovered from within this level, so level boundaries here are the
		// BFS's own.
		level := make([]string, 0, len(queue))
		levelKeys := make([]string, 0, len(queue))
		for len(queue) > 0 && len(visited) < maxPages {
			currentURL := queue[0]
			queue = queue[1:]
			currentKey := sectionKey(currentURL, course.RepoID)
			delete(queued, currentKey)
			if _, ok := visited[currentKey]; ok {
				continue
			}
			visited[currentKey] = struct{}{}
			level = append(level, currentURL)
			levelKeys = append(levelKeys, currentKey)
		}
		if len(level) == 0 {
			continue
		}

		// Titles are snapshotted into a slice rather than read from
		// sectionTitles inside the workers. Concurrent map *reads* would be
		// safe today, because the only writer (appendSectionFolderTargets)
		// runs in the serial merge below - but that is a property of where the
		// call sites happen to sit, not something the compiler will keep true.
		// Moving one write into the concurrent phase later would turn this into
		// a data race, in the part of the codebase least able to afford one.
		levelTitles := make([]string, len(level))
		for i, key := range levelKeys {
			levelTitles[i] = sectionTitles[key]
		}

		visits := pool.visitAll(level, func(i int) string { return levelTitles[i] })

		for i, visit := range visits {
			currentURL := level[i]
			currentKey := levelKeys[i]
			if visit.failed {
				// The visit already printed the specific warning; this is the
				// serial loop's `continue` branch, just accounted for here.
				sectionsFailed++
				continue
			}
			// Reaching here means this section's Goto and extraction both
			// succeeded (candidates may still legitimately be empty - the
			// empty-content warning lives in visitSection and does not mark
			// the visit failed). See sectionsVisited's doc comment above for
			// what this distinction is for.
			sectionsVisited++
			s.publishProgress(DiscoveryProgress{
				Phase:        PhaseSection,
				Course:       course.Title,
				Section:      sectionTitles[currentKey],
				SectionURL:   currentURL,
				SectionsDone: sectionsVisited,
			})

			candidates := visit.candidates
			sectionTitle := sectionTitles[currentKey]
			if strings.TrimSpace(sectionTitle) == "" {
				sectionTitle = deriveSectionTitleFromURL(course.Title, currentURL)
			}
			section := SectionRef{CourseRepoID: course.RepoID, Title: sectionTitle, URL: currentURL}
			filesBeforeSection := len(files)
			// showAllViaClick marks every candidate from this section when expansion
			// happened via an in-place click rather than navigating to a distinct
			// showAllURL (see downloadCandidate.ShowAllViaClick's doc comment) - these
			// files have no separate URL for the download-fallback to retry, so it
			// needs to know to re-click the same control on SourceURL instead.
			showAllViaClick := visit.expandedShowAll && strings.TrimSpace(visit.showAllURL) == ""
			expandedPageURL := visit.expandedPageURL
			if !visit.expandedShowAll {
				expandedPageURL = ""
			}
			files = appendSectionFiles(files, fileSeen, candidates, course, section, currentURL, visit.showAllURL, expandedPageURL, showAllViaClick, s.opalURL, downloadCandidates)
			// Record this visit for the persistent cross-run visit-effectiveness
			// log (internal/visitlog) - one entry per section actually reached
			// (Goto+extraction succeeded, past the `continue`s above), noting how
			// many *new* files this visit contributed. This is purely
			// observational (see visitlog's package doc): it does not change
			// what gets crawled, just records it for later human review.
			s.recordSectionVisit(course.Title, sectionTitle, currentURL, len(files)-filesBeforeSection)
			var skipped []skippedSection
			queue, skipped = appendSectionFolderTargets(queue, queued, visited, candidates, s.opalURL, course.RepoID, currentURL, course.URL, course.Title, sectionTitles, s.skipEnrollmentSections)
			for _, sk := range skipped {
				// Auditable, not silent - see appendSectionFolderTargets's doc
				// comment. Deliberately a distinct log line rather than
				// s.recordSectionVisit: that call is reserved for sections whose
				// page was actually navigated to and extracted (see its own doc
				// comment), which this section never was - recording it there
				// would misrepresent the persistent cross-run visit-effectiveness
				// log (internal/visitlog) as having visited a page it never
				// loaded.
				logging.Detail("Skipping section %q (%s): structurally cannot hold files (OPAL enrollment/Einschreibung course-node)", sk.Title, sk.URL)
			}
		}
	}
	// The caller's tab may have been swapped out by crash recovery inside the
	// pool; hand back whichever page is now theirs.
	page = pool.primary()

	if len(queue) > 0 && len(visited) >= maxPages {
		logging.Warn("course %q hit the %d-section crawl cap with %d section(s) still queued; some content may be missing", course.Title, maxPages, len(queue))
	}

	if sectionsVisited == 0 && sectionsFailed > 0 {
		// Every section this course attempted to visit (most commonly just the
		// root section, since a failed root never gets to queue any subfolder
		// links) hit a Goto/extraction failure - see sectionsVisited's doc
		// comment above. Reporting this as a real error (rather than returning
		// nil the way a genuinely empty/successfully-crawled course does) makes
		// newCourseFileCollector (orchestrator.go) log a distinct "Course crawl
		// error" instead of the misleading "crawled successfully but found 0
		// files" line, and drops the course from the results entirely rather
		// than silently reporting it as confirmed-empty.
		return page, files, downloadCandidates, fmt.Errorf("course %q: all %d attempted section page(s) failed to load or extract - result is incomplete, not a confirmed empty course", course.Title, sectionsFailed)
	}

	return page, files, downloadCandidates, nil
}

// sectionVisit is the result of the navigate-and-extract half of one BFS step.
//
// It exists so that half can run on its own tab, concurrently with its
// siblings, while everything that mutates the crawl's shared state
// (appendSectionFiles and its fileSeen dedupe, recordSectionVisit,
// appendSectionFolderTargets and the queue it feeds) stays serial and in the
// level's original order. That split is the whole safety argument for section
// concurrency: the only thing that becomes concurrent is page rendering, and
// the merge produces byte-identical output to a serial crawl.
type sectionVisit struct {
	url   string
	key   string
	title string

	candidates      []map[string]string
	showAllURL      string
	expandedPageURL string
	expandedShowAll bool

	// failed marks a section whose Goto or extraction never succeeded, i.e.
	// one the serial loop would have `continue`d past after counting it in
	// sectionsFailed. Its warning has already been printed by the visit.
	failed bool
}

// visitSection performs one BFS step's navigation, settle wait, stability
// poll and show-all expansion on the given page, and reports what it found.
//
// This is lifted verbatim out of the serial crawl loop - including both crash
// recovery branches and the retry-once-on-transient-Goto-error behaviour - so
// that running it on several tabs at once changes where it runs, not what it
// does. It returns the page the caller should keep using, which differs from
// the one passed in when a crash was recovered from.
func (s *OpalScraper) visitSection(page playwright.Page, currentURL, sectionTitle string) (playwright.Page, sectionVisit) {
	visit := sectionVisit{url: currentURL, title: sectionTitle}

	if _, err := s.gotoPolitely(page, currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); err != nil {
		if isPageCrashError(err) {
			// A crashed page is permanently unusable - retrying on it (like
			// the transient-error branch below does) would just crash again
			// and, worse, keep using the same dead page for every remaining
			// section. See isPageCrashError's doc comment for the
			// live-confirmed cascade this caused. Recover onto a fresh tab
			// before retrying.
			newPage, recErr := s.recoverFromPageCrash(page)
			if recErr != nil {
				logging.Warn("skipping section %q (%s): browser tab crashed and could not be recovered: %v (original error: %v)", sectionTitle, currentURL, recErr, err)
				visit.failed = true
				return page, visit
			}
			page = newPage
		} else {
			// Retry once after a short wait: net::ERR_ABORTED and similar are
			// commonly transient (a competing in-page navigation/redirect
			// racing our Goto), confirmed live - the same section often
			// succeeds on a second attempt.
			page.WaitForTimeout(contentFallbackWaitMs)
		}
		if _, retryErr := s.gotoPolitely(page, currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); retryErr != nil {
			logging.Warn("skipping section %q (%s): navigation failed after retry: %v", sectionTitle, currentURL, retryErr)
			visit.failed = true
			return page, visit
		}
	}
	visitStart := time.Now()
	settleStart := time.Now()
	// Not overhead in front of the stability poll below, though it looked like
	// it: measured 2026-07-27, skipping this wait is byte-identical and 51%
	// SLOWER (317.1s vs 210.3s), and skipping it while asserting the calm
	// verdict it would have produced is still 293.5s. The poll is the
	// expensive way to wait - every iteration is a full DOM extraction - and
	// this is the cheap way. See docs/sync-speed-campaign.md.
	_, sectionCalm := s.waitForInteractiveLinks(page, contentFallbackWaitMs)
	settleSpent := time.Since(settleStart)

	// waitForStableSectionContent polls extraction until the candidate
	// count stabilizes (see its doc comment and candidateStabilityPoll's
	// in navigation.go) rather than trusting the fixed wait above plus a
	// single fixed-wait retry to always be enough - the latter was
	// live-confirmed (queue task increase-parallel-tab-concurrency,
	// 2026-07-12) to sometimes not be, once several pages are rendering
	// under concurrent load at once, up to and including an entire course
	// silently coming back with 0 files. Section concurrency makes that
	// budget matter more, not less.
	stableStart := time.Now()
	candidates, err := s.waitForStableSectionContent(page, sectionCalm)
	stableSpent := time.Since(stableStart)
	// Recorded before the error branches below, so a section that fails after
	// waiting still contributes the time it spent waiting - otherwise the
	// measurement would quietly exclude the slowest cases.
	defer func() { s.sectionTiming.record(settleSpent, stableSpent, time.Since(visitStart)) }()
	if s.sectionProbe != nil {
		s.sectionProbe(settleSpent, stableSpent, len(candidates))
	}
	if err != nil {
		if isPageCrashError(err) {
			newPage, recErr := s.recoverFromPageCrash(page)
			if recErr != nil {
				logging.Warn("skipping section %q (%s): content extraction crashed and the tab could not be recovered: %v (original error: %v)", sectionTitle, currentURL, recErr, err)
				visit.failed = true
				return page, visit
			}
			page = newPage
			// The replacement tab starts blank; it has to be navigated back
			// to currentURL before there is anything for extraction to read.
			if _, gotoErr := s.gotoPolitely(page, currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); gotoErr != nil {
				logging.Warn("skipping section %q (%s): content extraction crashed; re-navigating the replacement tab failed: %v", sectionTitle, currentURL, gotoErr)
				visit.failed = true
				return page, visit
			}
			_, sectionCalm = s.waitForInteractiveLinks(page, contentFallbackWaitMs)
			candidates, err = s.waitForStableSectionContent(page, sectionCalm)
		}
		if err != nil {
			logging.Warn("skipping section %q (%s): content extraction failed after retry: %v", sectionTitle, currentURL, err)
			visit.failed = true
			return page, visit
		}
	}

	if len(candidates) == 0 {
		// waitForStableSectionContent already polled for up to
		// sectionContentMaxPolls*sectionContentPollIntervalMs beyond the
		// initial fixed wait looking for growing content before returning
		// here, so reaching a still-empty result is a much stronger signal
		// than it used to be under the old single-fixed-wait-retry scheme -
		// but it can still legitimately mean either a genuinely empty
		// section or an unusually slow render that outlasted even this
		// budget.
		// Logs page.URL() to distinguish "genuinely rendered the right
		// page with 0 items" from "landed on an unexpected page" (e.g. a
		// session-state mixup under concurrent load serving the wrong
		// content for this tab) - cheap, and directly useful for any
		// future incident matching this warning (see docs/OPERATIONS.md).
		actualPageURL := ""
		if page != nil {
			actualPageURL = page.URL()
		}
		logging.Warn("section %q (%s) returned no content after polling for stable render; it may be genuinely empty, or files may have been dropped (page.URL()=%s)", sectionTitle, currentURL, actualPageURL)
	}

	var showAllCandidates []map[string]string
	page, showAllCandidates, visit.showAllURL, visit.expandedPageURL, visit.expandedShowAll = s.expandShowAllInSection(page, currentURL, candidates)
	if visit.expandedShowAll {
		candidates = showAllCandidates
	}
	visit.candidates = candidates
	return page, visit
}

// expandShowAllInSection looks for OPAL's "Alle anzeigen" ("show all") pagination
// control among the already-extracted candidates for the current section/folder page
// and, if found, expands the file list and re-extracts candidates so the caller sees
// every file rather than just the first page (OPAL's table/folder views commonly cap
// a page at ~20 items by default).
//
// This handles the expansion as part of the single visit to currentURL - clicking or
// following the "show all" control does not change the page's canonical URL, so the
// crawl loop's visited/queued dedupe (keyed by sectionKey) is untouched and this
// cannot cause a requeue or infinite loop.
//
// CONFIRMED LIVE 2026-07-12 (queue task fix-show-all-pagination-unverified-guesswork,
// against this account's real "Analysis" course, whose "Übungsblätter" section has 28
// files - more than the ~20-item default page size): the control really is a text link
// reading "Alle anzeigen" pre-expansion, and clicking it really does work - both the
// text-needle match and the .Click() call in the loop below succeeded, live, exactly as
// originally guessed. The actual bug was not a wrong selector: it was that the fixed
// contentFallbackWaitMs wait used after the click was sometimes not long enough for the
// Wicket-AJAX-driven expansion to finish rendering the extra rows before extraction ran
// - confirmed by comparing an isolated single-course crawl (captured all 28 files) against
// the original bug report's concurrent course_concurrency=3 `list` run (captured only the
// pre-expansion 20), i.e. a timing race that gets worse under crawl concurrency/load, not
// a detection failure. See showAllControlClassNeedle's doc comment (files.go) for the
// confirmed "pager-showall" CSS class - now the primary detection/click signal, since it
// (unlike the text, which toggles to "Seiten" once expanded) does not depend on which
// state the control happens to be in - and waitForStableExpandedCandidates below for the
// polling fix that replaced the single fixed wait.
//
// It returns the page the caller should now consider "current" (see below), the
// re-extracted candidate list, the resolved "show all" URL (when the expansion was
// reached via direct navigation to a distinct URL rather than a click on a
// javascript:/onclick-driven control), and true when a "show all" control was found
// and acted on; otherwise it returns (page, nil, "", false) and the caller keeps using
// the candidates it already had.
//
// The returned URL lets callers record where files revealed only by this expansion can
// be found again later (e.g. for a browser-fallback download click). This function
// deliberately leaves page on the "show all" URL rather than navigating back to
// currentURL afterwards (perf-04 removed that extra round-trip): collectCourseFiles's
// caller only consumes the returned candidates slice and plain string URLs
// (appendSectionFiles/appendSectionFolderTargets take currentURL as a parameter, not
// page.URL()), and the crawl loop's next iteration unconditionally navigates page to
// whatever URL it dequeues next - so nothing downstream ever reads page's location
// between here and that next Goto. Do not reintroduce the navigate-back without first
// checking that invariant still holds.
//
// The returned page is ordinarily the same one passed in, but a freshly recovered one
// if a Playwright crash (isPageCrashError) was hit while expanding - see
// recoverAndReturnToSection below. Callers must use the returned page for anything
// after this call, not the one they passed in, the same way collectCourseFiles's main
// loop already does for its own Goto/extraction crash recovery.
func (s *OpalScraper) expandShowAllInSection(page playwright.Page, currentURL string, candidates []map[string]string) (playwright.Page, []map[string]string, string, string, bool) {
	if page == nil {
		return page, nil, "", "", false
	}

	linkTarget, found := findShowAllTarget(candidates)
	if !found {
		return page, nil, "", "", false
	}

	absURL := resolveURL(s.opalURL, linkTarget)
	navigated := false
	if looksLikeNavigableShowAllURL(linkTarget) {
		// Prefer navigating directly to the "show all" URL over clicking: it's a
		// plain link with a resolvable href, and direct navigation is more robust
		// in headless mode than dispatching a click event.
		_, err := s.gotoPolitely(page, absURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)})
		switch {
		case err == nil:
			navigated = true
		case isPageCrashError(err):
			return s.recoverAndReturnToSection(page, currentURL)
		}
	}

	// expansionSignalled records that Wicket itself told us the expansion AJAX
	// call completed successfully, so the DOM is final and a single read is
	// authoritative - no count-stability guessing needed. See wicket.go.
	expansionSignalled := false

	if !navigated {
		// Arm the watch BEFORE clicking: Wicket only delivers its topics to
		// subscribers present when the call fires, and the expansion can
		// complete in ~160ms.
		watchArmed, armErr := armWicketExpansionWatch(page)
		if armErr != nil && !isWicketWatchUnavailableError(armErr) {
			s.auditLog("wicket-arm", page, "", fmt.Sprintf("could not arm Wicket expansion watch for section %s: %v", currentURL, armErr))
		}

		clicked, crashErr := s.attemptShowAllExpandClick(page, currentURL)
		if crashErr != nil {
			return s.recoverAndReturnToSection(page, currentURL)
		}
		if !clicked {
			// Could not click or navigate to the control; keep whatever candidates
			// the caller already extracted rather than failing the whole section.
			warnShowAllTruncated(currentURL, len(candidates), "the control could not be activated")
			return page, nil, "", "", false
		}

		if watchArmed {
			signalled, failed := awaitWicketExpansionDone(page, wicketExpansionSignalTimeoutMs)
			switch {
			case signalled && failed:
				// A REAL failure signal from the framework, replacing the
				// isExecutionContextDestroyedError string-match proxy that
				// previously stood in for "the expansion was dropped". Re-click
				// once, exactly as that proxy did - bounded, not a retry loop.
				s.auditLog("wicket-expand", page, "", fmt.Sprintf("Wicket reported AJAX_CALL_FAILURE for show-all expansion on %s; re-clicking once", currentURL))
				rearmed, _ := armWicketExpansionWatch(page)
				reclicked, reclickCrashErr := s.attemptShowAllExpandClick(page, currentURL)
				if reclickCrashErr != nil {
					return s.recoverAndReturnToSection(page, currentURL)
				}
				if reclicked && rearmed {
					retrySignalled, retryFailed := awaitWicketExpansionDone(page, wicketExpansionSignalTimeoutMs)
					expansionSignalled = retrySignalled && !retryFailed
				}
			case signalled:
				expansionSignalled = true
			}
		}
	}

	// A successful AJAX_CALL_DONE means the expansion call has completed, so
	// the fixed contentFallbackWaitMs settle wait has nothing left to wait
	// for and is skipped. The stability poll below still runs.
	//
	// It deliberately does NOT skip that poll. An earlier version of this
	// change treated the signal as "DOM is final, read once" on the strength
	// of the research task's trailing-safe finding (0/8 parity mismatches).
	// A live full-account parity run refuted that: 290 files vs a 342-file
	// baseline, 52 lost, every one of them the tail of the largest paginated
	// section in Softwaretechnologie - i.e. exactly the past-page-1 loss this
	// waiter exists to prevent. The signal reliably marks a call as finished;
	// it does not, on large expansions, mark the resulting DOM as complete.
	if !expansionSignalled {
		contextWasDestroyed, _ := s.waitForInteractiveLinks(page, contentFallbackWaitMs)
		if contextWasDestroyed && !navigated {
			// THE ROOT CAUSE this task (fix-candidate-stability-poll-concurrent-
			// crawl-race) set out to find: see waitForInteractiveLinks's doc
			// comment (navigation.go) for the full live evidence. In short, an
			// "execution context was destroyed" event landing right after this
			// click - not caused by a real navigation, since the direct-navigate
			// branch above is excluded here via !navigated - is near-certain
			// proof the click's own in-flight AJAX expansion response had
			// nowhere left to land and was silently dropped, not merely slow.
			// waitForStableExpandedCandidates polling longer afterward cannot
			// recover content that was never coming; re-issuing the click is
			// the only way to actually get the expansion to happen. Bounded to
			// one extra attempt (attemptShowAllExpandClick internally retries up
			// to showAllClickMaxAttempts on its own) rather than looping, to
			// keep this a targeted response to a specific confirmed signal
			// rather than an open-ended "try harder" loop.
			reclicked, crashErr := s.attemptShowAllExpandClick(page, currentURL)
			if crashErr != nil {
				return s.recoverAndReturnToSection(page, currentURL)
			}
			if reclicked {
				s.waitForInteractiveLinks(page, contentFallbackWaitMs)
			}
		}
	}

	expanded, err := s.waitForStableExpandedCandidates(page)
	if err != nil {
		if isPageCrashError(err) {
			return s.recoverAndReturnToSection(page, currentURL)
		}
		warnShowAllTruncated(currentURL, len(candidates), fmt.Sprintf("reading the expanded list failed: %v", err))
		return page, nil, "", "", false
	}
	if len(expanded) == 0 {
		warnShowAllTruncated(currentURL, len(candidates), "the expanded list came back empty")
		return page, nil, "", "", false
	}
	// The expansion "succeeded" and produced no more rows than the collapsed
	// page had. That is the silent shape of this failure: nothing errored, the
	// section is simply capped at OPAL's default page size and the tail is
	// gone.
	//
	// This is not hypothetical. On 2026-07-26 a course_concurrency=2 run lost
	// exactly 9 files against a serial baseline, and the visit log placed all
	// nine in one section - "Übungsblätter", 29 files collapsing to 20, which
	// is precisely one OPAL page. Nothing in the run said so; it reported
	// success. See docs/sync-speed-campaign.md.
	// Report the two lists themselves before reducing them to a comparison.
	// See showAllProbe's doc comment (scraper.go) for why the counts alone
	// were not enough to diagnose Question 18.
	if s.showAllProbe != nil {
		s.showAllProbe(currentURL, candidates, expanded)
	}

	if len(expanded) <= len(candidates) {
		warnShowAllTruncated(currentURL, len(candidates),
			fmt.Sprintf("expansion completed but added nothing (%d rows before, %d after)", len(candidates), len(expanded)))
	}

	// Record the show-all URL (when distinct from currentURL) so the caller can point
	// later re-visits (e.g. download fallback) at the page where these files actually
	// render. See the doc comment above for why this intentionally does not navigate
	// page back to currentURL.
	showAllURL := ""
	if navigated && !strings.EqualFold(strings.TrimSpace(absURL), strings.TrimSpace(currentURL)) {
		showAllURL = absURL
	}

	expandedPageURL := ""
	if !navigated {
		expandedPageURL = expandedPageURLAfterClick(page, currentURL)
	}

	return page, expanded, showAllURL, expandedPageURL, true
}

// warnShowAllTruncated reports a section that advertised a "show all" control
// which then did not deliver more rows.
//
// WHY THIS IS WORTH A LINE OF OUTPUT. A failed expansion is invisible
// everywhere else: the section keeps the rows it already had, the crawl
// reports success, and the only trace is a file count that nobody has a
// baseline for. That is how course_concurrency=2 lost nine files on
// 2026-07-26 while reporting a clean run - the loss was found by diffing a
// visit log against a serial baseline hours later, which is not a
// detection mechanism anyone can rely on.
//
// The presence of the control is the useful part: it means OPAL itself said
// there is more than one page here. When expansion then adds nothing, the
// section is truncated at the default page size, and that is an incident to
// investigate rather than a normal outcome (see this repo's "reliability over
// features" principle).
//
// Deliberately only a warning. It changes no crawl behaviour, adds no retry
// and no timing, so it cannot itself cause the loss it reports.
//
// UNVERIFIED IN THE WILD, and here is exactly what was tried. A deliberately
// lossy run (`--section-concurrency 4`, which reliably drops files) produced
// **zero** of these warnings while losing 160 of 345 files. That is not a bug
// in the warning - it is a different failure mode, and the distinction is
// worth keeping straight:
//
//   - Course-concurrency loss (what this catches): the section renders its
//     file table, the "show all" control is found, and expanding it adds
//     nothing. 29 rows stay 20.
//   - Section-concurrency loss (what that run had): the file table never
//     renders at all, so there is no "show all" control to find and this
//     function returns at `!found` long before any of these checks.
//
// Catching the second case needs a different signal - a section that used to
// have files coming back with none - which is cross-run knowledge this process
// does not have. `scripts/compare-visit-runs.ps1` is that check, offline.
//
// So this fires on the mechanism that was actually diagnosed, and has not yet
// been observed firing, because that mechanism appears roughly one run in five
// and has not recurred since.
func warnShowAllTruncated(sectionURL string, rowsBefore int, reason string) {
	logging.Warn("section %s offered a \"show all\" control but the expansion did not add any files (%s); "+
		"this section is capped at its first page (%d rows) and later files are missing",
		sectionURL, reason, rowsBefore)
}

// expandedPageURLAfterClick returns page's *current* location now that a
// click-driven expansion has rendered - which is not currentURL but currentURL
// plus OPAL/Wicket's page-instance counter (observed live 2026-07-20:
// ".../CourseNode/<id>/Vorlesung?1032"). Wicket addresses each rendered page
// instance by that counter and keeps it in the session, so re-requesting this
// exact URL later can serve the *already-expanded* render rather than the
// collapsed first page that a plain re-fetch of currentURL always returns.
// That distinction is the whole reason beyond-the-first-page files in a
// click-expanded section systematically miss the counter-refresh fast path
// (see download_refresh.go): its plain HTTP re-fetch of currentURL simply does
// not contain their anchor. Kept as an extra retry target only
// (downloadCandidate.ExpandedPageURL), never as a replacement for SourceURL -
// a page instance can be evicted from the session, in which case this URL just
// re-renders collapsed and the retry falls through to the existing click-based
// path exactly as before.
//
// Returns "" when the URL is unchanged from currentURL (nothing extra to
// record). Callers must only use this on the click-driven branch; a direct
// navigation to the show-all URL is recorded as showAllURL instead.
func expandedPageURLAfterClick(page playwright.Page, currentURL string) string {
	if page == nil {
		return ""
	}
	expandedPageURL := strings.TrimSpace(page.URL())
	if strings.EqualFold(expandedPageURL, strings.TrimSpace(currentURL)) {
		return ""
	}
	return expandedPageURL
}

// wicketExpansionSignalTimeoutMs bounds how long expandShowAllInSection waits
// for Wicket's AJAX_CALL_DONE after clicking "show all", before giving up on
// the exact signal and falling back to the contentFallbackWaitMs settle plus
// waitForStableExpandedCandidates poll.
//
// 4s is >20x the 156-184ms the signal was measured to take live, so it is a
// generous ceiling rather than a tuned value, while still capping what a
// non-firing case (page-instance eviction, or a click that triggered no AJAX
// at all) adds on top of the unchanged fallback path. It deliberately does not
// try to be tight: the correctness bar here is byte-for-byte file parity, and
// spending a few extra seconds on a rare section is strictly preferable to
// concluding an expansion finished when it did not.
const wicketExpansionSignalTimeoutMs = 4000.0

// attemptShowAllExpandClick tries to click the "show all"/"Alle anzeigen" pagination
// control on page (already navigated to sectionURL), trying the confirmed structural CSS
// class first (more robust than the text-needle fallback, which depends on exact
// wording/locale and stops matching once the control's own label toggles from "Alle
// anzeigen" to "Seiten" after a successful expansion), retried up to
// showAllClickMaxAttempts times with a generous showAllClickTimeoutMs actionability
// budget each - see that constant's doc comment for why a single short-timeout attempt
// was previously root-caused to silently lose files under concurrent course crawling.
//
// Extracted from expandShowAllInSection's own click-based (non-navigable) branch so
// download.go's clickCandidateLinkOnPage can replay the identical click sequence on a
// download-fallback revisit of SourceURL - see downloadCandidate.ShowAllViaClick's doc
// comment for why that revisit needs its own click, not just a URL to navigate to.
//
// Returns clicked=true if a click succeeded. crashErr is non-nil only when a Playwright
// page-crash was hit mid-attempt (isPageCrashError) - callers that can recover a fresh
// page (like expandShowAllInSection, via recoverAndReturnToSection) should do so; callers
// that can't (like the download-fallback, which has no equivalent recovery path and just
// wants a best-effort retry) can treat any non-nil crashErr the same as clicked=false.
func (s *OpalScraper) attemptShowAllExpandClick(page playwright.Page, sectionURL string) (clicked bool, crashErr error) {
	for attempt := 0; attempt < showAllClickMaxAttempts && !clicked; attempt++ {
		if attempt > 0 {
			page.WaitForTimeout(showAllClickRetryWaitMs)
		}
		classSelector := "." + showAllControlClassNeedle
		classLocator := page.Locator(classSelector).First()
		s.auditLog("click", page, classSelector, fmt.Sprintf("show-all expand attempt (class) for section %s (try %d/%d)", sectionURL, attempt+1, showAllClickMaxAttempts))
		switch err := classLocator.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(showAllClickTimeoutMs)}); {
		case err == nil:
			s.auditLog("click-success", page, classSelector, "show-all expand succeeded (class) for section "+sectionURL)
			clicked = true
		case isPageCrashError(err):
			return false, err
		}

		if !clicked {
			for _, needle := range showAllControlTextNeedles {
				locator := page.GetByText(needle, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First()
				s.auditLog("click", page, needle, fmt.Sprintf("show-all expand attempt for section %s (try %d/%d)", sectionURL, attempt+1, showAllClickMaxAttempts))
				err := locator.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(showAllClickTimeoutMs)})
				if err == nil {
					s.auditLog("click-success", page, needle, "show-all expand succeeded for section "+sectionURL)
					clicked = true
					break
				}
				if isPageCrashError(err) {
					return false, err
				}
			}
		}
	}
	return clicked, nil
}

// showAllClickTimeoutMs/showAllClickMaxAttempts/showAllClickRetryWaitMs bound
// expandShowAllInSection's click attempts on the "show all" pagination
// control.
//
// THE ACTUAL ROOT CAUSE of this task (queue task
// fix-concurrent-crawl-ajax-race-and-raise-concurrency): PR #64/#65's
// waitForStableExpandedCandidates (below) already correctly poll-waits for
// the *post-click* AJAX-rendered rows to finish appearing, and this task's
// original hypothesis - generalizing that same poll to the main per-section
// content wait (see waitForStableSectionContent) - turned out NOT to be
// what was actually causing files to go missing at course_concurrency>1.
// Live A/B testing against the real TU Dresden account (8 courses, 341
// files) with that content-wait fix alone applied showed byte-identical
// losses to the unfixed code at course_concurrency=3 (Analysis: 21/29 files,
// Algorithmen und Datenstrukturen: 32/34, both runs) - the same course,
// same missing files, both with and without the content-stability-poll fix.
// Diffing the actual missing files (not just counts) showed the loss was
// always exactly the *tail* past OPAL's ~20-item default page size (e.g.
// Analysis21.pdf..Analysis28.pdf missing, Analysis01..20.pdf present) - the
// same shape every time, which a render-timing race (which would lose a
// varying/random subset) does not explain, but a click that never actually
// registered does: the section's *un-expanded* candidate list is exactly
// the first page.
//
// The real cause: this Click() call's own Timeout (3000ms before this fix)
// is Playwright's budget for waiting until the "show all" control becomes
// *actionable* (attached, visible, has a stable bounding box across
// consecutive frames, unobscured, receives pointer events) - not a
// render-content wait at all. Under concurrent course crawling, the
// browser's compositor/paint pipeline is servicing several tabs' renders at
// once, which can delay a specific element's layout from stabilizing long
// enough to exceed a 3s budget, especially against a background of a
// large/slow-rendering course crawling concurrently (confirmed live: the
// worst losses coincided with "Softwaretechnologie (SoSe 26)", a 198-file
// course that alone takes ~5 minutes, crawling at the same time). When the
// click times out, expandShowAllInSection gives up entirely and returns the
// caller's already-extracted (un-expanded, first-page-only) candidates -
// silently, with no error - which is exactly the observed symptom.
// Confirmed the fix: raising this timeout and retrying the whole
// class-then-text click sequence up to showAllClickMaxAttempts times
// resolved the same live test (see DefaultCourseConcurrency's doc comment
// in internal/config/config.go for the concurrency level this was
// ultimately verified safe at).
const showAllClickTimeoutMs = 10000.0
const showAllClickMaxAttempts = 3
const showAllClickRetryWaitMs = 1500.0

// showAllExpansionPollIntervalMs/showAllExpansionMaxPolls/
// showAllExpansionRequiredStableReads bound waitForStableExpandedCandidates
// below: up to an extra ~6s (15 * 400ms), on top of the initial
// contentFallbackWaitMs wait, spent re-extracting until the candidate count
// stops growing across showAllExpansionRequiredStableReads consecutive
// reads. See expandShowAllInSection's doc comment for the live-confirmed
// race this fixes - a fixed-duration wait after a successful "show all"
// click is not reliably enough time for OPAL's Wicket-AJAX-driven row
// expansion to finish rendering, especially under concurrent multi-course
// crawl load.
//
// requiredStableReads=3 (raised from an initial 2) was live-confirmed
// necessary, not just cheap insurance: with requiredStableReads=2, a live
// full-8-course concurrency=3 run still lost exactly 2 files
// (Vorlesung_9_10.pdf, the tail of a paginated section) in "Algorithmen und
// Datenstrukturen" even after the click-retry fix (showAllClickMaxAttempts)
// and the main-content-wait fix (sectionContentRequiredStableReads) were
// both already applied - i.e. the click itself was succeeding, but the
// post-click AJAX-rendered expansion was hitting the same "stops growing
// for a read, then grows again" staged-render pattern documented on
// sectionContentRequiredStableReads below, just after a click instead of
// after a Goto. This budget only applies to sections that actually have a
// "show all" control (expandShowAllInSection returns early for the common
// case of no pagination), so its wall-clock cost is far more contained than
// sectionContentRequiredStableReads's, which applies to every section
// visited.
//
// IMPORTANT CAVEAT (see DefaultCourseConcurrency's doc comment in
// internal/config/config.go for the full writeup): this and
// sectionContentRequiredStableReads below measurably reduce, but do NOT
// eliminate, file loss at course_concurrency>1 when a very large course
// (in live testing, a 198-file course taking ~5-7 minutes on its own) is
// crawled concurrently with others - don't read "live-confirmed necessary"
// above as "live-confirmed sufficient in every scenario tested."
const showAllExpansionPollIntervalMs = 400.0
const showAllExpansionMaxPolls = 15
const showAllExpansionRequiredStableReads = 3

// waitForStableExpandedCandidates re-extracts page's candidates repeatedly
// (up to showAllExpansionMaxPolls times, showAllExpansionPollIntervalMs
// apart) until showAllExpansionRequiredStableReads consecutive reads come
// back no larger than the best seen, and returns the largest set seen. This
// replaces trusting a single fixed-duration wait to be enough for a "show
// all" expansion's AJAX-loaded content to finish rendering (see
// expandShowAllInSection's doc comment for the live-confirmed case this was
// silently undercounting: a course crawled in isolation captured all 28
// files in a paginated section, but the same section crawled concurrently
// with two other courses captured only the pre-expansion 20 - the click
// succeeded both times, but the fixed wait alone was not always enough for
// the extra rows to render before extraction ran).
//
// A transient extraction error mid-poll is treated as "no progress this
// round" rather than fatal, so one flaky Evaluate call can't throw away an
// already-successful expansion; a page-crash error is still returned
// immediately so the caller's existing recovery path (recoverAndReturnToSection)
// runs. Delegates to candidateStabilityPoll (navigation.go), the same
// stability-polling engine now also used for the main per-section content
// wait (waitForStableSectionContent below) - see that function's doc
// comment for why polling-until-stable, not this function specifically, is
// what generalized to fix the broader concurrent-crawl AJAX race.
func (s *OpalScraper) waitForStableExpandedCandidates(page playwright.Page) ([]map[string]string, error) {
	// Logs each poll's candidate count under --debug-clicks (audit.go), the
	// same way waitForStableSectionContent's extractFn does for the main
	// per-section content wait. Added by queue task
	// fix-candidate-stability-poll-concurrent-crawl-race after Step 0 of
	// that task found this poll - not the main content wait - was the
	// actual source of concurrent-crawl file loss (a "show all" pagination
	// expansion settling on a stable-but-incomplete read), and that nobody
	// had ever been able to see its per-poll trajectory: every prior
	// investigation (PR #73/#78/#84) only ever looked at
	// "section-content-poll" (waitForStableSectionContent's own trace) and
	// concluded it wasn't exhausting its budget - true, but irrelevant,
	// since that is a different poll than this one. See this task's queue
	// file for the live A/B comparison (byte-identical main-content-poll
	// counts between a serial and a concurrent run, yet very different
	// final file counts - the discrepancy traced to exactly this poll).
	pollNum := 0
	extractFn := func() ([]map[string]string, error) {
		candidates, err := s.extractSectionContentCandidates(page)
		if s.debugClicks {
			pollNum++
			errStr := "nil"
			if err != nil {
				errStr = err.Error()
			}
			s.auditLog("showall-expand-poll", page, "", fmt.Sprintf("poll #%d: %d candidates, err=%s", pollNum, len(candidates), errStr))
		}
		return candidates, err
	}
	// Deliberately left on the old concurrency-gated patience, unlike the
	// section-content poll below. Two reasons, both measured rather than
	// assumed:
	//
	//  1. It is not where the time goes. A full-account run makes ~6 show-all
	//     expansions against ~284 section visits, so this poll's patience is
	//     a rounding error in the wall-clock - the section poll is the entire
	//     cost (9m06s of an 9m13s run at concurrency 2).
	//  2. Opening impatient here would contradict direct live evidence: a
	//     concurrency=3 run with a streak of 2 still lost the tail of a
	//     paginated section (see showAllExpansionRequiredStableReads's doc
	//     comment). Post-click expansion demonstrably shows the
	//     stops-growing-then-grows-again pattern that an impatient opening
	//     cannot survive, and there is no trustworthy per-section signal for
	//     it - Wicket's AJAX_CALL_DONE marks the call finished, not the DOM
	//     complete (see wicket.go).
	patient := showAllExpansionRequiredStableReads
	if !s.crawlingConcurrently() {
		patient = 1
	}
	return candidateStabilityPoll(
		extractFn,
		func() { page.WaitForTimeout(showAllExpansionPollIntervalMs) },
		showAllExpansionMaxPolls,
		patient,
		patient,
		isPageCrashError,
	)
}

// sectionContentPollIntervalMs/sectionContentMaxPolls/
// sectionContentRequiredStableReads bound waitForStableSectionContent
// below: up to an extra ~8s (20 * 400ms), on top of the initial
// contentFallbackWaitMs wait, spent re-extracting a section's main content
// until the candidate count stops growing across
// sectionContentRequiredStableReads consecutive reads.
//
// sectionContentRequiredStableReads is 4, not the 1 a first version of this
// fix used - requiring more than 1 consecutive non-growing read is A REAL
// PART OF THE FIX for the file-loss symptom this task set out to
// root-cause (queue task fix-concurrent-crawl-ajax-race-and-raise-
// concurrency), though see the IMPORTANT CAVEAT at the end of this comment
// before assuming it's the whole fix. A version of this polling wait that
// stopped at the first non-growing read (requiredStableReads effectively 1)
// was live A/B tested against the real TU Dresden account and did NOT fix
// the loss: course_concurrency=3 still lost exactly the same files as the
// unfixed code, byte-for-byte, across repeated runs (Analysis: 21/29 files,
// missing exactly the tail past OPAL's ~20-item default page size;
// Algorithmen und Datenstrukturen: 32/34). Diffing exactly which files were
// missing (not just counts) and cross-referencing with --debug-clicks audit
// output showed the "show all" pagination control's click was never even
// attempted for the affected sections - findShowAllTarget found no
// pager-showall candidate at all, meaning the *initial* section render
// itself never included that control by the time extraction ran. The
// cause: OPAL/Wicket renders a section's content in stages - the row/file
// list appears first, and can itself sit unchanged for one or more poll
// intervals (a coincidental plateau) before a later stage adds the
// pagination control - and a poll that trusts a single non-growing read as
// "done" catches that plateau and stops before the later stage ever fires,
// especially under concurrent-crawl contention where that later stage is
// delayed further. Requiring multiple consecutive non-growing reads (not
// just 1) before trusting a "stable" count measurably helped in repeated
// live re-tests, at both light and moderate concurrent load.
//
// IMPORTANT CAVEAT: this does NOT fully eliminate the race in the worst
// case actually present in the maintainer's real account - a single course
// with radically more content than the others (198 files, ~5-7 minutes to
// crawl on its own) run concurrently with smaller courses that have
// paginated sections. Live re-tested repeatedly (course_concurrency 2 and
// 3, full 8-course account, this exact constant raised as high as 8 with
// an 8x-longer showAllExpansionRequiredStableReads companion change) and
// this specific combination intermittently still lost a small number of
// files (2-8 of 341, ~1-2%) even at the most generous budgets tried - while
// wall-clock time for the whole crawl grew substantially (~2.4-2.6x versus
// the course_concurrency=1 baseline) purely from every section paying this
// polling insurance cost, whether or not it was actually contended. Pushing
// this budget higher did not reliably close the gap and made the common
// case significantly slower, so this was deliberately tuned back down to a
// more moderate value rather than chasing full reliability at any wall-clock
// cost. See DefaultCourseConcurrency's doc comment in
// internal/config/config.go for why the default stays at 1 given this, and
// docs/OPERATIONS.md for the current incident-playbook guidance.
// Lowered 400 -> 150 on 2026-07-21, with sectionContentMaxPolls raised to
// keep the TOTAL budget unchanged (~8s). This is a sampling-rate change, not
// a patience cut: a settled page now confirms in 150ms instead of 400ms,
// while a slow one still gets the same ~8s to finish.
//
// Measured on the real account, discovery-only full runs: 400ms took
// 4m13s-4m41s, 150ms took 2m13s, and every run at 150ms returned the
// complete 322-file set. Sampling more often also makes the poll MORE likely
// to observe growth and escalate to the patient streak (see
// candidateStabilityPoll), so the finer rate is not a correctness trade.
//
// Caveat kept deliberately: 1 of 3 runs at the OLD 400ms setting silently
// lost 15 files (the tail of two paginated sections, including
// Vorlesung_9_10.pdf - the same file named in past incident comments). That
// intermittent loss is NOT proven fixed by this change; three clean runs are
// not enough to prove absence. It is a reason not to trust either setting as
// loss-free, not evidence that 150ms is.
const sectionContentPollIntervalMs = 150.0
const sectionContentMaxPolls = 53
const sectionContentRequiredStableReads = 4

// waitForStableSectionContent extracts page's main-content candidates
// (extractSectionContentCandidates) repeatedly until the read stabilizes,
// replacing the fixed-wait-then-extract-with-one-retry-on-empty scheme
// collectCourseFiles's main loop used before this. See
// candidateStabilityPoll's doc comment (navigation.go) and
// sectionContentRequiredStableReads's doc comment above for the root cause
// this fixes: a fixed wait (or a single-non-growing-read poll) tuned for a
// single course rendering alone is not reliably enough/robust once several
// courses' pages are rendering under concurrent load, because OPAL's
// staged rendering can produce a false-stable plateau before all content
// (notably pagination controls) has actually appeared.
//
// Callers should still call waitForInteractiveLinks first (as
// collectCourseFiles's main loop does) - the initial fixed wait remains
// cheap insurance against the very first extraction attempt reading a
// completely blank page, before there is anything to measure growth
// against.
//
// A page-crash error is returned immediately (isPageCrashError) so the
// caller's existing recovery path runs, exactly as the pre-existing
// extraction-error handling in collectCourseFiles's main loop already
// expected; any other error is retried within the poll budget rather than
// surfaced, so this can also return a nil error with a shorter-than-hoped
// candidates slice if every retry within budget kept failing transiently -
// callers must still treat a persistently empty/erroring result the same
// way as before (see the sectionsVisited/sectionsFailed accounting around
// this call in collectCourseFiles).
func (s *OpalScraper) waitForStableSectionContent(page playwright.Page, calm bool) ([]map[string]string, error) {
	// Logs each poll's candidate count under --debug-clicks (audit.go), so a
	// stuck-at-N-forever plateau can be distinguished from "never reached
	// the poll loop at all" - this is exactly the diagnostic that found the
	// staged-render/false-plateau root cause documented on
	// sectionContentRequiredStableReads above (queue task
	// fix-concurrent-crawl-ajax-race-and-raise-concurrency), so it's kept as
	// a permanent diagnostic rather than removed, matching audit.go's
	// "always-available diagnostic flag, not a temporary patch" philosophy.
	pollNum := 0
	extractFn := func() ([]map[string]string, error) {
		candidates, err := s.extractSectionContentCandidates(page)
		if s.debugClicks {
			pollNum++
			errStr := "nil"
			if err != nil {
				errStr = err.Error()
			}
			s.auditLog("section-content-poll", page, "", fmt.Sprintf("poll #%d: %d candidates, err=%s", pollNum, len(candidates), errStr))
		}
		return candidates, err
	}
	// This is the poll the measurement indicted: 284 calls per run, averaging
	// 451ms serial but 1.922s at concurrency 2 - a 4.26x jump matching the
	// old global 1->4 stable-read gate exactly, and accounting for
	// essentially all of that run's +4m12s.
	//
	// Patience is now earned per section. calm means the MutationObserver
	// watched the content root go a full debounce window without a single
	// mutation, which is positive evidence this page is done rendering; such
	// a page opens impatient. A page that was still mutating when the
	// observer ran out of budget opens on the full streak, exactly as the old
	// gate did. Either way, growth escalates.
	patient := sectionContentRequiredStableReads
	if !s.crawlingConcurrently() {
		patient = 1
	}
	initial := patient
	if calm {
		initial = 1
	}
	return candidateStabilityPoll(
		extractFn,
		func() { page.WaitForTimeout(sectionContentPollIntervalMs) },
		sectionContentMaxPolls,
		initial,
		sectionContentRequiredStableReads,
		isPageCrashError,
	)
}

// NOTE: requiredStableReads (a global gate that handed every section the
// patient streak whenever course_concurrency>1) was removed on 2026-07-21.
// Measured on the real account, that gate WAS the concurrency penalty: at
// concurrency 2 the section-content poll averaged 1.922s per section against
// 451ms serial - a 4.26x jump matching the 1->4 stable-read multiplier
// exactly - and the resulting +4m00s of extra waiting accounted for
// essentially all of the +4m12s the whole run got slower. Patience is now
// earned per section instead (see waitForStableSectionContent above and
// candidateStabilityPoll in navigation.go).

// recoverAndReturnToSection is expandShowAllInSection's crash path: it opens a fresh
// replacement page/tab (closing the crashed one - see recoverFromPageCrash) and
// re-navigates it back to currentURL, so the crawl loop's next section visit starts
// from a known-good page instead of continuing to drive a Playwright Page object whose
// renderer process has already died (see isPageCrashError's doc comment for the
// cascading-failure history this caused). The show-all expansion itself is abandoned
// for this section - the caller already has its un-expanded candidates from before this
// call and keeps using those.
func (s *OpalScraper) recoverAndReturnToSection(page playwright.Page, currentURL string) (playwright.Page, []map[string]string, string, string, bool) {
	newPage, recErr := s.recoverFromPageCrash(page)
	if recErr != nil {
		// Nothing more we can do here; hand back the original (crashed) page - the
		// crawl loop's next navigation attempt will hit the same crash error and
		// go through its own recovery attempt there.
		return page, nil, "", "", false
	}
	if _, err := s.gotoPolitely(newPage, currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); err != nil {
		// Recovered a fresh page but couldn't get back to currentURL; still hand it
		// back so the crawl loop continues on a healthy page rather than the
		// crashed one, even though this section's show-all expansion is lost.
		return newPage, nil, "", "", false
	}
	s.waitForInteractiveLinks(newPage, contentFallbackWaitMs)
	return newPage, nil, "", "", false
}

// looksLikeNavigableShowAllURL reports whether a "show all" control's link target is
// a plain URL worth navigating to directly (as opposed to a javascript:/onclick-driven
// control that only works via a real click).
func looksLikeNavigableShowAllURL(linkTarget string) bool {
	trimmed := strings.TrimSpace(linkTarget)
	if trimmed == "" || trimmed == "#" {
		return false
	}
	return !strings.HasPrefix(strings.ToLower(trimmed), "javascript:")
}

// skippedSection describes one section-folder link that was structurally
// classified as unable to hold files (see isNonFileSectionType) and so was
// deliberately left out of the crawl queue instead of costing a page visit.
// appendSectionFolderTargets returns these (rather than skipping silently)
// so the caller (collectCourseFiles) can log each one - see its own call
// site for why.
type skippedSection struct {
	Title string
	URL   string
}

// appendSectionFolderTargets scans candidates for section/folder links to
// queue for a later crawl visit. skipNonFileSections, when true, also
// applies the structural (not title-text, not visit-history) classification
// in isNonFileSectionType: a candidate whose own CSS class marks it as an
// OPAL course-node type that can never hold a downloadable file (currently
// just "node-en", OPAL's Enrollment/"Einschreibung" building block - see
// nonFileSectionTypeClasses's doc comment) is left out of the returned
// queue and instead reported via the returned []skippedSection, so callers
// can log it (skips must be auditable, not silent) rather than the crawl
// silently spending a page visit on it. When skipNonFileSections is false,
// classification is not applied at all and no section is ever reported as
// skipped - the pre-existing behavior of visiting every discovered
// section/folder link is unchanged, matching this project's
// easy-to-disable-when-wrong precedent (course_concurrency, maxPages).
func appendSectionFolderTargets(queue []string, queued, visited map[string]struct{}, candidates []map[string]string, opalURL, repoID, currentURL, courseRootURL, courseTitle string, sectionTitles map[string]string, skipNonFileSections bool) ([]string, []skippedSection) {
	currentKey := sectionKey(currentURL, repoID)
	rootKey := sectionKey(courseRootURL, repoID)
	var skipped []skippedSection

	for _, candidate := range candidates {
		linkTarget := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
		if linkTarget == "" {
			continue
		}
		title := deriveSectionTitle(candidate["title"], candidate["text"], candidate["rootText"])
		if !looksLikeSectionFolderLink(linkTarget, title) {
			continue
		}
		// A link nested under /CourseNode/ satisfies looksLikeSectionFolderLink
		// even when it's actually a downloadable file (OPAL serves files at
		// .../CourseNode/<id>/<filename>.pdf) - confirmed live, where raising
		// maxPages exposed dozens of these being queued as "sections" and then
		// failing Goto with "Download is starting". Anything that also looks
		// like a file link is content, not a folder to descend into.
		fileName := deriveFileName(candidate["title"], candidate["text"], linkTarget)
		if looksLikeFileLink(linkTarget, fileName) {
			continue
		}

		absURL := resolveURL(opalURL, linkTarget)
		if !isSectionURLAllowedForCourse(absURL, repoID) {
			continue
		}
		if !isAllowedFolderNavigationTarget(title, absURL, courseTitle) {
			continue
		}

		key := sectionKey(absURL, repoID)
		if key == currentKey || key == rootKey {
			continue
		}
		if _, ok := visited[key]; ok {
			continue
		}
		if _, ok := queued[key]; ok {
			continue
		}

		if skipNonFileSections && isNonFileSectionType(candidate) {
			// Mark as queued too, not just skipped, so a second link to the
			// same course-node elsewhere in this section's candidates (e.g.
			// duplicated markup) doesn't get reported as skipped twice.
			queued[key] = struct{}{}
			skipped = append(skipped, skippedSection{Title: title, URL: absURL})
			continue
		}

		queued[key] = struct{}{}
		if sectionTitles != nil {
			if _, has := sectionTitles[key]; !has && strings.TrimSpace(title) != "" {
				sectionTitles[key] = sanitizeFilename(title)
			}
		}
		queue = append(queue, absURL)
	}

	return queue, skipped
}

// isAllowedFolderNavigationTarget decides whether a folder-shaped link found in a
// section's content area should be descended into, rather than relying on a fixed
// list of known folder-name words. A link nested under a CourseNode's own path
// (e.g. ".../CourseNode/123/Uebungen") or pointing at an explicit fold_/grp_ target
// is structurally content belonging to the current section, regardless of its label.
// A bare CourseNode/RepositoryEntry link with no such nesting is a sibling section of
// the course tree; it's only excluded when its label is just a self-reference back to
// the course itself (e.g. a breadcrumb link), since everything else administrative
// (forum, calendar, members, ...) was already filtered out by looksLikeSectionFolderLink.
func isAllowedFolderNavigationTarget(title, absURL, courseTitle string) bool {
	if hasNestedCourseContentPath(absURL) {
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(title), strings.TrimSpace(courseTitle))
}

func hasNestedCourseContentPath(absURL string) bool {
	lower := strings.ToLower(absURL)
	if strings.Contains(lower, "target=fold_") || strings.Contains(lower, "target=grp_") {
		return true
	}
	match := courseNodeSectionKeyRe.FindStringSubmatch(absURL)
	return len(match) > 2 && match[2] != ""
}

func deriveSectionTitleFromURL(courseTitle, currentURL string) string {
	if strings.TrimSpace(currentURL) == "" {
		return sanitizeFilename(defaultString(courseTitle, "Kursinhalt"))
	}
	if strings.Contains(strings.ToLower(currentURL), "/coursenode/") {
		return sanitizeFilename("CourseNode")
	}
	return sanitizeFilename(defaultString(courseTitle, "Kursinhalt"))
}

func sectionKey(rawURL, repoID string) string {
	cleaned := strings.TrimSpace(rawURL)
	if cleaned == "" {
		return normalizedSectionKey(rawURL, repoID)
	}
	lower := strings.ToLower(cleaned)
	if strings.Contains(lower, "/coursenode/") {
		if match := courseNodeSectionKeyRe.FindStringSubmatch(cleaned); len(match) > 1 {
			suffix := ""
			if len(match) > 2 {
				suffix = strings.TrimSpace(match[2])
			}
			suffix = strings.Trim(suffix, "/")
			normalizedSuffix := ""
			if suffix != "" {
				decoded, err := url.PathUnescape(suffix)
				if err != nil {
					decoded = suffix
				}
				normalizedSuffix = strings.ToLower(strings.TrimSpace(decoded))
			}
			if repoID != "" {
				if normalizedSuffix != "" {
					return "repo|" + repoID + "|coursenode|" + match[1] + "|path|" + normalizedSuffix
				}
				return "repo|" + repoID + "|coursenode|" + match[1]
			}
			if normalizedSuffix != "" {
				return "coursenode|" + match[1] + "|path|" + normalizedSuffix
			}
			return "coursenode|" + match[1]
		}
	}
	return normalizedSectionKey(rawURL, repoID)
}
