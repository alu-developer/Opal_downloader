package scraper

import "fmt"

// This file is the production form of docs/sync-speed-model.md Question 36
// Step B2: seeding a course's section set from its root page's initial_data
// tree (coursetree.go) instead of discovering it by walking the tree in a
// browser. The discovery algorithm is unchanged from the rider proven in
// TestHTTPFirstSectionDiscovery (httpfirst_probe_test.go, Step B1 run 2):
// 286 of 286 sections, 0 missing, 21 expected extras (enrollment/forum/root
// nodes the browser path also skips), in 71.4s against that run's own
// 173.8s browser crawl.
//
// WHY THIS FUNCTION RETURNS FILES, NOT JUST SECTIONS. The first version of
// this file returned a section list, and scrapeCoursesHTTPFirst
// (orchestrator.go) called fetchSectionFilesHTTP (httpdiscovery_fetch.go)
// per section afterward to get files - mirroring scrapeCoursesHybrid's
// existing two-phase shape. Live-measured 2026-08-10: that fetches every
// section TWICE (once here to find its child links, once more in
// fetchSectionFilesHTTP to find its files), 604 requests for 302 sections
// against Step B1's 313-request rider, and the run took 4m45s against a
// 130s kill line. The fix is what the browser path has always done -
// visitSection (crawl.go) extracts both folder targets and files from the
// SAME page visit - so this function does the same: every section's body
// (and its show-all body, if paginated) is parsed into files right where it
// is already being parsed into folder-target candidates, via the shared
// extractSectionFiles helper below. fetchSectionFilesHTTP itself is
// untouched; it is still what the existing "1"/"verify" modes use.

// httpGetText issues one authenticated GET and returns the response body,
// erroring on a transport failure or a non-200 status. Always passes
// httpGetOptions() (httpdiscovery_fetch.go) - see httpFetchTimeoutMs's doc
// comment for why an explicit timeout is not optional here: without one, a
// single stalled connection wedges this whole method with no bounded
// failure, confirmed live 2026-08-10.
func httpGetText(fetch httpFetcher, url string) (string, error) {
	resp, err := fetch.Get(url, httpGetOptions())
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

// extractSectionFiles turns one section's already-fetched candidates (and,
// if it was paginated, its already-fetched show-all candidates) into
// FileRefs, via the exact appendSectionFiles predicate
// fetchSectionFilesHTTP uses - so HTTP-discovered and browser-discovered
// files share the same Path|URL merge key and dedupe cleanly. Kept
// independent of fetchSectionFilesHTTP (rather than having one call the
// other) because that function issues its own fetches; this one is called
// with candidates already in hand from discoverSectionsHTTP's own discovery
// fetch, which is the entire point - see this file's top doc comment.
func extractSectionFiles(existing []FileRef, fileSeen map[string]struct{}, course CourseRef, section SectionRef, candidates, showAllCandidates []map[string]string, showAllURL, opalURL string) []FileRef {
	files := appendSectionFiles(existing, fileSeen, candidates, course, section, section.URL, "", "", false, opalURL, nil)
	if showAllCandidates != nil {
		files = appendSectionFiles(files, fileSeen, showAllCandidates, course, section, showAllURL, "", "", false, opalURL, nil)
	}
	return files
}

// discoverSectionsHTTP returns every file a course has, discovered and
// fetched entirely over plain HTTP: seed the section set from the root
// page's initial_data course-tree (ParseCourseTreeNodes), expand with the
// crawl's own predicates (parseHTTPSectionCandidates +
// appendSectionFolderTargets), following extractShowAllURLFromHTML wherever
// a section is paginated - because rows past a section's ~20-row cap can be
// sub-sections and not just files (Step B1 run 1's diagnosed miss) - and
// extract files from every section's body as it is visited (see this file's
// top doc comment for why that must happen here rather than in a second
// pass).
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
// failure fetching a queued section or its show-all page is reported via
// onSectionError (may be nil) and that section is simply absent from the
// result - the section-level analogue of fetchSectionFilesHTTP's own
// per-section error handling in the existing HTTP phase, and (since
// httpGetOptions gives every fetch an explicit 20s budget) now a bounded
// failure rather than the indefinite hang Step B2's first live run hit.
//
// onSectionVisited (may be nil), if set, is called once per section actually
// reached (root included) with its title, URL and how many new files that
// section contributed - the same shape and the same "reached and extracted,
// not just queued" semantics as the browser path's recordSectionVisit call
// in collectCourseFiles (crawl.go). Without this, shipping HTTP-first as the
// default silently stopped internal/visitlog's persistent cross-run log from
// accumulating anything at all (found 2026-08-11, re-running Question 36's
// own Step B1 probe after B2 became the default: VisitRecords() came back
// empty because nothing on this path ever called recordSectionVisit).
//
// Returns every file found and the number of HTTP requests issued.
func discoverSectionsHTTP(fetch httpFetcher, course CourseRef, opalURL string, skipNonFileSections bool, onSectionError func(url string, err error), onSectionVisited func(sectionTitle, sectionURL string, filesFound int)) ([]FileRef, int, error) {
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

	var files []FileRef
	fileSeen := map[string]struct{}{}

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

		section := SectionRef{CourseRepoID: course.RepoID, Title: sectionTitles[k], URL: current}
		candidates := parseHTTPSectionCandidates(body)

		var showAllCandidates []map[string]string
		showAllURL := ""
		if rel := extractShowAllURLFromHTML(body); rel != "" {
			showAllURL = resolveURL(opalURL, rel)
			showBody, serr := httpGetText(fetch, showAllURL)
			requests++
			if serr != nil {
				if onSectionError != nil {
					onSectionError(showAllURL, serr)
				}
			} else {
				showAllCandidates = parseHTTPSectionCandidates(showBody)
			}
		}

		filesBefore := len(files)
		files = extractSectionFiles(files, fileSeen, course, section, candidates, showAllCandidates, showAllURL, opalURL)
		if onSectionVisited != nil {
			onSectionVisited(section.Title, current, len(files)-filesBefore)
		}

		expandCandidates := candidates
		if showAllCandidates != nil {
			expandCandidates = append(append([]map[string]string(nil), candidates...), showAllCandidates...)
		}
		queue, _ = appendSectionFolderTargets(queue, queued, visited, expandCandidates, opalURL, course.RepoID, current, course.URL, course.Title, sectionTitles, skipNonFileSections)
	}

	return files, requests, nil
}
