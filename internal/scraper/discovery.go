package scraper

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/mxschmitt/playwright-go"
)

func (s *OpalScraper) discoverCourseLinks(courseFilter []string) ([]CourseRef, error) {
	page := s.getPage()
	if page == nil {
		return nil, errors.New("no page available")
	}

	discovered := map[string]CourseRef{}
	// auth/resource/courses is OPAL's "Meine Kurse" listing and is the primary,
	// reliable source of enrolled-course tiles (rendered as `.content-preview`
	// cards, see extractCourseCardsFromCurrentPage). auth/RepositoryEntry/mycourses
	// redirects to the personal dashboard (auth/home, itself the last-opened course's
	// page) which also carries a "Favoriten" widget (`.list-group-item` links); both
	// are kept as a secondary source in case a course is favorited but missing from
	// the main listing. auth/home#my-courses was removed: it is a bare URL fragment
	// (never sent to the server) and this OPAL instance does not react to it
	// client-side, so it always resolves to the exact same page as plain auth/home.
	sourcePages := []string{
		resolveURL(s.opalURL, "auth/resource/courses"),
		resolveURL(s.opalURL, "auth/RepositoryEntry/mycourses"),
		resolveURL(s.opalURL, "auth/home"),
	}

	// Every source failing is not the same fact as the account having no
	// courses, and until 2026-07-27 this function reported them identically:
	// each failure warned and `continue`d, and an empty list came back with a
	// nil error. Live case that exposed it - the visible developer-mode login
	// window was gone by the time discovery ran, so all three sources failed
	// with "target closed" and the run finished as "Found 0 course links /
	// Discovered 0 remote files", which reads exactly like a healthy sync of
	// an empty account. Nothing was destroyed (the syncer never removes local
	// files on the strength of a remote listing) but the user is told their
	// courses are up to date when in fact nothing was ever read.
	failedSources := 0
	var lastFailure error

	for _, sourceURL := range sourcePages {
		if _, err := s.gotoPolitely(page, sourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
			// Retry once after a short wait, mirroring the transient-nav-failure
			// retry pattern already used throughout crawl.go (queue task
			// fix-course-level-crawl-flakiness, 2026-07-13). Before this, a single
			// failed Goto here silently dropped this entire source page's
			// contribution to the discovered course list with no log at all -
			// discovered courses are the union of what all sourcePages entries
			// surface, so a course that only appears on the page that failed
			// (e.g. only in the auth/home "Favoriten" widget, not in "Meine
			// Kurse") would vanish from the list entirely with nothing to
			// indicate why. This was not live-reproduced in the concurrency=1
			// re-baseline done for that task (8/8 courses found on two
			// back-to-back live runs), but the acceptance criteria calls for
			// hardening this regardless since it's a real, if rarer, gap.
			page.WaitForTimeout(contentFallbackWaitMs)
			if _, retryErr := s.gotoPolitely(page, sourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); retryErr != nil {
				logging.Warn("course discovery source %s failed to load after retry: %v (courses only listed on this page may be missing from the result)", sourceURL, retryErr)
				failedSources++
				lastFailure = retryErr
				continue
			}
		}
		s.waitForCourseEntries(sourceURL)

		candidates, err := s.extractCourseCardsFromCurrentPage()
		if err != nil {
			logging.Warn("course discovery source %s failed to extract course cards: %v (courses only listed on this page may be missing from the result)", sourceURL, err)
			failedSources++
			lastFailure = err
			continue
		}
		appendDiscoveredCourses(discovered, candidates, s.opalURL, courseFilter)
	}

	if allCourseSourcesFailed(failedSources, len(sourcePages)) {
		return nil, courseDiscoveryFailure(len(sourcePages), lastFailure)
	}

	items := mapValues(discovered)
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title) })
	return items, nil
}

// allCourseSourcesFailed reports whether *every* course-listing page errored,
// which is the only case discoverCourseLinks may treat as a failed discovery.
//
// A partial failure deliberately stays a warning: the sources overlap, so one
// of them dying usually costs nothing, and turning that into a hard error
// would make a routine transient navigation failure abort a whole sync.
//
// An empty result with no failures is likewise *not* an error - `courses:` in
// config.yaml can legitimately filter every discovered course away, and an
// account really can have none.
func allCourseSourcesFailed(failed, total int) bool {
	return total > 0 && failed == total
}

