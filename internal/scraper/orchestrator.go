package scraper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/logging"
	"github.com/alu-developer/opal-downloader/internal/timing"
)

// effectiveCourseConcurrency resolves s.courseConcurrency the same way
// scrapeCoursesBrowser always has (falling back to
// config.DefaultCourseConcurrency when unset/non-positive), but is also
// exposed as its own method so crawl.go's per-section wait budgets
// (waitForStableSectionContent/waitForStableExpandedCandidates) can check
// whether a crawl is actually running concurrently - see
// sectionContentRequiredStableReads's doc comment in crawl.go for why that
// matters: the extra multi-consecutive-stable-read patience those waits
// need under real concurrent-crawl contention is dead weight (only slows
// things down) for a purely serial course_concurrency=1 crawl, which was
// never the class of race this task root-caused and had already been
// live-verified fine with a single-stable-read wait.
func (s *OpalScraper) effectiveCourseConcurrency() int {
	concurrency := s.courseConcurrency
	if concurrency <= 0 {
		concurrency = config.DefaultCourseConcurrency
	}
	return concurrency
}

// scrapeCoursesBrowser discovers courses matching courseFilter and crawls
// each for its files. ctx is checked before discovery starts and again after
// the concurrent crawl (collectCourseFilesConcurrently) finishes - if ctx was
// cancelled mid-crawl (the GUI's /sync/cancel handler), the crawl's course
// dispatch loop stops queuing further courses (see
// collectCourseFilesConcurrently) and this returns whatever partial results
// were collected alongside ctx.Err(), so callers can tell a cancelled run
// apart from a genuinely completed or failed one.
func (s *OpalScraper) scrapeCoursesBrowser(ctx context.Context, courseFilter []string) ([]RemoteFile, error) {
	// First thing, before the guards below: a watcher that is immediately
	// stopped by an early return costs a goroutine that lives for microseconds,
	// and starting here means nothing can be added above it later that hangs
	// without being watched. It also makes the wiring testable without a
	// browser, which matters - a mutation deleting this line passed every
	// other test in stallwatch_test.go, because they all call watchForStall
	// directly and so check the machinery rather than whether anyone uses it.
	//
	// Runs for the whole scrape, not per course: with course concurrency the
	// courses share one browser, so a wedged browser stops all of them and the
	// question worth asking is whether anything at all is still moving.
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

	concurrency := s.effectiveCourseConcurrency()

	// Suspend s.page tracking for the duration of the concurrent crawl below:
	// each course gets its own throwaway page (newCourseFileCollector), and
	// without suspending, trackActivePage's ctx.OnPage hook (session.go)
	// would retarget the shared s.page at whichever course's page was opened
	// last, which is already closed by the time downloads start. See
	// pageTrackingSuspended's doc comment in scraper.go.
	s.suspendPageTracking()
	fileCollectionTimer := timing.StartTimer()
	remoteFiles := collectCourseFilesConcurrently(ctx, courses, concurrency, s.newCourseFileCollector(len(courses)), s.mergeDownloadCandidates)
	fileCollectionElapsed := fileCollectionTimer.Elapsed()
	s.resumePageTracking()
	timing.PrintProfileLine("file collection (aggregate): %s", fileCollectionElapsed)

	logging.User("Discovered %d remote files", len(remoteFiles))
	if err := ctx.Err(); err != nil {
		// Cancelled mid-crawl: report whatever files were actually collected
		// as the (partial) result, but surface ctx.Err() so the caller
		// (ultimately publishCancelOrError in internal/gui/sync.go) reports
		// "cancelled" rather than treating this as a normal completion.
		return remoteFiles, err
	}
	return remoteFiles, nil
}

