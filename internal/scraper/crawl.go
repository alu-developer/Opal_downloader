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
func (s *OpalScraper) collectCourseFiles(page playwright.Page, course CourseRef) ([]FileRef, map[string]downloadCandidate, error) {
	if page == nil {
		return nil, nil, errors.New("no page available")
	}
	if course.RepoID == "" {
		return nil, nil, errors.New("course repo id is required")
	}

	files := make([]FileRef, 0)
	downloadCandidates := map[string]downloadCandidate{}
	fileSeen := map[string]struct{}{}
	visited := map[string]struct{}{}
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
			// Retry once after a short wait: net::ERR_ABORTED and similar are
			// commonly transient (a competing in-page navigation/redirect
			// racing our Goto), confirmed live - the same section often
			// succeeds on a second attempt.
			page.WaitForTimeout(contentFallbackWaitMs)
			if _, retryErr := page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); retryErr != nil {
				fmt.Printf("  Warning: skipping section %q (%s): navigation failed after retry: %v\n", sectionTitles[currentKey], currentURL, retryErr)
				continue
			}
		}
		s.waitForInteractiveLinks(page, contentSelectorTimeoutMs, contentFallbackWaitMs)

		candidates, err := s.extractSectionContentCandidates(page)
		if err != nil {
			// Same transient-race class as the Goto retry above: "execution
			// context was destroyed" happens when the page is still settling
			// (e.g. a client-side redirect) when we evaluate - confirmed live
			// to often succeed on a second attempt after a short wait.
			page.WaitForTimeout(contentFallbackWaitMs)
			candidates, err = s.extractSectionContentCandidates(page)
			if err != nil {
				fmt.Printf("  Warning: skipping section %q (%s): content extraction failed after retry: %v\n", sectionTitles[currentKey], currentURL, err)
				continue
			}
		}
		if len(candidates) == 0 {
			page.WaitForTimeout(contentFallbackWaitMs)
			retryCandidates, retryErr := s.extractSectionContentCandidates(page)
			if retryErr == nil && len(retryCandidates) > 0 {
				candidates = retryCandidates
			} else if retryErr != nil {
				fmt.Printf("  Warning: section %q (%s) returned no content and retry failed: %v\n", sectionTitles[currentKey], currentURL, retryErr)
			} else {
				fmt.Printf("  Warning: section %q (%s) returned no content even after retry; it may be genuinely empty, or files may have been dropped\n", sectionTitles[currentKey], currentURL)
			}
		}

		showAllURL := ""
		if expanded, expandedURL, ok := s.expandShowAllInSection(page, currentURL, candidates); ok {
			candidates = expanded
			showAllURL = expandedURL
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

	return files, downloadCandidates, nil
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
// NOTE: the exact OPAL markup for this control could not be verified against a live
// OPAL instance in this environment (no OPAL login available here). The detection in
// looksLikeShowAllControl is a best-effort guess based on common OPAL/ILIAS UI
// patterns (German "Alle anzeigen"-style link text, or a length=-1/showAll-style URL
// parameter). A human should manually verify this against a real OPAL course known to
// have more than 20 files in one section once this lands.
//
// It returns the re-extracted candidate list, the resolved "show all" URL (when the
// expansion was reached via direct navigation to a distinct URL rather than a click on
// a javascript:/onclick-driven control), and true when a "show all" control was found
// and acted on; otherwise it returns (nil, "", false) and the caller keeps using the
// candidates it already had.
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
func (s *OpalScraper) expandShowAllInSection(page playwright.Page, currentURL string, candidates []map[string]string) ([]map[string]string, string, bool) {
	if page == nil {
		return nil, "", false
	}

	linkTarget, found := findShowAllTarget(candidates)
	if !found {
		return nil, "", false
	}

	absURL := resolveURL(s.opalURL, linkTarget)
	navigated := false
	if looksLikeNavigableShowAllURL(linkTarget) {
		// Prefer navigating directly to the "show all" URL over clicking: it's a
		// plain link with a resolvable href, and direct navigation is more robust
		// in headless mode than dispatching a click event.
		if _, err := page.Goto(absURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); err == nil {
			navigated = true
		}
	}

	if !navigated {
		clicked := false
		for _, needle := range showAllControlTextNeedles {
			locator := page.GetByText(needle, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First()
			s.auditLog("click", page, needle, "show-all expand attempt for section "+currentURL)
			if err := locator.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(3000)}); err == nil {
				s.auditLog("click-success", page, needle, "show-all expand succeeded for section "+currentURL)
				clicked = true
				break
			}
		}
		if !clicked {
			// Could not click or navigate to the control; keep whatever candidates
			// the caller already extracted rather than failing the whole section.
			return nil, "", false
		}
	}

	s.waitForInteractiveLinks(page, contentSelectorTimeoutMs, contentFallbackWaitMs)

	expanded, err := s.extractSectionContentCandidates(page)
	if err != nil || len(expanded) == 0 {
		return nil, "", false
	}

	// Record the show-all URL (when distinct from currentURL) so the caller can point
	// later re-visits (e.g. download fallback) at the page where these files actually
	// render. See the doc comment above for why this intentionally does not navigate
	// page back to currentURL.
	showAllURL := ""
	if navigated && !strings.EqualFold(strings.TrimSpace(absURL), strings.TrimSpace(currentURL)) {
		showAllURL = absURL
	}

	return expanded, showAllURL, true
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
