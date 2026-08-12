package gui

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mxschmitt/playwright-go"

	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// The Konami handler was reported not working (maintainer, 2026-08-03) after
// being called "live-verified" here. It was not: the check fired
// KeyboardEvents with dispatchEvent, which only proves the listener's body
// runs when called. It says nothing about whether a real keypress reaches it -
// untrusted synthetic events skip the entire browser input path, which is the
// part that can actually be broken.
//
// So this presses real keys. Playwright's Keyboard.Press goes through CDP's
// input domain and produces trusted events, the same way a person's keyboard
// does.
func TestBrowserKonamiWalk(t *testing.T) {
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

	if _, err := page.Goto(ts.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateLoad,
	}); err != nil {
		t.Fatalf("goto landing: %v", err)
	}

	logo := page.Locator("#logo-mark")
	if count, err := logo.Count(); err != nil || count != 1 {
		t.Fatalf("the landing page has no #logo-mark for the handler to find (count=%d err=%v)", count, err)
	}

	code := []string{"ArrowUp", "ArrowUp", "ArrowDown", "ArrowDown",
		"ArrowLeft", "ArrowRight", "ArrowLeft", "ArrowRight", "b", "a"}

	// --- a wrong sequence must do nothing ------------------------------------
	for _, key := range []string{"ArrowUp", "ArrowDown", "x"} {
		if err := page.Keyboard().Press(key); err != nil {
			t.Fatalf("press %s: %v", key, err)
		}
	}
	if turns := datasetTurns(t, page); turns != "" {
		t.Errorf("a wrong sequence triggered the easter egg (turns=%q)", turns)
	}

	// --- the real thing ------------------------------------------------------
	for _, key := range code {
		if err := page.Keyboard().Press(key); err != nil {
			t.Fatalf("press %s: %v", key, err)
		}
	}

	turns := datasetTurns(t, page)
	if turns != "3" {
		transform, _ := page.Evaluate(`document.getElementById('logo-mark').style.transform || ''`)
		t.Fatalf("real keypresses did not trigger the easter egg: data-turns=%q transform=%v", turns, transform)
	}

	// The rotation itself must reach the element, not just the dataset. This
	// reads the inline style rather than getComputedStyle on purpose: a
	// transition means the computed value is still at its start for the first
	// frame, and in a headless/occluded page it may never advance at all -
	// which is exactly the artifact that sent the first investigation of this
	// bug down the wrong path.
	transform, err := page.Evaluate(`document.getElementById('logo-mark').style.transform`)
	if err != nil {
		t.Fatalf("read transform: %v", err)
	}
	if transform != "rotate(1080deg)" {
		t.Errorf("logo transform = %v, want rotate(1080deg)", transform)
	}

	// --- the hover shimmer shares this element with the spin -----------------
	// Both decorate the same <img>, and the spin sets an inline transition
	// that replaces the stylesheet's - so a shimmer that quietly stopped
	// happening would look exactly like a shimmer that was never there.
	// Waited for rather than read once: it transitions in, so the first frame
	// after the pointer arrives is still the un-hovered value.
	if err := logo.Hover(); err != nil {
		t.Fatalf("hover the logo: %v", err)
	}
	if _, err := page.WaitForFunction(
		`getComputedStyle(document.getElementById('logo-mark')).filter !== 'none'`, nil,
	); err != nil {
		filter, _ := page.Evaluate(`getComputedStyle(document.getElementById('logo-mark')).filter`)
		t.Errorf("hovering the logo did not light it up (filter=%v): %v", filter, err)
	}

	// --- typing in a field must never be eaten -------------------------------
	// The landing page has no inputs, so this is checked where inputs live.
	if _, err := page.Goto(ts.URL+"/settings", playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateLoad,
	}); err != nil {
		t.Fatalf("goto settings: %v", err)
	}
	if err := page.Locator("#download_path").Fill(""); err != nil {
		t.Fatalf("clear download_path: %v", err)
	}
	if err := page.Locator("#download_path").Type("ba"); err != nil {
		t.Fatalf("type into download_path: %v", err)
	}
	value, err := page.Locator("#download_path").InputValue()
	if err != nil {
		t.Fatalf("read download_path: %v", err)
	}
	if value != "ba" {
		t.Errorf("typing into a field lost characters to a key handler: got %q, want %q", value, "ba")
	}

	if len(pageErrors) > 0 {
		t.Fatalf("the page threw JavaScript errors: %v", pageErrors)
	}
}

func datasetTurns(t *testing.T, page playwright.Page) string {
	t.Helper()
	v, err := page.Evaluate(`document.getElementById('logo-mark').dataset.turns || ''`)
	if err != nil {
		t.Fatalf("read data-turns: %v", err)
	}
	s, _ := v.(string)
	return s
}