// scrapeCoursesHybrid is the serial-hybrid discovery path (option A,
// docs/RESUME.md). It runs the browser crawl first (the trusted source, and
// the only thing that can enumerate the JS-rendered section tree), then -
// STRICTLY AFTER the browser is done, never concurrently - bulk-fetches each
// section's file table over HTTP and compares.
//
// mode=="verify": returns the BROWSER result (so a verification run cannot
// silently lose files) but logs a per-course diff between what HTTP found and
// what the browser found, plus timing. This is how the 345-file contract gets
// checked: HTTP must find every file the browser did, with zero missing.
// mode=="1": returns the HTTP result instead (faster), used only after verify
// has shown diff=0.
//
// The HTTP fetch reuses the browser's already-authenticated session context
// (s.getContext().Request()), which shares the session cookies - which is
// exactly why the two phases must be serial. See fetchSectionFilesHTTP's
// doc comment for the measured concurrency hazard.
func (s *OpalScraper) scrapeCoursesHybrid(ctx context.Context, courseFilter []string, mode string) ([]RemoteFile, error) {
	// Phase 1: the browser crawl, unchanged. This is the source of truth for
	// section URLs AND files in verify mode.
	browserFiles, err := s.scrapeCoursesBrowser(ctx, courseFilter)
	if err != nil {
		return nil, err
	}
	if mode == "1" {
		// Future: once verify shows diff=0, return HTTP result. For now, until
		// verification lands, mode=1 behaves as verify to avoid returning an
		// unverified faster path. Fall through to verify logging.
	}

	// Phase 2: HTTP fetch, serial after the browser. The browser crawl records
	// every section it actually visited (recordSectionVisit/VisitRecords),
	// including each section's URL - that is the exact set HTTP must re-fetch
	// for a fair comparison. RemoteFile drops SectionURL, so VisitRecords is the
	// source here.
	httpFetcher := s.httpDiscoveryFetcher()
	if httpFetcher == nil {
		logging.Warn("OPAL_HTTP_DISCOVERY: no authenticated request context; skipping HTTP phase")
		return browserFiles, nil
	}

	visitRecords := s.VisitRecords()
	// Dedupe section URLs (a section can be recorded once per scrape; across
	// multi-course scrapes the list is already per-section). Keep first course
	// title seen for each URL for logging.
	seenSection := map[string]bool{}
	type sectionInfo struct {
		course CourseRef
		sect   SectionRef
	}
	var sections []sectionInfo
	for _, vr := range visitRecords {
		if vr.SectionURL == "" || seenSection[vr.SectionURL] {
			continue
		}
		seenSection[vr.SectionURL] = true
		repoID := repositoryEntryID(vr.SectionURL)
		sections = append(sections, sectionInfo{
			course: CourseRef{RepoID: repoID, Title: vr.Course, URL: repositoryEntryRoot(vr.SectionURL)},
			sect:   SectionRef{CourseRepoID: repoID, Title: vr.SectionTitle, URL: vr.SectionURL},
		})
	}

	// Fetch every distinct section URL over HTTP - the same set the browser
	// visited. Group results by course title for the diff.
	var httpFiles []FileRef
	httpStart := time.Now()
	httpRequests := 0
	for _, si := range sections {
		files, reqs, ferr := fetchSectionFilesHTTP(httpFetcher, si.course, si.sect, s.opalURL)
		httpRequests += reqs
		if ferr != nil {
			logging.Warn("OPAL_HTTP_DISCOVERY: %v", ferr)
			continue
		}
		httpFiles = append(httpFiles, files...)
	}
	httpElapsed := time.Since(httpStart)

	// Distinct-name sets per course for the diff. httpFiles carry CourseTitle
	// (set by appendSectionFiles); browserFiles carry Course (same value).
	httpByCourse := map[string]map[string]bool{}
	for _, hf := range httpFiles {
		if httpByCourse[hf.CourseTitle] == nil {
			httpByCourse[hf.CourseTitle] = map[string]bool{}
		}
		httpByCourse[hf.CourseTitle][hf.Name] = true
	}
	browserByCourse := map[string]map[string]bool{}
	for _, bf := range browserFiles {
		if browserByCourse[bf.Course] == nil {
			browserByCourse[bf.Course] = map[string]bool{}
		}
		browserByCourse[bf.Course][bf.Name] = true
	}

	var missingTotal, extraTotal int
	for course, browserSet := range browserByCourse {
		httpSet := httpByCourse[course]
		var missing, extra []string
		for name := range browserSet {
			if !httpSet[name] {
				missing = append(missing, name)
			}
		}
		for name := range httpSet {
			if !browserSet[name] {
				extra = append(extra, name)
			}
		}
		missingTotal += len(missing)
		extraTotal += len(extra)
		logging.User("OPAL_HTTP_DISCOVERY diff [%s]: browser=%d http=%d missing=%d extra=%d",
			course, len(browserSet), len(httpSet), len(missing), len(extra))
		if len(missing) > 0 && len(missing) <= 20 {
			for _, m := range missing {
				logging.Detail("  missing: %s", m)
			}
		}
	}
	logging.User("OPAL_HTTP_DISCOVERY summary: %d sections, %d HTTP requests, HTTP phase %s; total missing=%d extra=%d",
		len(sections), httpRequests, httpElapsed.Round(time.Millisecond), missingTotal, extraTotal)

	// verify mode returns the trusted browser result. mode=1 (future) returns
	// the HTTP result; until verification lands diff=0, keep returning browser.
	_ = httpFiles
	return browserFiles, nil
}