// courseDiscoveryFailure builds the error for the all-sources-failed case.
//
// The closed-browser hint is worth its own sentence because it is the failure
// actually observed in the wild, and it is the one a user can fix themselves:
// in developer mode the crawl keeps running in the same visible window the
// interactive login used, so closing that window once login looks finished -
// the natural thing to do, since nothing asks you to leave it open - takes the
// rest of the run down with it.
func courseDiscoveryFailure(totalSources int, lastFailure error) error {
	hint := ""
	if isClosedBrowserError(lastFailure) {
		hint = " - the browser window appears to have been closed; leave it open until the run finishes"
	}
	return fmt.Errorf("could not read the course list: all %d OPAL course-listing pages failed%s (last error: %v)", totalSources, hint, lastFailure)
}

// isClosedBrowserError matches Playwright's wording for "the thing you are
// driving is gone". Matched on text because playwright-go reports these as
// plain errors with no distinguishable type to test against.
func isClosedBrowserError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	// "context or browser has been closed" is deliberately not a third check:
	// it contains the second as a substring, so it was already matched.
	return strings.Contains(lower, "target closed") || strings.Contains(lower, "browser has been closed")
}

func (s *OpalScraper) extractCourseCardsFromCurrentPage() ([]map[string]string, error) {
	page := s.getPage()
	if page == nil {
		return nil, errors.New("no page available")
	}

	value, err := page.Evaluate(`() => {
			const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim();
			const rootSelectors = ['section#main-content', '[role="main"]', 'main', '#main-content', '#content'];
			// '.content-preview' matches the "Meine Kurse" (auth/resource/courses) repository
			// tile OPAL currently renders per enrolled course. It is listed first (and searched
			// in its own pass below) so it takes priority over the generic fallback selectors,
			// which can otherwise match unrelated wrapper <li>/<article> elements.
			const cardSelectors = ['.content-preview', '.o_repository_entry', '.o_repoentry', '.o_infoPanel', '.o_card', '.list-group-item', '.dynamic-tab', 'article', 'li'];
			// '.content-preview-title' holds the actual course title on a '.content-preview'
			// tile. It must be checked before the generic h1/h2/h3 fallbacks: a course's rich
			// text description (rendered inside the same tile) can itself contain h1/h2/h3
			// headings (e.g. "Was lernt man in diesem Kurs?"), which querySelector('h1,h2,h3')
			// would otherwise match first since it appears earlier in document order than the
			// real title.
			const titleSelectors = ['.content-preview-title', '.o_repository_entry_title', '.o_title', '.card-title', '.list-group-item-heading', 'h1', 'h2', 'h3'];
			const boilerplatePhrases = ['kurs öffnen', 'weitere kursinhalte ansehen', 'weitere kursinhalte', 'kursinhalte ansehen', 'zum kurs', 'kurs starten', 'details anzeigen', 'mehr anzeigen', 'inhalte anzeigen'];
			const isBoilerplateLabel = (value) => {
				const lower = (value || '').toLowerCase();
				return boilerplatePhrases.some((phrase) => lower.includes(phrase));
			};

			const roots = [];
			const seenRoots = new Set();
			for (const selector of rootSelectors) {
				for (const root of document.querySelectorAll(selector)) {
					if (seenRoots.has(root)) continue;
					seenRoots.add(root);
					roots.push(root);
				}
			}
			if (roots.length === 0) roots.push(document.body);

			const out = [];
			const seen = new Set();
			for (const root of roots) {
				for (const cardSelector of cardSelectors) {
					for (const card of root.querySelectorAll(cardSelector)) {
						const links = Array.from(card.querySelectorAll('a[href]'));
						let openHref = '';
						for (const link of links) {
							const href = normalize(link.getAttribute('href'));
							const label = normalize(link.getAttribute('title') || link.textContent).toLowerCase();
							if (!href || !href.toLowerCase().includes('/repositoryentry/') || href.toLowerCase().includes('/coursenode/')) continue;
							if (!label.includes('kurs öffnen')) continue;
							openHref = href;
							break;
						}
						if (!openHref) continue;

						let courseTitle = '';
						for (const titleSelector of titleSelectors) {
							const titleNode = card.querySelector(titleSelector);
							if (!titleNode) continue;
							const text = normalize(titleNode.textContent);
							if (!text) continue;
							if (isBoilerplateLabel(text)) continue;
							courseTitle = text;
							break;
						}
						if (!courseTitle) {
							for (const link of links) {
								const href = normalize(link.getAttribute('href'));
								const text = normalize(link.getAttribute('title') || link.textContent);
								if (!href || !href.toLowerCase().includes('/repositoryentry/') || href.toLowerCase().includes('/coursenode/')) continue;
								if (!text || isBoilerplateLabel(text)) continue;
								courseTitle = text;
								break;
							}
						}
						if (!courseTitle) continue;

						const item = { courseTitle, openHref };
						const key = JSON.stringify(item);
						if (seen.has(key)) continue;
						seen.add(key);
						out.push(item);
					}
				}
			}
			return out;
		}`)
	if err != nil {
		return nil, err
	}

	return toStringMapSlice(value), nil
}

