package scraper

import (
	"errors"
	"net/url"
	"regexp"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

var courseNodeSectionKeyRe = regexp.MustCompile(`(?i)/coursenode/(\d+)(/[^?#]*)?`)

func (s *OpalScraper) collectCourseFiles(course CourseRef) ([]FileRef, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}
	if course.RepoID == "" {
		return nil, errors.New("course repo id is required")
	}

	files := make([]FileRef, 0)
	fileSeen := map[string]struct{}{}
	visited := map[string]struct{}{}
	rootKey := sectionKey(course.URL, course.RepoID)
	queue := []string{course.URL}
	queued := map[string]struct{}{rootKey: {}}
	maxPages := 16

	for len(queue) > 0 && len(visited) < maxPages {
		currentURL := queue[0]
		queue = queue[1:]
		currentKey := sectionKey(currentURL, course.RepoID)
		delete(queued, currentKey)
		if _, ok := visited[currentKey]; ok {
			continue
		}
		visited[currentKey] = struct{}{}

		if _, err := s.page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); err != nil {
			continue
		}
		s.waitForInteractiveLinks(contentSelectorTimeoutMs, contentFallbackWaitMs)

		candidates, err := s.extractSectionContentCandidates()
		if err != nil {
			continue
		}
		if len(candidates) == 0 {
			s.page.WaitForTimeout(contentFallbackWaitMs)
			retryCandidates, retryErr := s.extractSectionContentCandidates()
			if retryErr == nil && len(retryCandidates) > 0 {
				candidates = retryCandidates
			}
		}

		if expanded, ok := s.expandShowAllInSection(currentURL, candidates); ok {
			candidates = expanded
		}

		sectionTitle := deriveSectionTitleFromURL(course.Title, currentURL)
		section := SectionRef{CourseRepoID: course.RepoID, Title: sectionTitle, URL: currentURL}
		files = appendSectionFiles(files, fileSeen, candidates, course, section, currentURL, s.opalURL, s.downloadCandidates)
		queue = appendSectionFolderTargets(queue, queued, visited, candidates, s.opalURL, course.RepoID, currentURL, course.URL, course.Title)
	}

	return files, nil
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
// It returns the re-extracted candidate list and true when a "show all" control was
// found and acted on; otherwise it returns (nil, false) and the caller keeps using the
// candidates it already had.
func (s *OpalScraper) expandShowAllInSection(currentURL string, candidates []map[string]string) ([]map[string]string, bool) {
	if s.page == nil {
		return nil, false
	}

	linkTarget, found := findShowAllTarget(candidates)
	if !found {
		return nil, false
	}

	absURL := resolveURL(s.opalURL, linkTarget)
	navigated := false
	if looksLikeNavigableShowAllURL(linkTarget) {
		// Prefer navigating directly to the "show all" URL over clicking: it's a
		// plain link with a resolvable href, and direct navigation is more robust
		// in headless mode than dispatching a click event.
		if _, err := s.page.Goto(absURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)}); err == nil {
			navigated = true
		}
	}

	if !navigated {
		clicked := false
		for _, needle := range showAllControlTextNeedles {
			locator := s.page.GetByText(needle, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First()
			if err := locator.Click(playwright.LocatorClickOptions{Timeout: playwright.Float(3000)}); err == nil {
				clicked = true
				break
			}
		}
		if !clicked {
			// Could not click or navigate to the control; keep whatever candidates
			// the caller already extracted rather than failing the whole section.
			return nil, false
		}
	}

	s.waitForInteractiveLinks(contentSelectorTimeoutMs, contentFallbackWaitMs)

	expanded, err := s.extractSectionContentCandidates()
	if err != nil || len(expanded) == 0 {
		return nil, false
	}

	// If we navigated to a dedicated "show all" URL, go back to the section's
	// canonical URL afterwards so any further link resolution / folder discovery
	// in the caller stays anchored to currentURL rather than the show-all variant.
	if navigated && !strings.EqualFold(strings.TrimSpace(absURL), strings.TrimSpace(currentURL)) {
		_, _ = s.page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(contentGotoTimeoutMs)})
	}

	return expanded, true
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

func appendSectionFolderTargets(queue []string, queued, visited map[string]struct{}, candidates []map[string]string, opalURL, repoID, currentURL, courseRootURL, courseTitle string) []string {
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