// httpDiscoveryFetcher returns the authenticated HTTP request context the
// hybrid path uses for leaf-table fetches, or nil if the browser session was
// torn down. Lives here so the hybrid method reads as one block.
func (s *OpalScraper) httpDiscoveryFetcher() httpFetcher {
	if ctx := s.getContext(); ctx != nil {
		return ctx.Request()
	}
	return nil
}

// repositoryEntryRoot returns the RepositoryEntry/<id> URL that owns a section
// URL, by stripping any /CourseNode/... tail. Used to reconstruct a course
// root URL from a file's SectionURL in the hybrid path.
func repositoryEntryRoot(sectionURL string) string {
	idx := strings.Index(sectionURL, "/CourseNode/")
	if idx < 0 {
		return sectionURL
	}
	return sectionURL[:idx]
}

// repositoryEntryID extracts the numeric RepositoryEntry id from an OPAL URL,
// e.g. ".../RepositoryEntry/53228666883/CourseNode/..." -> "53228666883".
// Returns "" when no RepositoryEntry segment is present. Used by the hybrid
// path to build a CourseRef.RepoID from a section URL when reconstructing what
// the browser crawled.
func repositoryEntryID(sectionURL string) string {
	const marker = "/RepositoryEntry/"
	idx := strings.Index(sectionURL, marker)
	if idx < 0 {
		return ""
	}
	rest := sectionURL[idx+len(marker):]
	end := strings.IndexAny(rest, "/?")
	if end < 0 {
		end = len(rest)
	}
	return rest[:end]
}

// newPageStaggerMs is how long newCourseFileCollector holds s.newPageMu
// after a successful ctx.NewPage() before releasing it for the next
// worker's page creation. See newPageMu's doc comment (scraper.go) for the
// live-confirmed reason this exists: a burst of several workers opening a
// fresh tab at the same instant (most commonly right after a fixed-size
// worker pool has a couple of short courses finish around the same time)
// measurably worsened the AJAX-render race even with a generous
// candidateStabilityPoll budget - staggering tab creation itself removes
// that burst without limiting how many tabs may be actively
// crawling/rendering concurrently once open.
const newPageStaggerMs = 800 * time.Millisecond

