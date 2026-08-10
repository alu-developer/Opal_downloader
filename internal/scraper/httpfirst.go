package scraper

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/alu-developer/opal-downloader/internal/timing"
)

// This file is discovery path B2 (docs/sync-speed-model.md Question 36,
// docs/BACKLOG.md "Now"): production code for the HTTP-first section
// discovery that Step B1 proved as a read-only rider on an ordinary crawl
// (httpfirst_probe_test.go, 286/286 sections, 0 missing, 71.4s against the
// same run's 173.8s browser crawl). Unlike scrapeCoursesHybrid
// (orchestrator.go), which still runs the whole browser crawl before ever
// touching HTTP, this path never walks a course's content tree in the
// browser at all - only discoverCourseLinks (a different page, the "Meine
// Kurse" dashboard) still needs it.
//
// Gated by OPAL_HTTP_DISCOVERY=2 (see scraper.go's ScrapeWithSavedSession)
// until a byte-for-byte diff against the 349-file ground truth
// (scripts/compare-visit-runs.ps1) has passed on a live account - this is one
// of the three discovery paths that have silently lost files before
// (CLAUDE.md), so it does not become a default on the strength of the Step B1
// probe alone.
//
// Kept serial per course (course_concurrency is not applied here, unlike
// scrapeCoursesBrowser): Step B1 measured all 6 courses' HTTP discovery at
// 71.4s total, already well inside the target, and concurrent HTTP GETs
// against OPAL's stateful Wicket session are untested territory this task
// did not need to open. Raising it is future work, not a correctness
// requirement.
func (s *OpalScraper) scrapeCoursesHTTPFirst(ctx context.Context, courseFilter []string) ([]RemoteFile, error) {
	defer s.watchForStall(ctx)()

	if s.getPage() == nil {
		return nil, errors.New("no page available")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	discoveryTimer := timing.StartTimer()
	courses, err := s.discoverCourseLinks(courseFilter)
	discoveryElapsed := discoveryTimer.Elapsed()
	if err != nil {
		return nil, preferCancellation(ctx.Err(), err)
	}
	logging.User("Found %d course links", len(courses))
	timing.PrintDiscoverySummary(discoveryElapsed, len(courses))
	s.publishProgress(DiscoveryProgress{Phase: PhaseCoursesFound, TotalCourses: len(courses)})

	fetch := s.httpDiscoveryFetcher()
	if fetch == nil {
		return nil, errors.New("OPAL_HTTP_DISCOVERY=2: no authenticated HTTP request context available")
	}

	fileCollectionTimer := timing.StartTimer()
	remoteFiles := collectCourseFilesConcurrently(ctx, courses, 1, s.newHTTPCourseFileCollector(fetch, len(courses)), s.mergeDownloadCandidates)
	fileCollectionElapsed := fileCollectionTimer.Elapsed()
	timing.PrintProfileLine("file collection (aggregate, HTTP-first): %s", fileCollectionElapsed)

	logging.User("Discovered %d remote files", len(remoteFiles))
	if err := ctx.Err(); err != nil {
		return remoteFiles, err
	}
	return remoteFiles, nil
}

// newHTTPCourseFileCollector adapts httpFirstCourseCrawl to the
// collectCourseFilesConcurrently/courseCrawlResult shape newCourseFileCollector
// (orchestrator.go) already uses for the browser path, so both paths share
// the same worker-pool/merge/progress machinery and only the per-course
// crawl itself differs.
func (s *OpalScraper) newHTTPCourseFileCollector(fetch httpFetcher, totalCourses int) func(CourseRef) (courseCrawlResult, error) {
	return func(course CourseRef) (courseCrawlResult, error) {
		s.publishProgress(DiscoveryProgress{
			Phase:        PhaseCourseStarted,
			Course:       course.Title,
			CourseIndex:  s.nextCourseIndex(),
			TotalCourses: totalCourses,
		})

		files, downloadCandidates, err := s.httpFirstCourseCrawl(fetch, course)
		if err != nil {
			return courseCrawlResult{}, err
		}
		if len(files) == 0 {
			logging.Warn("course %q crawled successfully but found 0 files - verify this course actually has no content", course.Title)
		}
		s.publishProgress(DiscoveryProgress{
			Phase:     PhaseCourseDone,
			Course:    course.Title,
			FileCount: len(files),
		})
		return courseCrawlResult{files: files, downloadCandidates: downloadCandidates}, nil
	}
}

// httpFirstCourseCrawl discovers and fetches one course's files entirely over
// HTTP: it seeds the BFS queue from the course root's own initial_data tree
// (ParseCourseTreeNodes, Question 34 - the complete course-node tree arrives
// in the first response) and expands it exactly the way the browser path does
// in collectCourseFiles (crawl.go), sharing appendSectionFolderTargets and
// appendSectionFiles so both paths apply the identical file/section
// predicates - the only thing that differs is that a section's HTML comes
// from a plain GET instead of a rendered page. extractShowAllURLFromHTML is
// followed before folder-target expansion, not just file extraction: rows
// past a section's pagination cap include sub-sections, not just files (Step
// B1's own first-run miss).
func (s *OpalScraper) httpFirstCourseCrawl(fetch httpFetcher, course CourseRef) ([]FileRef, map[string]downloadCandidate, error) {
	if course.RepoID == "" {
		return nil, nil, errors.New("course repo id is required")
	}

	files := make([]FileRef, 0)
	downloadCandidates := map[string]downloadCandidate{}
	fileSeen := map[string]struct{}{}
	visited := map[string]struct{}{}
	rootKey := sectionKey(course.URL, course.RepoID)
	queue := []string{course.URL}
	queued := map[string]struct{}{rootKey: {}}
	sectionTitles := map[string]string{rootKey: course.Title}
	// Mirrors collectCourseFiles' maxPages sanity cap (crawl.go) - the
	// visited/queued dedup already prevents infinite loops on legitimately
	// distinct sections, so this only guards a runaway/malformed response.
	const maxPages = 500

	sectionsVisited := 0
	sectionsFailed := 0

	for len(queue) > 0 && len(visited) < maxPages {
		currentURL := queue[0]
		queue = queue[1:]
		currentKey := sectionKey(currentURL, course.RepoID)
		delete(queued, currentKey)
		if _, ok := visited[currentKey]; ok {
			continue
		}
		visited[currentKey] = struct{}{}

		body, err := httpFirstGetText(fetch, currentURL)
		if err != nil {
			sectionsFailed++
			logging.Warn("HTTP-first: skipping section %s: %v", currentURL, err)
			continue
		}
		sectionsVisited++

		if currentURL == course.URL {
			for _, n := range ParseCourseTreeNodes(body) {
				k := sectionKey(n.URL, course.RepoID)
				if _, dup := queued[k]; dup {
					continue
				}
				if _, done := visited[k]; done {
					continue
				}
				queued[k] = struct{}{}
				if strings.TrimSpace(n.Title) != "" {
					sectionTitles[k] = n.Title
				}
				queue = append(queue, n.URL)
			}
		}

		candidates := parseHTTPSectionCandidates(body)
		showAllURL := ""
		if rel := extractShowAllURLFromHTML(body); rel != "" {
			showAllURL = resolveURL(s.opalURL, rel)
			showBody, showErr := httpFirstGetText(fetch, showAllURL)
			if showErr != nil {
				logging.Warn("HTTP-first show-all fetch failed for %s: %v (continuing with first-page candidates only)", currentURL, showErr)
				showAllURL = ""
			} else {
				candidates = append(candidates, parseHTTPSectionCandidates(showBody)...)
			}
		}

		sectionTitle := sectionTitles[currentKey]
		if strings.TrimSpace(sectionTitle) == "" {
			sectionTitle = deriveSectionTitleFromURL(course.Title, currentURL)
		}
		section := SectionRef{CourseRepoID: course.RepoID, Title: sectionTitle, URL: currentURL}
		filesBefore := len(files)
		files = appendSectionFiles(files, fileSeen, candidates, course, section, currentURL, showAllURL, "", false, s.opalURL, downloadCandidates)
		s.publishProgress(DiscoveryProgress{
			Phase:        PhaseSection,
			Course:       course.Title,
			Section:      sectionTitle,
			SectionURL:   currentURL,
			SectionsDone: sectionsVisited,
		})
		// Feeds scripts/compare-visit-runs.ps1, the byte-diff tool this path's
		// own rollout depends on - see this file's package doc comment.
		s.recordSectionVisit(course.Title, sectionTitle, currentURL, len(files)-filesBefore)

		var skipped []skippedSection
		queue, skipped = appendSectionFolderTargets(queue, queued, visited, candidates, s.opalURL, course.RepoID, currentURL, course.URL, course.Title, sectionTitles, s.skipEnrollmentSections)
		for _, sk := range skipped {
			logging.Detail("Skipping section %q (%s): structurally cannot hold files (OPAL enrollment/Einschreibung course-node)", sk.Title, sk.URL)
		}
	}

	if len(queue) > 0 && len(visited) >= maxPages {
		logging.Warn("course %q hit the %d-section crawl cap with %d section(s) still queued; some content may be missing", course.Title, maxPages, len(queue))
	}

	if sectionsVisited == 0 && sectionsFailed > 0 {
		// Mirrors collectCourseFiles' identical guard (crawl.go): every
		// attempted section failing is reported as an error rather than a
		// confirmed-empty course, so it is dropped from the results instead of
		// silently reporting 0 files.
		return files, downloadCandidates, fmt.Errorf("course %q: all %d attempted section page(s) failed to load - result is incomplete, not a confirmed empty course", course.Title, sectionsFailed)
	}

	return files, downloadCandidates, nil
}

// httpFirstGetText fetches url over fetch and returns its body, treating any
// non-200 status as an error alongside a transport failure - the same
// contract fetchSectionFilesHTTP (httpdiscovery_fetch.go) already applies for
// the hybrid path's leaf-table fetches.
func httpFirstGetText(fetch httpFetcher, url string) (string, error) {
	resp, err := fetch.Get(url)
	if err != nil {
		return "", fmt.Errorf("GET %s: %w", url, err)
	}
	body, err := responseBodyText(resp)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", url, err)
	}
	if status := resp.Status(); status != 200 {
		return "", fmt.Errorf("GET %s: status %d", url, status)
	}
	return body, nil
}
