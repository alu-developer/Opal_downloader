package scraper

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestConvertFileRefsToRemoteFiles(t *testing.T) {
	items := []FileRef{
		{
			CourseRepoID: "123",
			CourseTitle:  "Programmierung 1",
			SectionTitle: "Materialien",
			Name:         "Folien.pdf",
			URL:          "https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile",
			Path:         "Programmierung 1/Folien.pdf",
		},
	}

	remoteFiles := convertFileRefsToRemoteFiles(items)
	if len(remoteFiles) != 1 {
		t.Fatalf("expected one remote file, got %d: %#v", len(remoteFiles), remoteFiles)
	}
	if remoteFiles[0].Name != "Folien.pdf" || remoteFiles[0].Course != "Programmierung 1" {
		t.Fatalf("unexpected remote file conversion: %#v", remoteFiles[0])
	}
	if remoteFiles[0].SectionTitle != "Materialien" {
		t.Fatalf("expected SectionTitle to be carried over from FileRef, got %q", remoteFiles[0].SectionTitle)
	}
	if remoteFiles[0].Path != "Programmierung 1/Folien.pdf" {
		t.Fatalf("unexpected remote file path: %#v", remoteFiles[0])
	}
}

func TestAppendUniqueRemoteFilesDeduplicatesByPathAndURL(t *testing.T) {
	existing := []RemoteFile{{
		Name:   "Folien.pdf",
		URL:    "https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile",
		Course: "Programmierung 1",
		Path:   "Programmierung 1/Folien.pdf",
	}}
	seen := map[string]struct{}{
		"Programmierung 1/Folien.pdf|https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile": {},
	}
	candidates := []RemoteFile{
		{
			Name:   "Folien.pdf",
			URL:    "https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile",
			Course: "Programmierung 1",
			Path:   "Programmierung 1/Folien.pdf",
		},
		{
			Name:   "Uebung.pdf",
			URL:    "https://bildungsportal.sachsen.de/opal/goto.php?target=file_56&cmd=sendfile",
			Course: "Programmierung 1",
			Path:   "Programmierung 1/Uebung.pdf",
		},
	}

	files := appendUniqueRemoteFiles(existing, seen, candidates)
	if len(files) != 2 {
		t.Fatalf("expected one new file after dedupe, got %d: %#v", len(files), files)
	}
	if files[1].Name != "Uebung.pdf" {
		t.Fatalf("unexpected appended file: %#v", files[1])
	}
}

