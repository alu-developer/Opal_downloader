package gui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubLogPage points the logs page at a temp file and at a recording opener,
// so no test can read the maintainer's real log or open a window on their
// desktop. Returns the path it pointed at.
func stubLogPage(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "opal-downloader.log")
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatalf("write log: %v", err)
		}
	}

	oldPath, oldReveal := logPathForPage, revealLogFile
	logPathForPage = func() string { return path }
	revealLogFile = func(string) error { return nil }
	t.Cleanup(func() { logPathForPage, revealLogFile = oldPath, oldReveal })
	return path
}

func TestLogsPageShowsTheLogAndItsPath(t *testing.T) {
	path := stubLogPage(t, "line one\nline two\nline three\n")

	rec := httptest.NewRecorder()
	handleLogsPage(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	// The path matters as much as the contents: it is what someone types into
	// a file manager, or quotes in a bug report, when the page is not enough.
	if !strings.Contains(body, path) {
		t.Errorf("page does not name the log path %q", path)
	}
	for _, want := range []string{"line one", "line two", "line three"} {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing log line %q", want)
		}
	}
}

func TestLogsPageSaysThereIsNoLogYetInsteadOfErroring(t *testing.T) {
	// A fresh install has no log until something runs. That is the normal
	// first-run state, so it must not read as a fault.
	stubLogPage(t, "")

	rec := httptest.NewRecorder()
	handleLogsPage(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "No log file yet") {
		t.Errorf("page does not explain the missing log, body: %s", body)
	}
	if strings.Contains(body, "Could not read") {
		t.Errorf("a missing log is reported as a read failure")
	}
}

func TestLogsPageShowsOnlyTheEndOfAHugeLog(t *testing.T) {
	// The file rotates at 2 MiB, so it can be far too big to put in a page.
	var b strings.Builder
	for i := 0; i < logTailLines*3; i++ {
		fmt.Fprintf(&b, "entry %d\n", i)
	}
	stubLogPage(t, b.String())

	rec := httptest.NewRecorder()
	handleLogsPage(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))
	body := rec.Body.String()

	if !strings.Contains(body, fmt.Sprintf("entry %d", logTailLines*3-1)) {
		t.Errorf("page does not show the newest entry")
	}
	if strings.Contains(body, "entry 0\n") {
		t.Errorf("page shows the oldest entry, so it is not tailing")
	}
	if !strings.Contains(body, "Showing the end of the log") {
		t.Errorf("page does not say that it truncated")
	}
}

func TestLogsPageEscapesLogContents(t *testing.T) {
	// The log is scrubbed of credentials, not of HTML. A crawl records section
	// titles and URLs straight from OPAL, so a course named with a tag would
	// otherwise inject markup into this page.
	stubLogPage(t, "visiting <script>alert(1)</script>\n")

	rec := httptest.NewRecorder()
	handleLogsPage(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))
	body := rec.Body.String()

	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("log contents are rendered as live markup")
	}
	if !strings.Contains(body, "alert(1)") {
		t.Errorf("log contents were dropped entirely rather than escaped")
	}
}

func TestOpeningTheLogFolderReportsFailureInsteadOfSwallowingIt(t *testing.T) {
	stubLogPage(t, "anything\n")
	revealLogFile = func(string) error { return errors.New("no file manager") }

	rec := httptest.NewRecorder()
	handleLogsOpen(rec, httptest.NewRequest(http.MethodPost, "/logs/open", nil))

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want 303", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "opened=") || strings.Contains(loc, "opened=1") {
		t.Fatalf("redirect %q does not carry the failure", loc)
	}

	// And the page it redirects to must actually say so - a redirect carrying
	// an error nobody renders is the same as swallowing it.
	rec2 := httptest.NewRecorder()
	handleLogsPage(rec2, httptest.NewRequest(http.MethodGet, loc, nil))
	if !strings.Contains(rec2.Body.String(), "Could not open the folder") {
		t.Errorf("the redirect target does not report the failure")
	}
}

func TestFeedbackPageLinksToTheLog(t *testing.T) {
	// The whole point of the page: the log was unreachable from the GUI, and
	// a bug report is exactly when someone needs it.
	srv := &server{}
	rec := httptest.NewRecorder()
	srv.handleFeedbackPage(rec, httptest.NewRequest(http.MethodGet, "/feedback", nil))

	if !strings.Contains(rec.Body.String(), `href="/logs"`) {
		t.Errorf("feedback page does not link to the diagnostic log")
	}
}
