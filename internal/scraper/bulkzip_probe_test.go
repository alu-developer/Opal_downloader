package scraper

// Bulk-download-as-ZIP feasibility probe for Question 43
// (docs/sync-speed-model.md): does OPAL's course folder UI expose a
// read-permission bulk "download" control (OpenOLAT's FolderController.
// doBulkDownload -> FolderZipMediaResource) that could replace N per-file
// downloads with one request per section?
//
// This is Step B of Question 43's own staged plan - the source read (Step A)
// already confirmed the button exists in OpenOLAT's source and is gated only
// on not being in the trash, no edit permission required. This probe answers
// the three things only a live account can: (1) is the button actually
// rendered on this deployment, and what is the selection model; (2) does a
// triggered bulk download return a real ZIP with usable per-file
// modification timestamps (the incremental-skip logic in internal/syncer
// depends on them - no timestamps means "real but not a net win"); (3) how
// does bulk compare to today's one-click-per-file path on the same section.
//
// Deliberately narrow: it inspects and times, it does not change how sync
// downloads files. That decision, if this probe confirms a real win, is a
// separate follow-up.
//
// Usage:
//
//	OPAL_BULKZIP_PROBE=1 go test ./internal/scraper/ -run TestBulkZipProbe -count=1 -v -timeout 15m
//
// Optionally override the section under test:
//
//	OPAL_BULKZIP_SECTION_URL=<full CourseNode URL>
import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

// softwaretechnologiePart3SectionURL is the section this probe targets by
// default. The bare CourseNode id (.../CourseNode/1615865126729195011,
// "Vorlesungsfolien und -materialien (Sammlung)") is a collection page, not
// a leaf folder - confirmed live 2026-08-12, it renders the course's nav
// shell with no file table. Its "/Part-3" sub-path is the actual leaf
// folder with the most files (48, per ~/.opal-visit-log.json's most recent
// visit records for this course - Part-1/2/4/5 have 22/22/13/6), so it is
// the one with room for a real bulk-vs-per-file timing difference to show.
const softwaretechnologiePart3SectionURL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53228666883/CourseNode/1615865126729195011/Part-3"