func TestOrchestratorSectionKeyDeduplicationByURLVariants(t *testing.T) {
	course := CourseRef{RepoID: "123", Title: "Algorithmen und Datenstrukturen"}
	sections := []SectionRef{
		{CourseRepoID: "123", Title: "Algorithmen und Datenstrukturen", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123"},
		{CourseRepoID: "123", Title: "Algorithmen und Datenstrukturen", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123?cmd=view&ref_id=123"},
		{CourseRepoID: "123", Title: "Materialien", URL: "https://bildungsportal.sachsen.de/opal/goto.php?target=fold_1234"},
	}

	seen := map[string]struct{}{}
	unique := make([]SectionRef, 0, len(sections))
	for _, section := range sections {
		key := normalizedSectionKey(section.URL, course.RepoID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, section)
	}

	if len(unique) != 2 {
		t.Fatalf("expected two unique sections after key-based dedupe, got %d: %#v", len(unique), unique)
	}
}

// TestCollectCourseFilesConcurrentlyAllFilesPresentRegardlessOfOrder exercises
// the worker-pool orchestration added for perf-03 (parallel course
// discovery): every course's files must end up in the aggregated result even
// when courses complete out of dispatch order (the fake collectFn here gives
// earlier-queued courses a longer delay than later ones, so completion order
// is deliberately reversed from dispatch order).
func TestCollectCourseFilesConcurrentlyAllFilesPresentRegardlessOfOrder(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A", URL: "https://opal.example/course/1"},
		{RepoID: "2", Title: "Course B", URL: "https://opal.example/course/2"},
		{RepoID: "3", Title: "Course C", URL: "https://opal.example/course/3"},
		{RepoID: "4", Title: "Course D", URL: "https://opal.example/course/4"},
	}

	collectFn := func(course CourseRef) (courseCrawlResult, error) {
		// Reverse the natural dispatch order: the first course queued sleeps
		// longest, so it finishes last despite being dispatched first.
		delayMs := 40 - (10 * mustAtoi(course.RepoID))
		if delayMs > 0 {
			time.Sleep(time.Duration(delayMs) * time.Millisecond)
		}
		return courseCrawlResult{
			files: []FileRef{{
				CourseRepoID: course.RepoID,
				CourseTitle:  course.Title,
				Name:         "file.pdf",
				URL:          course.URL + "/file.pdf",
				Path:         course.Title + "/file.pdf",
			}},
		}, nil
	}

	remoteFiles := collectCourseFilesConcurrently(context.Background(), courses, 3, collectFn, nil)

	if len(remoteFiles) != len(courses) {
		t.Fatalf("expected %d files (one per course), got %d: %#v", len(courses), len(remoteFiles), remoteFiles)
	}

	gotCourses := make([]string, 0, len(remoteFiles))
	for _, f := range remoteFiles {
		gotCourses = append(gotCourses, f.Course)
	}
	sort.Strings(gotCourses)
	want := []string{"Course A", "Course B", "Course C", "Course D"}
	for i := range want {
		if gotCourses[i] != want[i] {
			t.Fatalf("expected courses %v in result, got %v", want, gotCourses)
		}
	}
}

// TestCollectCourseFilesConcurrentlyOneErrorDoesNotDropOthers matches the
// acceptance criterion that a single course's crawl error must not abort the
// others (the same continue-on-error behavior the old sequential loop had).
func TestCollectCourseFilesConcurrentlyOneErrorDoesNotDropOthers(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A", URL: "https://opal.example/course/1"},
		{RepoID: "2", Title: "Course B (broken)", URL: "https://opal.example/course/2"},
		{RepoID: "3", Title: "Course C", URL: "https://opal.example/course/3"},
	}

	collectFn := func(course CourseRef) (courseCrawlResult, error) {
		if course.RepoID == "2" {
			return courseCrawlResult{}, fmt.Errorf("simulated crawl failure for %s", course.Title)
		}
		return courseCrawlResult{
			files: []FileRef{{
				CourseRepoID: course.RepoID,
				CourseTitle:  course.Title,
				Name:         "file.pdf",
				URL:          course.URL + "/file.pdf",
				Path:         course.Title + "/file.pdf",
			}},
		}, nil
	}

	remoteFiles := collectCourseFilesConcurrently(context.Background(), courses, 2, collectFn, nil)

	if len(remoteFiles) != 2 {
		t.Fatalf("expected 2 files (broken course excluded, others kept), got %d: %#v", len(remoteFiles), remoteFiles)
	}
	for _, f := range remoteFiles {
		if f.Course == "Course B (broken)" {
			t.Fatalf("expected broken course to be excluded from results, got: %#v", remoteFiles)
		}
	}
}

