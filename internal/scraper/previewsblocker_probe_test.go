package scraper

// docs/sync-speed-model.md, Question 23: local, no-OPAL-account correctness
// check for the raw-CDP rewrite of blockInlineFilePreviews
// (attachInlinePreviewBlocker, previews.go). isDiscardablePreview's own unit
// test (previews_test.go) already covers the decision logic exhaustively
// without a browser; what that cannot cover is whether the raw CDP wiring
// around it - Page.getFrameTree for the root frame ID, Fetch.enable with a
// real pattern, the goroutine-wrapped Fetch.continueRequest/failRequest -
// actually behaves the same way against a real Chromium instance that the
// old ctx.Route version did.
//
// Two navigations against a local httptest server, same page:
//  1. A subframe (iframe) load of a /FolderResource/ URL - must be blocked,
//     the server must never see the request.
//  2. A main-frame navigation to a /FolderResource/ URL (what
//     downloadFileViaBrowser, download.go, does as a last resort) - must NOT
//     be blocked, the server must see it and the page must show its body.
//
// This is a correctness probe, not a timing one - Question 8's synthetic
// probe (ctxroutecost_probe_test.go) already measured the raw-CDP tax in
// isolation. The real-account before/after byte-diff
// (filelist_probe_test.go, OPAL_FILELIST) is still the shipping safety bar;
// this only rules out the raw-CDP wiring itself being broken before that
// more expensive run is spent.
//
// Usage:
//
//	OPAL_PREVIEW_BLOCK_PROBE=1 go test ./internal/scraper/ -run TestInlinePreviewBlockerAgainstARealBrowser -v
import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

func TestInlinePreviewBlockerAgainstARealBrowser(t *testing.T) {
	if os.Getenv("OPAL_PREVIEW_BLOCK_PROBE") == "" {
		t.Skip("set OPAL_PREVIEW_BLOCK_PROBE=1 to run the real-browser inline-preview-blocker probe")
	}

	var mu sync.Mutex
	hits := map[string]int{}
	record := func(path string) {
		mu.Lock()
		defer mu.Unlock()
		hits[path]++
	}
	hitCount := func(path string) int {
		mu.Lock()
		defer mu.Unlock()
		return hits[path]
	}

	const previewPath = "/opal/FolderResource/1/inline-preview.pdf"
	const downloadPath = "/opal/FolderResource/2/real-download.pdf"

	mux := http.NewServeMux()
	mux.HandleFunc("/main", func(w http.ResponseWriter, _ *http.Request) {
		record("/main")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><body>
			<p id="marker">main page loaded</p>
			<iframe src="%s"></iframe>
		</body></html>`, previewPath)
	})
	mux.HandleFunc(previewPath, func(w http.ResponseWriter, _ *http.Request) {
		record(previewPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body>this is the inline preview - nothing should ever read it</body></html>`))
	})
	mux.HandleFunc(downloadPath, func(w http.ResponseWriter, _ *http.Request) {
		record(downloadPath)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><p id="marker">download page loaded</p></body></html>`))
	})
	ts := httptest.NewServer(mux)
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

	bctx, err := browser.NewContext()
	if err != nil {
		t.Fatalf("new context: %v", err)
	}
	defer bctx.Close()

	page, err := bctx.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	defer page.Close()

	if err := os.Setenv(blockPreviewsEnv, "1"); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	defer func() { _ = os.Unsetenv(blockPreviewsEnv) }()

	s := New(ts.URL+"/opal/", "")
	s.attachInlinePreviewBlocker(bctx, page)

	t.Run("a subframe FolderResource load is blocked", func(t *testing.T) {
		if _, err := page.Goto(ts.URL+"/main", playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateLoad,
		}); err != nil {
			t.Fatalf("goto /main: %v", err)
		}
		// The iframe's failed/blocked load races the top page's own "load"
		// event depending on ordering; give the (failing) subframe request a
		// moment to have definitely been dispatched and refused before
		// asserting the server never saw it.
		time.Sleep(300 * time.Millisecond)

		if n := hitCount(previewPath); n != 0 {
			t.Errorf("server saw %d request(s) for the subframe preview, want 0 (must be blocked before it leaves the browser)", n)
		}
		if n := hitCount("/main"); n != 1 {
			t.Errorf("main page itself should have loaded once, got %d", n)
		}
		if blocked := s.previewsBlocked; blocked != 1 {
			t.Errorf("previewsBlocked = %d, want exactly 1", blocked)
		}
	})

	t.Run("a main-frame FolderResource navigation is NOT blocked", func(t *testing.T) {
		if _, err := page.Goto(ts.URL+downloadPath, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateLoad,
		}); err != nil {
			t.Fatalf("goto %s: %v", downloadPath, err)
		}
		if n := hitCount(downloadPath); n != 1 {
			t.Errorf("server saw %d request(s) for the main-frame download URL, want exactly 1 (must NOT be blocked - this is downloadFileViaBrowser's path)", n)
		}
		text, err := page.Locator("#marker").TextContent()
		if err != nil {
			t.Fatalf("read marker: %v", err)
		}
		if text != "download page loaded" {
			t.Errorf("main frame did not actually navigate to the download page, got marker text %q", text)
		}
		// previewsBlocked must still read exactly 1 - the main-frame
		// navigation must not have incremented it.
		if blocked := s.previewsBlocked; blocked != 1 {
			t.Errorf("previewsBlocked = %d after the main-frame navigation, want unchanged at 1", blocked)
		}
	})
}
