package scraper

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

func (s *OpalScraper) scrapeCoursesBrowser(courseFilter []string) ([]RemoteFile, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	courses, err := s.discoverCourseLinks(courseFilter, 12)
	if err != nil {
		return nil, err
	}
	fmt.Printf("Found %d course links\n", len(courses))

	files := make([]RemoteFile, 0)
	for _, course := range courses {
		courseName := course[0]
		courseURL := course[1]

		if !strictCourseFilterMatches(courseName, courseFilter) {
			fmt.Printf("  Skipping unmatched course: %s\n", courseName)
			continue
		}

		fmt.Printf("  Processing: %s\n", courseName)
		courseFiles, crawlErr := s.crawlCourseFiles(courseName, courseURL)
		if crawlErr != nil {
			fmt.Printf("  Error processing course: %v\n", crawlErr)
			continue
		}
		files = append(files, courseFiles...)
	}

	return files, nil
}

func (s *OpalScraper) discoverCourseLinks(courseFilter []string, maxPages int) ([][2]string, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}
	_ = maxPages

	discovered := map[string][2]string{}
	sourcePages := []string{
		resolveURL(s.opalURL, "auth/resource/courses"),
		resolveURL(s.opalURL, "auth/RepositoryEntry/mycourses"),
		resolveURL(s.opalURL, "auth/home#my-courses"),
		resolveURL(s.opalURL, "auth/home"),
	}

	for _, sourceURL := range sourcePages {
		_, err := s.page.Goto(sourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		if err != nil {
			continue
		}
		s.waitForCourseEntries(sourceURL)

		candidates, err := s.extractCourseCandidatesFromCurrentPage()
		if err != nil {
			continue
		}

		for _, candidate := range candidates {
			linkTarget := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
			if linkTarget == "" {
				continue
			}
			absURL := resolveURL(s.opalURL, linkTarget)
			repoID := extractRepositoryEntryID(absURL)
			if repoID == "" {
				continue
			}
			if !looksLikeCourseCandidate(candidate) {
				continue
			}

			label := deriveCourseLabel(candidate["title"], candidate["text"], candidate["cardText"], repoID)
			upsertDiscoveredCourse(discovered, repoID, label, resolveURL(s.opalURL, "auth/RepositoryEntry/"+repoID), courseFilter)
		}
	}

	items := mapValues(discovered)
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i][0]) < strings.ToLower(items[j][0]) })
	return items, nil
}

func (s *OpalScraper) discoverFromCatalogForPatterns(missingPatterns []string, maxPages int) (map[string][2]string, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	discovered := map[string][2]string{}
	visited := map[string]struct{}{}
	queue := []string{
		resolveURL(s.opalURL, "auth/repository/catalog"),
		resolveURL(s.opalURL, "auth/repository/catalog/3571713"),
	}
	queued := map[string]struct{}{}
	for _, item := range queue {
		queued[normalizeURLForCrawl(item)] = struct{}{}
	}

	for len(queue) > 0 && len(visited) < maxPages {
		currentURL := queue[0]
		queue = queue[1:]
		key := normalizeURLForCrawl(currentURL)
		delete(queued, key)
		if _, ok := visited[key]; ok {
			continue
		}
		visited[key] = struct{}{}

		_, err := s.page.Goto(currentURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		if err != nil {
			continue
		}
		s.waitForCourseEntries(currentURL)

		candidates, err := s.extractCourseCandidatesFromCurrentPage()
		if err == nil {
			for _, candidate := range candidates {
				linkTarget := extractLinkTarget(candidate["href"], candidate["onclick"], candidate["dataHref"], candidate["dataUrl"])
				if linkTarget == "" {
					continue
				}
				absURL := resolveURL(s.opalURL, linkTarget)
				repoID := extractRepositoryEntryID(absURL)
				if repoID == "" || !looksLikeCourseCandidate(candidate) {
					continue
				}
				label := deriveCourseLabel(candidate["title"], candidate["text"], candidate["cardText"], repoID)
				upsertDiscoveredCourse(discovered, repoID, label, resolveURL(s.opalURL, "auth/RepositoryEntry/"+repoID), missingPatterns)
			}
		}

		value, evalErr := s.page.Evaluate(`() => Array.from(document.querySelectorAll('a[href]'))
                .map(a => (a.getAttribute('href') || '').trim())
                .filter(href => href.includes('/auth/repository/catalog/'))`)
		if evalErr == nil {
			for _, href := range toStringSlice(value) {
				absURL := resolveURL(s.opalURL, href)
				absKey := normalizeURLForCrawl(absURL)
				if _, seen := visited[absKey]; seen {
					continue
				}
				if _, pending := queued[absKey]; pending {
					continue
				}
				queue = append(queue, absURL)
				queued[absKey] = struct{}{}
			}
		}

		if len(getUnmatchedPatterns(missingPatterns, mapValues(discovered))) == 0 {
			break
		}
	}

	return discovered, nil
}

func (s *OpalScraper) extractCourseCandidatesFromCurrentPage() ([]map[string]string, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}
	value, err := s.page.Evaluate(`() => {
                const out = [];
                for (const el of document.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
                    const card = el.closest('li, article, .list-group-item, .dynamic-tab, .o_repository_entry, .o_repoentry, .o_infoPanel, .o_card') || el.parentElement;
                    out.push({
                        href: (el.getAttribute('href') || '').trim(),
                        onclick: (el.getAttribute('onclick') || '').trim(),
                        dataHref: (el.getAttribute('data-href') || '').trim(),
                        dataUrl: (el.getAttribute('data-url') || '').trim(),
                        text: (el.textContent || '').trim(),
                        title: (el.getAttribute('title') || '').trim(),
                        cardClass: ((card && card.getAttribute('class')) || '').trim(),
                        cardText: ((card && card.textContent) || '').trim(),
                    });
                }
                return out;
            }`)
	if err != nil {
		return nil, err
	}
	return toStringMapSlice(value), nil
}