// TestCollectCourseFilesConcurrentlyRespectsConcurrencyCap verifies courses
// are actually crawled in parallel (not serially disguised as a worker pool)
// while never exceeding the configured concurrency cap - the core
// requirement of perf-03 ("small worker pool, default 3-4 concurrent tabs").
func TestCollectCourseFilesConcurrentlyRespectsConcurrencyCap(t *testing.T) {
	const concurrency = 3
	const courseCount = 9

	courses := make([]CourseRef, 0, courseCount)
	for i := 0; i < courseCount; i++ {
		courses = append(courses, CourseRef{RepoID: fmt.Sprintf("%d", i), Title: fmt.Sprintf("Course %d", i), URL: "https://opal.example/course"})
	}

	var current int32
	var maxObserved int32
	collectFn := func(course CourseRef) (courseCrawlResult, error) {
		cur := atomic.AddInt32(&current, 1)
		defer atomic.AddInt32(&current, -1)
		for {
			max := atomic.LoadInt32(&maxObserved)
			if cur <= max || atomic.CompareAndSwapInt32(&maxObserved, max, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		return courseCrawlResult{files: []FileRef{{CourseRepoID: course.RepoID, CourseTitle: course.Title, Name: "f.pdf", URL: course.URL, Path: course.Title + "/f.pdf"}}}, nil
	}

	remoteFiles := collectCourseFilesConcurrently(context.Background(), courses, concurrency, collectFn, nil)

	if len(remoteFiles) != courseCount {
		t.Fatalf("expected %d files, got %d", courseCount, len(remoteFiles))
	}
	if got := atomic.LoadInt32(&maxObserved); got < 2 {
		t.Fatalf("expected courses to run concurrently (max observed concurrency %d), delay should have made overlap observable", got)
	}
	if got := atomic.LoadInt32(&maxObserved); got > concurrency {
		t.Fatalf("expected concurrency to be capped at %d, observed %d", concurrency, got)
	}
}

// TestCollectCourseFilesConcurrentlyMergesDownloadCandidatesViaCallback
// verifies the onResult callback (used by scrapeCoursesBrowser to fold each
// course's locally-recorded downloadCandidates into the shared
// s.downloadCandidates map) is invoked exactly once per successfully
// crawled course, with that course's candidates, and never for a course
// whose crawl errored.
func TestCollectCourseFilesConcurrentlyMergesDownloadCandidatesViaCallback(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
		{RepoID: "2", Title: "Course B (broken)"},
		{RepoID: "3", Title: "Course C"},
	}

	collectFn := func(course CourseRef) (courseCrawlResult, error) {
		if course.RepoID == "2" {
			return courseCrawlResult{}, fmt.Errorf("simulated crawl failure")
		}
		return courseCrawlResult{
			files: []FileRef{{CourseRepoID: course.RepoID, CourseTitle: course.Title, Name: "f.pdf", URL: "https://opal.example/" + course.RepoID, Path: course.Title + "/f.pdf"}},
			downloadCandidates: map[string]downloadCandidate{
				"https://opal.example/" + course.RepoID: {SourceURL: "https://opal.example/" + course.RepoID},
			},
		}, nil
	}

	var mu sync.Mutex
	merged := map[string]downloadCandidate{}
	onResult := func(candidates map[string]downloadCandidate) {
		mu.Lock()
		defer mu.Unlock()
		for k, v := range candidates {
			merged[k] = v
		}
	}

	collectCourseFilesConcurrently(context.Background(), courses, 2, collectFn, onResult)

	if len(merged) != 2 {
		t.Fatalf("expected 2 merged download candidates (one per successful course), got %d: %#v", len(merged), merged)
	}
	if _, ok := merged["https://opal.example/1"]; !ok {
		t.Fatalf("expected candidate from course 1 to be merged, got: %#v", merged)
	}
	if _, ok := merged["https://opal.example/3"]; !ok {
		t.Fatalf("expected candidate from course 3 to be merged, got: %#v", merged)
	}
	if _, ok := merged["https://opal.example/2"]; ok {
		t.Fatalf("did not expect candidate from broken course 2 to be merged, got: %#v", merged)
	}
}

// TestCollectCourseFilesConcurrentlyStopsQueuingAfterCancel is a regression
// test for the queue task fix-gui-cancel-doesnt-abort-course-loop: before the
// fix, cancelling the GUI's Sync/List job only closed the browser - the
// course-dispatch loop here kept queuing (and instant-failing) every
// remaining course instead of stopping. With concurrency=1, cancelling ctx
// from inside the first course's collectFn (before it returns) must mean the
// second course is never dispatched at all.
func TestCollectCourseFilesConcurrentlyStopsQueuingAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
		{RepoID: "2", Title: "Course B"},
		{RepoID: "3", Title: "Course C"},
		{RepoID: "4", Title: "Course D"},
		{RepoID: "5", Title: "Course E"},
	}

	var processed int32
	collectFn := func(course CourseRef) (courseCrawlResult, error) {
		atomic.AddInt32(&processed, 1)
		if course.RepoID == "1" {
			// Simulate the GUI's /sync/cancel handler firing while the first
			// course is still crawling.
			cancel()
		}
		return courseCrawlResult{
			files: []FileRef{{CourseRepoID: course.RepoID, CourseTitle: course.Title, Name: "f.pdf", URL: "https://opal.example/" + course.RepoID, Path: course.Title + "/f.pdf"}},
		}, nil
	}

	remoteFiles := collectCourseFilesConcurrently(ctx, courses, 1, collectFn, nil)

	if got := atomic.LoadInt32(&processed); got != 1 {
		t.Fatalf("expected exactly 1 course to be processed before cancellation stopped further dispatch, got %d", got)
	}
	if len(remoteFiles) != 1 {
		t.Fatalf("expected 1 course's files in result, got %d: %#v", len(remoteFiles), remoteFiles)
	}
}

func mustAtoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}

func TestPreferCancellationReportsTheCancelNotItsWreckage(t *testing.T) {
	// The exact shape of the real case: the user cancelled, which killed the
	// browser, which made discovery fail. Both errors are true; only one of
	// them is what happened.
	discoveryFailure := errors.New("could not read the course list: all 3 OPAL course-listing pages failed - the browser window appears to have been closed; leave it open until the run finishes")

	got := preferCancellation(context.Canceled, discoveryFailure)
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("a cancelled run must report as cancelled, got: %v", got)
	}

	// A genuine failure with nobody cancelling must survive untouched -
	// otherwise this would swallow real breakage.
	if got := preferCancellation(nil, discoveryFailure); got != discoveryFailure {
		t.Fatalf("an uncancelled failure must be reported as itself, got: %v", got)
	}
	if got := preferCancellation(nil, nil); got != nil {
		t.Fatalf("no error either way must stay nil, got: %v", got)
	}
}