func TestBulkZipProbe(t *testing.T) {
	if os.Getenv("OPAL_BULKZIP_PROBE") == "" {
		t.Skip("set OPAL_BULKZIP_PROBE=1 to run (optionally OPAL_BULKZIP_SECTION_URL=<CourseNode URL>)")
	}
	beginLiveProbe(t)

	sectionURL := os.Getenv("OPAL_BULKZIP_SECTION_URL")
	if sectionURL == "" {
		sectionURL = softwaretechnologiePart3SectionURL
	}

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// A bare Playwright context loading only the saved storage state has no
	// login fallback: this probe's first live run landed on the course's
	// public info page with a visible "Login" link - the saved session had
	// gone stale between runs. Using the full OpalScraper here instead gets
	// ensureSession's headless-reuse-then-TU-Fast-auto-login fallback for
	// free, the same path `list`/`sync` take (CLAUDE.md: unattended runs can
	// always log in, TU-Fast completes 2FA by itself).
	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()
	// Question 43's own Step B plan (docs/sync-speed-model.md) specified
	// "the existing browser-mode CLI (--dev-mode / visible browser, as walk
	// 3/walk 5 already do routinely)", not headless - and headless is what
	// every attempt so far in this session used, with the row-selection
	// checkbox column rendering unreliably across repeated navigations for
	// reasons this probe could not otherwise pin down. Matching the
	// registered plan exactly before concluding anything about the UI's
	// actual behavior.
	sc.SetDeveloperMode(true)
	if serr := sc.ensureSession(false); serr != nil {
		t.Fatalf("ensure session: %v", serr)
	}
	page := sc.getPage()
	if page == nil {
		t.Fatalf("ensureSession succeeded but no page is available")
	}

	t.Logf("navigating to section: %s", sectionURL)
	if _, gerr := sc.gotoPolitely(page, sectionURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); gerr != nil {
		t.Fatalf("goto: %v", gerr)
	}

	// Fixed waits do not hold on this deployment: a first attempt (3s fixed
	// wait) found the download button but 0 checkboxes, immediately followed
	// by a second, unrelated diagnostic query that DID find checkboxes with
	// type=checkbox in the same run, and a later ad-hoc poll (2 consecutive
	// equal reads, up to 10s) still found 0 for the whole budget on a
	// separate run. This project has hit exactly this shape before (the
	// Wicket AJAX_CALL_DONE-vs-DOM finding that cost 52 files) and already
	// has a production-proven fix for it: waitForInteractiveLinks +
	// waitForStableSectionContent, what visitSection uses for every real
	// crawl of this exact page type, every day. Reusing it here rather than
	// reinventing a weaker poll.
	_, calm := sc.waitForInteractiveLinks(page, contentFallbackWaitMs)
	if _, cerr := sc.waitForStableSectionContent(page, calm); cerr != nil {
		t.Logf("waitForStableSectionContent: %v (continuing - this only affects the file-link extraction this probe does not use)", cerr)
	}

	// Question 43 direction (a), cycle 2026-09-02: trace-only mode. Instead of
	// selecting rows and clicking, attach request/response listeners and poll
	// the row-selection column's presence every 500ms for 30s, logging both on
	// one timeline. The question this answers: does a periodic Wicket XHR
	// (a self-updating timer behavior) correlate with the checkbox column
	// appearing/disappearing - the flake that blocked Step B on 2026-08-12?
	if os.Getenv("OPAL_BULKZIP_NETTRACE") != "" {
		runSelectionColumnNetTrace(t, page)
		return
	}

	checkboxCount := waitForStableCheckboxCount(t, page)
	t.Logf("checkbox column stabilized at %d row(s) (calm=%v)", checkboxCount, calm)

	if checkboxCount == 0 {
		// The page's own text includes "Einträge auswählen - Optionen"
		// ("Select entries - Options") separate from the per-row "Diesen
		// Eintrag auswählen" label seen in an earlier run - possibly a mode
		// toggle that must be turned on before the row-selection column
		// renders at all, which would explain checkboxes existing in the DOM
		// only sometimes. Try clicking it once, then re-check.
		if toggled, terr := page.Evaluate(`() => {
			const nodes = Array.from(document.querySelectorAll('a, button, [role="button"]'));
			const match = nodes.find(el => (el.textContent || '').includes('Einträge auswählen'));
			if (!match) return false;
			match.click();
			return true;
		}`); terr == nil {
			if did, _ := toggled.(bool); did {
				t.Log(`clicked an "Einträge auswählen" control - re-checking checkbox count`)
				checkboxCount = waitForStableCheckboxCount(t, page)
				t.Logf("checkbox column after toggle attempt: %d row(s)", checkboxCount)
			} else {
				t.Log(`no "Einträge auswählen" control found to try as a selection-mode toggle`)
			}
		}
	}

	// Last distinct attempt before concluding anything: every diagnostic run
	// this session found the header's "Alle sichtbaren Einträge auswählen"
	// ("select all visible entries", class="table-select") reliably present,
	// unlike the per-row checkboxes. Click it directly - a different element
	// than anything tried above - and see whether it selects rows even when
	// the individual row checkboxes do not render.
	if checkboxCount == 0 {
		if clicked, cerr := page.Evaluate(`() => {
			const el = document.querySelector('th [class*="table-select"], thead [class*="table-select"]');
			if (!el) return false;
			el.click();
			return true;
		}`); cerr == nil {
			if did, _ := clicked.(bool); did {
				t.Log(`clicked the header's "select all visible entries" control directly - re-checking checkbox count`)
				checkboxCount = waitForStableCheckboxCount(t, page)
				t.Logf("checkbox column after select-all-header attempt: %d row(s)", checkboxCount)
			} else {
				t.Log("no select-all header control found either")
			}
		}
	}

	// --- Step 1: is a bulk-download control rendered, and what is its
	// selection model? Dump every button/link whose text, title or class
	// mentions "download" (English) or "herunterladen" (German - this
	// deployment's UI language, confirmed live 2026-08-12: "Gewählte Dateien
	// herunterladen" = "Download selected files") or "zip" - broad on
	// purpose, since we do not know this deployment's exact markup.
	found, err := page.Evaluate(`() => {
		const selectAll = document.querySelector('th [class*="table-select"], input[type=checkbox][class*="select-all"]');
		const controls = [];
		for (const el of document.querySelectorAll('a, button')) {
			const text = (el.textContent || '').trim();
			const title = el.getAttribute('title') || '';
			const cls = el.getAttribute('class') || '';
			const hay = (text + ' ' + title + ' ' + cls).toLowerCase();
			if (hay.includes('download') || hay.includes('herunterladen') || hay.includes('zip')) {
				controls.push({tag: el.tagName, text: text.slice(0, 60), title: title.slice(0, 60), class: cls.slice(0, 120), href: el.getAttribute('href') || ''});
			}
		}
		return { hasSelectAll: !!selectAll, controls: controls };
	}`)
	if err != nil {
		t.Fatalf("evaluate step 1: %v", err)
	}
	summary, _ := found.(map[string]interface{})
	hasSelectAll, _ := summary["hasSelectAll"].(bool)
	controls, _ := summary["controls"].([]interface{})

	t.Logf("STEP 1: %d checkboxes on the page, select-all present=%v, %d download/zip-labeled controls found",
		checkboxCount, hasSelectAll, len(controls))
	for i, c := range controls {
		cm, _ := c.(map[string]interface{})
		t.Logf("  control[%d]: tag=%v text=%q title=%q class=%q href=%q",
			i, cm["tag"], cm["text"], cm["title"], cm["class"], cm["href"])
	}

	if len(controls) == 0 {
		t.Log("RESULT: no download/zip-labeled control found on this page - REFUTED for this section as rendered. " +
			"Either this page is not the folder browser this project expected, or the control uses an icon with no " +
			"matching text/title/class substring this probe looked for.")
		return
	}
	if checkboxCount == 0 {
		t.Log("RESULT: found download-labeled controls but no row checkboxes ever appeared - REFUTED as a bulk mechanism on this page.")
		return
	}

	// --- Step 2: select a small, fixed number of rows (not "select all" -
	// keeps this probe's load on the account small and its timing comparable
	// to a same-sized per-file run) and trigger the download control step 1
	// found, then inspect what came back.
	const selectN = 5
	selected, err := page.Evaluate(fmt.Sprintf(`() => {
		const boxes = Array.from(document.querySelectorAll('tbody td:first-child input[type=checkbox]'));
		let n = 0;
		for (const b of boxes) {
			if (n >= %d) break;
			b.click();
			n++;
		}
		return n;
	}`, selectN))
	if err != nil {
		t.Fatalf("evaluate step 2 selection: %v", err)
	}
	// playwright-go returns a JS integer .length / counter as Go int, not
	// float64 - a v.(float64) assertion here silently yields 0. This exact
	// bug (three sites in this file) is most of what the 2026-08-12 cycle saw
	// as "the row-selection column renders unreliably": the DOM was fine, the
	// probe's own reads were broken. Use toInt everywhere a count comes back.
	n := toInt(selected)
	t.Logf("STEP 2: selected %d row(s)", n)
	if n == 0 {
		t.Log("RESULT: checkboxes were counted but none were clickable at selection time - REFUTED as a bulk mechanism on this page.")
		return
	}

	tmpDir := filepath.Join(repo, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	zipPath := filepath.Join(tmpDir, "bulkzip-probe.zip")

	bulkStart := time.Now()
	download, derr := page.ExpectDownload(func() error {
		// Click the first control step 1 found whose text/title suggests the
		// actual download action rather than a destructive "zip here"
		// create-a-file action - "download"/"herunterladen" beats a bare
		// "zip" match, matching Question 43's own bulkDownloadButton vs
		// bulkZipButton distinction.
		_, evalErr := page.Evaluate(`() => {
			const nodes = Array.from(document.querySelectorAll('a, button'));
			const scored = nodes.map(el => {
				const text = (el.textContent || '').trim();
				const title = el.getAttribute('title') || '';
				const cls = el.getAttribute('class') || '';
				const hay = (text + ' ' + title + ' ' + cls).toLowerCase();
				let score = -1;
				if (hay.includes('download') || hay.includes('herunterladen')) score = 2;
				else if (hay.includes('zip')) score = 1;
				return {el, score};
			}).filter(s => s.score >= 0).sort((a,b) => b.score - a.score);
			if (scored.length === 0) return false;
			scored[0].el.click();
			return true;
		}`)
		return evalErr
	}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(30000)})
	bulkElapsed := time.Since(bulkStart)

	if derr != nil {
		t.Logf("RESULT: clicking the download control did not trigger a browser download within 30s: %v — "+
			"REFUTED as a one-click bulk mechanism (either it needs a confirmation dialog this probe did not "+
			"handle, or it is not wired to the selection).", derr)
		return
	}

	if saveErr := download.SaveAs(zipPath); saveErr != nil {
		t.Fatalf("save download: %v", saveErr)
	}
	t.Logf("STEP 2 result: bulk download of %d selected row(s) took %s, suggested filename %q",
		n, bulkElapsed.Round(time.Millisecond), download.SuggestedFilename())

	// Inspect what came back: is it a real zip, and do entries carry usable
	// per-file modification timestamps? This is Question 43's kill criterion.
	r, zerr := zip.OpenReader(zipPath)
	if zerr != nil {
		t.Logf("RESULT: downloaded file at %s is not a readable zip (%v) - REFUTED, content-type/format is not "+
			"what FolderZipMediaResource was expected to return, or this deployment serves something else.", zipPath, zerr)
		return
	}
	defer r.Close()

	t.Logf("STEP 2 zip contents: %d entries", len(r.File))
	usableTimestamps := 0
	for i, f := range r.File {
		if i < 20 {
			t.Logf("  entry[%d]: name=%q size=%d modified=%s", i, f.Name, f.UncompressedSize64, f.Modified.Format(time.RFC3339))
		}
		// The zip epoch (1980-01-01, what a library emits when it never set a
		// timestamp) is the tell that per-file mtimes were not carried
		// through - anything after this project's own existence is a real,
		// usable timestamp for the incremental-skip logic to compare against.
		if f.Modified.Year() > 1980 {
			usableTimestamps++
		}
	}
	t.Logf("STEP 2: %d of %d entries carry a usable (non-zip-epoch) modification timestamp", usableTimestamps, len(r.File))

	if usableTimestamps == 0 && len(r.File) > 0 {
		t.Log("RESULT: zip entries carry no usable per-file timestamps (all zip-epoch 1980-01-01) - this is Question 43's " +
			"registered kill criterion. CLOSES as 'real but not a net win': the control exists and works, but adopting it " +
			"would break internal/syncer's incremental-skip logic, which depends on per-file modification times.")
	} else {
		t.Logf("RESULT: bulk download is real, returns a genuine zip, and %d/%d entries carry usable timestamps - "+
			"does NOT hit the registered kill criterion on timestamps. Selection model: %d checkbox click(s), no visible "+
			"'select all' shortcut used in this run (hasSelectAll=%v). Bulk download of %d files took %s "+
			"(~%s/file at this batch size - not directly comparable to a 210-file whole-section run without a "+
			"same-scale trial).",
			usableTimestamps, len(r.File), n, hasSelectAll, n, bulkElapsed.Round(time.Millisecond),
			(bulkElapsed / time.Duration(max(n, 1))).Round(time.Millisecond))
	}

	if rmErr := os.Remove(zipPath); rmErr != nil {
		t.Logf("cleanup: could not remove %s: %v", zipPath, rmErr)
	}
}

