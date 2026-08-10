package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/statuslog"
)

// These cover the bug the server-side dismissal exists for: the GUI binds a
// fresh ephemeral port on every launch, so a banner dismissed in the browser
// (localStorage, scoped per origin) came back the next time the window was
// opened. The fix is that the *server* decides, so a dismissed status never
// reaches the page at all.

// withDismissFakes swaps the dismissal marker for an in-memory one, so no
// test ever touches the real ~/.opal-downloader/dismissed-scheduled-run.json.
func withDismissFakes(t *testing.T) *time.Time {
	t.Helper()
	var stored time.Time

	origRead, origWrite := readDismissedFunc, writeDismissedFunc
	readDismissedFunc = func() time.Time { return stored }
	writeDismissedFunc = func(ts time.Time) error { stored = ts; return nil }
	t.Cleanup(func() { readDismissedFunc, writeDismissedFunc = origRead, origWrite })

	return &stored
}

func failureStatusAt(ts time.Time) statuslog.Status {
	return statuslog.Status{
		Timestamp: ts,
		Outcome:   statuslog.OutcomeFailure,
		Message:   "No internet connection.",
	}
}

func TestDismissSurvivesARestart(t *testing.T) {
	ts := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	withScheduledStatusFake(t, func() (statuslog.Status, bool) { return failureStatusAt(ts), true })
	withDismissFakes(t)

	srv := &server{}

	// Before dismissing, the banner has something to render.
	rec := httptest.NewRecorder()
	srv.handleScheduledStatus(rec, httptest.NewRequest(http.MethodGet, "/scheduled-status", nil))
	if strings.TrimSpace(rec.Body.String()) == "null" {
		t.Fatal("expected the failure to be reported before it is dismissed")
	}

	// Dismiss.
	rec = httptest.NewRecorder()
	srv.handleScheduledStatusDismiss(rec, httptest.NewRequest(http.MethodPost, "/scheduled-status/dismiss", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("dismiss status = %d, want 204 (body: %s)", rec.Code, rec.Body.String())
	}

	// A brand new server value stands in for the next GUI launch on a
	// different port: nothing about the page or its origin carries over,
	// only the file the dismissal was written to.
	next := &server{}
	rec = httptest.NewRecorder()
	next.handleScheduledStatus(rec, httptest.NewRequest(http.MethodGet, "/scheduled-status", nil))
	if got := strings.TrimSpace(rec.Body.String()); got != "null" {
		t.Fatalf("dismissed status came back after a restart: %s", got)
	}
}

// Dismissing one failure must not silence the next one - the marker is keyed
// by the run's timestamp for exactly this reason.
func TestDismissDoesNotHideALaterFailure(t *testing.T) {
	dismissed := time.Date(2026, 8, 10, 3, 0, 0, 0, time.UTC)
	current := dismissed
	withScheduledStatusFake(t, func() (statuslog.Status, bool) { return failureStatusAt(current), true })
	withDismissFakes(t)

	srv := &server{}
	rec := httptest.NewRecorder()
	srv.handleScheduledStatusDismiss(rec, httptest.NewRequest(http.MethodPost, "/scheduled-status/dismiss", nil))

	// The next day's scheduled run fails too, writing a new timestamp.
	current = dismissed.Add(24 * time.Hour)

	rec = httptest.NewRecorder()
	srv.handleScheduledStatus(rec, httptest.NewRequest(http.MethodGet, "/scheduled-status", nil))
	if got := strings.TrimSpace(rec.Body.String()); got == "null" {
		t.Fatal("a new failure must show again after an older one was dismissed")
	}
}

func TestDismissRejectsGet(t *testing.T) {
	withScheduledStatusFake(t, func() (statuslog.Status, bool) { return failureStatusAt(time.Now()), true })
	withDismissFakes(t)

	srv := &server{}
	rec := httptest.NewRecorder()
	srv.handleScheduledStatusDismiss(rec, httptest.NewRequest(http.MethodGet, "/scheduled-status/dismiss", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// Nothing to dismiss is not an error: the button already hid the banner
// client-side by the time this request lands.
func TestDismissWithNoStatusIsANoOp(t *testing.T) {
	withScheduledStatusFake(t, func() (statuslog.Status, bool) { return statuslog.Status{}, false })
	stored := withDismissFakes(t)

	srv := &server{}
	rec := httptest.NewRecorder()
	srv.handleScheduledStatusDismiss(rec, httptest.NewRequest(http.MethodPost, "/scheduled-status/dismiss", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if !stored.IsZero() {
		t.Fatalf("nothing should have been written, got %v", *stored)
	}
}

// The banner script must actually call the endpoint - without this the
// server-side marker is never written and the bug is unchanged.
func TestBannerDismissPostsToTheServer(t *testing.T) {
	if !strings.Contains(bannerChrome, "/scheduled-status/dismiss") {
		t.Fatal("the banner's Dismiss button must POST to /scheduled-status/dismiss")
	}
	if strings.Contains(bannerChrome, "localStorage") {
		t.Fatal("dismissal must not go back to localStorage: the GUI's port, and so its origin, changes every launch")
	}
}
