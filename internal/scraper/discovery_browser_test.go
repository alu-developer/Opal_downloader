package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
