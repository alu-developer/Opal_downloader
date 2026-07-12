package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

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

	for len(queue) > 0 && len(visited) < maxPages {
		currentURL := queue[0]
		queue = queue[1:]
		currentKey := sectionKey(currentURL, course.RepoID)
		delete(queued, currentKey)
		if _, ok := visited[currentKey]; ok {
			continue
		}
		visited[currentKey] = struct{}{}

		if _, err := page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); err != nil {
			if isPageCrashError(err) {
				// A crashed page is permanently unusable - retrying on it (like
				// the transient-error branch below does) would just crash again
				// and, worse, keep using the same dead page for every remaining
				// section in this course. See isPageCrashError's doc comment for
				// the live-confirmed cascade this caused. Recover onto a fresh
				// tab before retrying.
				newPage, recErr := s.recoverFromPageCrash(page)
				if recErr != nil {
					fmt.Printf("  Warning: skipping section %q (%s): browser tab crashed and could not be recovered: %v (original error: %v)\n", sectionTitles[currentKey], currentURL, recErr, err)
					sectionsFailed++
					continue
				}
				page = newPage
			} else {
				// Retry once after a short wait: net::ERR_ABORTED and similar are
				// commonly transient (a competing in-page navigation/redirect
				// racing our Goto), confirmed live - the same section often
				// succeeds on a second attempt.
				page.WaitForTimeout(contentFallbackWaitMs)
			}
			if _, retryErr := page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); retryErr != nil {
				fmt.Printf("  Warning: skipping section %q (%s): navigation failed after retry: %v\n", sectionTitles[currentKey], currentURL, retryErr)
				sectionsFailed++
				continue
			}
		}
		s.waitForInteractiveLinks(page, contentFallbackWaitMs)

		candidates, err := s.extractSectionContentCandidates(page)
		if err != nil {
			if isPageCrashError(err) {
				newPage, recErr := s.recoverFromPageCrash(page)
				if recErr != nil {
					fmt.Printf("  Warning: skipping section %q (%s): content extraction crashed and the tab could not be recovered: %v (original error: %v)\n", sectionTitles[currentKey], currentURL, recErr, err)
					sectionsFailed++
					continue
				}
				page = newPage
				// The replacement tab starts blank; it has to be navigated back
				// to currentURL before there is anything for extraction to read.
				if _, gotoErr := page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); gotoErr != nil {
					fmt.Printf("  Warning: skipping section %q (%s): content extraction crashed; re-navigating the replacement tab failed: %v\n", sectionTitles[currentKey], currentURL, gotoErr)
					sectionsFailed++
					continue
				}
				s.waitForInteractiveLinks(page, contentFallbackWaitMs)
			} else {
				// Same transient-race class as the Goto retry above: "execution
				// context was destroyed" happens when the page is still settling
				// (e.g. a client-side redirect) when we evaluate - confirmed live
				// to often succeed on a second attempt after a short wait.
				page.WaitForTimeout(contentFallbackWaitMs)
			}
			candidates, err = s.extractSectionContentCandidates(page)
			if err != nil {
				fmt.Printf("  Warning: skipping section %q (%s): content extraction failed after retry: %v\n", sectionTitles[currentKey], currentURL, err)
				sectionsFailed++
				continue
			}
		}
		// Reaching here means this section's Goto and extraction both
		// succeeded (candidates may still legitimately be empty - see the
		// len(candidates) == 0 handling below, which does not `continue` and
		// so is not counted as a failure). See sectionsVisited's doc comment
		// above for what this distinction is for.
		sectionsVisited++
		if len(candidates) == 0 {
			page.WaitForTimeout(contentFallbackWaitMs)
			retryCandidates, retryErr := s.extractSectionContentCandidates(page)
			if retryErr == nil && len(retryCandidates) > 0 {
				candidates = retryCandidates
			} else if retryErr != nil {
				if isPageCrashError(retryErr) {
					if newPage, recErr := s.recoverFromPageCrash(page); recErr == nil {
						page = newPage
					}
				}
				fmt.Printf("  Warning: section %q (%s) returned no content and retry failed: %v\n", sectionTitles[currentKey], currentURL, retryErr)
			} else {
				fmt.Printf("  Warning: section %q (%s) returned no content even after retry; it may be genuinely empty, or files may have been dropped\n", sectionTitles[currentKey], currentURL)
			}
		}

		var showAllURL string
		var showAllCandidates []map[string]string
		var expandedShowAll bool
		page, showAllCandidates, showAllURL, expandedShowAll = s.expandShowAllInSection(page, currentURL, candidates)
		if expandedShowAll {
			candidates = showAllCandidates
		}

		sectionTitle := sectionTitles[currentKey]
		if strings.TrimSpace(sectionTitle) == "" {
			sectionTitle = deriveSectionTitleFromURL(course.Title, currentURL)
		}
		section := SectionRef{CourseRepoID: course.RepoID, Title: sectionTitle, URL: currentURL}
		files = appendSectionFiles(files, fileSeen, candidates, course, section, currentURL, showAllURL, s.opalURL, downloadCandidates)
		queue = appendSectionFolderTargets(queue, queued, visited, candidates, s.opalURL, course.RepoID, currentURL, course.URL, course.Title, sectionTitles)
	}

	if len(queue) > 0 && len(visited) >= maxPages {
		fmt.Printf("  Warning: course %q hit the %d-section crawl cap with %d section(s) still queued; some content may be missing\n", course.Title, maxPages, len(queue))
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
func (s *OpalScraper) expandShowAllInSection(page playwright.Page, currentURL string, candidates []map[string]string) (playwright.Page, []map[string]string, string, bool) {
	if page == nil {
		return page, nil, "", false
	}

	linkTarget, found := findShowAllTarget(candidates)
	if !found {
		return page, nil, "", false
	}

	absURL := resolveURL(s.opalURL, linkTarget)
	navigated := false
	if looksLikeNavigableShowAllURL(linkTarget) {
		// Prefer navigating directly to the "show all" URL over clicking: it's a
		// plain link with a resolvable href, and direct navigation is more robust
		// in headless mode than dispatching a click event.
		_, err := page.Goto(absURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)})
		switch {
		case err == nil:
			navigated = true
		case isPageCrashError(err):
			return s.recoverAndReturnToSection(page, currentURL)
		}
	}

	if !navigated {
		clicked := false
		// Try the confirmed structural CSS class first (showAllControlClassNeedle,
		// files.go) - more robust than the text-needle fallback below, which depends
		// on exact wording/locale and stops matching once the control's own label
		// toggles from "Alle anzeigen" to "Seiten" after a successful expansion.
		classSelector := "." + showAllControlClassNeedle
		classLocator := page.Locator(classSelector).First()
		s.auditLog("click", page, classSelector, "show-all expand attempt (class) for section "+currentURL)
		switch err := classLocator.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(3000)}); {
		case err == nil:
			s.auditLog("click-success", page, classSelector, "show-all expand succeeded (class) for section "+currentURL)
			clicked = true
		case isPageCrashError(err):
			return s.recoverAndReturnToSection(page, currentURL)
		}

		if !clicked {
			for _, needle := range showAllControlTextNeedles {
				locator := page.GetByText(needle, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First()
				s.auditLog("click", page, needle, "show-all expand attempt for section "+currentURL)
				err := locator.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(3000)})
				if err == nil {
					s.auditLog("click-success", page, needle, "show-all expand succeeded for section "+currentURL)
					clicked = true
					break
				}
				if isPageCrashError(err) {
					return s.recoverAndReturnToSection(page, currentURL)
				}
			}
		}
		if !clicked {
			// Could not click or navigate to the control; keep whatever candidates
			// the caller already extracted rather than failing the whole section.
			return page, nil, "", false
		}
	}

	s.waitForInteractiveLinks(page, contentFallbackWaitMs)

	expanded, err := s.waitForStableExpandedCandidates(page)
	if err != nil {
		if isPageCrashError(err) {
			return s.recoverAndReturnToSection(page, currentURL)
		}
		return page, nil, "", false
	}
	if len(expanded) == 0 {
		return page, nil, "", false
	}

	// Record the show-all URL (when distinct from currentURL) so the caller can point
	// later re-visits (e.g. download fallback) at the page where these files actually
	// render. See the doc comment above for why this intentionally does not navigate
	// page back to currentURL.
	showAllURL := ""
	if navigated && !strings.EqualFold(strings.TrimSpace(absURL), strings.TrimSpace(currentURL)) {
		showAllURL = absURL
	}

	return page, expanded, showAllURL, true
}