// traceEvent is one entry on the correlation timeline: a network request/
// response, or a change in the row-selection column's DOM presence.
type traceEvent struct {
	ms   int64
	kind string // "REQ", "RESP", "COL"
	text string
}

// runSelectionColumnNetTrace is Question 43 direction (a): with the folder-
// browser page already navigated and settled, watch for a periodic Wicket XHR
// and poll the row-selection column's presence, then print both on one
// timeline so a reader can see whether the column's appear/disappear tracks a
// timer request, tracks this probe's own DOM reads, or tracks nothing.
func runSelectionColumnNetTrace(t *testing.T, page playwright.Page) {
	t.Helper()

	var (
		mu     sync.Mutex
		events []traceEvent
		start  = time.Now()
	)
	add := func(kind, text string) {
		mu.Lock()
		events = append(events, traceEvent{ms: time.Since(start).Milliseconds(), kind: kind, text: text})
		mu.Unlock()
	}

	// Wicket's own AJAX calls and heartbeat go through XHR/fetch; static asset
	// GETs (js/css/png) are noise for this question. Keep everything but label
	// the likely-Wicket ones so the summary can focus.
	var reqSeen, respSeen int
	page.On("request", func(req playwright.Request) {
		mu.Lock()
		reqSeen++
		mu.Unlock()
		add("REQ", fmt.Sprintf("%s %s [%s]", req.Method(), truncURL(req.URL()), req.ResourceType()))
	})
	page.On("response", func(resp playwright.Response) {
		mu.Lock()
		respSeen++
		mu.Unlock()
		add("RESP", fmt.Sprintf("%d %s", resp.Status(), truncURL(resp.URL())))
	})

	// Sanity check the page handle and the listeners before the 30s window:
	// how many resources did this page load in total, and does a bare
	// .length evaluate (the exact shape waitForStableCheckboxCount already
	// uses successfully in this file) come back as a number?
	if u := page.URL(); u != "" {
		t.Logf("trace starting on page: %s", truncURL(u))
	}
	if rc, e := page.Evaluate(`() => performance.getEntriesByType('resource').length`); e == nil {
		t.Logf("page has loaded %v resources so far (performance API)", rc)
	} else {
		t.Logf("resource-count probe evaluate failed: %v", e)
	}
	if pv, e := page.Evaluate(`() => document.querySelectorAll('input[type=checkbox]').length`); e == nil {
		t.Logf("initial all-checkboxes count: %v (%T)", pv, pv)
	}

	const (
		pollInterval = 500 * time.Millisecond
		budget       = 30 * time.Second
	)
	lastRows, lastSelAll, polls := -2, -2, 0
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		polls++
		rowsV, err := page.Evaluate(`() => document.querySelectorAll('tbody td:first-child input[type=checkbox]').length`)
		if err != nil {
			add("COL", fmt.Sprintf("row-checkbox poll evaluate error: %v", err))
			continue
		}
		selV, _ := page.Evaluate(`() => document.querySelector('th [class*="table-select"], thead [class*="table-select"]') ? 1 : 0`)
		allV, _ := page.Evaluate(`() => document.querySelectorAll('input[type=checkbox]').length`)
		rows := toInt(rowsV)
		selAll := toInt(selV)
		rowsAll := toInt(allV)
		if polls == 1 || polls%10 == 0 {
			add("COL", fmt.Sprintf("[heartbeat poll %d] row-checkboxes=%d select-all-header=%d all-checkboxes=%d", polls, rows, selAll, rowsAll))
		}
		if rows != lastRows || selAll != lastSelAll {
			add("COL", fmt.Sprintf("row-checkboxes=%d select-all-header=%d (all checkboxes on page=%d)", rows, selAll, rowsAll))
			lastRows, lastSelAll = rows, selAll
		}
	}
	t.Logf("poll loop ran %d times; request events=%d response events=%d", polls, reqSeen, respSeen)

	// Detach listeners before analysis so nothing appends mid-print.
	mu.Lock()
	defer mu.Unlock()
	sort.SliceStable(events, func(i, j int) bool { return events[i].ms < events[j].ms })

	t.Logf("=== TIMELINE (%d events, ms since nav-settle) ===", len(events))
	for _, e := range events {
		t.Logf("  %6dms  %-4s  %s", e.ms, e.kind, e.text)
	}

	// Analysis 1: repeated request URLs and their inter-arrival gaps - a
	// roughly-fixed gap is the signature of a self-updating timer behavior.
	reqTimes := map[string][]int64{}
	for _, e := range events {
		if e.kind != "REQ" {
			continue
		}
		reqTimes[e.text] = append(reqTimes[e.text], e.ms)
	}
	t.Log("=== repeated requests (2+ hits) ===")
	anyRepeated := false
	for url, ts := range reqTimes {
		if len(ts) < 2 {
			continue
		}
		anyRepeated = true
		gaps := make([]int64, 0, len(ts)-1)
		for i := 1; i < len(ts); i++ {
			gaps = append(gaps, ts[i]-ts[i-1])
		}
		t.Logf("  %dx  gaps=%vms  %s", len(ts), gaps, url)
	}
	if !anyRepeated {
		t.Log("  (none - no request URL fired more than once in the 30s window)")
	}

	// Analysis 2: for each column-state transition, the nearest preceding
	// network event within 2s. If transitions consistently trail a RESP, the
	// column is re-rendered on that response. If they trail nothing, the
	// trigger is not network.
	t.Log("=== column transitions vs nearest preceding network event (<=2s) ===")
	anyCol := false
	for i, e := range events {
		if e.kind != "COL" {
			continue
		}
		anyCol = true
		var near string
		for j := i - 1; j >= 0; j-- {
			if events[j].kind == "COL" {
				continue
			}
			if e.ms-events[j].ms > 2000 {
				break
			}
			near = fmt.Sprintf("%dms earlier: %s %s", e.ms-events[j].ms, events[j].kind, events[j].text)
			break
		}
		if near == "" {
			near = "(no network event within 2s before it)"
		}
		t.Logf("  transition @%dms [%s]  <-  %s", e.ms, e.text, near)
	}
	if !anyCol {
		t.Log("  (no column-state transition observed in 30s - the column never changed state this run)")
	}

	t.Log("RESULT: read the three blocks above against the cycle's registered prediction " +
		"(docs/sync-speed-model.md 'Next experiment', 2026-09-02). World 1 = a repeated request " +
		"with ~fixed gaps AND transitions trailing its RESPs. World 2 = transitions with no " +
		"network event before them (this probe's own Evaluate calls are the trigger). World 3 = " +
		"no repeated request and no transitions / no pattern (direction (b), human observation, is all that's left).")
}

