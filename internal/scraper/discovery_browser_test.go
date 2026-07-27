package scraper

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// The unit tests above cover allCourseSourcesFailed and isClosedBrowserError
// as predicates. This probe covers the thing they cannot: that
// discoverCourseLinks actually *calls* them. That gap is not hypothetical -
// docs/BACKLOG.md records the stall watchdog shipping fully unit-tested and
// wired to nothing, because every test invoked the machinery directly.
//
// It reproduces the real 2026-07-27 incident rather than approximating it:
// the browser goes away mid-run and every course-listing page then fails with
// Playwright's "target closed". Before the fix that finished as a cheerful
// "Found 0 course links".
//
// Opt-in like the other browser probes here, since a fresh clone has no
// Playwright browsers installed:
//
//	OPAL_SCRAPER_BROWSER_PROBE=1 go test ./internal/scraper/ -run TestDiscoveryAgainstARealBrowser -v
func TestDiscoveryAgainstARealBrowser(t *testing.T) {
	if os.Getenv("OPAL_SCRAPER_BROWSER_PROBE") == "" {
		t.Skip("set OPAL_SCRAPER_BROWSER_PROBE=1 to run the real-browser discovery probe")
	}

	// Shaped like the ".content-preview" tile OPAL renders per enrolled course
	// on auth/resource/courses, including the "Kurs öffnen" link the extractor
	// keys on. Served for every path, since all three source pages are fetched.
	const coursesHTML = `<!doctype html><html><body>
		<section id="main-content">
			<div class="content-preview">
				<div class="content-preview-title">Analysis</div>
				<a href="/opal/auth/RepositoryEntry/50999590912" title="Kurs öffnen">Kurs öffnen</a>
			</div>
		</section>
	</body></html>`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(coursesHTML))
	}))
	defer ts.Close()

	EnsurePlaywrightBrowsersPath()
	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("playwright: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Fatalf("launch chromium: %v", err)
	}
	defer browser.Close()

	// The healthy direction first. Without it, a fix that simply always
	// errored would pass the interesting half of this test.
	t.Run("a readable listing discovers its course and does not error", func(t *testing.T) {
		page, err := browser.NewPage()
		if err != nil {
			t.Fatalf("new page: %v", err)
		}
		defer page.Close()

		s := New(ts.URL+"/opal/", "")
		s.setPage(page)

		courses, err := s.discoverCourseLinks(nil)
		if err != nil {
			t.Fatalf("a working listing must not report a discovery failure: %v", err)
		}
		if len(courses) != 1 || courses[0].Title != "Analysis" {
			t.Fatalf("expected the one served course, got %+v", courses)
		}
	})

	// The incident.
	t.Run("a closed browser is an error, not an empty course list", func(t *testing.T) {
		page, err := browser.NewPage()
		if err != nil {
			t.Fatalf("new page: %v", err)
		}

		s := New(ts.URL+"/opal/", "")
		s.setPage(page)

		// Exactly what happened: login finished, the window went away, and the
		// crawl carried on against a page that no longer exists.
		if err := page.Close(); err != nil {
			t.Fatalf("close page: %v", err)
		}

		courses, err := s.discoverCourseLinks(nil)
		if err == nil {
			t.Fatalf("discovery against a closed browser returned no error and %d courses - this is the bug: a run that read nothing reports as a healthy empty account", len(courses))
		}
		if len(courses) != 0 {
			t.Fatalf("expected no courses alongside the error, got %+v", courses)
		}
		if !strings.Contains(err.Error(), "leave it open") {
			t.Fatalf("the error must tell the user their browser window closed, got: %v", err)
		}
	})
}

// TestCancellingDuringDiscoveryReportsCancellation covers the wiring, not the
// predicate. preferCancellation has its own unit test, and removing its call
// site in scrapeCoursesBrowser passes that test, the build and go vet - the
// same unwired-code gap the stall watchdog fell into.
//
// It reproduces the maintainer's 2026-07-27 report ("ich hatte einfach das
// ding gecanceled... also da war alles normal"): cancelling kills the browser,
// which makes every course-listing page fail, which used to surface as
// "could not read the course list" plus advice to leave the window open - a
// broken-tool story told to someone who deliberately stopped the run.
//
//	OPAL_SCRAPER_BROWSER_PROBE=1 go test ./internal/scraper/ -run TestCancellingDuringDiscovery -v
func TestCancellingDuringDiscoveryReportsCancellation(t *testing.T) {
	if os.Getenv("OPAL_SCRAPER_BROWSER_PROBE") == "" {
		t.Skip("set OPAL_SCRAPER_BROWSER_PROBE=1 to run the real-browser cancellation probe")
	}

	// The ctx must still be live when scrapeCoursesBrowser's early guard runs,
	// and cancelled by the time discovery reports its failure - otherwise the
	// early guard catches it and this proves nothing. So the server blocks the
	// first request until the test has cancelled.
	reached := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(reached) })
		<-release
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body></body></html>"))
	}))
	defer ts.Close()

	EnsurePlaywrightBrowsersPath()
	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("playwright: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Fatalf("launch chromium: %v", err)
	}
	defer browser.Close()

	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}

	s := New(ts.URL+"/opal/", "")
	s.setPage(page)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-reached
		// Exactly what the GUI's /sync/cancel does: cancel, then tear the
		// browser down out from under the in-flight call.
		cancel()
		_ = page.Close()
		close(release)
	}()

	_, err = s.scrapeCoursesBrowser(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("a run cancelled during discovery must report as cancelled, got: %v", err)
	}
	if strings.Contains(err.Error(), "leave it open") {
		t.Fatalf("a cancelled run must not be told to leave its browser window open, got: %v", err)
	}
}
