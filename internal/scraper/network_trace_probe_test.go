package scraper

// One-off feasibility check for the sync-speed campaign's 2026-07-27 lead
// (docs/sync-speed-campaign.md): "the file table arrives in a Wicket AJAX
// response the browser already receives and parses" - proposed as the basis
// for a positive completion signal to replace the settle-wait/stability-poll
// debounce that costs ~84s of a 216.6s run.
//
// navigation.go's own waitForInteractiveLinks doc comment already claims the
// opposite, from an earlier research task (investigate-load-completion-
// detection, 2026-07-16): "network trace confirmed no separate 'populate
// content' AJAX request exists to hook a response-based wait on instead" for
// the initial per-section render (as opposed to the post-click show-all
// expansion, which does fire a real AJAX call and already uses
// AJAX_CALL_DONE, see wicket.go). This probe settles which claim holds by
// actually recording every network response during a real section crawl.
//
// Usage: OPAL_NETWORK_TRACE=1 go test ./internal/scraper/ -run TestNetworkTraceDuringSectionCrawl -v -timeout 5m
import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

func TestNetworkTraceDuringSectionCrawl(t *testing.T) {
	if os.Getenv("OPAL_NETWORK_TRACE") == "" {
		t.Skip("set OPAL_NETWORK_TRACE=1 to run the real-account network trace probe")
	}

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// A small course keeps this cheap: enough sections to be representative,
	// few enough to read the whole trace by eye.
	courseName := os.Getenv("OPAL_NETWORK_TRACE_COURSE")
	if courseName == "" {
		courseName = "Algorithmen und Datenstrukturen"
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()

	if err := sc.ensureSession(false); err != nil {
		t.Fatalf("ensureSession: %v", err)
	}

	courses, err := sc.discoverCourseLinks([]string{courseName})
	if err != nil {
		t.Fatalf("discoverCourseLinks: %v", err)
	}
	if len(courses) == 0 {
		t.Fatalf("course %q not found among discovered courses", courseName)
	}
	course := courses[0]
	t.Logf("tracing course %q (%s)", course.Title, course.URL)

	page := sc.getPage()
	if page == nil {
		t.Fatalf("no page available after ensureSession")
	}

	type respRecord struct {
		url          string
		resourceType string
		status       int
	}
	var responses []respRecord
	page.OnResponse(func(r playwright.Response) {
		req := r.Request()
		responses = append(responses, respRecord{
			url:          r.URL(),
			resourceType: req.ResourceType(),
			status:       r.Status(),
		})
	})

	_, files, _, err := sc.collectCourseFiles(page, course)
	if err != nil {
		t.Fatalf("collectCourseFiles: %v", err)
	}

	t.Logf("crawl found %d files across %d network responses", len(files), len(responses))

	counts := map[string]int{}
	var xhrFetch []respRecord
	for _, r := range responses {
		counts[r.resourceType]++
		if r.resourceType == "xhr" || r.resourceType == "fetch" {
			xhrFetch = append(xhrFetch, r)
		}
	}

	var kinds []string
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		t.Logf("resourceType=%-12s count=%d", k, counts[k])
	}

	t.Logf("--- xhr/fetch responses (%d) ---", len(xhrFetch))
	for _, r := range xhrFetch {
		t.Logf("  [%d] %s %s", r.status, r.resourceType, r.url)
	}

	// The claim under test: does ANY xhr/fetch response fire during a normal
	// (non-paginated) section's initial render? If the count is zero, the
	// campaign's 2026-07-27 premise (a Wicket AJAX response carries the file
	// table on the initial render) is false for this course, matching
	// navigation.go's existing claim rather than contradicting it.
	if len(xhrFetch) == 0 {
		t.Logf("RESULT: zero xhr/fetch responses observed - no AJAX call exists for the initial section render in this course. The proposed positive-signal idea has nothing to key off for ordinary sections.")
	} else {
		t.Logf("RESULT: %d xhr/fetch responses observed - navigation.go's 'no separate AJAX request' claim may be stale or course-specific. Inspect the URLs above before building anything.", len(xhrFetch))
	}
}
