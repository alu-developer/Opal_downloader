package gui

import (
	"encoding/json"
	"net/http"
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

// handleScheduledStatusDismiss records that the user has dismissed the
// banner for whatever the current status file says, so it stays dismissed
// across GUI restarts.
//
// Takes no request body on purpose: the timestamp to dismiss is read from
// the status file server-side, so there is nothing a caller can hand over to
// get some other run marked dismissed, and no parsing to get wrong. If there
// is nothing to dismiss (no status file, or the last run succeeded) it is a
// no-op that still answers 204 - the banner has already hidden itself
// client-side by then, and reporting an error for a button that visibly
// worked would be worse than doing nothing.
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
	if err := writeDismissedFunc(status.Timestamp); err != nil {
		// Same "degrade silently" contract as the banner itself: the click
		// already hid it for this session, and a failed write only means it
		// comes back next launch - not something to show an error page over.
		http.Error(w, "could not save the dismissal", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
