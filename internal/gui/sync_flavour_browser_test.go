package gui

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"

	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// The two bits of flavour on the sync page (maintainer's request, 2026-08-03:
// "so paar kleine sachen, die einfach bissl aufpeppen") are still UI, and
// decoration that quietly breaks is worse than no decoration - a stuck quip
// line or a summary that stops rendering because a helper threw would both
// look like the app itself is broken. So they get the same real-browser
// treatment as the rest of the page.
//
// This drives the job directly rather than running a sync: publish is the
// same seam TestBrowserSyncProgressWalk uses, and it means the test needs no
// OPAL account, no network, and no three minutes.
func TestBrowserSyncFlavourWalk(t *testing.T) {
	if os.Getenv("OPAL_GUI_BROWSER_WALK") == "" {
		t.Skip("set OPAL_GUI_BROWSER_WALK=1 to run the browser walk (needs Playwright browsers)")
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	yaml := "download_path: " + filepath.ToSlash(filepath.Join(dir, "downloads")) + "\n" +
		"courses:\n  - Analysis I\nsync: true\n" +
		"opal_url: https://bildungsportal.sachsen.de/opal/\n" +
		"session_state_file: " + filepath.ToSlash(filepath.Join(dir, "state.json")) + "\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	srv := &server{configPath: configPath, buildVersion: "test", feedback: &feedbackState{}}
	ts := httptest.NewServer(newMux(srv, configPath))
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
	var pageErrors []string
	page.On("pageerror", func(err error) { pageErrors = append(pageErrors, err.Error()) })

	// 40ms instead of the shipped 9s, so the rotation assertion below costs a
	// second rather than a minute and a half. Same escape hatch the page
	// already provides for the stall detector's threshold.
	//
	// document.hasFocus is stubbed because the finished-run mark in the tab
	// title is deliberately conditional on *not* looking at the page: a
	// headless page reports itself focused, which would take the restore path
	// immediately and leave nothing to assert. Stubbing it is the only way to
	// exercise the branch a real backgrounded tab takes.
	if err := page.AddInitScript(playwright.Script{
		Content: playwright.String(`window.OPAL_QUIP_EVERY_MS = 40;
			document.hasFocus = function () { return false; };`),
	}); err != nil {
		t.Fatalf("add init script: %v", err)
	}

	if _, err := page.Goto(ts.URL+"/sync", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
	}); err != nil {
		t.Fatalf("goto sync: %v", err)
	}
	if _, err := page.WaitForFunction(`document.getElementById('status').textContent === 'Idle.'`, nil); err != nil {
		t.Fatalf("sync page never settled to idle: %v", err)
	}

	// Idle is the resting state, and it must be quiet. A quip sitting there
	// before anything has started would read as a status line about a run
	// that is not happening.
	if quip, _ := page.Locator("#quip").TextContent(); strings.TrimSpace(quip) != "" {
		t.Fatalf("the flavour line is showing while idle: %q", quip)
	}

	// Same for the tab: an idle page must look like an idle page from the tab
	// strip too, which is also the state the page has to return to later.
	const baseTitle = "Opal Downloader - Sync"
	if title, _ := page.Title(); title != baseTitle {
		t.Fatalf("idle tab title = %q, want %q", title, baseTitle)
	}
	if href := faviconHref(t, page); href != "/logo.svg" {
		t.Fatalf("idle favicon = %q, want the shipped logo", href)
	}

	// --- a run starts: flavour appears, status stays the status --------------
	// Discovery first, because that is the order a real run goes in and the
	// half where a user actually walks away: it reads every section of every
	// course before a single file is fetched.
	srv.syncJob.start(jobKindSync, func() {})
	srv.syncJob.publish(jobEvent{Kind: "discovery", Course: "Analysis I",
		Message: "Scanning course 3 of 6: Analysis I", CourseIndex: 3, TotalCourses: 6})

	if _, err := page.WaitForFunction(
		`document.getElementById('quip').textContent.trim().length > 0`, nil,
	); err != nil {
		t.Fatalf("no flavour line once a run was in flight: %v", err)
	}

	// The rule the quips are allowed to exist under: they never touch
	// #status. That line is the stall detector's only evidence, and a
	// rotating message there would make a hung run look busy.
	status, err := page.Locator("#status").TextContent()
	if err != nil {
		t.Fatalf("read status: %v", err)
	}
	if !strings.Contains(status, "Analysis I") {
		t.Fatalf("the flavour line displaced the real status: %q", status)
	}

	// --- the tab carries the run for someone who walked away -----------------
	// The point of the title and the favicon is that they are the only parts
	// of this page visible from another tab, so they have to say the same
	// thing #status does, and go back to normal afterwards.
	if _, err := page.WaitForFunction(
		`document.title.indexOf('(3/6)') === 0`, nil,
	); err != nil {
		title, _ := page.Title()
		t.Fatalf("the tab title never picked up discovery progress (%q): %v", title, err)
	}
	// "Working", not "Syncing": this page connected while nothing was running
	// and the job was started behind it, so it has never been told the kind.
	// Claiming a sync here could be claiming a download that never happens.
	title, _ := page.Title()
	if !strings.Contains(title, "Working") || !strings.Contains(title, baseTitle) {
		t.Errorf("tab title = %q, want the progress prefixed onto %q", title, baseTitle)
	}
	if href := faviconHref(t, page); !strings.HasPrefix(href, "data:image/png") {
		t.Errorf("the favicon did not become a progress ring during the run: %.40q", href)
	}

	// The download phase counts its own 1..N over the same courses. The title
	// follows it, exactly as #status does.
	srv.syncJob.publish(jobEvent{Kind: "course_started", Course: "Analysis I", CourseIndex: 1, TotalCourses: 6})
	if _, err := page.WaitForFunction(
		`document.title.indexOf('(1/6)') === 0`, nil,
	); err != nil {
		title, _ := page.Title()
		t.Fatalf("the tab title never followed the download phase (%q): %v", title, err)
	}

	// Rotation must actually rotate, and it must not repeat a line while
	// others are unused - drawing independently at random would show the
	// same line twice in a row often enough to look stuck, which is the
	// specific failure this shuffle exists to prevent.
	//
	// Sampled over a window of 8 ticks at 40ms; the pool is 10, so every
	// sample in that window must be distinct.
	if _, err := page.AddScriptTag(playwright.PageAddScriptTagOptions{
		Content: playwright.String(`
			window.__opalQuipSeen = [];
			setInterval(function () {
				var text = document.getElementById('quip').textContent;
				var seen = window.__opalQuipSeen;
				if (text && seen[seen.length - 1] !== text) { seen.push(text); }
			}, 5);
		`),
	}); err != nil {
		t.Fatalf("add sampler: %v", err)
	}
	if _, err := page.WaitForFunction(`window.__opalQuipSeen.length >= 8`, nil); err != nil {
		samples, _ := page.Evaluate(`window.__opalQuipSeen`)
		t.Fatalf("the flavour line never rotated (samples: %v): %v", samples, err)
	}
	samples, err := page.Evaluate(`window.__opalQuipSeen.slice(0, 8)`)
	if err != nil {
		t.Fatalf("read samples: %v", err)
	}
	seen := map[string]bool{}
	for _, s := range samples.([]any) {
		text, _ := s.(string)
		if seen[text] {
			t.Errorf("a flavour line repeated while others were unused: %q in %v", text, samples)
			break
		}
		seen[text] = true
	}

	// --- the Konami code swaps the pool --------------------------------------
	// Real keypresses, not dispatchEvent: synthetic events skip the browser's
	// input path entirely, which is exactly how the landing page's egg was
	// once declared working while being broken (see konami_browser_test.go).
	for _, key := range []string{"ArrowUp", "ArrowUp", "ArrowDown", "ArrowDown",
		"ArrowLeft", "ArrowRight", "ArrowLeft", "ArrowRight", "b", "a"} {
		if err := page.Keyboard().Press(key); err != nil {
			t.Fatalf("press %s: %v", key, err)
		}
	}
	if _, err := page.WaitForFunction(
		`document.getElementById('quip').dataset.konami === '1'`, nil,
	); err != nil {
		t.Fatalf("the Konami code did not reach the sync page: %v", err)
	}

	// The flag alone would pass even if the pool never actually changed, so
	// this waits for a line only the unlocked pool contains. Duplicated from
	// KONAMI_QUIPS in sync.go on purpose: it is the one string that proves
	// which pool is on screen, so it is worth having to change in two places.
	if _, err := page.WaitForFunction(
		`window.__opalQuipSeen.indexOf('Downloading harder.') !== -1`, nil,
	); err != nil {
		samples, _ := page.Evaluate(`window.__opalQuipSeen`)
		t.Errorf("the unlocked pool never showed up (samples: %v): %v", samples, err)
	}

	// --- the run ends: flavour stops, and the summary carries the reward -----
	srv.syncJob.publish(jobEvent{Kind: "done", Downloaded: 12, Skipped: 333, Errors: 0,
		Message: "Done. 12 downloaded, 333 already up to date, 0 failed."})
	srv.syncJob.finish()

	if _, err := page.WaitForFunction(
		`document.getElementById('summary').textContent.length > 0`, nil,
	); err != nil {
		t.Fatalf("no summary after the run finished: %v", err)
	}

	if _, err := page.WaitForFunction(
		`document.getElementById('quip').textContent.trim().length === 0`, nil,
	); err != nil {
		quip, _ := page.Locator("#quip").TextContent()
		t.Fatalf("the flavour line kept going after the run ended (%q): %v", quip, err)
	}

	summary, err := page.Locator("#summary").TextContent()
	if err != nil {
		t.Fatalf("read summary: %v", err)
	}
	// 345 files checked * 4 clicks = 1380, rounded to the nearest hundred.
	if !strings.Contains(summary, "1,400 clicks you did not make") {
		t.Errorf("summary is missing the clicks-spared tail, or miscounted it: %q", summary)
	}
	// It is a tail, not a replacement: the real numbers must still be there.
	if !strings.Contains(summary, "12 new files downloaded") || !strings.Contains(summary, "333 already up to date") {
		t.Errorf("the flavour tail displaced the actual result: %q", summary)
	}

	// --- and the tab goes back to being a tab --------------------------------
	// A finished run must not leave "(1/1) Syncing" sitting in the tab strip:
	// that is the failure mode this is worth testing for, because it looks
	// exactly like a run that never ended.
	if _, err := page.WaitForFunction(
		`document.title.indexOf('Done') !== -1`, nil,
	); err != nil {
		title, _ := page.Title()
		t.Fatalf("the tab title did not report the outcome (%q): %v", title, err)
	}
	if title, _ := page.Title(); strings.Contains(title, "(1/1)") {
		t.Errorf("the finished tab title still carries live progress: %q", title)
	}
	if href := faviconHref(t, page); href != "/logo.svg" {
		t.Errorf("the progress ring outlived the run: %.40q", href)
	}

	// Coming back to the page is what the mark was waiting for, so looking at
	// it clears it.
	if _, err := page.Evaluate(`window.dispatchEvent(new Event('focus'))`); err != nil {
		t.Fatalf("dispatch focus: %v", err)
	}
	if _, err := page.WaitForFunction(
		`document.title === 'Opal Downloader - Sync'`, nil,
	); err != nil {
		title, _ := page.Title()
		t.Errorf("the tab title never went back to normal (%q): %v", title, err)
	}

	if len(pageErrors) > 0 {
		t.Fatalf("the sync page threw JavaScript errors: %v", pageErrors)
	}
}

// faviconHref reads the <link rel=icon> the page is currently pointing at.
// Deliberately the attribute rather than the .href property: the property is
// resolved to an absolute URL, which would make "/logo.svg" depend on the
// httptest server's random port.
func faviconHref(t *testing.T, page playwright.Page) string {
	t.Helper()
	v, err := page.Evaluate(`document.querySelector('link[rel="icon"]').getAttribute('href')`)
	if err != nil {
		t.Fatalf("read favicon href: %v", err)
	}
	s, _ := v.(string)
	return s
}