// showAllExpansionPollIntervalMs/showAllExpansionMaxPolls bound
// waitForStableExpandedCandidates below: up to an extra ~4s (10 * 400ms), on
// top of the initial contentFallbackWaitMs wait, spent re-extracting until
// the candidate count stops growing. See expandShowAllInSection's doc
// comment for the live-confirmed race this fixes - a fixed-duration wait
// after a successful "show all" click is not reliably enough time for
// OPAL's Wicket-AJAX-driven row expansion to finish rendering, especially
// under concurrent multi-course crawl load.
const showAllExpansionPollIntervalMs = 400.0
const showAllExpansionMaxPolls = 10

// waitForStableExpandedCandidates re-extracts page's candidates repeatedly
// (up to showAllExpansionMaxPolls times, showAllExpansionPollIntervalMs
// apart) until a read comes back no larger than the previous one, and
// returns the largest set seen. This replaces trusting a single
// fixed-duration wait to be enough for a "show all" expansion's AJAX-loaded
// content to finish rendering (see expandShowAllInSection's doc comment for
// the live-confirmed case this was silently undercounting: a course crawled
// in isolation captured all 28 files in a paginated section, but the same
// section crawled concurrently with two other courses captured only the
// pre-expansion 20 - the click succeeded both times, but the fixed wait
// alone was not always enough for the extra rows to render before
// extraction ran).
//
// A transient extraction error mid-poll is treated as "no progress this
// round" rather than fatal, so one flaky Evaluate call can't throw away an
// already-successful expansion; a page-crash error is still returned
// immediately so the caller's existing recovery path (recoverAndReturnToSection)
// runs.
func (s *OpalScraper) waitForStableExpandedCandidates(page playwright.Page) ([]map[string]string, error) {
	best, err := s.extractSectionContentCandidates(page)
	if err != nil {
		if isPageCrashError(err) {
			return nil, err
		}
		best = nil
	}

	for i := 0; i < showAllExpansionMaxPolls; i++ {
		page.WaitForTimeout(showAllExpansionPollIntervalMs)
		next, err := s.extractSectionContentCandidates(page)
		if err != nil {
			if isPageCrashError(err) {
				return nil, err
			}
			continue
		}
		if len(next) <= len(best) {
			// This round didn't grow the candidate set - the AJAX update has
			// settled (or never happens to grow further). best already holds
			// the largest read seen, so stop polling rather than overwriting
			// it with this equal-or-smaller round.
			break
		}
		best = next
	}
	return best, nil
}

