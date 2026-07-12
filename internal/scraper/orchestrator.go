package scraper

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/timing"
)

func (s *OpalScraper) scrapeCoursesBrowser(courseFilter []string) ([]RemoteFile, error) {
	if s.getPage() == nil {
		return nil, errors.New("no page available")
	}

	discoveryTimer := timing.StartTimer()
	courses, err := s.discoverCourseLinks(courseFilter)
	discoveryElapsed := discoveryTimer.Elapsed()
	if err != nil {
		return nil, err
	}
	fmt.Printf("Found %d course links\n", len(courses))
	timing.PrintDiscoverySummary(discoveryElapsed, len(courses))

	concurrency := s.courseConcurrency
	if concurrency <= 0 {
		concurrency = config.DefaultCourseConcurrency
	}

	// Suspend s.page tracking for the duration of the concurrent crawl below:
	// each course gets its own throwaway page (newCourseFileCollector), and
	// without suspending, trackActivePage's ctx.OnPage hook (session.go)
	// would retarget the shared s.page at whichever course's page was opened
	// last, which is already closed by the time downloads start. See
	// pageTrackingSuspended's doc comment in scraper.go.
	s.suspendPageTracking()
	fileCollectionTimer := timing.StartTimer()
	remoteFiles := collectCourseFilesConcurrently(courses, concurrency, s.newCourseFileCollector(), s.mergeDownloadCandidates)
	fileCollectionElapsed := fileCollectionTimer.Elapsed()
	s.resumePageTracking()
	timing.PrintProfileLine("file collection (aggregate): %s", fileCollectionElapsed)

	fmt.Printf("Discovered %d remote files\n", len(remoteFiles))
	return remoteFiles, nil
}

// newCourseFileCollector returns a function that crawls a single course on a
// freshly opened Playwright page/tab (s.context.NewPage()), reusing the
// shared authenticated browser context so login state carries over, and
// closes the page again once the crawl finishes. It is safe to call the
// returned function from multiple goroutines concurrently - each call gets
// its own page, and collectCourseFiles no longer touches the single shared
// s.page or the shared s.downloadCandidates map directly (see
// collectCourseFiles's doc comment in crawl.go).
func (s *OpalScraper) newCourseFileCollector() func(CourseRef) (courseCrawlResult, error) {
	return func(course CourseRef) (courseCrawlResult, error) {
		ctx := s.getContext()
		if ctx == nil {
			return courseCrawlResult{}, errors.New("no authenticated browser context available")
		}

		page, err := ctx.NewPage()
		if err != nil {
			// A dying-but-not-yet-fully-dead browser process (see
			// isPageCrashError's doc comment in navigation.go for the crash
			// class that can lead here - confirmed live surfacing as "target
			// closed: could not read protocol padding: EOF") can make even
			// opening a new tab fail transiently. One short retry before
			// giving up on this course entirely mirrors the wait-and-retry
			// pattern already used throughout crawl.go/navigation.go for
			// other transient Playwright failures.
			time.Sleep(1 * time.Second)
			page, err = ctx.NewPage()
			if err != nil {
				return courseCrawlResult{}, fmt.Errorf("failed to open browser tab for course %q: %w", course.Title, err)
			}
		}

		finalPage, files, downloadCandidates, crawlErr := s.collectCourseFiles(page, course)
		// collectCourseFiles may have recovered from one or more mid-crawl
		// browser crashes by swapping in a replacement tab (see
		// recoverFromPageCrash in navigation.go), in which case finalPage is a
		// different Playwright Page object than the one opened above (which
		// recovery already closed). Close whichever page is actually still
		// open rather than unconditionally closing the original.
		if finalPage != nil {
			_ = finalPage.Close()
		} else {
			_ = page.Close()
		}
		if crawlErr != nil {
			return courseCrawlResult{}, crawlErr
		}
		if len(files) == 0 {
			// Not necessarily a bug (a course can genuinely have no files yet),
			// but crawl.go's own per-section warnings only fire on Goto/extraction
			// failure - a course whose visited sections all "succeeded" but never
			// contained a recognizable file/folder link produces zero files with
			// no other diagnostic at all. Surfacing it here is what let the
			// fix-list-flaky-missing-files investigation notice a course silently
			// dropping to 0 files on some runs.
			fmt.Printf("  Warning: course %q crawled successfully but found 0 files - verify this course actually has no content\n", course.Title)
		}
		return courseCrawlResult{files: files, downloadCandidates: downloadCandidates}, nil
	}
}

// mergeDownloadCandidates copies a single course's locally-recorded
// downloadCandidates into the shared s.downloadCandidates map. It must only
// ever be called from a single goroutine at a time (collectCourseFilesConcurrently
// only calls it from the goroutine draining resultCh), preserving the
// existing invariant documented on OpalScraper.downloadCandidates: writes
// happen synchronously during discovery, before any download worker
// goroutine starts reading it, so no additional locking is needed here.
func (s *OpalScraper) mergeDownloadCandidates(candidates map[string]downloadCandidate) {
	for url, candidate := range candidates {
		s.downloadCandidates[url] = candidate
	}
}

