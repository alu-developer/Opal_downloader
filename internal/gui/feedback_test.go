package gui

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func newTestServer() *server {
	return &server{buildVersion: "1.2.3", feedback: &feedbackState{}}
}

func TestHandleFeedbackPage_NoCrash(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/feedback", nil)
	w := httptest.NewRecorder()
	s.handleFeedbackPage(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, "A crash was detected") {
		t.Errorf("feedback page shows crash banner with no crash recorded:\n%s", body)
	}
	if !strings.Contains(body, "Version: 1.2.3") {
		t.Errorf("feedback page missing diagnostics version:\n%s", body)
	}
}

func TestHandleFeedbackPage_WithCrash(t *testing.T) {
	s := newTestServer()
	s.feedback.setCrash("### Crash report (auto-generated)\n\nboom")

	req := httptest.NewRequest(http.MethodGet, "/feedback?crash=1", nil)
	w := httptest.NewRecorder()
	s.handleFeedbackPage(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "A crash was detected") {
		t.Errorf("feedback page should show crash banner:\n%s", body)
	}
	if !strings.Contains(body, "boom") {
		t.Errorf("feedback page should include the crash report text:\n%s", body)
	}
}

// TestHandleFeedbackOpen_OpensGitHubIssueURLWithBody verifies the acceptance
// criterion that submitting the feedback form opens a
// github.com/.../issues/new?... link with the textarea content prefilled in
// the body, without actually launching a real browser (openBrowser is
// stubbed).
func TestHandleFeedbackOpen_OpensGitHubIssueURLWithBody(t *testing.T) {
	s := newTestServer()
	var openedURL string
	s.openBrowser = func(u string) error {
		openedURL = u
		return nil
	}

	form := url.Values{"description": {"Sync hangs on course Foo"}}
	req := httptest.NewRequest(http.MethodPost, "/feedback/open", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleFeedbackOpen(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body:\n%s", w.Code, w.Body.String())
	}
	if !strings.HasPrefix(openedURL, "https://github.com/alu-developer/Opal_downloader/issues/new?") {
		t.Fatalf("opened URL = %q, want github issues/new prefix", openedURL)
	}
	parsed, err := url.Parse(openedURL)
	if err != nil {
		t.Fatalf("could not parse opened URL: %v", err)
	}
	bodyParam := parsed.Query().Get("body")
	if !strings.Contains(bodyParam, "Sync hangs on course Foo") {
		t.Errorf("issue body missing description:\n%s", bodyParam)
	}
	if !strings.Contains(bodyParam, "Version: 1.2.3") {
		t.Errorf("issue body missing diagnostics:\n%s", bodyParam)
	}

	// Scrub check: no credential/session/full-home-path markers anywhere in
	// what would have been opened.
	for _, forbidden := range []string{"password", "session_state", "cookie"} {
		if strings.Contains(strings.ToLower(bodyParam), forbidden) {
			t.Errorf("issue body unexpectedly contains %q:\n%s", forbidden, bodyParam)
		}
	}
}

func TestHandleFeedbackOpen_BrowserOpenFailureShowsManualLink(t *testing.T) {
	s := newTestServer()
	s.openBrowser = func(u string) error {
		return errTestOpenFailed
	}

	form := url.Values{"description": {"test"}}
	req := httptest.NewRequest(http.MethodPost, "/feedback/open", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handleFeedbackOpen(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "Could not open your browser automatically") {
		t.Errorf("expected manual-link fallback message, got:\n%s", body)
	}
	if !strings.Contains(body, "https://github.com/alu-developer/Opal_downloader/issues/new") {
		t.Errorf("expected manual issue link in fallback page:\n%s", body)
	}
}

// TestWithRecover_PanicDoesNotCrashProcessAndServerKeepsServing exercises the
// panic-recovery middleware directly: a handler that panics must produce a
// 500 error page (not crash the test process/goroutine), record the crash
// for the feedback page, and a subsequent unrelated request must still
// succeed normally afterward.
func TestWithRecover_PanicDoesNotCrashProcessAndServerKeepsServing(t *testing.T) {
	s := newTestServer()

	panicking := s.withRecover(func(w http.ResponseWriter, r *http.Request) {
		panic("boom: simulated handler panic")
	})

	req := httptest.NewRequest(http.MethodGet, "/panic-test", nil)
	w := httptest.NewRecorder()
	panicking(w, req) // must not panic out of this call

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if !strings.Contains(w.Body.String(), "boom: simulated handler panic") {
		t.Errorf("panic page missing panic message:\n%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "/feedback?crash=1") {
		t.Errorf("panic page missing link to feedback flow:\n%s", w.Body.String())
	}

	if s.feedback.crash() == "" {
		t.Error("expected panic to be recorded in feedback state")
	}

	// Server keeps serving other requests: a normal handler wrapped the same
	// way still works fine after a prior panic.
	ok := s.withRecover(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("still alive"))
	})
	req2 := httptest.NewRequest(http.MethodGet, "/ok-test", nil)
	w2 := httptest.NewRecorder()
	ok(w2, req2)
	if w2.Code != http.StatusOK || w2.Body.String() != "still alive" {
		t.Fatalf("server did not keep serving after a panic: status=%d body=%q", w2.Code, w2.Body.String())
	}
}

type testOpenError struct{}

func (testOpenError) Error() string { return "no browser found (test)" }

var errTestOpenFailed error = testOpenError{}