func (s *OpalScraper) waitForCourseEntries(pageURL string) {
	if s.page == nil {
		return
	}
	selectors := []string{
		"a[href*='/RepositoryEntry/']",
		"a[href*='/CourseNode/']",
		".list-group-item a[href]",
		".dynamic-tab a[href]",
	}

	isResourcesLike := strings.Contains(strings.ToLower(pageURL), "repositoryentry/resources") || strings.Contains(strings.ToLower(pageURL), "/auth/home") || strings.Contains(strings.ToLower(pageURL), "resource/resources")
	timeoutMs := 5000.0
	if isResourcesLike {
		timeoutMs = 12000
	}

	for _, selector := range selectors {
		if _, err := s.page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(timeoutMs)}); err == nil {
			s.page.WaitForTimeout(800)
			return
		}
	}
	s.page.WaitForTimeout(1500)
}

func upsertDiscoveredCourse(discovered map[string][2]string, repoID, label, canonicalURL string, patterns []string) {
	if !strictCourseFilterMatches(label, patterns) {
		return
	}
	previous, ok := discovered[repoID]
	if !ok || strings.HasPrefix(previous[0], "RepositoryEntry") || len(label) > len(previous[0]) {
		discovered[repoID] = [2]string{label, canonicalURL}
	}
}

func deriveCourseLabel(title, text, cardText, repoID string) string {
	for _, raw := range []string{title, text, cardText} {
		cleaned := strings.TrimSpace(regexp.MustCompile(`\s+`).ReplaceAllString(raw, " "))
		if cleaned == "" {
			continue
		}
		cleaned = regexp.MustCompile(`(?i)^Als Favorit markieren\s+`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`(?i)\bVerantwortliche\(r\):.*$`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`(?i)\bZuletzt angesehen:.*$`).ReplaceAllString(cleaned, "")
		cleaned = regexp.MustCompile(`(?i)\bAufrufe:.*$`).ReplaceAllString(cleaned, "")
		cleaned = strings.Trim(cleaned, " -,")
		if isCourseLabel(cleaned) {
			return cleaned
		}
	}
	return "RepositoryEntry " + repoID
}

func getUnmatchedPatterns(patterns []string, courses [][2]string) []string {
	if len(patterns) == 0 || (len(patterns) == 1 && patterns[0] == "*") {
		return nil
	}
	courseNames := make([]string, 0, len(courses))
	for _, course := range courses {
		courseNames = append(courseNames, course[0])
	}
	unmatched := make([]string, 0)
	for _, pattern := range patterns {
		matched := false
		for _, name := range courseNames {
			if strictCourseFilterMatches(name, []string{pattern}) {
				matched = true
				break
			}
		}
		if !matched {
			unmatched = append(unmatched, pattern)
		}
	}
	return unmatched
}
