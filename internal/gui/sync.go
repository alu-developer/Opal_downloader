package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/statuslog"
	"github.com/alu-developer/opal-downloader/internal/syncer"
)

// syncPage wires up the GUI's sync/list page: one shared job slot (only one
// action runs at a time), an SSE stream of its events, and start/cancel
// endpoints. It mirrors the CLI's runSync/runList in
// cmd/opal-downloader/root.go, but reports progress incrementally to the
// browser instead of printing to stdout and returning only at the end.
// (dump-links stays CLI-only - see cmd/opal-downloader/root.go's
// runDumpLinks - it's a maintainer debugging tool with no GUI entry point.)
type syncPage struct {
	configPath string
	job        *job
	srv        *server
}

func newSyncPage(configPath string, srv *server) *syncPage {
	return &syncPage{configPath: configPath, job: newJob(), srv: srv}
}

func registerSyncRoutes(mux *http.ServeMux, srv *server, configPath string) {
	sp := newSyncPage(configPath, srv)
	// Share the job with the server so handleLanding can render the "Sync
	// now" button's running/idle state from the same source of truth the
	// /sync page uses, rather than a second, drifting one.
	srv.syncJob = sp.job
	mux.HandleFunc("/sync", srv.withRecover(sp.handlePage))
	mux.HandleFunc("/sync/stream", srv.withRecover(sp.handleStream))
	mux.HandleFunc("/sync/start", srv.withRecover(sp.handleStart))
	mux.HandleFunc("/sync/cancel", srv.withRecover(sp.handleCancel))
}

// handleStart launches a full sync (mirrors `opal-downloader sync`) in a
// background goroutine and returns immediately (202 Accepted). Progress is
// reported only via the /sync/stream SSE endpoint and the in-memory job
// event log - see job.go's lifecycle note for exactly what "background"
// means here (survives tab close, dies with the server process).
func (sp *syncPage) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	force := r.URL.Query().Get("force") == "1" || r.FormValue("force") == "on"
	devMode := r.URL.Query().Get("dev") == "1" || r.FormValue("dev") == "on"
	kind := jobKindSync
	if r.URL.Query().Get("list_only") == "1" {
		kind = jobKindList
	}

	ctx, cancel := context.WithCancel(context.Background())

	loaded, err := config.Load(sp.configPath)
	if err != nil {
		cancel()
		writeJSONError(w, http.StatusBadRequest, "could not load config: "+err.Error())
		return
	}

	sc := scraper.New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	sc.SetDeveloperMode(devMode)
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	sc.SetSectionConcurrency(loaded.App.SectionConcurrency)
	sc.SetSkipEnrollmentSections(loaded.App.SkipEnrollmentSections)

	// cancelFn closes the scraper's browser/Playwright process out from
	// under any in-flight call, which is the only interruption mechanism
	// available: Playwright-go's API has no context-aware/cooperative
	// cancellation for a call like ScrapeWithSavedSession already in
	// progress. Closing the browser makes the blocked Playwright call
	// return an error promptly, which the goroutine below turns into a
	// "cancelled" event instead of a crash.
	cancelFn := func() {
		cancel()
		_ = sc.Close()
	}

	if !sp.job.start(kind, cancelFn) {
		cancel()
		_ = sc.Close()
		writeJSONError(w, http.StatusConflict, "a sync/list job is already running")
		return
	}

	go sp.runJob(ctx, sc, loaded, force, kind)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "started", "kind": string(kind)})
}

