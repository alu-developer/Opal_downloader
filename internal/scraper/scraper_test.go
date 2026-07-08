package scraper

import (
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNormalizePersistentProfileSettings(t *testing.T) {
	tests := []struct {
		name         string
		userDataDir  string
		profileDir   string
		wantUserData string
		wantProfile  string
	}{
		{
			name:         "explicit profile directory wins",
			userDataDir:  "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data",
			profileDir:   "Profile 2",
			wantUserData: filepath.Clean("C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"),
			wantProfile:  "Profile 2",
		},
		{
			name:         "infer default profile from full profile path",
			userDataDir:  "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data/Default",
			wantUserData: filepath.Clean("C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"),
			wantProfile:  "Default",
		},
		{
			name:         "infer numbered profile from full profile path",
			userDataDir:  "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data/Profile 1",
			wantUserData: filepath.Clean("C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"),
			wantProfile:  "Profile 1",
		},
		{
			name:         "leave plain user data dir unchanged",
			userDataDir:  "C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data",
			wantUserData: filepath.Clean("C:/Users/test/AppData/Local/BraveSoftware/Brave-Browser/User Data"),
			wantProfile:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotUserData, gotProfile := normalizePersistentProfileSettings(tt.userDataDir, tt.profileDir)
			if filepath.Clean(gotUserData) != filepath.Clean(tt.wantUserData) || gotProfile != tt.wantProfile {
				t.Fatalf("normalizePersistentProfileSettings(%q, %q) = (%q, %q), want (%q, %q)", tt.userDataDir, tt.profileDir, gotUserData, gotProfile, tt.wantUserData, tt.wantProfile)
			}
		})
	}
}

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
	s := New("", "", "", "", "")

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
	s := New("", "", "", "", "")

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
