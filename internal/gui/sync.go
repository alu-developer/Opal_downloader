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

// syncPage wires up the GUI's sync/list/dump-links page: one shared job
// slot (only one action runs at a time), an SSE stream of its events, and
// start/cancel endpoints. It mirrors the CLI's runSync/runList/runDumpLinks
// in cmd/opal-downloader/root.go, but reports progress incrementally to the
// browser instead of printing to stdout and returning only at the end.
type syncPage struct {
	configPath string
	job        *job
}

func newSyncPage(configPath string) *syncPage {
	return &syncPage{configPath: configPath, job: newJob()}
}

func registerSyncRoutes(mux *http.ServeMux, srv *server, configPath string) {
	sp := newSyncPage(configPath)
	mux.HandleFunc("/sync", srv.withRecover(sp.handlePage))
	mux.HandleFunc("/sync/stream", srv.withRecover(sp.handleStream))
	mux.HandleFunc("/sync/start", srv.withRecover(sp.handleStart))
	mux.HandleFunc("/sync/cancel", srv.withRecover(sp.handleCancel))
	mux.HandleFunc("/sync/dump-links/start", srv.withRecover(sp.handleDumpLinksStart))
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
		writeJSONError(w, http.StatusConflict, "a sync/list/dump-links job is already running")
		return
	}

	go sp.runJob(ctx, sc, loaded, force, kind)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "started", "kind": string(kind)})
}

// handleDumpLinksStart mirrors `opal-downloader dump-links --url <url>`.
func (sp *syncPage) handleDumpLinksStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	targetURL := strings.TrimSpace(r.FormValue("url"))
	if targetURL == "" {
		writeJSONError(w, http.StatusBadRequest, "url is required")
		return
	}
	outputPath := strings.TrimSpace(r.FormValue("out"))
	if outputPath == "" {
		outputPath = "tmp/opal-links.json"
	}
	devMode := r.FormValue("dev") == "on"

	credentials, err := config.LoadCredentials(sp.configPath)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "could not load config: "+err.Error())
		return
	}

	sc := scraper.New(credentials.URL, credentials.StateFile)
	sc.SetDeveloperMode(devMode)

	_, cancel := context.WithCancel(context.Background())
	cancelFn := func() {
		cancel()
		_ = sc.Close()
	}

	if !sp.job.start(jobKindDumpLinks, cancelFn) {
		cancel()
		_ = sc.Close()
		writeJSONError(w, http.StatusConflict, "a sync/list/dump-links job is already running")
		return
	}

	go func() {
		defer cancel()
		defer sc.Close()
		defer sp.job.finish()

		sp.job.publish(jobEvent{Kind: "log", Message: fmt.Sprintf("Dumping links from %s...", targetURL)})
		if err := sc.DumpPageLinks(targetURL, outputPath); err != nil {
			sp.job.publish(jobEvent{Kind: "failed", Error: err.Error(), Message: "dump-links failed"})
			return
		}
		sp.job.publish(jobEvent{Kind: "done", Message: "Link dump written to " + outputPath})
	}()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "started", "kind": string(jobKindDumpLinks)})
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
			sp.job.publish(jobEvent{Kind: "course_started", Course: e.Course})
		case syncer.EventFileDownloaded:
			sp.job.publish(jobEvent{Kind: "file_downloaded", Course: e.Course, File: e.File})
		case syncer.EventFileSkipped:
			sp.job.publish(jobEvent{Kind: "file_skipped", Course: e.Course, File: e.File})
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

func (sp *syncPage) handlePage(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/sync" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = syncTemplate.Execute(w, struct{ ConfigPath string }{ConfigPath: sp.configPath})
}