// runJob performs the actual sync or list-only run and publishes progress
// events as it goes. It always closes sc and calls job.finish() on the way
// out, whether it completed, failed, or was cancelled.
func (sp *syncPage) runJob(ctx context.Context, sc *scraper.OpalScraper, loaded config.Loaded, force bool, kind jobKind) {
	defer sc.Close()
	defer sp.job.finish()

	if kind == jobKindList {
		sp.job.publish(jobEvent{Kind: "log", Message: "Fetching courses from OPAL..."})
		// Course-by-course as the crawl actually finds them, not batched until
		// the whole thing returns - collectCourseFilesConcurrently already
		// publishes PhaseCourseDone per course (both discovery paths), this
		// job just wasn't listening. Mirrors the CLI's equivalent fix
		// (timing.PrintCourseProgress) for the friction-campaign Walk 3
		// finding: a `list` run with nothing to show for minutes reads as
		// hung. No SetDiscoveryProgress(nil) needed on the way out - sc is
		// single-use per job (fresh in handleStart, closed and discarded by
		// the defer above), never reused for a later run.
		courses := map[string]struct{}{}
		// totalCourses/emptyCourses: same "what happened to the rest"
		// accounting as the CLI's ListAvailableCourses (internal/syncer) -
		// a course with 0 files otherwise vanishes from "Found N courses"
		// with no explanation (friction-campaign Walk 3 finding).
		totalCourses := 0
		emptyCourses := 0
		sc.SetDiscoveryProgress(func(p scraper.DiscoveryProgress) {
			switch p.Phase {
			case scraper.PhaseCoursesFound:
				totalCourses = p.TotalCourses
			case scraper.PhaseCourseDone:
				if p.FileCount == 0 {
					emptyCourses++
				}
				sp.job.publish(jobEvent{Kind: "log", Course: p.Course, Message: fmt.Sprintf("%d files", p.FileCount)})
			}
		})
		files, err := sc.ScrapeWithSavedSession(ctx, []string{"*"})
		if err != nil {
			sp.publishCancelOrError(ctx, err)
			return
		}
		for _, f := range files {
			courses[f.Course] = struct{}{}
		}
		doneMsg := fmt.Sprintf("Found %d courses", len(courses))
		if emptyCourses > 0 {
			doneMsg = fmt.Sprintf("%s (%d of %d enrolled courses had no files)", doneMsg, emptyCourses, totalCourses)
		}
		sp.job.publish(jobEvent{Kind: "done", Message: doneMsg})
		return
	}

	sp.job.publish(jobEvent{Kind: "log", Message: fmt.Sprintf(
		"Download path: %s | course patterns: %s", loaded.App.DownloadPath, strings.Join(loaded.App.Courses, ", "))})

	progress := func(e syncer.Event) {
		switch e.Type {
		case syncer.EventCourseStarted:
			sp.job.publish(jobEvent{Kind: "course_started", Course: e.Course, CourseIndex: e.CourseIndex, TotalCourses: e.TotalCourses})
		case syncer.EventFileDownloaded:
			sp.job.publish(jobEvent{Kind: "file_downloaded", Course: e.Course, File: e.File})
		case syncer.EventFileSkipped:
			sp.job.publish(jobEvent{Kind: "file_skipped", Course: e.Course, File: e.File})
		case syncer.EventMigration:
			sp.job.publish(jobEvent{Kind: "log", Message: e.Message})
		case syncer.EventDiscovery:
			// Published as its own kind rather than a plain "log" so the page
			// can collapse the discovery chatter into a single updating status
			// line - a course with 100+ sections would otherwise bury the run's
			// real output under one log row per section.
			sp.job.publish(jobEvent{Kind: "discovery", Course: e.Course, Message: e.Message,
				CourseIndex: e.CourseIndex, TotalCourses: e.TotalCourses})
		case syncer.EventError:
			msg := ""
			if e.Err != nil {
				msg = e.Err.Error()
			}
			sp.job.publish(jobEvent{Kind: "error", Course: e.Course, File: e.File, Error: msg})
		case syncer.EventComplete:
			sp.job.publish(jobEvent{
				Kind:       "done",
				Downloaded: e.Stats.Downloaded,
				Skipped:    e.Stats.Skipped,
				Errors:     e.Stats.Errors,
				Message:    fmt.Sprintf("Done. %d downloaded, %d already up to date, %d failed.", e.Stats.Downloaded, e.Stats.Skipped, e.Stats.Errors),
			})
		}
	}

	stats, err := syncer.SyncCoursesWithProgress(ctx, sc, loaded.App, force, progress)

	// Record this run as the most recent sync of any kind, so the landing
	// page's "last sync" line reflects the GUI's own button and not just
	// `sync --scheduled`. This is the second of the two places a real sync
	// can be started - the CLI's runSync is the other - because the GUI
	// syncs in-process through syncer rather than shelling out to the CLI.
	// Miss this one and the line would be stale in the most common case
	// there is: the user clicked "Sync now" and the page still reports
	// yesterday's scheduled run.
	//
	// Deliberately after the listOnly branch above returns, so a preview
	// `list` never counts as a sync - it downloads nothing.
	writeLastSyncFunc(buildGUISyncStatus(time.Now(), ctx, err, stats))

	if err != nil {
		sp.publishCancelOrError(ctx, err)
	}
}

// writeLastSyncFunc is the package-level indirection over
// statuslog.WriteLastSyncDefault, matching this package's existing
// test-override convention (see readScheduledStatusFunc in
// scheduled_status.go) - `go test` must never write into the real
// ~/.opal-downloader of the machine running it.
//
// The error is dropped rather than surfaced: a sync that downloaded the
// user's files succeeded whether or not the note about it reached disk, and
// the sync stream has already reported the real outcome.
var writeLastSyncFunc = func(status statuslog.Status) {
	_ = statuslog.WriteLastSyncDefault(status)
}

