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

	_ = json.NewEncoder(w).Encode(scheduledStatusResponse{
		Outcome:   string(status.Outcome),
		Message:   status.Message,
		Timestamp: status.Timestamp.Format(time.RFC3339Nano),
	})
}