// waitForCourseEntries was previously not covered by the --debug-clicks
// audit at all (queue task click-audit-analysis-and-cleanup, 2026-07-12,
// found this gap: audit.go's doc comment claimed to cover "every wait call
// in navigation.go's waitForInteractiveLinks" but this discovery.go function
// - called 3 times per discoverCourseLinks run, once per sourcePages entry -
// had no auditLog calls at all). Logging is now added below, matching
// navigation.go's wait-selector-timeout/wait-selector-resolved/
// wait-fallback-done kinds, so a future --debug-clicks run can show whether
// this wait's WaitForSelector calls ever resolve early the way
// waitForInteractiveLinks's turned out never to (see contentFallbackWaitMs's
// doc comment in navigation.go) - that was not re-verified live here since
// this function runs 3x per discoverCourseLinks call versus
// waitForInteractiveLinks's 300+, so it was not the reported slowness; a
// live audit log gap fix is still owed regardless of impact.
func (s *OpalScraper) waitForCourseEntries(pageURL string) {
	page := s.getPage()
	if page == nil {
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
		start := time.Now()
		if _, err := page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(timeoutMs)}); err == nil {
			s.auditLog("wait-selector-resolved", page, selector, fmt.Sprintf("course-entries selector resolved after %s for %s", time.Since(start), pageURL))
			page.WaitForTimeout(800)
			return
		}
		s.auditLog("wait-selector-timeout", page, selector, fmt.Sprintf("course-entries selector did not resolve within %v (waited %s) for %s", timeoutMs, time.Since(start), pageURL))
	}
	s.auditLog("wait-fallback-done", page, strings.Join(selectors, " | "), fmt.Sprintf("no course-entries selector resolved for %s; fixed 1500ms fallback wait", pageURL))
	page.WaitForTimeout(1500)
}

func shouldReplaceCourseRef(existing CourseRef, newTitle string) bool {
	if strings.HasPrefix(existing.Title, "RepositoryEntry ") {
		return true
	}
	return len(strings.TrimSpace(newTitle)) > len(strings.TrimSpace(existing.Title))
}

// boilerplateCourseTitles lists known OPAL UI call-to-action phrases that
// must never be stored as a course title. This mirrors the denylist applied
// during in-page extraction (extractCourseCardsFromCurrentPage) and acts
// as a defense-in-depth guard in case a generic link's text ever reaches
// this far as a candidate courseTitle.
var boilerplateCourseTitles = []string{
	"kurs öffnen",
	"weitere kursinhalte ansehen",
	"weitere kursinhalte",
	"kursinhalte ansehen",
	"zum kurs",
	"kurs starten",
	"details anzeigen",
	"mehr anzeigen",
	"inhalte anzeigen",
	// Common rich-text subheading OPAL course descriptions use (e.g. rendered
	// inside a ".content-preview-desc" block on auth/resource/courses). Before
	// the .content-preview/.content-preview-title selector fix, a bare
	// 'h1,h2,h3' title fallback could pick this up instead of the real course
	// title, surfacing it as a fake "course".
	"was lernt man in diesem kurs",
}

func isBoilerplateCourseTitle(title string) bool {
	return containsAny(strings.ToLower(strings.TrimSpace(title)), boilerplateCourseTitles)
}

func appendDiscoveredCourses(discovered map[string]CourseRef, candidates []map[string]string, opalURL string, courseFilter []string) {
	for _, candidate := range candidates {
		linkTarget := strings.TrimSpace(candidate["openHref"])
		if linkTarget == "" {
			continue
		}

		absURL := resolveURL(opalURL, linkTarget)
		repoID := extractRepositoryEntryID(absURL)
		if repoID == "" {
			continue
		}
		title := strings.TrimSpace(candidate["courseTitle"])
		if title == "" || isBoilerplateCourseTitle(title) || !strictCourseFilterMatches(title, courseFilter) {
			continue
		}

		canonicalURL := resolveURL(opalURL, "auth/RepositoryEntry/"+repoID)
		previous, ok := discovered[repoID]
		if !ok || shouldReplaceCourseRef(previous, title) {
			discovered[repoID] = CourseRef{RepoID: repoID, Title: title, URL: canonicalURL}
		}
	}
}
