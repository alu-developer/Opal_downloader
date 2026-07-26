package scraper

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// newTestPool builds a pool with no browser behind it. openPage hands back nil
// pages, which is fine: the stubbed visit never touches them.
func newTestPool(workers int, visit func(playwright.Page, string, string) (playwright.Page, sectionVisit)) *sectionTabPool {
	p := &sectionTabPool{s: &OpalScraper{}, workers: workers, pages: []playwright.Page{nil}}
	p.visit = visit
	p.openPage = func() (playwright.Page, error) { return nil, nil }
	p.stagger = 0
	return p
}

// THE PROPERTY SECTION CONCURRENCY RESTS ON.
//
// The crawl merges a level's results serially, in the order the sections were
// popped, so that a concurrent crawl produces the same files in the same order
// with the same dedupe outcomes as a serial one. That is only true if visitAll
// returns results positionally, never in completion order.
//
// This makes the sections finish in deliberately reversed order - the last one
// returns immediately, the first one dawdles - which is exactly the case where
// an implementation that appended results as they arrived would look correct
// under light load and scramble the crawl under real load.
func TestSectionPoolReturnsResultsInInputOrderNotCompletionOrder(t *testing.T) {
	level := make([]string, 12)
	for i := range level {
		level[i] = fmt.Sprintf("https://opal.example/section/%02d", i)
	}

	visit := func(page playwright.Page, url, title string) (playwright.Page, sectionVisit) {
		// Later sections finish sooner.
		var idx int
		fmt.Sscanf(url, "https://opal.example/section/%d", &idx)
		time.Sleep(time.Duration(len(level)-idx) * 3 * time.Millisecond)
		return page, sectionVisit{url: url, title: title}
	}

	pool := newTestPool(4, visit)
	results := pool.visitAll(level, func(i int) string { return fmt.Sprintf("Section %d", i) })

	if len(results) != len(level) {
		t.Fatalf("got %d results for %d sections", len(results), len(level))
	}
	for i, r := range results {
		if r.url != level[i] {
			t.Fatalf("result %d is %q, want %q - results came back in completion order, "+
				"which would reorder every file the crawl finds", i, r.url, level[i])
		}
		if want := fmt.Sprintf("Section %d", i); r.title != want {
			t.Fatalf("result %d carries title %q, want %q - titles are paired by index", i, r.title, want)
		}
	}
}

// Every section must be visited exactly once. A worker-striding bug (an
// off-by-one in the `i += workers` loop, say) would silently skip or repeat
// sections, which reads downstream as missing files - the exact failure mode
// this whole area has a history of.
func TestSectionPoolVisitsEverySectionExactlyOnce(t *testing.T) {
	for _, workers := range []int{1, 2, 3, 4, 8, 16} {
		for _, count := range []int{1, 2, 3, 7, 12, 33} {
			level := make([]string, count)
			for i := range level {
				level[i] = fmt.Sprintf("s%d", i)
			}

			var mu sync.Mutex
			seen := map[string]int{}
			visit := func(page playwright.Page, url, title string) (playwright.Page, sectionVisit) {
				mu.Lock()
				seen[url]++
				mu.Unlock()
				return page, sectionVisit{url: url}
			}

			pool := newTestPool(workers, visit)
			results := pool.visitAll(level, func(i int) string { return "" })

			if len(results) != count {
				t.Fatalf("workers=%d count=%d: got %d results", workers, count, len(results))
			}
			for _, url := range level {
				if seen[url] != 1 {
					t.Fatalf("workers=%d count=%d: section %q visited %d times, want exactly 1",
						workers, count, url, seen[url])
				}
			}
		}
	}
}

// No tab may be driven by two goroutines at once - Playwright pages are not
// safe for that, and the failure would be a mid-crawl crash or, worse, one
// tab's content read into another section's results.
func TestSectionPoolNeverDrivesOneTabConcurrently(t *testing.T) {
	level := make([]string, 40)
	for i := range level {
		level[i] = fmt.Sprintf("s%d", i)
	}

	// The stub reports which worker slot it was called on by comparing the
	// page pointer it was handed; instead of that indirection, count
	// simultaneous entries per page identity.
	var mu sync.Mutex
	inFlight := map[playwright.Page]int{}
	var overlaps int64

	visit := func(page playwright.Page, url, title string) (playwright.Page, sectionVisit) {
		mu.Lock()
		inFlight[page]++
		if inFlight[page] > 1 {
			atomic.AddInt64(&overlaps, 1)
		}
		mu.Unlock()

		time.Sleep(2 * time.Millisecond)

		mu.Lock()
		inFlight[page]--
		mu.Unlock()
		return page, sectionVisit{url: url}
	}

	// Distinct non-nil page identities per worker, so "same page" is
	// meaningful. identityPage is a minimal stand-in; only its identity matters.
	pages := make([]playwright.Page, 6)
	for i := range pages {
		pages[i] = &identityPage{id: i}
	}
	pool := newTestPool(len(pages), visit)
	pool.pages = pages[:1]
	next := 1
	pool.openPage = func() (playwright.Page, error) {
		p := pages[next]
		next++
		return p, nil
	}

	pool.visitAll(level, func(i int) string { return "" })

	if got := atomic.LoadInt64(&overlaps); got != 0 {
		t.Fatalf("%d concurrent uses of a single tab; each one is a page being driven by two goroutines", got)
	}
}

