package gui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/statuslog"
)

// withScheduledStatusFake overrides readScheduledStatusFunc for the
// duration of a test, restoring the original afterward - same pattern as
// withScheduleFakes in schedule_test.go.
func withScheduledStatusFake(t *testing.T, fn func() (statuslog.Status, bool)) {
	t.Helper()
	orig := readScheduledStatusFunc
	readScheduledStatusFunc = fn
	t.Cleanup(func() { readScheduledStatusFunc = orig })
}

// TestHandleScheduledStatus_NoStatusDegradesSilently covers the "GUI must
// degrade silently" acceptance criterion at the HTTP-handler layer: when
// there's nothing to report (missing/corrupt status file - modeled here by
// readScheduledStatusFunc returning ok=false, matching statuslog.Read's own
// contract), the endpoint must still respond 200 with a body the client
// treats as "nothing to show" (bannerChrome's render() checks `!status`),
// never an HTTP error status.
func TestHandleScheduledStatus_NoStatusDegradesSilently(t *testing.T) {
	withScheduledStatusFake(t, func() (statuslog.Status, bool) { return statuslog.Status{}, false })

	srv := &server{}
	req := httptest.NewRequest(http.MethodGet, "/scheduled-status", nil)
	rec := httptest.NewRecorder()
	srv.handleScheduledStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "null" {
		t.Fatalf("expected body \"null\" when there's nothing to report, got %q", got)
	}
}

// TestHandleScheduledStatus_SuccessOutcomeAlsoRespondsNull covers "once a
// subsequent run succeeds, the banner should not keep reappearing" from the
// client side's perspective: the endpoint itself treats a success outcome
// the same as "nothing to report" so the banner clears without the client
// needing any special-case logic beyond checking outcome !== 'success'
// (defense in depth - see bannerChrome's own render() check too).
func TestHandleScheduledStatus_SuccessOutcomeAlsoRespondsNull(t *testing.T) {
	withScheduledStatusFake(t, func() (statuslog.Status, bool) {
		return statuslog.Status{Outcome: statuslog.OutcomeSuccess, Message: "Synced successfully: 3 downloaded, 0 skipped."}, true
	})

	srv := &server{}
	req := httptest.NewRequest(http.MethodGet, "/scheduled-status", nil)
	rec := httptest.NewRecorder()
	srv.handleScheduledStatus(rec, req)

	if got := strings.TrimSpace(rec.Body.String()); got != "null" {
		t.Fatalf("expected body \"null\" for a success outcome, got %q", got)
	}
}

// TestHandleScheduledStatus_FailureOutcomeReturnsDetails covers the actual
// banner-driving case: a failure/partial status is surfaced as real JSON
// with the fields bannerChrome's script reads (outcome, message,
// timestamp).
func TestHandleScheduledStatus_FailureOutcomeReturnsDetails(t *testing.T) {
	when := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	withScheduledStatusFake(t, func() (statuslog.Status, bool) {
		return statuslog.Status{
			Timestamp: when,
			Outcome:   statuslog.OutcomeFailure,
			Message:   "could not reach OPAL at https://example.invalid/opal/ - check your internet connection and opal_url in config.yaml",
		}, true
	})

	srv := &server{}
	req := httptest.NewRequest(http.MethodGet, "/scheduled-status", nil)
	rec := httptest.NewRecorder()
	srv.handleScheduledStatus(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"outcome":"failure"`) {
		t.Fatalf("expected outcome=failure in response, got %s", body)
	}
	if !strings.Contains(body, "could not reach OPAL") {
		t.Fatalf("expected message to be included in response, got %s", body)
	}
	if !strings.Contains(body, when.Format(time.RFC3339)) {
		t.Fatalf("expected timestamp to be included in response, got %s", body)
	}
}

// TestHandleScheduledStatus_PartialOutcomeReturnsDetails covers the
// "had problems" (not "failed") wording path bannerChrome's script picks
// based on outcome.
func TestHandleScheduledStatus_PartialOutcomeReturnsDetails(t *testing.T) {
	withScheduledStatusFake(t, func() (statuslog.Status, bool) {
		return statuslog.Status{
			Timestamp: time.Now(),
			Outcome:   statuslog.OutcomePartial,
			Message:   "Synced with 2 file error(s) (10 downloaded, 1 skipped).",
		}, true
	})

	srv := &server{}
	req := httptest.NewRequest(http.MethodGet, "/scheduled-status", nil)
	rec := httptest.NewRecorder()
	srv.handleScheduledStatus(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"outcome":"partial"`) {
		t.Fatalf("expected outcome=partial in response, got %s", body)
	}
}

// TestHandleScheduledStatus_RejectsNonGET mirrors this package's existing
// method-guarding convention for POST-only endpoints, applied here in
// reverse (GET-only).
func TestHandleScheduledStatus_RejectsNonGET(t *testing.T) {
	srv := &server{}
	req := httptest.NewRequest(http.MethodPost, "/scheduled-status", nil)
	rec := httptest.NewRecorder()
	srv.handleScheduledStatus(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST, got %d", rec.Code)
	}
}
