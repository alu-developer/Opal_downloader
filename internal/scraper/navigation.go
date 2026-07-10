package scraper

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// contentSelectorTimeoutMs/contentFallbackWaitMs bound waitForInteractiveLinks
// (below). perf-04 (2026-07-08) assumed WaitForSelector "resolves
// near-instantly on the happy path" and left contentSelectorTimeoutMs at
// 2500ms unverified against a live run. Queue task
// click-wait-audit-and-speedup's --debug-clicks audit (2026-07-10, live
// `list --dev --debug-clicks --profile` against the real account, 6 courses,
// 205 files) disproved that assumption with real timestamps: the selector
// wait timed out 261/261 times (100%) - every single section visit paid the
// full contentSelectorTimeoutMs before falling back to the fixed
// contentFallbackWaitMs wait, which then reliably let content extraction
// succeed. So on this OPAL instance the selector wait never once resolves
// early; it is pure worst-case overhead, not a bounded fallback. Lowered
// from 2500ms to 400ms (small margin above zero in case some page or a
// different OPAL deployment ever does resolve it quickly) rather than
// removing the wait outright, since a live re-run at 400ms
// (see click-wait-audit-and-speedup's PR) confirmed identical file counts
// with no regression. contentFallbackWaitMs (700ms) is left unchanged - the
// audit confirmed it reliably succeeds and was not the bottleneck.
const (
	contentGotoTimeoutMs     = 15000.0
	contentSelectorTimeoutMs = 400.0
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
