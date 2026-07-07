package scraper

import (
	"regexp"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

const (
	navGotoTimeoutMs         = 15000.0
	navSelectorTimeoutMs     = 2500.0
	navFallbackWaitMs        = 700.0
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

func (s *OpalScraper) waitForInteractiveLinks(selectorTimeoutMs, fallbackWaitMs float64) {
	page := s.getPage()
	if page == nil {
		return
	}
	if _, err := page.WaitForSelector("a[href], [onclick], [data-href], [data-url]", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(selectorTimeoutMs)}); err != nil {
		page.WaitForTimeout(fallbackWaitMs)
	}
}