// A pool that cannot open extra tabs must still visit everything, just with
// fewer workers. Degrading to slower is fine; degrading to incomplete is not.
func TestSectionPoolFallsBackToOneTabWhenNoneCanBeOpened(t *testing.T) {
	level := []string{"a", "b", "c", "d", "e"}
	var visits int64
	visit := func(page playwright.Page, url, title string) (playwright.Page, sectionVisit) {
		atomic.AddInt64(&visits, 1)
		return page, sectionVisit{url: url}
	}

	pool := newTestPool(4, visit)
	pool.openPage = func() (playwright.Page, error) { return nil, errNoBrowserContext }

	results := pool.visitAll(level, func(i int) string { return "" })
	if int(atomic.LoadInt64(&visits)) != len(level) {
		t.Fatalf("visited %d of %d sections when no extra tab could be opened", visits, len(level))
	}
	for i, r := range results {
		if r.url != level[i] {
			t.Fatalf("result %d is %q, want %q", i, r.url, level[i])
		}
	}
}

// Section concurrency has an off switch, and it has to be a real one: at 1 the
// crawl must stay on the caller's single tab and open nothing. That is the
// escape hatch if this axis ever turns out to lose files the way course
// concurrency 4 did, and the baseline the comparison runs measure against.
func TestSectionConcurrencyOfOneOpensNoExtraTabs(t *testing.T) {
	var opened int64
	visit := func(page playwright.Page, url, title string) (playwright.Page, sectionVisit) {
		return page, sectionVisit{url: url}
	}
	pool := newTestPool(1, visit)
	pool.openPage = func() (playwright.Page, error) {
		atomic.AddInt64(&opened, 1)
		return nil, nil
	}

	pool.visitAll([]string{"a", "b", "c"}, func(i int) string { return "" })
	if opened != 0 {
		t.Fatalf("opened %d extra tab(s) at section concurrency 1", opened)
	}
}

// The stability polls buy extra patience only while more than one tab may be
// rendering. That gate asked about course concurrency alone, which was right
// until section concurrency existed and then became a silent trap: four
// section tabs at course concurrency 1 render concurrently while the gate
// calls the crawl serial, so it would take the impatient budget under exactly
// the load the patient one exists for. Losing the tail of a paginated section
// is what that looks like from the outside - no error, just fewer files.
func TestConcurrentCrawlDetectionConsidersBothAxes(t *testing.T) {
	cases := []struct {
		name           string
		course, sectio int
		want           bool
	}{
		{"both serial", 1, 1, false},
		{"courses concurrent", 2, 1, true},
		{"sections concurrent", 1, 4, true},
		{"both concurrent", 2, 4, true},
	}
	for _, tc := range cases {
		s := &OpalScraper{}
		s.SetCourseConcurrency(tc.course)
		s.SetSectionConcurrency(tc.sectio)
		if got := s.crawlingConcurrently(); got != tc.want {
			t.Fatalf("%s (course=%d section=%d): crawlingConcurrently()=%v, want %v",
				tc.name, tc.course, tc.sectio, got, tc.want)
		}
	}
}

func TestEffectiveSectionConcurrencyDefaultsAndFloor(t *testing.T) {
	s := &OpalScraper{}
	if got := s.effectiveSectionConcurrency(); got < 1 {
		t.Fatalf("unset section concurrency resolved to %d", got)
	}
	s.SetSectionConcurrency(1)
	if got := s.effectiveSectionConcurrency(); got != 1 {
		t.Fatalf("explicit 1 resolved to %d; the off switch must be honoured", got)
	}
	s.SetSectionConcurrency(-3)
	if got := s.effectiveSectionConcurrency(); got < 1 {
		t.Fatalf("negative section concurrency resolved to %d", got)
	}
}

// identityPage is a playwright.Page that exists only to have a distinct identity.
type identityPage struct {
	playwright.Page
	id int
}
