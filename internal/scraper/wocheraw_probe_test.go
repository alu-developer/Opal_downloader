package scraper

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// TestWocheRawHTML is the direct follow-up to TestCourseNodeType
// (coursenodetype_probe_test.go): now that Woche 05..13 are known to be
// node-st (Structure) pages, not node-bc folders, does the page's raw HTML
// carry a per-file date anywhere near a known file's link - a possible
// cheaper "option E" for Question 45's Woche cluster? See
// docs/sync-speed-model.md "Next experiment", cycle 2026-09-14 (second cycle
// this run) for the registered prediction.
//
// Deliberately writes only the raw HTML to disk; the grep-by-hand inspection
// happens outside this test, per that cycle's own "no parser yet" design.
//
// Usage:
//
//	OPAL_WOCHE_RAW=1 go test ./internal/scraper/ -run TestWocheRawHTML -count=1 -v -timeout 5m
func TestWocheRawHTML(t *testing.T) {
	if os.Getenv("OPAL_WOCHE_RAW") == "" {
		t.Skip("set OPAL_WOCHE_RAW=1 to fetch the Woche 05 node-st page's raw HTML")
	}
	beginLiveProbe(t)

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Resolved by TestCourseNodeType's live run, 2026-09-14 - So26
	// Programmieren's "Woche 05" node-st page.
	const woche05URL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53722382336/CourseNode/1778121512916852005"

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()
	if serr := sc.ensureSession(false); serr != nil {
		t.Fatalf("ensure session: %v", serr)
	}

	fetch := sc.httpDiscoveryFetcher()
	if fetch == nil {
		t.Fatal("no authenticated request context after ensureSession; cannot fetch page")
	}
	resp, gerr := fetch.Get(woche05URL)
	if gerr != nil {
		t.Fatalf("HTTP GET %s: %v", woche05URL, gerr)
	}
	body, berr := responseBodyText(resp)
	if berr != nil || resp.Status() != 200 {
		t.Fatalf("HTTP read %s: status %d err %v", woche05URL, resp.Status(), berr)
	}

	out := filepath.Join(repo, "tmp", "woche05-raw.html")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	t.Logf("raw HTML (%d bytes) written to %s", len(body), out)
}