// recoverAndReturnToSection is expandShowAllInSection's crash path: it opens a fresh
// replacement page/tab (closing the crashed one - see recoverFromPageCrash) and
// re-navigates it back to currentURL, so the crawl loop's next section visit starts
// from a known-good page instead of continuing to drive a Playwright Page object whose
// renderer process has already died (see isPageCrashError's doc comment for the
// cascading-failure history this caused). The show-all expansion itself is abandoned
// for this section - the caller already has its un-expanded candidates from before this
// call and keeps using those.
func (s *OpalScraper) recoverAndReturnToSection(page playwright.Page, currentURL string) (playwright.Page, []map[string]string, string, bool) {
	newPage, recErr := s.recoverFromPageCrash(page)
	if recErr != nil {
		// Nothing more we can do here; hand back the original (crashed) page - the
		// crawl loop's next navigation attempt will hit the same crash error and
		// go through its own recovery attempt there.
		return page, nil, "", false
	}
	if _, err := newPage.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); err != nil {
		// Recovered a fresh page but couldn't get back to currentURL; still hand it
		// back so the crawl loop continues on a healthy page rather than the
		// crashed one, even though this section's show-all expansion is lost.
		return newPage, nil, "", false
	}
	s.waitForInteractiveLinks(newPage, contentFallbackWaitMs)
	return newPage, nil, "", false
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

func appendSectionFolderTargets(queue []string, queued, visited map[string]struct{}, candidates []map[string]string, opalURL, repoID, currentURL, courseRootURL, courseTitle string, sectionTitles map[string]string) []string {
	currentKey := sectionKey(currentURL, repoID)
	rootKey := sectionKey(courseRootURL, repoID)

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

		queued[key] = struct{}{}
		if sectionTitles != nil {
			if _, has := sectionTitles[key]; !has && strings.TrimSpace(title) != "" {
				sectionTitles[key] = sanitizeFilename(title)
			}
		}
		queue = append(queue, absURL)
	}

	return queue
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
