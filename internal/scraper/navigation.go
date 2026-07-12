package scraper

import (
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
// What shipped: skip the WaitForSelector call (it never succeeds, so it is a
// pure extra Playwright round-trip with no early-exit benefit) but keep the
// *total* wait duration unchanged at 1100ms - the fixed value below is 1100,
// not 700. This can't regress content-render time versus the unmodified
// code (every visit there also blocked for exactly ~1100ms before
// extraction, 100% of the time), while removing one wasted IPC round-trip
// per section visit (316 of them in the click-audit-analysis-and-cleanup
// live run). The collectCourseFiles retry-on-empty-candidates logic
// (crawl.go) remains the safety net for any section that still needs more
// than this to render.
const contentGotoTimeoutMs = 15000.0
const contentFallbackWaitMs = 1100.0

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
// the fixed fallbackWaitMs wait - see the contentFallbackWaitMs doc comment
// above for the live-audit history of why that was removed (it never
// resolved early, twice, and the obvious fix - waiting for Attached instead
// of Playwright's default Visible - was live-verified to cause real file
// count regressions instead).
func (s *OpalScraper) waitForInteractiveLinks(page playwright.Page, fallbackWaitMs float64) {
	if page == nil {
		return
	}
	const selector = "a[href], [onclick], [data-href], [data-url]"
	start := time.Now()
	page.WaitForTimeout(fallbackWaitMs)
	s.auditLog("wait-fixed", page, selector, fmt.Sprintf("fixed wait took %s (no selector early-exit attempted - see contentFallbackWaitMs doc comment)", time.Since(start)))
}