// newCourseFileCollector returns a function that crawls a single course on a
// freshly opened Playwright page/tab (s.context.NewPage()), reusing the
// shared authenticated browser context so login state carries over, and
// closes the page again once the crawl finishes. It is safe to call the
// returned function from multiple goroutines concurrently - each call gets
// its own page, and collectCourseFiles no longer touches the single shared
// s.page or the shared s.downloadCandidates map directly (see
// collectCourseFiles's doc comment in crawl.go).
// totalCourses is only used to label progress events ("course 2 of 6"); it
// does not affect crawling.
func (s *OpalScraper) newCourseFileCollector(totalCourses int) func(CourseRef) (courseCrawlResult, error) {
	return func(course CourseRef) (courseCrawlResult, error) {
		ctx := s.getContext()
		if ctx == nil {
			return courseCrawlResult{}, errors.New("no authenticated browser context available")
		}

		s.publishProgress(DiscoveryProgress{
			Phase:        PhaseCourseStarted,
			Course:       course.Title,
			CourseIndex:  s.nextCourseIndex(),
			TotalCourses: totalCourses,
		})

		// newPageMu + the stagger sleep below serialize and space out tab
		// creation across concurrent workers - see newPageMu's doc comment
		// (scraper.go) and newPageStaggerMs above for why. Held only around
		// page creation itself, not the crawl that follows, so already-open
		// tabs still render/crawl fully concurrently.
		s.newPageMu.Lock()
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
		}
		if err == nil {
			time.Sleep(newPageStaggerMs)
		}
		s.newPageMu.Unlock()
		if err != nil {
			return courseCrawlResult{}, fmt.Errorf("failed to open browser tab for course %q: %w", course.Title, err)
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
//
// ctx cancellation (the GUI's /sync/cancel handler, via cancelFn in
// internal/gui/sync.go) stops the feeder goroutine from queuing any further
// not-yet-dispatched course onto jobCh - it does not abort a course crawl
// already in flight (collectFn/Playwright have no cooperative-cancellation
// hook), but those in-flight calls return quickly on their own once the
// caller's Close() has torn down the browser, and no *new* course is queued
// after cancellation is observed. This is what turns the previous
// "grind through every remaining course as fast per-course errors after the
// browser closes" behavior into "stop iterating promptly".
func collectCourseFilesConcurrently(ctx context.Context, courses []CourseRef, concurrency int, collectFn func(CourseRef) (courseCrawlResult, error), onResult func(map[string]downloadCandidate)) []RemoteFile {
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
		defer close(jobCh)
		for _, course := range courses {
			select {
			case <-ctx.Done():
				// Cancelled: stop queuing further courses immediately rather
				// than dispatching the rest of the list only to have each one
				// fail fast against a closed browser.
				return
			default:
			}
			select {
			case jobCh <- course:
				// Printed here (the single feeder goroutine, in course order),
				// and only once the send above has actually been received by
				// a worker, so "Processing: X" reliably means this course was
				// dispatched - not just attempted before losing a race against
				// cancellation. Printing from the feeder rather than the
				// worker also keeps these lines in deterministic dispatch
				// order even at concurrency=1: a worker printing its own line
				// right after handing a result to resultCh can otherwise race
				// with the result-draining loop's print for the previous
				// course.
				logging.Detail("Processing: %s", course.Title)
			case <-ctx.Done():
				return
			}
		}
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
			logging.Warn("Course crawl error: %v", wr.err)
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

// preferCancellation reports a cancelled run as cancelled, even when the thing
// that surfaced first was the wreckage cancelling caused.
//
// Cancelling tears the browser down, so a cancel during discovery makes every
// course-listing page fail, and discoverCourseLinks then reports - accurately,
// on its own terms - that it could not read the course list. Handed to a user
// that reads as a broken tool, when in fact the tool did exactly what it was
// told. It also produces actively wrong advice: the closed-browser hint says
// "leave it open until the run finishes", which is nonsense addressed to
// someone who deliberately stopped the run.
//
// Reported by the maintainer (2026-07-27): "das eine problem, dass du gefixt
// hast: ich hatte einfach das ding gecanceled... also da war alles normal."
func preferCancellation(ctxErr, err error) error {
	if ctxErr != nil {
		return ctxErr
	}
	return err
}