var syncTemplate = template.Must(template.New("sync").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Opal Downloader - Sync</title>
<style>` + pageStyle + `
	.actions { display: flex; gap: 0.5rem; flex-wrap: wrap; margin: 1rem 0; align-items: center; }
	button.primary { background: #1a73e8; border-color: #1a73e8; font-weight: 600; }
	button.stop { background: #d93025; color: #fff; border-color: #d93025; }
	label.opt { font-size: 0.85rem; display: inline-flex; align-items: center; gap: 0.3rem; }
	input[type=text] { padding: 0.35rem 0.5rem; border: 1px solid #ccc; border-radius: 4px; font: inherit; }
	#status { font-weight: 600; margin: 0.5rem 0; }
	#summary { margin: 0.5rem 0 1rem; }
	#log { border: 1px solid #ddd; border-radius: 6px; padding: 0.5rem 0.75rem; max-height: 24rem; overflow-y: auto; font-family: ui-monospace, monospace; font-size: 0.85rem; background: #fafafa; }
	.row { padding: 0.15rem 0; border-bottom: 1px solid #eee; }
	.row.error { color: #a00; }
	.row.done { color: #1a7a1a; font-weight: 600; }
	.row.cancelled { color: #a06a00; font-weight: 600; }
	.row.failed { color: #a00; font-weight: 600; }
	.dump-form { display: flex; gap: 0.5rem; margin: 1rem 0; align-items: center; flex-wrap: wrap; }
</style>
</head>
<body>
	<h1>Sync</h1>
	<p class="hint">Config: <code>{{.ConfigPath}}</code>. Progress is shown live below.</p>

	<div class="actions">
		<button class="primary" id="btn-sync">Sync</button>
		<button id="btn-list">List courses</button>
		<button class="stop" id="btn-cancel" disabled>Cancel</button>
		<label class="opt"><input type="checkbox" id="opt-force"> Force re-download (ignore previous sync history)</label>
		<label class="opt"><input type="checkbox" id="opt-dev"> dev mode (visible browser)</label>
	</div>
	<p class="hint">Normally, files already downloaded are skipped. Check this to re-download everything regardless.</p>

	<details>
		<summary>Advanced / debugging tools</summary>
		<div class="dump-form">
			<label for="dump-url">Debug: dump detected file links for a URL</label>
			<input type="text" id="dump-url" placeholder="https://bildungsportal.sachsen.de/opal/...">
			<button id="btn-dump">Dump links</button>
		</div>
		<p class="hint">Advanced/debugging tool - scrapes one OPAL page and writes every file link it finds to a JSON file. Not needed for normal syncing.</p>
	</details>

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
		var btnDump = document.getElementById('btn-dump');

		function setRunning(running) {
			btnSync.disabled = running;
			btnList.disabled = running;
			btnDump.disabled = running;
			btnCancel.disabled = !running;
		}

		function addRow(kind, text) {
			var div = document.createElement('div');
			div.className = 'row ' + kind;
			div.textContent = text;
			logEl.appendChild(div);
			logEl.scrollTop = logEl.scrollHeight;
		}

		function describe(e) {
			switch (e.kind) {
				case 'course_started': return '[course] ' + e.course;
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

		function handleEvent(e) {
			addRow(e.kind, describe(e));
			if (e.kind === 'done' || e.kind === 'cancelled' || e.kind === 'failed') {
				statusEl.textContent = e.kind === 'done' ? 'Done.' : (e.kind === 'cancelled' ? 'Cancelled.' : 'Failed.');
				setRunning(false);
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
				statusEl.textContent = data.running ? ('Running: ' + data.kind) : 'Idle.';
			});
			['course_started','file_downloaded','file_skipped','error','log','done','cancelled','failed'].forEach(function (kind) {
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
		btnDump.addEventListener('click', function () {
			var url = document.getElementById('dump-url').value.trim();
			if (!url) { statusEl.textContent = 'Enter a URL to dump links for.'; return; }
			var params = ['url=' + encodeURIComponent(url)];
			if (document.getElementById('opt-dev').checked) params.push('dev=on');
			start('/sync/dump-links/start', params.join('&'));
		});
		btnCancel.addEventListener('click', function () {
			btnCancel.disabled = true;
			statusEl.textContent = 'Cancelling...';
			fetch('/sync/cancel', { method: 'POST' }).then(function () {
				statusEl.textContent = 'Cancel requested...';
			});
		});
	})();
	</script>
</body>
</html>
`))
