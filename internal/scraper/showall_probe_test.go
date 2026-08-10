package scraper

// Show-all AJAX probe for the sync-speed campaign. The HTTP probe found that
// paginated sections lose files over plain HTTP (the first page caps at ~20
// rows). Part-3's raw HTML wires its "show all" control as a Wicket AJAX
// callback to a plain GET URL: `Wicket.Ajax.ajax({"u":"<section>?<path>-pager-showAllLink"})`.
// The browser's "click" just fires that GET. This probe tests whether fetching
// that URL over plain HTTP returns the FULL file table — which would mean
// discovery needs NO browser at all, not even for pager sections.
//
// Usage:
//   OPAL_SHOWALL_URL=<full show-all AJAX url> \
//     OPAL_SHOWALL_DUMP=<path> \
//     go test ./internal/scraper/ -run TestShowAllProbe -v -timeout 5m

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

func TestShowAllProbe(t *testing.T) {
	showAllURL := os.Getenv("OPAL_SHOWALL_URL")
	if showAllURL == "" {
		t.Skip("set OPAL_SHOWALL_URL=<full pager-showAllLink AJAX url>")
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
	reqCtx := bctx.Request()

	resp, err := reqCtx.Get(showAllURL)
	if err != nil {
		t.Fatalf("GET show-all: %v", err)
	}
	body, _ := resp.Text()
	t.Logf("show-all response: status %d, %d bytes", resp.Status(), len(body))

	if dump := os.Getenv("OPAL_SHOWALL_DUMP"); dump != "" {
		_ = os.WriteFile(dump, []byte(body), 0o644)
		t.Logf("dumped to %s", dump)
	}

	// Count files in the show-all response using the same predicate as the
	// HTTP probe, so the comparison is apples-to-apples.
	names := fileLinksInHTML(body)
	distinct := map[string]bool{}
	for _, n := range names {
		distinct[n] = true
	}
	sorted := make([]string, 0, len(distinct))
	for n := range distinct {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	// Also count raw data-file-name attrs (the authoritative file rows).
	dataFileNames := dataFileNameRe.FindAllStringSubmatch(body, -1)

	t.Logf("show-all files via looksLikeFileLink: %d (%d distinct)", len(names), len(distinct))
	t.Logf("show-all data-file-name attrs      : %d", len(dataFileNames))
	t.Logf("(Part-3 ground truth total per its HTML: 57 Eintraege)")
}
