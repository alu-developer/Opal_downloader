package gui

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// This used to assert that the feedback page links to /logs, and its sibling
// below that it links to /logs/download - both pinning the 2026-07-27 fix for
// "the page tells you to attach the log and gives you no way to get it".
// 2026-08-12 replaced the mechanism rather than the goal: the log now rides
// along in the form by itself, so a link the user has to go and follow is no
// longer the fix. The requirement being pinned is unchanged - a report reaches
// the maintainer with the log in it - so these test the new route to it.
func TestFeedbackPageCarriesTheLogInTheForm(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opal-downloader.log")
	if err := os.WriteFile(path, []byte("first line\nsomething went wrong\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	restore := logPathForPage
	logPathForPage = func() string { return path }
	defer func() { logPathForPage = restore }()

	srv := &server{}
	rec := httptest.NewRecorder()
	srv.handleFeedbackPage(rec, httptest.NewRequest(http.MethodGet, "/feedback", nil))

	body := rec.Body.String()
	if !strings.Contains(body, "something went wrong") {
		t.Errorf("feedback page does not carry the log tail; got:\n%s", body)
	}
	// Named, so it is submitted with the form - an unnamed field would render
	// identically and silently send nothing.
	if !strings.Contains(body, `name="log"`) {
		t.Errorf("the log field must be named so it is actually submitted")
	}
	// Editable, not readonly: that is the entire privacy design for putting
	// course and file names into a public issue tracker automatically.
	if strings.Contains(body, `name="log" class="diagnostics" rows="12" readonly`) {
		t.Errorf("the log field must stay editable so the user can cut lines out")
	}
}

// A fresh install has no log. The page must still work, and must not show an
// empty "Recent log" block inviting the user to wonder what is missing.
func TestFeedbackPageOmitsTheLogSectionWhenThereIsNoLog(t *testing.T) {
	restore := logPathForPage
	logPathForPage = func() string { return filepath.Join(t.TempDir(), "nope.log") }
	defer func() { logPathForPage = restore }()

	srv := &server{}
	rec := httptest.NewRecorder()
	srv.handleFeedbackPage(rec, httptest.NewRequest(http.MethodGet, "/feedback", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "Recent log") {
		t.Errorf("no log exists, so the page must not show a log section")
	}
}

// The feedback page asked people to attach the log and gave them no way to get
// it - reported by the maintainer, 2026-07-27. These pin the fix in both
// directions: the file is actually served as a download, and the page that
// asks for it actually links to it.
func TestLogDownloadServesTheWholeFileAsAnAttachment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opal-downloader.log")
	const contents = "line one\nline two\nline three\n"
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}

	restore := logPathForPage
	logPathForPage = func() string { return path }
	defer func() { logPathForPage = restore }()

	rec := httptest.NewRecorder()
	handleLogsDownload(rec, httptest.NewRequest(http.MethodGet, "/logs/download", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if got := rec.Body.String(); got != contents {
		t.Fatalf("expected the whole file, got %q", got)
	}
	// Without this the browser renders the log in a tab instead of saving it,
	// which is the same dead end as before: visible, not attachable.
	if disp := rec.Header().Get("Content-Disposition"); !strings.Contains(disp, "attachment") {
		t.Fatalf("log must download rather than render, got Content-Disposition %q", disp)
	}
}

func TestLogDownloadSaysSoWhenThereIsNoLogYet(t *testing.T) {
	restore := logPathForPage
	logPathForPage = func() string { return filepath.Join(t.TempDir(), "nope.log") }
	defer func() { logPathForPage = restore }()

	rec := httptest.NewRecorder()
	handleLogsDownload(rec, httptest.NewRequest(http.MethodGet, "/logs/download", nil))

	// An empty file would be worse than an error: the user attaches it to a
	// bug report believing it contains something.
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 when no log exists, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "no log file yet") {
		t.Fatalf("expected an explanation, got %q", rec.Body.String())
	}
}

// The download link survives, but only where it is earned: on the result
// page, when the report actually had to leave log lines out. Kept because
// that is the one case where the report genuinely does not carry everything
// and the user needs the file itself.
func TestFeedbackResultOffersTheDownloadOnlyWhenLinesWereDropped(t *testing.T) {
	restore := logPathForPage
	logPathForPage = func() string { return filepath.Join(t.TempDir(), "nope.log") }
	defer func() { logPathForPage = restore }()

	srv := &server{openBrowser: func(string) error { return nil }}

	short := url.Values{"description": {"it broke"}, "log": {"one short line"}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/feedback/open", strings.NewReader(short.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handleFeedbackOpen(rec, req)
	if strings.Contains(rec.Body.String(), `href="/logs/download"`) {
		t.Errorf("nothing was dropped, so the page must not push the download at the user")
	}

	long := url.Values{"description": {"it broke"}, "log": {strings.Repeat("a fairly typical looking log line with a timestamp\n", 400)}}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/feedback/open", strings.NewReader(long.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	srv.handleFeedbackOpen(rec2, req2)
	if !strings.Contains(rec2.Body.String(), `href="/logs/download"`) {
		t.Errorf("lines were dropped, so the user must be told where the full log is; got:\n%s", rec2.Body.String())
	}
}