// buildGUISyncStatus composes the last-sync record for a GUI-initiated run.
// Pure (no I/O, no job publishing) so it can be unit tested directly, the
// same shape as the CLI's buildScheduledRunStatus.
//
// A user-cancelled run is recorded as a failure rather than a success: it
// stopped early, so the files on disk are not what a completed sync would
// have left, and "last sync: just now" would overstate what happened.
// LoginPath stays Unknown - the GUI does not track which session branch
// ensureSession took, and inventing a value here would put a guess into a
// field the scheduled path fills in from fact.
func buildGUISyncStatus(now time.Time, ctx context.Context, runErr error, stats syncer.Stats) statuslog.Status {
	status := statuslog.Status{
		Timestamp:       now,
		FilesDownloaded: stats.Downloaded,
		FilesSkipped:    stats.Skipped,
		FilesErrored:    stats.Errors,
		LoginPath:       statuslog.LoginPathUnknown,
	}

	switch {
	case runErr != nil && ctx.Err() != nil:
		status.Outcome = statuslog.OutcomeFailure
		status.Message = "Cancelled by the user before it finished."
	case runErr != nil:
		status.Outcome = statuslog.OutcomeFailure
		status.Message = statuslog.SanitizeMessage(runErr.Error())
	case stats.Errors > 0:
		status.Outcome = statuslog.OutcomePartial
		status.Message = fmt.Sprintf("Synced with %d file error(s) (%d downloaded, %d skipped).", stats.Errors, stats.Downloaded, stats.Skipped)
	default:
		status.Outcome = statuslog.OutcomeSuccess
		status.Message = fmt.Sprintf("Synced successfully: %d downloaded, %d skipped.", stats.Downloaded, stats.Skipped)
	}

	return status
}

// publishCancelOrError distinguishes an operator-triggered cancellation
// (ctx.Err() != nil because /sync/cancel called the cancel func) from any
// other scraper failure, so the stream reports "cancelled" rather than
// "failed" when the user asked for the stop.
func (sp *syncPage) publishCancelOrError(ctx context.Context, err error) {
	if ctx.Err() != nil {
		sp.job.publish(jobEvent{Kind: "cancelled", Message: "Sync cancelled by user", Error: err.Error()})
		return
	}
	sp.job.publish(jobEvent{Kind: "failed", Error: err.Error(), Message: "Sync failed"})
}

// handleCancel stops the in-progress job, if any, by invoking its cancel
// func (see job.cancel doc comment for why this closes the browser rather
// than doing a graceful stop). Safe to call when nothing is running.
func (sp *syncPage) handleCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ok := sp.job.cancel()
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "no_job_running"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "cancel_requested"})
}

// handleStream serves the job's event log over Server-Sent Events (SSE),
// stdlib net/http only (no WebSocket library, per task constraint). A
// client connecting mid-job (or after it's done) first receives a replay
// of every event published so far, then live events as they arrive. The
// connection's request context is used only to detect the *client*
// disconnecting (to stop wasting server resources writing to a closed
// response) - it deliberately does NOT cancel the job itself; see job.go's
// lifecycle note for why a job outlives any single viewer/request.
func (sp *syncPage) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	ch, replay, unsubscribe := sp.job.subscribe()
	defer unsubscribe()

	running, kind := sp.job.isRunning()
	fmt.Fprintf(w, "event: state\ndata: {\"running\":%t,\"kind\":%q}\n\n", running, string(kind))
	flusher.Flush()

	for _, e := range replay {
		writeSSEEvent(w, e)
	}
	flusher.Flush()

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			// Client (browser tab) went away. This only stops *this*
			// stream's writer goroutine; the job itself keeps running
			// server-side (see job.go lifecycle note) so a page refresh or
			// a second tab can reattach and see the full replay.
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			writeSSEEvent(w, e)
			flusher.Flush()
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, e jobEvent) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Kind, e.marshal())
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// syncPageData drives the /sync page's own readiness gate. It mirrors the
// landing page's SyncReady/SetupNeeded/SyncBlockedReason (see gui.go's
// applySyncReadiness) so the same click that the landing page carefully
// disables and explains isn't left live one link away, on the page that
// actually performs it.
type syncPageData struct {
	SyncReady         bool
	SetupNeeded       bool
	SyncBlockedReason string
}

func (sp *syncPage) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/sync" {
		http.NotFound(w, r)
		return
	}
	data := syncPageData{SyncReady: true}
	if sp.srv != nil {
		landing := sp.srv.sessionStatus()
		sp.srv.applySyncReadiness(&landing)
		data.SyncReady = landing.SyncReady
		data.SetupNeeded = landing.SetupNeeded
		data.SyncBlockedReason = landing.SyncBlockedReason
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = syncTemplate.Execute(w, data)
}

