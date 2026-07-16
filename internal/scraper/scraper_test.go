package scraper

import (
	"sync"
	"testing"
	"time"
)

// TestCloseIsSafeDuringConcurrentScrapeFieldAccess exercises the exact race
// found in PR #22 review: the GUI's /sync/cancel handler calls sc.Close()
// from the HTTP-handler goroutine while runJob's goroutine is still mid-scrape,
// reading/writing the same pw/browser/context/page fields via the get*/set*
// accessors on OpalScraper (see the fieldMu doc comment on OpalScraper in
// scraper.go). This doesn't spin up a real browser - Playwright isn't
// available/needed to prove the fields themselves are race-free - it drives
// many goroutines hammering the getters/setters (standing in for a long-running
// crawl/discovery/download loop that repeatedly touches s.page/s.context) while
// another goroutine repeatedly calls Close() concurrently (standing in for
// /sync/cancel), the way job.cancel's cancelFn does in internal/gui/sync.go.
//
// Run with `go test -race ./internal/scraper/...` to have the race detector
// confirm there is no unsynchronized read/write of the guarded fields, and a
// clean, panic-free shutdown even under heavy concurrent Close() calls.
func TestCloseIsSafeDuringConcurrentScrapeFieldAccess(t *testing.T) {
	s := New("", "")

	const scrapeWorkers = 8
	const closers = 4
	const iterations = 200

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Simulate a long-running scrape/crawl/download loop repeatedly reading
	// and writing s.page/s.context/s.browser/s.pw, the way discovery.go,
	// crawl.go, download.go and session.go do throughout a real scrape.
	for i := 0; i < scrapeWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = s.getPage()
				_ = s.getContext()
				_ = s.getBrowser()
				_ = s.getPw()
				if j%7 == 0 {
					// trackActivePage's ctx.OnPage callback (session.go) and
					// launchBrowser both reassign these fields mid-flight;
					// mimic that here.
					s.setPage(nil)
					s.setContext(nil)
					s.setBrowser(nil)
					s.setPw(nil)
				}
			}
		}()
	}

	// Simulate /sync/cancel calling sc.Close() concurrently and repeatedly -
	// job.cancel() in internal/gui/job.go invokes the cancelFn (which calls
	// sc.Close()) with no coordination against the in-flight scrape goroutine.
	var closeWG sync.WaitGroup
	for i := 0; i < closers; i++ {
		closeWG.Add(1)
		go func() {
			defer closeWG.Done()
			for j := 0; j < iterations; j++ {
				if err := s.Close(); err != nil {
					t.Errorf("Close() returned error during concurrent access: %v", err)
				}
			}
		}()
	}

	closeWG.Wait()
	close(stop)
	wg.Wait()

	// A final Close() after everything has quiesced must still be safe
	// (idempotent) and must not panic or error.
	if err := s.Close(); err != nil {
		t.Fatalf("final Close() after concurrent access returned error: %v", err)
	}
}

// TestCloseDuringBlockingOperationReturnsPromptly exercises the specific
// cancel-mid-scrape scenario from the PR #22 review: a goroutine holds onto a
// page/context reference (as a scrape method does for its whole duration) and
// keeps "using" it while another goroutine calls Close(). Close() must return
// promptly (it must not block waiting for the scrape to finish) and must not
// race with the scrape goroutine's field access done via the accessors.
func TestCloseDuringBlockingOperationReturnsPromptly(t *testing.T) {
	s := New("", "")

	scraping := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		close(scraping)
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			_ = s.getPage()
			_ = s.getContext()
		}
	}()

	<-scraping
	closeDone := make(chan error, 1)
	go func() {
		closeDone <- s.Close()
	}()

	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("Close() returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return promptly while a scrape-like goroutine was still accessing fields")
	}

	<-done
}

// TestVisitRecordsAccumulatesAndReturnsCopy exercises the in-memory
// recording side of the visit-effectiveness log (internal/visitlog):
// recordSectionVisit is called from collectCourseFiles (crawl.go) once per
// successfully visited section, and VisitRecords is what
// cmd/opal-downloader/root.go's persistVisitLog reads afterward to append
// into the persistent cross-run log file. This only exercises the
// accumulate/copy behavior in isolation (no Playwright/browser needed) -
// the persistence-across-runs behavior itself is covered by
// internal/visitlog's own tests.
func TestVisitRecordsAccumulatesAndReturnsCopy(t *testing.T) {
	s := New("", "")

	if got := s.VisitRecords(); len(got) != 0 {
		t.Fatalf("expected no visit records on a fresh scraper, got %d", len(got))
	}

	s.recordSectionVisit("Analysis", "Übungen", "https://opal/1", 3)
	s.recordSectionVisit("Analysis", "Forum", "https://opal/2", 0)

	records := s.VisitRecords()
	if len(records) != 2 {
		t.Fatalf("expected 2 recorded visits, got %d: %#v", len(records), records)
	}
	if records[0].Course != "Analysis" || records[0].SectionTitle != "Übungen" || records[0].SectionURL != "https://opal/1" || records[0].FilesFound != 3 {
		t.Fatalf("unexpected first record: %#v", records[0])
	}
	if records[1].FilesFound != 0 {
		t.Fatalf("expected second record to have FilesFound=0, got %#v", records[1])
	}

	// VisitRecords must return an independent copy - mutating the returned
	// slice must not corrupt the scraper's own accumulated state, since
	// callers (persistVisitLog) own the slice they get back.
	records[0].Course = "mutated"
	if again := s.VisitRecords(); again[0].Course != "Analysis" {
		t.Fatalf("expected VisitRecords to return a copy, but internal state was mutated: %#v", again[0])
	}
}

// TestRecordSectionVisitConcurrentSafe mirrors
// TestCloseIsSafeDuringConcurrentScrapeFieldAccess above: collectCourseFiles
// runs concurrently across courses (see collectCourseFilesConcurrently in
// orchestrator.go, each course on its own goroutine), so
// recordSectionVisit/VisitRecords must be safe to call from many goroutines
// at once. Run with `go test -race` to have the race detector confirm this.
func TestRecordSectionVisitConcurrentSafe(t *testing.T) {
	s := New("", "")

	const workers = 8
	const perWorker = 50
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				s.recordSectionVisit("Course", "Section", "https://opal/section", j%3)
				_ = s.VisitRecords()
			}
		}(i)
	}
	wg.Wait()

	if got := len(s.VisitRecords()); got != workers*perWorker {
		t.Fatalf("expected %d accumulated visit records, got %d", workers*perWorker, got)
	}
}
