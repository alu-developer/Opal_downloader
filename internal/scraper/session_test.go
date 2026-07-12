package scraper

import (
	"fmt"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// fakePage is a minimal stand-in for playwright.Page used to test
// trackActivePage/handleContextPageOpened's page-tracking logic without a
// real browser. It embeds the (nil) playwright.Page interface so it
// satisfies the interface for free, and only overrides the two methods
// handleContextPageOpened actually calls; any other method would panic if
// invoked, which none of these tests do.
type fakePage struct {
	playwright.Page
	name string
}

func (f *fakePage) SetDefaultTimeout(timeout float64)           {}
func (f *fakePage) SetDefaultNavigationTimeout(timeout float64) {}

// TestConcurrentCourseCrawlDoesNotClobberSharedPage reproduces the bug
// described in the queue task: newCourseFileCollector (orchestrator.go)
// opens one throwaway page per course via ctx.NewPage() during concurrent
// crawling, and ctx.OnPage fires for every page opened in the context - not
// just login-flow tabs. Before the pageTrackingSuspended fix, that left
// s.page pointing at whichever course's already-closed crawl page was
// opened last, breaking every subsequent browser-fallback download with
// "target closed" errors. This simulates that firing pattern directly via
// handleContextPageOpened (see session.go) and asserts the shared page
// survives the concurrent crawl unclobbered when tracking is suspended
// around it, exactly as orchestrator.go's scrapeCoursesBrowser now does.
func TestConcurrentCourseCrawlDoesNotClobberSharedPage(t *testing.T) {
	s := New("", "", "", "", "")

	loginPage := &fakePage{name: "login/dashboard page"}
	s.setPage(loginPage)

	courses := make([]CourseRef, 0, 6)
	for i := 0; i < 6; i++ {
		courses = append(courses, CourseRef{RepoID: fmt.Sprintf("%d", i), Title: fmt.Sprintf("Course %d", i), URL: "https://opal.example/course"})
	}

	collectFn := func(course CourseRef) (courseCrawlResult, error) {
		coursePage := &fakePage{name: course.Title + " crawl page"}
		// Simulate ctx.OnPage firing for this course's freshly opened tab,
		// the way it does for real when newCourseFileCollector calls
		// ctx.NewPage().
		s.handleContextPageOpened(coursePage)
		// Simulate newCourseFileCollector's defer page.Close() once the
		// course's crawl finishes.
		return courseCrawlResult{files: []FileRef{{CourseRepoID: course.RepoID, CourseTitle: course.Title, Name: "f.pdf", URL: course.URL, Path: course.Title + "/f.pdf"}}}, nil
	}

	s.suspendPageTracking()
	remoteFiles := collectCourseFilesConcurrently(courses, 3, collectFn, nil)
	s.resumePageTracking()

	if len(remoteFiles) != len(courses) {
		t.Fatalf("expected %d files, got %d", len(courses), len(remoteFiles))
	}

	got, ok := s.getPage().(*fakePage)
	if !ok || got != loginPage {
		t.Fatalf("expected shared s.page to still be the login/dashboard page after concurrent crawl, got %#v", s.getPage())
	}
}

// TestPageTrackingResumesAfterConcurrentCrawl verifies resumePageTracking
// actually re-enables handleContextPageOpened afterward, so interactive
// login's tab-switching handling (trackActivePage's original purpose) keeps
// working for any login that happens after a scrape's concurrent crawl
// phase (e.g. session re-login on the next command invocation).
func TestPageTrackingResumesAfterConcurrentCrawl(t *testing.T) {
	s := New("", "", "", "", "")

	s.suspendPageTracking()
	s.resumePageTracking()

	newLoginTab := &fakePage{name: "post-redirect login tab"}
	s.handleContextPageOpened(newLoginTab)

	got, ok := s.getPage().(*fakePage)
	if !ok || got != newLoginTab {
		t.Fatalf("expected handleContextPageOpened to retarget s.page once tracking resumed, got %#v", s.getPage())
	}
}

// TestPageTrackingSuspendedIgnoresNewPages is a narrower unit test of the
// suspend flag itself: while suspended, handleContextPageOpened must not
// change s.page at all, regardless of any concurrent crawl machinery.
func TestPageTrackingSuspendedIgnoresNewPages(t *testing.T) {
	s := New("", "", "", "", "")

	original := &fakePage{name: "original"}
	s.setPage(original)

	s.suspendPageTracking()
	s.handleContextPageOpened(&fakePage{name: "should be ignored"})

	got, ok := s.getPage().(*fakePage)
	if !ok || got != original {
		t.Fatalf("expected s.page to remain unchanged while tracking suspended, got %#v", s.getPage())
	}
}

// TestShouldRelaunchHeadlessAfterInteractiveLogin covers the branch in
// ensureSession that was wrongly taken before this fix: sync/list
// (ensureSession(false)) falling back to an interactive login (because the
// saved session was missing or expired) must relaunch headlessly afterward
// so the crawl that follows doesn't run in the same visible window, while
// the standalone `login` command (forceInteractive) and explicit --dev
// tracing (developerMode) must not, since forceInteractive has no
// subsequent crawl to protect and developerMode explicitly wants a visible
// browser. See queue task investigate-sync-list-not-headless.
func TestShouldRelaunchHeadlessAfterInteractiveLogin(t *testing.T) {
	cases := []struct {
		name             string
		forceInteractive bool
		developerMode    bool
		wantRelaunch     bool
	}{
		{"sync/list fallback login, normal mode", false, false, true},
		{"sync/list fallback login, dev mode", false, true, false},
		{"standalone login command, normal mode", true, false, false},
		{"standalone login command, dev mode", true, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldRelaunchHeadlessAfterInteractiveLogin(tc.forceInteractive, tc.developerMode)
			if got != tc.wantRelaunch {
				t.Fatalf("shouldRelaunchHeadlessAfterInteractiveLogin(forceInteractive=%v, developerMode=%v) = %v, want %v",
					tc.forceInteractive, tc.developerMode, got, tc.wantRelaunch)
			}
		})
	}
}