// courseCrawlResult bundles what a single course's crawl produces: the
// files found and the local downloadCandidates map recorded for those files
// (see appendSectionFiles). Kept together so collectCourseFilesConcurrently
// can hand both back to the caller for merging on the single goroutine that
// drains its result channel.
type courseCrawlResult struct {
	files              []FileRef
	downloadCandidates map[string]downloadCandidate
}

// courseWorkerResult is what a collectCourseFilesConcurrently worker reports
// back for a single course. Mirrors the downloadResult pattern used by
// syncer.go's worker pool (see processRemoteFiles): workers only ever
// populate their own courseWorkerResult value and send it on a channel, and
// all shared-state mutation (remoteFiles/remoteFileSeen/s.downloadCandidates)
// happens on the single goroutine draining resultCh below, so no locking is
// needed for that shared state despite the concurrent crawling.
type courseWorkerResult struct {
	course  CourseRef
	result  courseCrawlResult
	elapsed timing.Timer
	err     error
}

// collectCourseFilesConcurrently crawls courses across a worker pool sized
// by concurrency (each worker calling collectFn, which is expected to run
// each course crawl on its own Playwright page - see
// newCourseFileCollector), and merges the results into a single deduplicated
// []RemoteFile. One course's crawl error is logged and that course is
// skipped, matching the previous sequential continue-on-error behavior; it
// does not abort the other courses. collectFn is injected so this worker
// pool can be unit tested without a real Playwright browser (see
// orchestrator_test.go). onResult, when non-nil, is invoked once per
// successfully crawled course (on the same single goroutine that mutates
// remoteFiles/remoteFileSeen) with that course's downloadCandidates, so a
// caller can merge them into shared state without needing its own locking.
func collectCourseFilesConcurrently(courses []CourseRef, concurrency int, collectFn func(CourseRef) (courseCrawlResult, error), onResult func(map[string]downloadCandidate)) []RemoteFile {
	remoteFiles := make([]RemoteFile, 0)
	remoteFileSeen := map[string]struct{}{}

	if len(courses) == 0 {
		return remoteFiles
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(courses) {
		concurrency = len(courses)
	}

	jobCh := make(chan CourseRef)
	resultCh := make(chan courseWorkerResult)

	var workers sync.WaitGroup
	workers.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer workers.Done()
			for course := range jobCh {
				timer := timing.StartTimer()
				result, err := collectFn(course)
				resultCh <- courseWorkerResult{course: course, result: result, elapsed: timer, err: err}
			}
		}()
	}

	go func() {
		for _, course := range courses {
			// Printed here (the single feeder goroutine, in course order)
			// rather than inside the worker, so "Processing: X" lines stay
			// in deterministic dispatch order even at concurrency=1 - a
			// worker goroutine printing its own "Processing" line right
			// after handing a result to resultCh can otherwise race with
			// the result-draining loop's own print for the previous course.
			fmt.Printf("  Processing: %s\n", course.Title)
			jobCh <- course
		}
		close(jobCh)
	}()

	go func() {
		workers.Wait()
		close(resultCh)
	}()

	// All shared-state mutation happens here, on the single goroutine
	// draining resultCh, so concurrent workers never race on remoteFiles,
	// remoteFileSeen, or (indirectly, via the caller) s.downloadCandidates.
	for wr := range resultCh {
		timing.PrintProfileLine("course %q: %s (%d files)", wr.course.Title, wr.elapsed.Elapsed(), len(wr.result.files))
		if wr.err != nil {
			fmt.Printf("  Course crawl error: %v\n", wr.err)
			continue
		}
		if onResult != nil {
			onResult(wr.result.downloadCandidates)
		}
		remoteFiles = appendUniqueRemoteFiles(remoteFiles, remoteFileSeen, convertFileRefsToRemoteFiles(wr.result.files))
	}

	return remoteFiles
}

func convertFileRefsToRemoteFiles(items []FileRef) []RemoteFile {
	remoteFiles := make([]RemoteFile, 0, len(items))
	for _, item := range items {
		remoteFiles = append(remoteFiles, RemoteFile{
			Name:         item.Name,
			URL:          item.URL,
			Course:       item.CourseTitle,
			SectionTitle: item.SectionTitle,
			Path:         item.Path,
			Size:         item.Size,
			Modified:     item.Modified,
		})
	}
	return remoteFiles
}

func appendUniqueRemoteFiles(existing []RemoteFile, seen map[string]struct{}, candidates []RemoteFile) []RemoteFile {
	files := append([]RemoteFile(nil), existing...)
	for _, candidate := range candidates {
		key := candidate.Path + "|" + candidate.URL
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		files = append(files, candidate)
	}
	return files
}
