package gui

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/mxschmitt/playwright-go"
)

// Opt-in, like the other browser-driven tests here: a fresh clone has no
// browsers installed. This one exists because assertions cannot see a page.
// The last GUI bug found here was white text on a near-white button - every
// assertion in the package passed, because the markup was never wrong. The
// only way to catch that class is to render the page and look at the image.
//
// Usage: OPAL_GUI_SCREENSHOT=1 go test ./internal/gui/ -run TestScreenshotLogsPage -v
func TestScreenshotLogsPage(t *testing.T) {
	if os.Getenv("OPAL_GUI_SCREENSHOT") == "" {
		t.Skip("set OPAL_GUI_SCREENSHOT=1 to render the logs page to an image")
	}

	stubLogPage(t, "time=2026-07-27T09:12:03Z level=INFO msg=\"Checking 345 files\"\n"+
		"time=2026-07-27T09:12:44Z level=WARN msg=\"Skipping section \\\"Einschreibung\\\"\"\n"+
		"time=2026-07-27T09:13:01Z level=ERROR msg=\"download failed: context deadline exceeded\"\n")

	srv := &server{}
	ts := httptest.NewServer(newMux(srv, filepath.Join(t.TempDir(), "config.yaml")))
	defer ts.Close()

	scraper.EnsurePlaywrightBrowsersPath()
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

	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}
	if _, err := page.Goto(ts.URL+"/logs", playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateLoad}); err != nil {
		t.Fatalf("goto /logs: %v", err)
	}

	out := filepath.Join("..", "..", "tmp", "logs-page.png")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := page.Screenshot(playwright.PageScreenshotOptions{
		Path:     playwright.String(out),
		FullPage: playwright.Bool(true),
	}); err != nil {
		t.Fatalf("screenshot: %v", err)
	}
	t.Logf("wrote %s", out)
}