func truncURL(u string) string {
	const max = 140
	if len(u) <= max {
		return u
	}
	return u[:max] + "…"
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case float32:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case int32:
		return int(n)
	case bool:
		if n {
			return 1
		}
		return 0
	default:
		return -1
	}
}

// waitForStableCheckboxCount polls the row-selection column's checkbox count
// until two consecutive reads agree (or a budget runs out), instead of
// guessing a fixed wait. See the call site's comment: a first attempt with a
// flat 3s wait found the download button but zero checkboxes, and an
// unrelated query moments later in the same run found real ones - this
// column enhances in on its own schedule.
func waitForStableCheckboxCount(t *testing.T, page playwright.Page) int {
	t.Helper()
	const pollInterval = 500.0
	const maxPolls = 20 // 10s budget, well over what any read so far has needed
	last := -1
	for i := 0; i < maxPolls; i++ {
		page.WaitForTimeout(pollInterval)
		v, err := page.Evaluate(`() => document.querySelectorAll('tbody td:first-child input[type=checkbox]').length`)
		if err != nil {
			t.Logf("checkbox poll %d: evaluate error: %v", i, err)
			continue
		}
		count := toInt(v)
		if count == last && last > 0 {
			return last
		}
		last = count
	}
	if last < 0 {
		return 0
	}
	return last
}
