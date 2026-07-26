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
		files, err := sc.ScrapeWithSavedSession(ctx, []string{"*"})
		if err != nil {
			sp.publishCancelOrError(ctx, err)
			return
		}
		courses := map[string]int{}
		for _, f := range files {
			courses[f.Course]++
		}
		for name, count := range courses {
			sp.job.publish(jobEvent{Kind: "log", Course: name, Message: fmt.Sprintf("%d files", count)})
		}
		sp.job.publish(jobEvent{Kind: "done", Message: fmt.Sprintf("Found %d courses", len(courses))})
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
			sp.job.publish(jobEvent{Kind: "discovery", Course: e.Course, Message: e.Message})
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
				Message:    fmt.Sprintf("Done. downloaded=%d skipped=%d errors=%d", e.Stats.Downloaded, e.Stats.Skipped, e.Stats.Errors),
			})
		}
	}

	_, err := syncer.SyncCoursesWithProgress(ctx, sc, loaded.App, force, progress)
	if err != nil {
		sp.publishCancelOrError(ctx, err)
	}
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

	<p class="hint">Preview checks every course exactly the way a sync does and
	reports what it found, without downloading anything. It therefore takes
	about as long as a sync — several minutes — so it is a way to see what is
	there, not a quick lookup.</p>

	<label class="opt"><input type="checkbox" id="opt-force"> Force re-download (ignore previous sync history)</label>
	<p class="hint">Normally, files already downloaded are skipped. Check this to re-download everything regardless.</p>

	<label class="opt"><input type="checkbox" id="opt-dev"> dev mode (visible browser)</label>

	<div id="status">Idle.</div>
	<div id="summary"></div>
	<div id="log"></div>

	<p class="back"><a href="/">&larr; Back</a></p>

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
				case 'file_skipped': return '  skipped: ' + e.course + ' / ' + e.file;
				case 'error': return '  ERROR: ' + e.course + ' / ' + e.file + ' - ' + e.error;
				case 'log': return e.course ? ('[' + e.course + '] ' + e.message) : e.message;
				case 'done': return e.message || 'Done.';
				case 'cancelled': return e.message || 'Cancelled.';
				case 'failed': return 'FAILED: ' + (e.error || e.message || 'unknown error');
				default: return JSON.stringify(e);
			}
		}

		// statusText renders a one-line "what's happening right now" summary
		// for the #status line, distinct from describe()'s full log-row text
		// (which stays in #log). Returns null for event kinds that shouldn't
		// change the live-progress line (done/cancelled/failed are handled
		// separately in handleEvent since they end the run).
		function statusText(e) {
			switch (e.kind) {
				case 'course_started':
					return 'Running: ' + courseProgress(e) + '- ' + e.course;
				case 'file_downloaded':
					return 'Running: downloading - ' + e.course + ' / ' + e.file;
				case 'file_skipped':
					return 'Running: skipping - ' + e.course + ' / ' + e.file;
				case 'error':
					return 'Running: error on ' + e.course + ' / ' + e.file;
				case 'log':
					return e.course ? ('Running: ' + e.course + ' - ' + e.message) : ('Running: ' + e.message);
				default:
					return null;
			}
		}

		function handleEvent(e) {
			// Discovery events update the live status line only. A real course
			// can have 100+ sections, so logging a row each would bury the
			// run's actual result under scan chatter - but the status line
			// ticking every couple of seconds is exactly what makes a long
			// crawl distinguishable from a hang.
			if (e.kind === 'discovery') {
				statusEl.textContent = 'Scanning: ' + e.message;
				return;
			}
			addRow(e.kind, describe(e));
			if (e.kind === 'done' || e.kind === 'cancelled' || e.kind === 'failed') {
				statusEl.textContent = e.kind === 'done' ? 'Done.' : (e.kind === 'cancelled' ? 'Cancelled.' : 'Failed.');
				setRunning(false);
			} else {
				var st = statusText(e);
				if (st) { statusEl.textContent = st; }
			}
			if (e.kind === 'done' && (e.downloaded || e.skipped || e.errors)) {
				summaryEl.textContent = 'downloaded=' + (e.downloaded||0) + ' skipped=' + (e.skipped||0) + ' errors=' + (e.errors||0);
			}
		}

		var es = null;
		function connect() {
			if (es) { es.close(); }
			es = new EventSource('/sync/stream');
			es.addEventListener('state', function (ev) {
				var data = JSON.parse(ev.data);
				setRunning(data.running);
				// The wire keeps calling it "list" - that is the job kind and
				// the CLI subcommand, both of which stay as they are. Only the
				// word the user reads changes, because listing courses is not
				// what the button does.
				var kindLabel = data.kind === 'list' ? 'preview' : data.kind;
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
			var params = [];
			if (document.getElementById('opt-force').checked) params.push('force=on');
			if (document.getElementById('opt-dev').checked) params.push('dev=on');
			start('/sync/start', params.join('&'));
		});
		btnList.addEventListener('click', function () {
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
