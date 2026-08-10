package scraper

import "fmt"

// This file is the production form of docs/sync-speed-model.md Question 36
// Step B2: seeding a course's section set from its root page's initial_data
// tree (coursetree.go) instead of discovering it by walking the tree in a
// browser. The algorithm is unchanged from the rider proven in
// TestHTTPFirstSectionDiscovery (httpfirst_probe_test.go, Step B1 run 2):
// 286 of 286 sections, 0 missing, 21 expected extras (enrollment/forum/root
// nodes the browser path also skips), in 71.4s against that run's own
// 173.8s browser crawl. Only its home changed - from a rider that merely
// compares against a browser crawl, to the thing scrapeCoursesHTTPFirst
// (orchestrator.go) calls instead of running one.

// httpGetText issues one authenticated GET and returns the response body,
// erroring on a transport failure or a non-200 status. Small enough that
// fetchSectionFilesHTTP (httpdiscovery_fetch.go) inlines its own copy rather
// than share this one; kept separate here so this file has no dependency
// beyond httpFetcher.
func httpGetText(fetch httpFetcher, url string) (string, error) {
	resp, err := fetch.Get(url)
	if err != nil {
		return "", err
	}
	body, err := responseBodyText(resp)
	if err != nil {
		return "", err
	}
	if resp.Status() != 200 {
		return "", fmt.Errorf("status %d", resp.Status())
	}
	return body, nil
}

// discoverSectionsHTTP returns every section a course has - the course root
// plus everything reachable from it - discovered entirely over plain HTTP:
// seed from the root page's initial_data course-tree (ParseCourseTreeNodes),
// then expand with the crawl's own predicates (parseHTTPSectionCandidates +
// appendSectionFolderTargets), following extractShowAllURLFromHTML wherever
// a section is paginated, because rows past a section's ~20-row cap can be
// sub-sections and not just files (Step B1 run 1's diagnosed miss).
//
// skipNonFileSections mirrors appendSectionFolderTargets's own parameter and
// is applied twice: once here, directly against the tree seed's own node
// class, and once inside appendSectionFolderTargets during expansion. The
// seed bypasses appendSectionFolderTargets entirely for the nodes it
// contributes, so without this an enrollment node the tree carries becomes a
// needless fetch every time (measured: 21 extras across the account when
// this filter is skipped, Step B1 run 1).
//
// A course root fetch failure is fatal (nothing can be seeded without it); a
// failure fetching a queued section or its show-all page is logged by the
// caller and that section is simply absent from the result, matching
// fetchSectionFilesHTTP's own per-section error handling in the existing
// HTTP phase. Returns the sections found, in discovery order (root first),
// and the number of HTTP requests issued.
func discoverSectionsHTTP(fetch httpFetcher, course CourseRef, opalURL string, skipNonFileSections bool, onSectionError func(url string, err error)) ([]SectionRef, int, error) {
	rootBody, err := httpGetText(fetch, course.URL)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP GET course root %s: %w", course.URL, err)
	}
	requests := 1

	rootKey := sectionKey(course.URL, course.RepoID)
	queue := []string{course.URL}
	queued := map[string]struct{}{rootKey: {}}
	visited := map[string]struct{}{}
	sectionTitles := map[string]string{rootKey: course.Title}

	for _, n := range ParseCourseTreeNodes(rootBody) {
		k := sectionKey(n.URL, course.RepoID)
		if _, dup := queued[k]; dup {
			continue
		}
		if skipNonFileSections && isNonFileSectionType(map[string]string{"className": n.Class}) {
			// Marked queued (not just skipped) so a duplicate tree entry or a
			// later expansion candidate pointing at the same node doesn't
			// re-offer it - mirrors appendSectionFolderTargets's own
			// bookkeeping for the same case.
			queued[k] = struct{}{}
			continue
		}
		queued[k] = struct{}{}
		sectionTitles[k] = n.Title
		queue = append(queue, n.URL)
	}

	var sections []SectionRef
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		k := sectionKey(current, course.RepoID)
		delete(queued, k)
		if _, seen := visited[k]; seen {
			continue
		}
		visited[k] = struct{}{}

		body := rootBody
		if current != course.URL {
			body, err = httpGetText(fetch, current)
			requests++
			if err != nil {
				if onSectionError != nil {
					onSectionError(current, err)
				}
				continue
			}
		}

		sections = append(sections, SectionRef{CourseRepoID: course.RepoID, Title: sectionTitles[k], URL: current})

		candidates := parseHTTPSectionCandidates(body)
		if rel := extractShowAllURLFromHTML(body); rel != "" {
			showURL := resolveURL(opalURL, rel)
			showBody, serr := httpGetText(fetch, showURL)
			requests++
			if serr != nil {
				if onSectionError != nil {
					onSectionError(showURL, serr)
				}
			} else {
				candidates = append(candidates, parseHTTPSectionCandidates(showBody)...)
			}
		}
		queue, _ = appendSectionFolderTargets(queue, queued, visited, candidates, opalURL, course.RepoID, current, course.URL, course.Title, sectionTitles, skipNonFileSections)
	}

	return sections, requests, nil
}
