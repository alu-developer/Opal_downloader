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
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

func TestNetworkTraceDuringSectionCrawl(t *testing.T) {
	if os.Getenv("OPAL_NETWORK_TRACE") == "" {
		t.Skip("set OPAL_NETWORK_TRACE=1 to run the real-account network trace probe")
	}

	// The package's working directory during `go test` is internal/scraper, so
	// the repo's own config.yaml is two levels up. A hardcoded absolute path
	// here would only ever work on one machine, in a public repo.
	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
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
		// Say which names *are* available: the configured titles carry semester
		// suffixes ("Softwaretechnologie (SoSe 26)"), so a plausible-looking
		// course name misses, and a bare "not found" costs a whole rediscovery
		// round trip to work out why.
		all, listErr := sc.discoverCourseLinks(nil)
		if listErr != nil {
			t.Fatalf("course %q not found, and listing all courses failed too: %v", courseName, listErr)
		}
		var titles []string
		for _, c := range all {
			titles = append(titles, c.Title)
		}
		t.Fatalf("course %q not found; discovered courses are: %q", courseName, titles)
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
		mainFrame    bool
		bytes        int64
	}
	var responses []respRecord
	mainFrame := page.MainFrame()
	page.OnResponse(func(r playwright.Response) {
		req := r.Request()
		// Frame and size, because the interesting question turned out not to be
		// "was there an AJAX call" but "what is this navigation actually
		// fetching". A document response in a *sub*frame is an inline preview
		// the crawl never reads; content-length says what that costs.
		// r.Headers() reads the headers already delivered with the event.
		// HeaderValue()/AllHeaders() must NOT be used here: they are round
		// trips back into the browser, and issuing one from inside an event
		// handler deadlocks the dispatch loop that would carry the reply. That
		// is not theoretical - it wedged a live run of this probe for 55
		// minutes at zero CPU on 2026-07-27 before it was killed.
		var size int64
		if cl, ok := r.Headers()["content-length"]; ok {
			if n, err := strconv.ParseInt(strings.TrimSpace(cl), 10, 64); err == nil {
				size = n
			}
		}
		responses = append(responses, respRecord{
			url:          r.URL(),
			resourceType: req.ResourceType(),
			status:       r.Status(),
			mainFrame:    req.Frame() == mainFrame,
			bytes:        size,
		})
	})

	_, files, _, err := sc.collectCourseFiles(page, course)
	if err != nil {
		t.Fatalf("collectCourseFiles: %v", err)
	}

	// Report to the test log *and* to a file. This probe takes minutes against a
	// large course, and `go test` output lives only in whatever captured the
	// process - which on 2026-07-27 was an agent session that ended before the
	// run finished, taking the only copy of the result with it. A findings file
	// under tmp/ (gitignored) outlives the thing that started the run.
	var report []string
	say := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		report = append(report, line)
		t.Log(line)
	}

	say("course %q (%s)", course.Title, course.URL)
	say("crawl found %d files across %d network responses", len(files), len(responses))

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
		say("resourceType=%-12s count=%d", k, counts[k])
	}

	say("--- xhr/fetch responses (%d) ---", len(xhrFetch))
	for _, r := range xhrFetch {
		say("  [%d] %s %s", r.status, r.resourceType, r.url)
	}

	// Document responses, and why they matter as much as the xhr count. The
	// first run of this probe recorded 406 documents for a ~160-section course -
	// more than one per section - while this codebase contains no iframe
	// handling whatsoever (no FrameLocator, no ContentFrame, no page.Frames()).
	// If the browser is fetching a second document per section, that is where a
	// server-rendered file table could be living, and it would explain how
	// HTTP-first discovery got zero files out of this exact course while calling
	// it "client-side rendered" (2026-07-21 entry, docs/sync-speed-campaign.md):
	// it fetched the outer URL and never saw the inner document.
	var docs []respRecord
	for _, r := range responses {
		if r.resourceType == "document" {
			docs = append(docs, r)
		}
	}
	distinctDocs := map[string]int{}
	for _, r := range docs {
		distinctDocs[r.url]++
	}
	say("--- document responses: %d total, %d distinct URLs ---", len(docs), len(distinctDocs))

	// Group by the path segment straight after /opal/, which is what separates
	// a course-node page (auth/...) from a served file (FolderResource/...). The
	// first run printed an alphabetical sample and every visible entry was a
	// FolderResource PDF, which said something was fetching file *contents*
	// during a discovery-only pass but not how much.
	type bucket struct {
		count     int
		mainFrame int
		bytes     int64
	}
	buckets := map[string]*bucket{}
	for _, r := range docs {
		kind := "other"
		if _, after, found := strings.Cut(r.url, "/opal/"); found {
			kind, _, _ = strings.Cut(after, "/")
		}
		b := buckets[kind]
		if b == nil {
			b = &bucket{}
			buckets[kind] = b
		}
		b.count++
		b.bytes += r.bytes
		if r.mainFrame {
			b.mainFrame++
		}
	}
	var kindNames []string
	for k := range buckets {
		kindNames = append(kindNames, k)
	}
	sort.Strings(kindNames)
	for _, k := range kindNames {
		b := buckets[k]
		say("  /opal/%-20s %4d document(s), %4d in the main frame, %d bytes (content-length)", k+"/", b.count, b.mainFrame, b.bytes)
	}

	// Print a bounded sample rather than all of them: on the large course this
	// is hundreds of lines, and the question here is what *shape* they have.
	var docURLs []string
	for u := range distinctDocs {
		docURLs = append(docURLs, u)
	}
	sort.Strings(docURLs)
	const sample = 15
	for i, u := range docURLs {
		if i >= sample {
			say("  ... and %d more distinct document URLs", len(docURLs)-sample)
			break
		}
		say("  x%-3d %s", distinctDocs[u], u)
	}

	// The claim under test: does any xhr/fetch response fire during a normal
	// (non-paginated) section's initial render? A raw count cannot answer that,
	// because the post-click "show all" expansion is a real AJAX call that this
	// code already drives and already waits on via AJAX_CALL_DONE (wicket.go).
	// Counting those as evidence would report a contradiction on any course with
	// a paginated folder - which is what the first Softwaretechnologie run did,
	// with all three of its calls being showAllLink. Only an *unaccounted* call
	// would put navigation.go's claim in doubt.
	var unaccounted []respRecord
	for _, r := range xhrFetch {
		if !strings.Contains(r.url, "showAllLink") {
			unaccounted = append(unaccounted, r)
		}
	}
	knownShowAll := len(xhrFetch) - len(unaccounted)

	if len(unaccounted) == 0 {
		say("RESULT: no unaccounted xhr/fetch responses (%d total, all %d of them the known showAllLink expansion). No AJAX call exists for the initial section render in this course, so the proposed positive-signal idea has nothing to key off for ordinary sections.", len(xhrFetch), knownShowAll)
	} else {
		say("RESULT: %d unaccounted xhr/fetch response(s), beyond %d known showAllLink expansion(s) - navigation.go's 'no separate AJAX request' claim may be stale or course-specific. Inspect the URLs above before building anything.", len(unaccounted), knownShowAll)
	}

	// A failure to save is not a failure of the probe - the result is already in
	// the test log, and losing the copy is only as bad as the situation before
	// this existed. Say so rather than turning a good run red.
	outDir := filepath.Join("..", "..", "tmp")
	safe := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>|`, r) {
			return '-'
		}
		return r
	}, course.Title)
	outPath := filepath.Join(outDir, "network-trace-"+safe+".txt")
	header := "network trace recorded " + time.Now().Format(time.RFC3339) + "\n\n"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
		t.Logf("could not write %s, result is in this log only: %v", outPath, err)
	} else {
		t.Logf("findings written to %s", outPath)
	}
}
