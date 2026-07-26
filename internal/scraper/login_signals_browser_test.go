package scraper

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// readLoginSignals is the one part of the stall detection that cannot be
// checked by reasoning about it: it evaluates JavaScript in a real page and
// unpacks whatever playwright-go hands back. A wrong type assertion there
// fails silently as Unknown, which reads as "the page is busy" and would
// quietly disable stall detection altogether - the exact bug this replaced.
//
// Opt-in like internal/gui's browser walks, since a fresh clone has no
// Playwright browsers installed:
//
//	OPAL_SCRAPER_BROWSER_PROBE=1 go test ./internal/scraper/ -run TestReadLoginSignalsAgainstARealPage -v
func TestReadLoginSignalsAgainstARealPage(t *testing.T) {
	if os.Getenv("OPAL_SCRAPER_BROWSER_PROBE") == "" {
		t.Skip("set OPAL_SCRAPER_BROWSER_PROBE=1 to run the real-browser signal probe")
	}

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

	page, err := browser.NewPage()
	if err != nil {
		t.Fatalf("new page: %v", err)
	}

	// A login form shaped like Shibboleth's: a username, a password, and
	// controls that are not credential fields and must not be counted.
	//
	// Served over HTTP rather than set as page content, because stalled() also
	// weighs the URL - and SetContent leaves it at about:blank, which would
	// quietly exercise a different code path than the real one.
	const loginHTML = `<!doctype html><html><body>
		<form>
			<input type="text" id="username" name="j_username">
			<input type="password" id="password" name="j_password">
			<input type="checkbox" id="remember">
			<input type="submit" value="Login">
		</form>
	</body></html>`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginHTML))
	}))
	defer ts.Close()

	if _, err := page.Goto(ts.URL + "/idp/profile/SAML2/Redirect/SSO"); err != nil {
		t.Fatalf("goto login page: %v", err)
	}

	s := New("", "")

	empty := s.readLoginSignals(page)
	if empty.Unknown {
		t.Fatalf("a perfectly readable login page came back Unknown, which silently disables stall detection")
	}
	if empty.FieldCount != 2 {
		t.Errorf("expected the two credential fields to be counted (checkbox and submit excluded), got %d", empty.FieldCount)
	}
	if empty.FilledFields != 0 {
		t.Errorf("expected an untouched form to report nothing filled, got %d", empty.FilledFields)
	}

	// This is the reading that decides a reload happens.
	if !empty.stalled() {
		t.Errorf("an untouched login form did not read as stalled: %+v", empty)
	}

	// Now something types into it - a human, or TU-Fast.
	if err := page.Fill("#username", "s1234567"); err != nil {
		t.Fatalf("fill username: %v", err)
	}

	filled := s.readLoginSignals(page)
	if filled.FilledFields != 1 {
		t.Errorf("expected one filled field after typing, got %d", filled.FilledFields)
	}
	if !filled.changedFrom(empty) {
		t.Error("typing into the form did not register as the page doing something")
	}

	// The value itself must never be read. Nothing in a reading may carry it.
	if filled.URL == "s1234567" || empty.URL == "s1234567" {
		t.Error("a credential value leaked into the signal reading")
	}
}
