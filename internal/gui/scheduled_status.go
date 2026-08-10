package gui

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/statuslog"
)

// readScheduledStatusFunc is a package-level indirection over
// statuslog.ReadDefault, matching this package's existing test-override
// convention (see e.g. scheduleStatusFunc in schedule.go) - `go test` never
// depends on a real ~/.opal-downloader/last-scheduled-run.json existing (or
// not) on the machine running the tests.
var readScheduledStatusFunc = statuslog.ReadDefault

// readDismissedFunc/writeDismissedFunc are the same indirection for the
// banner's dismissal marker (see statuslog's dismissFileName for why the
// marker is a file rather than the browser's localStorage it used to be).
var (
	readDismissedFunc  = statuslog.ReadDismissedDefault
	writeDismissedFunc = statuslog.WriteDismissedDefault
)

// scheduledStatusResponse is the small JSON shape bannerChrome's client-side
// script (gui.go) fetches on every page load. Only the fields the banner
// actually needs are exposed - FilesDownloaded/FilesSkipped/FilesErrored
// and LoginPath exist in statuslog.Status for the maintainer to inspect the
// raw file directly, but aren't needed by the banner itself.
type scheduledStatusResponse struct {
	Outcome   string `json:"outcome"`
	Message   string `json:"message"`
	Timestamp string `json:"timestamp"`
}

// handleScheduledStatus serves the most recent `sync --scheduled` run's
// outcome as JSON for bannerChrome's client-side script to render. Always
// responds 200 with a JSON body - either a real status object, or the bare
// JSON literal `null` when there is nothing to report (no status file yet,
// it's unreadable/corrupt, the last run's outcome was "success", or it was
// "skipped" - see statuslog.OutcomeSkipped's doc comment for why a skipped
// run is treated the same as success here) - never an HTTP error status, so
// a missing/corrupt file degrades silently on the client (bannerChrome's
// render() already treats a falsy body as "show nothing") rather than
// surfacing as a visible error. See this task's "GUI must degrade silently"
// acceptance criterion.
func (s *server) handleScheduledStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	status, ok := readScheduledStatusFunc()
	if !ok || status.Outcome == statuslog.OutcomeSuccess || status.Outcome == statuslog.OutcomeSkipped {
		_, _ = w.Write([]byte("null"))
		return
	}

	// Dismissal is decided here, on the server, rather than by the script in
	// bannerChrome: the GUI serves from a fresh ephemeral port on every
	// launch, so anything the page remembers per origin (localStorage,
	// sessionStorage, a cookie bound to that port) is gone the next time the
	// window opens. That is the whole of the "dismiss does not stick" bug.
	if statuslog.IsDismissed(readDismissedFunc(), status.Timestamp) {
		_, _ = w.Write([]byte("null"))
		return
	}

	_ = json.NewEncoder(w).Encode(scheduledStatusResponse{
		Outcome:   string(status.Outcome),
		Message:   status.Message,
		Timestamp: status.Timestamp.Format(time.RFC3339Nano),
	})
}

// dismissRequest is the body bannerChrome's Dismiss button sends: the
// timestamp of the run the page actually rendered.
type dismissRequest struct {
	Timestamp string `json:"timestamp"`
}

// handleScheduledStatusDismiss records that the user has dismissed the
// banner for the run they were shown, so it stays dismissed across GUI
// restarts.
//
// The body carries the timestamp the page rendered, and the dismissal is
// only written when it still matches the status file. Without that check the
// handler dismissed whatever the file said *at click time* (weekly review,
// 2026-08-10): if a scheduled run finished in the window between page load
// and the click, the click silently dismissed the new, never-shown failure
// instead of the one on screen - defeating the notification for exactly the
// run the user most needed to see. Narrow window, but the failure is silent
// and costs a whole run's report.
//
// A request with no body, or one whose timestamp cannot be parsed, still
// dismisses the current status: an already-open page from an older build
// sends nothing, and refusing there would break the button for the one
// person running it. Mismatches answer 204 rather than an error - the banner
// has already hidden itself client-side, and a visible failure for a button
// that plainly worked would be worse than a no-op. The stale run reappears
// on the next page load, which is the correct outcome: it was never seen.
func (s *server) handleScheduledStatusDismiss(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status, ok := readScheduledStatusFunc()
	if !ok {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// io.LimitReader: this is a local GUI, but a handler that reads an
	// unbounded body on a POST is a habit worth not having.
	var req dismissRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err == nil {
		if seen, perr := time.Parse(time.RFC3339Nano, strings.TrimSpace(req.Timestamp)); perr == nil {
			if !seen.Equal(status.Timestamp) {
				// The user dismissed a different run than the one on file.
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
	}

	if err := writeDismissedFunc(status.Timestamp); err != nil {
		// Same "degrade silently" contract as the banner itself: the click
		// already hid it for this session, and a failed write only means it
		// comes back next launch - not something to show an error page over.
		http.Error(w, "could not save the dismissal", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
