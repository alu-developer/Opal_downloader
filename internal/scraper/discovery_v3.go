package scraper

import (
	"errors"
	"sort"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

func (s *OpalScraper) discoverCourseLinksV3(courseFilter []string) ([]CourseRefV2, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	discovered := map[string]CourseRefV2{}
	sourcePages := []string{
		resolveURL(s.opalURL, "auth/resource/courses"),
		resolveURL(s.opalURL, "auth/RepositoryEntry/mycourses"),
		resolveURL(s.opalURL, "auth/home#my-courses"),
		resolveURL(s.opalURL, "auth/home"),
	}

	for _, sourceURL := range sourcePages {
		if _, err := s.page.Goto(sourceURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
			continue
		}
		s.waitForCourseEntries(sourceURL)

		candidates, err := s.extractCourseCardsFromCurrentPageV3()
		if err != nil {
			continue
		}
		appendDiscoveredCoursesV3(discovered, candidates, s.opalURL, courseFilter)
	}

	items := mapValues(discovered)
	sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title) })
	return items, nil
}

func (s *OpalScraper) extractCourseCardsFromCurrentPageV3() ([]map[string]string, error) {
	if s.page == nil {
		return nil, errors.New("no page available")
	}

	value, err := s.page.Evaluate(`() => {
			const normalize = (value) => (value || '').replace(/\s+/g, ' ').trim();
			const rootSelectors = ['section#main-content', '[role="main"]', 'main', '#main-content', '#content'];
			const cardSelectors = ['.o_repository_entry', '.o_repoentry', '.o_infoPanel', '.o_card', '.list-group-item', '.dynamic-tab', 'article', 'li'];
			const titleSelectors = ['.o_repository_entry_title', '.o_title', '.card-title', '.list-group-item-heading', 'h1', 'h2', 'h3'];

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
							if (text.toLowerCase().includes('kurs öffnen')) continue;
							courseTitle = text;
							break;
						}
						if (!courseTitle) {
							for (const link of links) {
								const href = normalize(link.getAttribute('href'));
								const text = normalize(link.getAttribute('title') || link.textContent);
								if (!href || !href.toLowerCase().includes('/repositoryentry/') || href.toLowerCase().includes('/coursenode/')) continue;
								if (!text || text.toLowerCase().includes('kurs öffnen')) continue;
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

func appendDiscoveredCoursesV3(discovered map[string]CourseRefV2, candidates []map[string]string, opalURL string, courseFilter []string) {
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
		if title == "" || !strictCourseFilterMatches(title, courseFilter) {
			continue
		}

		canonicalURL := resolveURL(opalURL, "auth/RepositoryEntry/"+repoID)
		previous, ok := discovered[repoID]
		if !ok || shouldReplaceCourseRefV2(previous, title) {
			discovered[repoID] = CourseRefV2{RepoID: repoID, Title: title, URL: canonicalURL}
		}
	}
}
