package scraper

import (
	"regexp"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

const (
	navGotoTimeoutMsV2         = 15000.0
	navSelectorTimeoutMsV2     = 2500.0
	navFallbackWaitMsV2        = 700.0
	contentGotoTimeoutMsV2     = 15000.0
	contentSelectorTimeoutMsV2 = 2500.0
	contentFallbackWaitMsV2    = 700.0
)

var sectionTitleWhitespaceRe = regexp.MustCompile(`\s+`)

func deriveSectionTitleV2(title, text, rootText string) string {
	for _, raw := range []string{title, text, rootText} {
		cleaned := strings.TrimSpace(raw)
		if cleaned == "" {
			continue
		}
		cleaned = sectionTitleWhitespaceRe.ReplaceAllString(cleaned, " ")
		cleaned = strings.Trim(cleaned, " -,")
		if isValidSectionTitleV2(cleaned) {
			return cleaned
		}
	}
	return ""
}

func isValidSectionTitleV2(value string) bool {
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

func looksLikeSectionLinkV2(href, title string) bool {
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

func isSectionURLAllowedForCourseV2(absURL, repoID string) bool {
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

func (s *OpalScraper) waitForInteractiveLinksV2(selectorTimeoutMs, fallbackWaitMs float64) {
	if s.page == nil {
		return
	}
	if _, err := s.page.WaitForSelector("a[href], [onclick], [data-href], [data-url]", playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(selectorTimeoutMs)}); err != nil {
		s.page.WaitForTimeout(fallbackWaitMs)
	}
}
