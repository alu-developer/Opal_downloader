package scraper

import (
	"errors"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// effectiveSectionConcurrency resolves s.sectionConcurrency the same way
// effectiveCourseConcurrency resolves its own field: a value <= 0 means "use
// the documented default".
//
// 1 is a real, supported setting and means the crawl runs exactly as it did
// before this existed - one tab, no goroutines, no extra pages opened. That
// is deliberate: it is the escape hatch if section concurrency ever turns out
// to lose files the way course concurrency 4 did, and it is what the
// byte-for-byte comparison runs are measured against.
func (s *OpalScraper) effectiveSectionConcurrency() int {
	concurrency := s.sectionConcurrency
	if concurrency <= 0 {
		concurrency = config.DefaultSectionConcurrency
	}
	if concurrency < 1 {
		concurrency = 1
	}
	return concurrency
}

// crawlingConcurrently reports whether more than one browser tab may be
// rendering OPAL content at the same time.
//
// The stability polls buy extra patience only when this is true (see
// waitForStableSectionContent and waitForStableExpandedCandidates): the extra
// consecutive stable reads exist because competing renders delay each other,
// and paying for them in a genuinely serial crawl roughly doubled wall-clock
// time for nothing.
//
// It has to consider BOTH axes. Asking only about course concurrency was
// correct while that was the only way to get two tabs rendering at once - and
// became a silent trap the moment section concurrency existed: a run at
// course concurrency 1 and section concurrency 4 has four tabs rendering
// simultaneously while claiming to be serial, so it would take the *impatient*
// budget under exactly the load the patient one was written for. That is the
// shape of every file-loss bug in this file's history.
func (s *OpalScraper) crawlingConcurrently() bool {
	return s.effectiveCourseConcurrency() > 1 || s.effectiveSectionConcurrency() > 1
}

// sectionTabPool visits a BFS level's sections concurrently, each on its own
// Playwright tab.
//
// The pool owns every tab except the first: that one belongs to the caller
// (collectCourseFiles' `page` parameter, itself owned by
// newCourseFileCollector) and is never closed here. Extra tabs are opened
// lazily - a course whose levels are all one section wide never opens a second
// tab - and closed together when the crawl ends.
type sectionTabPool struct {
	s       *OpalScraper
	workers int

	// pages[0] is the caller's tab. Any entry may be replaced mid-crawl when
	// visitSection recovers from a browser crash onto a fresh tab.
	pages []playwright.Page

	// visit is s.visitSection in production. It is a field so tests can drive
	// the ordering guarantee below - the property the whole safety argument
	// for section concurrency rests on - without a browser, by making sections
	// finish deliberately out of order.
	visit func(page playwright.Page, url, title string) (playwright.Page, sectionVisit)

	// openPage is ctx.NewPage in production, likewise replaceable in tests.
	openPage func() (playwright.Page, error)

	// stagger is newPageStaggerMs in production. Tests set it to zero so the
	// suite does not spend real seconds imitating a delay whose only purpose is
	// to be real.
	stagger time.Duration
}

func newSectionTabPool(s *OpalScraper, primary playwright.Page, workers int) *sectionTabPool {
	if workers < 1 {
		workers = 1
	}
	p := &sectionTabPool{s: s, workers: workers, pages: []playwright.Page{primary}, stagger: newPageStaggerMs}
	p.visit = s.visitSection
	p.openPage = func() (playwright.Page, error) {
		ctx := s.getContext()
		if ctx == nil {
			return nil, errNoBrowserContext
		}
		page, err := ctx.NewPage()
		if err != nil {
			return nil, err
		}
		s.attachInlinePreviewBlocker(ctx, page)
		return page, nil
	}
	return p
}

var errNoBrowserContext = errors.New("no authenticated browser context available")

// primary returns the caller's tab, which crash recovery may have replaced.
func (p *sectionTabPool) primary() playwright.Page {
	if len(p.pages) == 0 {
		return nil
	}
	return p.pages[0]
}

// ensure opens tabs up to n, returning how many are actually available.
//
// Tab creation goes through the same s.newPageMu + stagger as
// newCourseFileCollector's, and for the same live-confirmed reason: a burst of
// tabs opening at the same instant measurably worsens the AJAX-render race
// even with a generous stability-poll budget. Section concurrency creates
// exactly such a burst at the first wide level, so it has to observe the same
// discipline rather than assume the problem was course-specific.
//
// Failing to open a tab is not fatal. The level is simply visited with fewer
// workers - slower, never less complete.
func (p *sectionTabPool) ensure(n int) int {
	if n > p.workers {
		n = p.workers
	}
	for len(p.pages) < n {
		p.s.newPageMu.Lock()
		page, err := p.openPage()
		if err == nil {
			time.Sleep(p.stagger)
		}
		p.s.newPageMu.Unlock()
		if err != nil {
			break
		}
		p.pages = append(p.pages, page)
	}
	return len(p.pages)
}

// closeExtras closes every tab the pool opened, leaving the caller's alone.
func (p *sectionTabPool) closeExtras() {
	for i := 1; i < len(p.pages); i++ {
		if p.pages[i] != nil {
			_ = p.pages[i].Close()
		}
	}
	if len(p.pages) > 1 {
		p.pages = p.pages[:1]
	}
}

// visitAll visits every URL in the level and returns the results in the SAME
// ORDER as the input, whatever order they finished in.
//
// That ordering guarantee is the point. The caller merges these results into
// the crawl's shared state serially, so a concurrent crawl produces the same
// files, in the same order, with the same dedupe outcomes, as a serial one.
// titleFor supplies each section's known title for the warning messages.
func (p *sectionTabPool) visitAll(level []string, titleFor func(i int) string) []sectionVisit {
	results := make([]sectionVisit, len(level))
	if len(level) == 0 {
		return results
	}

	// One section, or concurrency switched off: stay on the caller's tab and
	// skip the goroutine entirely, so the serial path is genuinely the old
	// code path rather than a one-worker emulation of the new one.
	workers := p.ensure(min(len(level), p.workers))
	if workers <= 1 || len(level) == 1 {
		for i, url := range level {
			page, visit := p.visit(p.pages[0], url, titleFor(i))
			p.pages[0] = page
			results[i] = visit
		}
		return results
	}

	// Static round-robin assignment rather than a shared index: each worker
	// owns one tab for the whole level, so no tab is ever driven by two
	// goroutines at once - which Playwright pages do not tolerate.
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := worker; i < len(level); i += workers {
				page, visit := p.visit(p.pages[worker], level[i], titleFor(i))
				p.pages[worker] = page
				results[i] = visit
			}
		}(w)
	}
	wg.Wait()
	return results
}