var syncTemplate = template.Must(template.New("sync").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
` + faviconLink + `
<title>Opal Downloader - Sync</title>
<style>` + pageStyle + `
	.actions { display: flex; gap: 0.5rem; flex-wrap: wrap; margin: 1rem 0; align-items: center; }
	button.primary { background: #1a73e8; border-color: #1a73e8; font-weight: 600; }
	button.stop { background: #d93025; color: #fff; border-color: #d93025; }
	label.opt { font-size: 0.85rem; display: flex; align-items: center; gap: 0.3rem; margin: 0.5rem 0 0.15rem; }
	input[type=text] { padding: 0.35rem 0.5rem; border: 1px solid #ccc; border-radius: 4px; font: inherit; }
	#status { font-weight: 600; margin: 0.5rem 0; }
	/* Deliberately unlike #status: lighter, italic, no weight. A run's real
	   state has to stay the one bold line on the page - see the quip script
	   below for why this is allowed to exist at all. */
	#quip { color: #777; font-style: italic; font-size: 0.85rem; margin: -0.25rem 0 0.5rem; min-height: 1.2em; }
	#summary { margin: 0.5rem 0 1rem; }
	#log { border: 1px solid #ddd; border-radius: 6px; padding: 0.5rem 0.75rem; max-height: 24rem; overflow-y: auto; font-family: ui-monospace, monospace; font-size: 0.85rem; background: #fafafa; }
	.row { padding: 0.15rem 0; border-bottom: 1px solid #eee; }
	.row.error { color: #a00; }
	.row.done { color: #1a7a1a; font-weight: 600; }
	.row.cancelled { color: #a06a00; font-weight: 600; }
	.row.failed { color: #a00; font-weight: 600; }
</style>
</head>
<body>
	` + bannerChrome + `
	<h1>Sync</h1>

	{{if not .SyncReady}}
	<div class="status warn">
		{{.SyncBlockedReason}}
		{{if .SetupNeeded}}<p style="margin: 0.5rem 0 0;"><a href="/settings">Set up opal-downloader</a></p>{{end}}
	</div>
	{{end}}

	<div class="actions">
		<button class="primary" id="btn-sync"{{if not .SyncReady}} disabled{{end}}>Sync</button>
		<button id="btn-list"{{if not .SyncReady}} disabled{{end}}>Preview sync (no download)</button>
		<button class="stop" id="btn-cancel" disabled>Cancel</button>
	</div>

	<p class="hint">Preview takes about as long as a sync &mdash; several
	minutes &mdash; since it checks every course the same way, just without downloading anything.</p>

	<label class="opt"><input type="checkbox" id="opt-force"> Force re-download (ignore previous sync history)</label>

	<label class="opt"><input type="checkbox" id="opt-dev"> dev mode (visible browser)</label>

	<div id="status">Idle.</div>
	<div id="quip"></div>
	<div id="stale" class="warning" style="display:none;"></div>
	<div id="summary"></div>
	<div id="log"></div>

	<p class="back"><a href="/">&larr; Back</a></p>

	` + konamiWatcher + `
	<script>
	(function () {
		var logEl = document.getElementById('log');
		var statusEl = document.getElementById('status');
		var summaryEl = document.getElementById('summary');
		var btnSync = document.getElementById('btn-sync');
		var btnList = document.getElementById('btn-list');
		var btnCancel = document.getElementById('btn-cancel');
		var syncBlocked = {{if .SyncReady}}false{{else}}true{{end}};

		function setRunning(running) {
			btnSync.disabled = running || syncBlocked;
			btnList.disabled = running || syncBlocked;
			btnCancel.disabled = !running;
			// Defined further down; both the function declaration and the
			// state they touch are hoisted, and nothing calls setRunning
			// before connect() at the end of this script.
			setQuipsRunning(running);
			setChromeRunning(running);
		}

		function addRow(kind, text) {
			var div = document.createElement('div');
			div.className = 'row ' + kind;
			div.textContent = text;
			logEl.appendChild(div);
			logEl.scrollTop = logEl.scrollHeight;
		}

		function courseProgress(e) {
			return e.totalCourses ? ('course ' + e.courseIndex + '/' + e.totalCourses + ' ') : '';
		}

		function describe(e) {
			switch (e.kind) {
				case 'course_started': return '[course] ' + courseProgress(e) + e.course;
				case 'file_downloaded': return '  downloaded: ' + e.course + ' / ' + e.file;
				case 'error': return '  ERROR: ' + e.course + ' / ' + e.file + ' - ' + e.error;
				case 'log': return e.course ? ('[' + e.course + '] ' + e.message) : e.message;
				case 'done': return e.message || 'Done.';
				case 'cancelled': return e.message || 'Cancelled.';
				case 'failed': return 'FAILED: ' + (e.error || e.message || 'unknown error');
				default: return JSON.stringify(e);
			}
		}

		// Running totals for the status line. A typical account is ~345 files
		// of which almost none change, so counting is the only honest way to
		// show what a run is doing: naming each already-current file produced
		// hundreds of rows a user has no use for, and left the status line
		// sitting on one arbitrary filename for minutes at a time, which reads
		// as a hang rather than as progress.
		var counts = { checked: 0, downloaded: 0, errors: 0 };
		var currentCourse = '';

		// statusText renders a one-line "what's happening right now" summary
		// for the #status line, distinct from describe()'s full log-row text
		// (which stays in #log). Returns null for event kinds that shouldn't
		// change the live-progress line (done/cancelled/failed are handled
		// separately in handleEvent since they end the run).
		function statusText(e) {
			switch (e.kind) {
				case 'course_started':
					currentCourse = e.course;
					return 'Running: ' + courseProgress(e) + '- ' + e.course;
				case 'file_downloaded':
				case 'file_skipped':
				case 'error':
					return runningTotals(e.course || currentCourse);
				case 'log':
					return e.course ? ('Running: ' + e.course + ' - ' + e.message) : ('Running: ' + e.message);
				default:
					return null;
			}
		}

		// summarize is the one line a user reads after a run. "Everything was
		// already up to date" is the normal outcome and deserves to be said in
		// words: downloaded=0 skipped=345 errors=0 makes a successful no-op
		// look like a run that did nothing for an unclear reason.
		function summarize(e) {
			var downloaded = e.downloaded || 0, skipped = e.skipped || 0, errors = e.errors || 0;
			var parts = [];
			if (downloaded) {
				parts.push(downloaded + ' new file' + (downloaded === 1 ? '' : 's') + ' downloaded');
			}
			if (skipped) {
				parts.push(skipped + ' already up to date');
			}
			if (errors) {
				parts.push(errors + ' could not be downloaded - see the log above');
			}
			if (!parts.length) { return 'Nothing to download.'; }
			if (!downloaded && !errors) { return 'Everything was already up to date (' + skipped + ' files checked).' + spared(skipped); }
			return parts.join(', ') + '.' + spared(downloaded + skipped);
		}

		// spared is the small reward at the end of a run: what the run just
		// saved you, in the unit you'd have paid it in.
		//
		// The number is honest rather than flattering. Fetching one file by
		// hand in OPAL is open the course, open the section, open the file,
		// save it - four, and that is the optimistic count that assumes you
		// never went to the wrong section. It is deliberately attached to
		// files *checked*, not files downloaded: checking is the part nobody
		// would do by hand, and it is what makes a 0-download run feel like
		// something happened rather than like a wasted three minutes.
		function spared(filesChecked) {
			if (!filesChecked) { return ''; }
			var clicks = filesChecked * 4;
			if (clicks < 100) { return ' That is about ' + clicks + ' clicks you did not make.'; }
			return ' That is roughly ' + (Math.round(clicks / 100) * 100).toLocaleString() + ' clicks you did not make.';
		}

		function runningTotals(course) {
			var parts = [counts.checked + ' file' + (counts.checked === 1 ? '' : 's') + ' checked'];
			if (counts.downloaded) { parts.push(counts.downloaded + ' downloaded'); }
			if (counts.errors) { parts.push(counts.errors + ' failed'); }
			return 'Running: ' + (course ? course + ' - ' : '') + parts.join(', ');
		}

		function handleEvent(e) {
			// Discovery events update the live status line only. A real course
			// can have 100+ sections, so logging a row each would bury the
			// run's actual result under scan chatter - but the status line
			// ticking every couple of seconds is exactly what makes a long
			// crawl distinguishable from a hang.
			markActivity();
			// Events arriving is itself proof a run is in flight. Relying only
			// on the "state" frame would miss a run that started after this
			// page connected, and that run is exactly the one worth watching.
			if (e.kind !== 'done' && e.kind !== 'cancelled' && e.kind !== 'failed') {
				running = true;
				// Events arriving is the only signal for a run that started
				// after this page connected - the "state" frame is sent once,
				// on connect, and never again. setQuipsRunning is idempotent,
				// so calling it per event just keeps an already-running
				// rotation running. Same for the tab's title and favicon.
				setQuipsRunning(true);
				setChromeRunning(true);
			}

			// The tab's progress, from the only denominator a run has. A run
			// walks its courses twice - discovery reads every section, then
			// the download phase visits them - and each phase counts 1..N of
			// its own, so they map onto one half of the ring each. That keeps
			// it moving forwards the whole way: filling up and resetting at
			// the phase change would read as the run starting over. The split
			// is by phase, not by time, and discovery is usually the longer
			// half - so the second half fills faster than the first.
			//
			// Course 1 of 6 having started means none are finished yet, hence
			// the courses *behind* it rather than the one in flight.
			if ((e.kind === 'discovery' || e.kind === 'course_started') && e.totalCourses && e.courseIndex) {
				var within = Math.max(0, (e.courseIndex - 1) / e.totalCourses) / 2;
				progress = e.kind === 'discovery' ? within : 0.5 + within;
				progressLabel = '(' + e.courseIndex + '/' + e.totalCourses + ')';
				paintChrome();
			}

			if (e.kind === 'discovery') {
				statusEl.textContent = 'Scanning: ' + e.message;
				return;
			}

			if (e.kind === 'file_downloaded' || e.kind === 'file_skipped') { counts.checked++; }
			if (e.kind === 'file_downloaded') { counts.downloaded++; }
			if (e.kind === 'error') { counts.errors++; }

			// A file that was already up to date is counted, not listed. It is
			// the overwhelmingly common outcome - on a routine run essentially
			// every file is one - so a row each buries the handful of lines
			// that say what the run actually did.
			if (e.kind !== 'file_skipped') {
				addRow(e.kind, describe(e));
			}
			if (e.kind === 'done' || e.kind === 'cancelled' || e.kind === 'failed') {
				statusEl.textContent = e.kind === 'done' ? 'Done.' : (e.kind === 'cancelled' ? 'Cancelled.' : 'Failed.');
				setRunning(false);
				running = false;
				// After setRunning, which has already put the title and the
				// favicon back the way it found them.
				showOutcomeInTitle(e.kind);
			} else {
				var st = statusText(e);
				if (st) { statusEl.textContent = st; }
			}
			if (e.kind === 'done' && (e.downloaded || e.skipped || e.errors)) {
				summaryEl.textContent = summarize(e);
			}
		}

		// --- something to read while it grinds ------------------------------
		// A sync takes minutes and the status line, correctly, spends most of
		// them repeating a number. This is the one bit of the UI that exists
		// purely to be nice to look at.
		//
		// Two rules keep it from becoming a liability, both encoded below:
		// it never writes to #status (the stall detector's evidence lives
		// there, and a rotating line would make a hung run look busy), and
		// every quip describes something the program genuinely does. Nothing
		// here invents an activity - "asking Wicket nicely" is a joke about a
		// real fight with a real framework, and a user who reads it and then
		// greps the code finds internal/scraper/wicket.go.
		var QUIPS = [
			'Opening tabs so you do not have to.',
			'Asking Wicket nicely to render the tree.',
			'Waiting for a page that swears it is nearly done.',
			'Counting PDFs. There are always more PDFs.',
			'Politely, one request at a time. OPAL has other students.',
			'Reading section names nobody has read since the semester started.',
			'Checking files that have not changed since 2019.',
			'Clicking "show all", because 25 per page is a choice someone made.',
			'Being patient at OPAL, so you can be impatient here.',
			'Somewhere in here is the slide deck you actually need.',
			'Skipping the files you already have. That is most of them.',
			'Comparing timestamps with a copy from last October.',
			'Uploading nothing. Deleting nothing. That is the whole deal.',
			'Turning a course name into a folder name. Umlauts and all.',
			'Following a link OPAL renders three levels deep.',
			'Reading your enrolment list, so you do not have to remember it.',
			'Holding one browser window open on your behalf.',
			'Refusing to hammer a university server, on principle.',
			'Every file it finds, it checks. Even the ones you forgot about.',
			'This part is slow because OPAL is slow. Sorry.'
		];
		// The reward for typing the Konami code on this page. Same two rules
		// (never writes #status, never claims an activity the program does not
		// perform) with the honesty rule read generously: nobody arrives here
		// by accident, and a line that is plainly a joke cannot be mistaken for
		// a status report the way "Downloading..." could. Nothing in here
		// asserts a number or a mechanism, which is what keeps that true.
		var KONAMI_QUIPS = [
			'Downloading harder.',
			'Turbo mode engaged. (It was already at turbo.)',
			'Bribing Wicket with a biscuit.',
			'Speedrunning a semester. Any% files.',
			'Politely, one request at a time. But with attitude.',
			'PDF number one million. Probably.',
			'The semester is a lie. The files are real.',
			'Somewhere in Saxony, a server sighs.',
			'Achievement unlocked: you still know the arrow keys.',
			'This line is not load-bearing.'
		];

		// Read off window for the same reason STALE_AFTER_MS is (see below):
		// so the browser walk can rotate ten lines in a second instead of
		// sitting through 90 real ones. Nothing sensitive, nothing else
		// reads it.
		var QUIP_EVERY_MS = window.OPAL_QUIP_EVERY_MS || 9000;
		var quipEl = document.getElementById('quip');
		var quipPool = QUIPS;
		var quipOrder = [];
		var quipTimer = null;

		// Shuffled rather than random-each-tick, so a long run shows every line
		// in the pool before repeating any - drawing independently would show
		// the same line twice in a row often enough to look broken.
		function nextQuip() {
			if (!quipOrder.length) {
				quipOrder = quipPool.slice();
				for (var i = quipOrder.length - 1; i > 0; i--) {
					var j = Math.floor(Math.random() * (i + 1));
					var tmp = quipOrder[i]; quipOrder[i] = quipOrder[j]; quipOrder[j] = tmp;
				}
			}
			quipEl.textContent = quipOrder.pop();
		}

		function setQuipsRunning(on) {
			if (on) {
				if (quipTimer) { return; }
				nextQuip();
				quipTimer = setInterval(nextQuip, QUIP_EVERY_MS);
			} else {
				if (quipTimer) { clearInterval(quipTimer); quipTimer = null; }
				quipEl.textContent = '';
			}
		}

		// One-way: once unlocked it stays unlocked for this page load, and
		// re-entering the code does nothing further. The dataset flag is how
		// the browser walk sees it happened; nothing in the program reads it.
		window.opalKonami(function () {
			if (quipPool === KONAMI_QUIPS) { return; }
			quipPool = KONAMI_QUIPS;
			quipOrder = [];
			quipEl.dataset.konami = '1';
			if (quipTimer) { nextQuip(); }
		});

		// --- what the tab shows while you are elsewhere ----------------------
		// A sync takes minutes, and the whole point of it is that you go and
		// do something else meanwhile - at which point this page is a tab you
		// cannot read. The two things still visible from another tab are the
		// title and the favicon, so both carry the run.
		//
		// Everything here is derived from events #status has already shown, so
		// it can never be the only place something is said, and it restores
		// itself completely when the run ends. Canvas work is wrapped: a
		// browser that refuses to hand back a data URL just keeps the logo.
		var BASE_TITLE = document.title;
		var faviconEl = document.querySelector('link[rel="icon"]');
		// "Working" until this page has a reason to be more specific: it learns
		// the job's kind either by starting it (the button handlers) or from
		// the state frame of a run already in flight when it connected. A run
		// that starts elsewhere afterwards sends events but no state frame, and
		// guessing "Syncing" there would put a claim in the tab strip that
		// nothing on the page can back up - a preview downloads nothing.
		var jobLabel = 'Working';
		var ringCanvas = null;
		var progress = -1;   // 0..1 once the course count is known, else -1
		var progressLabel = '';
		var spin = -Math.PI / 2;
		var chromeTimer = null;

		function drawRing() {
			try {
				if (!ringCanvas) {
					ringCanvas = document.createElement('canvas');
					ringCanvas.width = 32;
					ringCanvas.height = 32;
				}
				var ctx = ringCanvas.getContext('2d');
				if (!ctx) { return null; }
				ctx.clearRect(0, 0, 32, 32);
				ctx.lineWidth = 6;
				ctx.lineCap = 'round';
				ctx.strokeStyle = 'rgba(125, 125, 135, 0.25)';
				ctx.beginPath();
				ctx.arc(16, 16, 12, 0, Math.PI * 2);
				ctx.stroke();

				// Same four stops as the app mark in chrome.go's logoSVG, so
				// the tab keeps looking like this program while it works.
				var grad = ctx.createLinearGradient(0, 0, 32, 32);
				grad.addColorStop(0, '#3d8bfd');
				grad.addColorStop(0.45, '#7b5cff');
				grad.addColorStop(0.75, '#e15fd0');
				grad.addColorStop(1, '#2fd6c3');
				ctx.strokeStyle = grad;

				// A filled sweep once the course count is known, a rotating
				// quarter while it is not - discovery has no denominator, and
				// a bar that sits at zero for a minute reads as a hang.
				var from = progress >= 0 ? -Math.PI / 2 : spin;
				var sweep = progress >= 0 ? Math.max(progress, 0.04) * Math.PI * 2 : Math.PI / 2;
				ctx.beginPath();
				ctx.arc(16, 16, 12, from, from + sweep);
				ctx.stroke();
				return ringCanvas.toDataURL('image/png');
			} catch (err) {
				return null;
			}
		}

		function paintChrome() {
			var url = drawRing();
			if (url && faviconEl) {
				// The declared type has to move with the href: the shipped
				// link says image/svg+xml, and a browser that believes it
				// would refuse the PNG the canvas just produced.
				faviconEl.type = 'image/png';
				faviconEl.href = url;
			}
			document.title = (progressLabel ? progressLabel + ' ' : '') + jobLabel + ' - ' + BASE_TITLE;
		}

		function chromeTick() {
			// Nothing animates once there is a real fraction, and rewriting
			// the favicon four times a second for an unchanged picture is
			// work the tab does not need. Course changes repaint directly.
			if (progress >= 0) { return; }
			spin += 0.4;
			paintChrome();
		}

		function setChromeRunning(on) {
			if (on) {
				if (chromeTimer) { return; }
				paintChrome();
				chromeTimer = setInterval(chromeTick, 400);
			} else {
				if (chromeTimer) { clearInterval(chromeTimer); chromeTimer = null; }
				progress = -1;
				progressLabel = '';
				if (faviconEl) {
					faviconEl.type = 'image/svg+xml';
					faviconEl.href = '/logo.svg';
				}
				document.title = BASE_TITLE;
			}
		}

		// The outcome stays in the title until you come back and look, which is
		// the one moment it still has a job to do. One pair of listeners for
		// the life of the page rather than a pair per run: a run that finishes
		// while the last outcome is still on screen would otherwise stack them.
		var outcomePending = false;

		function clearOutcomeTitle() {
			if (!outcomePending || running) { return; }
			outcomePending = false;
			document.title = BASE_TITLE;
		}
		window.addEventListener('focus', clearOutcomeTitle);
		document.addEventListener('visibilitychange', function () {
			if (!document.hidden) { clearOutcomeTitle(); }
		});

		function showOutcomeInTitle(kind) {
			var mark = kind === 'done' ? '✓ Done' : (kind === 'cancelled' ? '✕ Cancelled' : '✕ Failed');
			outcomePending = true;
			document.title = mark + ' - ' + BASE_TITLE;
			// Already looking at it: the summary right below says everything
			// the title would, so it goes straight back to normal.
			if (document.hasFocus && document.hasFocus() && !document.hidden) {
				clearOutcomeTitle();
			}
		}

		// --- has this stopped moving? ---------------------------------------
		// A sync was reported stuck once (2026-07-26), and the only evidence
		// was a status line that had not changed. Nothing noticed, and nothing
		// could have: the page showed the last event it received and had no
		// opinion about how long ago that was.
		//
		// A crawl legitimately goes quiet for a while - a large section can
		// take a good few seconds - so this is deliberately not an alarm. It
		// says how long it has been, and points at Cancel, which is the only
		// thing the user can actually do about it.
		// Three minutes. Read off window so the browser walk can shorten it
		// rather than sitting through the real thing; there is nothing
		// sensitive about the number and nothing else reads it.
		var STALE_AFTER_MS = window.OPAL_STALE_AFTER_MS || 180000;
		var lastEventAt = Date.now();
		var running = false;
		var staleEl = document.getElementById('stale');

		function markActivity() {
			lastEventAt = Date.now();
			staleEl.textContent = '';
			staleEl.style.display = 'none';
		}

		function checkStale() {
			if (!running) { staleEl.style.display = 'none'; return; }
			var quietMs = Date.now() - lastEventAt;
			if (quietMs < STALE_AFTER_MS) { staleEl.style.display = 'none'; return; }
			var mins = Math.floor(quietMs / 60000);
			staleEl.textContent = 'No progress for ' + mins + ' minute' + (mins === 1 ? '' : 's') +
				'. A big section can take a while, so this is not necessarily wrong – but if it stays here, use Cancel and try again.';
			staleEl.style.display = 'block';
		}
		setInterval(checkStale, 5000);

		var es = null;
		function connect() {
			if (es) { es.close(); }
			es = new EventSource('/sync/stream');
			es.addEventListener('state', function (ev) {
				var data = JSON.parse(ev.data);
				setRunning(data.running);
			running = data.running;
			markActivity();
				// The wire keeps calling it "list" - that is the job kind and
				// the CLI subcommand, both of which stay as they are. Only the
				// word the user reads changes, because listing courses is not
				// what the button does.
				var kindLabel = data.kind === 'list' ? 'preview' : data.kind;
				// Same distinction for the tab title: a preview downloads
				// nothing, so calling it "Syncing" there would be a lie told
				// in the one place you can read without coming back. Only
				// meaningful while something is running - the idle frame
				// carries no kind.
				if (data.running) {
					jobLabel = data.kind === 'list' ? 'Previewing' : 'Syncing';
				}
				statusEl.textContent = data.running ? ('Running: ' + kindLabel) : 'Idle.';
			});
			['course_started','file_downloaded','file_skipped','error','log','discovery','done','cancelled','failed'].forEach(function (kind) {
				es.addEventListener(kind, function (ev) { handleEvent(JSON.parse(ev.data)); });
			});
			es.onerror = function () {
				// EventSource auto-reconnects; nothing to do here.
			};
		}
		connect();

		function start(url, body) {
			logEl.textContent = '';
			summaryEl.textContent = '';
			statusEl.textContent = 'Starting...';
			fetch(url, { method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded'}, body: body || '' })
				.then(function (r) {
					if (r.status === 409) {
						statusEl.textContent = 'Already running.';
						return;
					}
					if (!r.ok) {
						return r.json().then(function (j) { statusEl.textContent = 'Error: ' + (j.error || r.statusText); });
					}
					setRunning(true);
				})
				.catch(function (err) { statusEl.textContent = 'Request failed: ' + err; });
		}

		btnSync.addEventListener('click', function () {
			jobLabel = 'Syncing';
			var params = [];
			if (document.getElementById('opt-force').checked) params.push('force=on');
			if (document.getElementById('opt-dev').checked) params.push('dev=on');
			start('/sync/start', params.join('&'));
		});
		btnList.addEventListener('click', function () {
			jobLabel = 'Previewing';
			var params = ['list_only=1'];
			if (document.getElementById('opt-dev').checked) params.push('dev=on');
			start('/sync/start?list_only=1', params.join('&'));
		});
		btnCancel.addEventListener('click', function () {
			btnCancel.disabled = true;
			statusEl.textContent = 'Cancelling...';
			fetch('/sync/cancel', { method: 'POST' }).then(function () {
				statusEl.textContent = 'Cancel requested...';
			});
		});

		// Arriving here via the scheduled-sync banner's "Run now" link
		// (gui.go's bannerChrome, ?autostart=1) starts a normal manual sync
		// immediately - same as clicking "Sync" by hand - instead of making
		// the user land on this page and then still have to click Sync
		// themselves.
		if (/[?&]autostart=1(&|$)/.test(window.location.search)) {
			btnSync.click();
		}
	})();
	</script>
</body>
</html>
`))
