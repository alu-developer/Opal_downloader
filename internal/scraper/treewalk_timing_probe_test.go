package scraper

// Tree-walk timing probe for the sync-speed campaign's last speed lever.
// The question it answers: do a section's FOLDER-NAV links (the ones the BFS
// tree-walk descends into) appear early in the DOM, before the file table has
// finished rendering? If yes, a navigation-only visit can use a much shorter
// wait than the ~300ms settle that exists to render the file table - and the
// serial-hybrid (option A) gets its real speedup: browser walks the tree fast,
// HTTP fetches the leaf file tables.
//
// The campaign already measured that the whole page finishes structurally in
// ~36ms, and that lowering the file-table debounce loses files (150ms -> 322/
// 345). This probe separates the two: it asks whether the TREE part is stable
// at ~50ms even when the file table is not - a question that was never asked.
//
// Usage:
//   OPAL_TREEWALK_URL=<section url with subfolders> \
//     go test ./internal/scraper/ -run TestTreeWalkTimingProbe -v -timeout 5m

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

func TestTreeWalkTimingProbe(t *testing.T) {
	url := os.Getenv("OPAL_TREEWALK_URL")
	if url == "" {
		t.Skip("set OPAL_TREEWALK_URL=<section url with subfolders>")
	}
	beginLiveProbe(t)

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	EnsurePlaywrightBrowsersPath()
	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("playwright: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer browser.Close()

	ctxOpts := playwright.BrowserNewContextOptions{}
	if _, statErr := os.Stat(loaded.Credentials.StateFile); statErr == nil {
		ctxOpts.StorageStatePath = playwright.String(loaded.Credentials.StateFile)
	}
	bctx, err := browser.NewContext(ctxOpts)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	page, err := bctx.NewPage()
	if err != nil {
		t.Fatalf("page: %v", err)
	}

	// Navigation with domcontentloaded (matches visitSection), then snapshot
	// the DOM at several delays and count folder-nav links vs file links at
	// each. The injection reuses the same predicates the crawl uses
	// (looksLikeSectionFolderLink / looksLikeFileLink) so the counts reflect
	// what the real tree-walk vs leaf-extraction would see.
	if _, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatalf("goto: %v", err)
	}

	countAt := func(delayMs float64) (folders, files int) {
		if delayMs > 0 {
			page.WaitForTimeout(delayMs)
		}
		// Pull all <a> candidates via the same JS the crawler uses, then
		// classify each through the Go predicates (exact crawl logic).
		val, evalErr := page.Evaluate(`() => {
			const out = [];
			for (const el of document.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
				out.push({
					href: el.getAttribute('href') || '',
					onclick: el.getAttribute('onclick') || '',
					dataHref: el.getAttribute('data-href') || '',
					dataUrl: el.getAttribute('data-url') || '',
					text: (el.textContent || '').trim(),
					title: el.getAttribute('title') || ''
				});
			}
			return out;
		}`)
		if evalErr != nil {
			t.Fatalf("evaluate at %vms: %v", delayMs, evalErr)
		}
		items := toStringMapSlice(val)
		folderSeen := map[string]bool{}
		fileSeen := map[string]bool{}
		for _, c := range items {
			target := extractLinkTarget(c["href"], c["onclick"], c["dataHref"], c["dataUrl"])
			name := deriveFileName(c["title"], c["text"], target)
			if looksLikeSectionFolderLink(target, name) && !looksLikeFileLink(target, name) {
				folderSeen[target] = true
			}
			if looksLikeFileLink(target, name) {
				fileSeen[name] = true
			}
		}
		return len(folderSeen), len(fileSeen)
	}

	// Repeat N times (fresh load each) to separate noise from signal, since a
	// single page-load timing is not evidence.
	const runs = 3
	delays := []float64{0, 50, 100, 200, 400}
	type sample struct {
		delay          float64
		folders, files int
	}
	var allSamples []sample
	for r := 0; r < runs; r++ {
		if r > 0 {
			if _, err := page.Goto(url, playwright.PageGotoOptions{
				WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			}); err != nil {
				t.Fatalf("goto run %d: %v", r, err)
			}
		}
		for _, d := range delays {
			f, fl := countAt(d)
			allSamples = append(allSamples, sample{d, f, fl})
		}
	}

	// Aggregate: at each delay, report the min/max folders and files across runs.
	byDelay := map[float64][]sample{}
	for _, s := range allSamples {
		byDelay[s.delay] = append(byDelay[s.delay], s)
	}
	var delaysSorted []float64
	for d := range byDelay {
		delaysSorted = append(delaysSorted, d)
	}
	sort.Float64s(delaysSorted)
	t.Logf("tree-walk timing for %s (%d runs each):", url, runs)
	t.Logf("  delay(ms) | folders min-max | files min-max")
	for _, d := range delaysSorted {
		var minF, maxF, minFiles, maxFiles int = -1, 0, -1, 0
		for _, s := range byDelay[d] {
			if minF < 0 || s.folders < minF {
				minF = s.folders
			}
			if s.folders > maxF {
				maxF = s.folders
			}
			if minFiles < 0 || s.files < minFiles {
				minFiles = s.files
			}
			if s.files > maxFiles {
				maxFiles = s.files
			}
		}
		t.Logf("  %9.0f | %8d-%-5d | %6d-%-5d", d, minF, maxF, minFiles, maxFiles)
	}
	t.Logf("interpretation: if folders reach max by ~50ms while files keep growing,")
	t.Logf("a navigation-only short wait is viable (the speedup lever is real).")
}
