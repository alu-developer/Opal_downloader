package scraper

import (
	"errors"
	"sort"
	"strings"

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

	for _, sourceURL := range sourcePages {
		if _, err := page.Goto(sourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
			continue
		}
		s.waitForCourseEntries(sourceURL)

		candidates, err := s.extractCourseCardsFromCurrentPage()
		if err != nil {
			continue
		}
		appendDiscoveredCourses(discovered, candidates, s.opalURL, courseFilter)
	}

	items := mapValues(discovered)
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title) })
	return items, nil
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
		if _, err := page.WaitForSelector(selector, playwright.PageWaitForSelectorOptions{Timeout: playwright.Float(timeoutMs)}); err == nil {
			page.WaitForTimeout(800)
			return
		}
	}
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
