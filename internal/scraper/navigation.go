package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// contentSelectorTimeoutMs/contentFallbackWaitMs bound waitForInteractiveLinks
// (below): WaitForSelector already returns as soon as any link-like element
// is attached to the DOM, so contentSelectorTimeoutMs is a worst-case cap,
// not a cost paid on every call - OPAL pages already have nav/header links
// present by the time WaitUntilStateDomcontentloaded fires, so this resolves
// near-instantly on the happy path. contentFallbackWaitMs only fires when
// that selector wait itself times out (a genuinely slow-rendering page), so
// it's a bounded fallback, not blind per-page overhead. perf-04 audited
// these against a live run (see PR description) and found no unconditional
// fixed cost worth removing here; the previously-unused navGotoTimeoutMs/
// navSelectorTimeoutMs/navFallbackWaitMs constants (dead leftovers from
// pre-perf-03 single-page code, superseded by the content* ones below but
// never deleted) were removed as part of that audit.
const (
	contentGotoTimeoutMs     = 15000.0
	contentSelectorTimeoutMs = 2500.0
	contentFallbackWaitMs    = 700.0
)

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

// waitForInteractiveLinks waits for interactive link-like elements to appear
// on page. page is taken as an explicit parameter (rather than defaulting to
// s.getPage()) so this also works correctly against one of the per-worker
// tabs opened by collectCourseFilesConcurrently for parallel course
// crawling, not just the single shared s.page.
func (s *OpalScraper) waitForInteractiveLinks(page playwright.Page, selectorTimeoutMs, fallbackWaitMs float64) {
	if page == nil {
		return
	}
	const selector = "a[href], [onclick], [data-href], [data-url]"
	start := time.Now()
	if _, err := page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(selectorTimeoutMs)}); err != nil {
		s.auditLog("wait-selector-timeout", page, selector, fmt.Sprintf("selector wait did not resolve within %v (waited %s); falling back to fixed %v ms wait", selectorTimeoutMs, time.Since(start), fallbackWaitMs))
		fallbackStart := time.Now()
		page.WaitForTimeout(fallbackWaitMs)
		s.auditLog("wait-fallback-done", page, selector, fmt.Sprintf("fixed fallback wait took %s", time.Since(fallbackStart)))
	} else {
		s.auditLog("wait-selector-resolved", page, selector, fmt.Sprintf("selector resolved after %s", time.Since(start)))
	}
}
