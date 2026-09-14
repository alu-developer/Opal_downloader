package scraper

// TestBulkZipLA20Uebungen combines two open threads in one live navigation:
// Question 43 (docs/sync-speed-model.md) still needs a whole-section scale
// timing and a pinned "select all" control after Step B's n=5 pass on
// Softwaretechnologie/Part-3; Question 45 still has an open subset,
// "2026 LA20/Übungen" (15 files, all signal-less - no size, no modified -
// under the course_folders-remapped manifest key). If the bulk-ZIP control
// also works on this exact section with real per-file timestamps, it would
// replace option D's per-section-XLSX-cadence fix with something better: one
// bulk fetch instead of N browser-fallback downloads, no maintainer call
// needed either way.
//
// Routes around guessing the section's URL by reading it straight out of the
// course root's own tree payload (ParseCourseTreeNodes), the same technique
// this run's first cycle used for the Woche-cluster node-type question -
// one HTTP GET, no DOM guessing, no wrong-URL risk.
//
// Usage:
//
//	OPAL_BULKZIP_LA20=1 go test ./internal/scraper/ -run TestBulkZipLA20Uebungen -count=1 -v -timeout 10m
//
// Report written to tmp/bulkzip-la20-uebungen.txt; the zip itself to
// tmp/bulkzip-la20-uebungen.zip (both gitignored, read-only against the
// account - no config, manifest, or real download_path touched).
import (
	"archive/zip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

func TestBulkZipLA20Uebungen(t *testing.T) {
	if os.Getenv("OPAL_BULKZIP_LA20") == "" {
		t.Skip("set OPAL_BULKZIP_LA20=1 to probe bulk-ZIP on the real 2026 LA20/Übungen section")
	}
	beginLiveProbe(t)

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	const courseTitle = "2026 LA20"
	const nodeTitle = "Übungen"

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()
	sc.SetDeveloperMode(true)
	if serr := sc.ensureSession(false); serr != nil {
		t.Fatalf("ensure session: %v", serr)
	}

	refs, derr := sc.discoverCourseLinks([]string{courseTitle})
	if derr != nil {
		t.Fatalf("discover course links: %v", derr)
	}
	var course *CourseRef
	for i := range refs {
		if strings.EqualFold(refs[i].Title, courseTitle) {
			course = &refs[i]
			break
		}
	}
	if course == nil {
		t.Fatalf("PREDICTION HOLE: course %q not found among %d discovered courses", courseTitle, len(refs))
	}
	t.Logf("course root: %s", course.URL)

	fetch := sc.httpDiscoveryFetcher()
	if fetch == nil {
		t.Fatal("no authenticated request context after ensureSession; cannot fetch course root")
	}
	resp, gerr := fetch.Get(course.URL)
	if gerr != nil {
		t.Fatalf("HTTP GET course root %s: %v", course.URL, gerr)
	}
	body, berr := responseBodyText(resp)
	if berr != nil || resp.Status() != 200 {
		t.Fatalf("HTTP read course root %s: status %d err %v", course.URL, resp.Status(), berr)
	}

	tree := ParseCourseTreeNodes(body)
	if len(tree) == 0 {
		t.Fatalf("PREDICTION HOLE: course root's initial_data payload parsed to 0 nodes")
	}
	var node *CourseTreeNode
	for i := range tree {
		if strings.EqualFold(tree[i].Title, nodeTitle) {
			node = &tree[i]
			break
		}
	}
	if node == nil {
		var titles []string
		for _, n := range tree {
			titles = append(titles, n.Title)
		}
		t.Fatalf("PREDICTION HOLE: no node titled %q found among %d tree nodes: %v", nodeTitle, len(tree), titles)
	}
	t.Logf("resolved %q node: class=%q url=%s", nodeTitle, node.Class, node.URL)

	page := sc.getPage()
	if page == nil {
		t.Fatalf("ensureSession succeeded but no page is available")
	}
	if _, gerr := sc.gotoPolitely(page, node.URL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); gerr != nil {
		t.Fatalf("goto %s: %v", node.URL, gerr)
	}
	_, calm := sc.waitForInteractiveLinks(page, contentFallbackWaitMs)
	if _, cerr := sc.waitForStableSectionContent(page, calm); cerr != nil {
		t.Logf("waitForStableSectionContent: %v (continuing)", cerr)
	}

	// Follow-up #2 (pin the select-all control): first attempt, the header's
	// "select all visible entries" control. DIAGNOSED live (first run of this
	// cycle): it visually checks every row (15/15 counted) but leaves the
	// download button disabled - the button's enabled state is driven by
	// Wicket's own per-row AJAX selection-changed callback, not by the
	// checked attribute alone, and the header control's bulk toggle does not
	// fire that callback per row. So: click every row checkbox individually
	// instead (exactly what Question 43's original n=5 probe did, which DID
	// enable the button) - answers "is select-all one click or N" for real
	// (N, one click each) rather than assuming the header control suffices.
	selected, serr := page.Evaluate(`() => {
		const boxes = Array.from(document.querySelectorAll('tbody td:first-child input[type=checkbox]'));
		let n = 0;
		for (const b of boxes) { b.click(); n++; }
		return n;
	}`)
	if serr != nil {
		t.Fatalf("evaluate per-row selection: %v", serr)
	}
	checkboxCount := toInt(selected)
	t.Logf("clicked %d row checkbox(es) individually", checkboxCount)
	if checkboxCount == 0 {
		t.Log("RESULT: no row checkboxes found to click - REFUTED as a bulk mechanism on this section.")
		return
	}

	tmpDir := filepath.Join(repo, "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	zipPath := filepath.Join(tmpDir, "bulkzip-la20-uebungen.zip")

	// Diagnostic dump before triggering the click: list every candidate
	// control (matching the same download/herunterladen/zip heuristic the
	// click itself uses) with its disabled state, so a failed trigger below
	// can be diagnosed from this log instead of guessed at.
	if dump, derr3 := page.Evaluate(`() => {
		const nodes = Array.from(document.querySelectorAll('a, button'));
		return nodes.map(el => {
			const text = (el.textContent || '').trim();
			const title = el.getAttribute('title') || '';
			const cls = el.getAttribute('class') || '';
			const hay = (text + ' ' + title + ' ' + cls).toLowerCase();
			if (!(hay.includes('download') || hay.includes('herunterladen') || hay.includes('zip'))) return null;
			return {tag: el.tagName, text: text.slice(0,60), title: title.slice(0,60), class: cls.slice(0,140), href: el.getAttribute('href')||'', disabled: el.disabled === true || cls.includes('disabled')};
		}).filter(x => x !== null);
	}`); derr3 == nil {
		if list, ok := dump.([]interface{}); ok {
			t.Logf("candidate controls before trigger: %d found", len(list))
			for i, c := range list {
				cm, _ := c.(map[string]interface{})
				t.Logf("  control[%d]: tag=%v text=%q title=%q class=%q href=%q disabled=%v",
					i, cm["tag"], cm["text"], cm["title"], cm["class"], cm["href"], cm["disabled"])
			}
		}
	} else {
		t.Logf("diagnostic dump failed (non-fatal): %v", derr3)
	}

	bulkStart := time.Now()
	download, derr2 := page.ExpectDownload(func() error {
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

	if derr2 != nil {
		t.Logf("RESULT: clicking the download control did not trigger a browser download within 30s: %v - REFUTED as a one-click bulk mechanism on this section.", derr2)
		return
	}
	if saveErr := download.SaveAs(zipPath); saveErr != nil {
		t.Fatalf("save download: %v", saveErr)
	}
	t.Logf("bulk download of %d selected row(s) took %s, suggested filename %q",
		checkboxCount, bulkElapsed.Round(time.Millisecond), download.SuggestedFilename())

	r, zerr := zip.OpenReader(zipPath)
	if zerr != nil {
		t.Logf("RESULT: downloaded file at %s is not a readable zip (%v) - REFUTED.", zipPath, zerr)
		return
	}
	closeReader := func() {
		if r != nil {
			_ = r.Close()
			r = nil
		}
	}
	defer closeReader()

	knownFiles := map[string]bool{}
	for i := 1; i <= 14; i++ {
		knownFiles[fmt.Sprintf("U%02d.pdf", i)] = false
	}
	knownFiles["Kapitel5.pdf"] = false

	var report []string
	report = append(report, fmt.Sprintf("section: %s / %s", courseTitle, nodeTitle))
	report = append(report, fmt.Sprintf("node url: %s", node.URL))
	report = append(report, fmt.Sprintf("rows selected: %d", checkboxCount))
	report = append(report, fmt.Sprintf("bulk download elapsed: %s", bulkElapsed.Round(time.Millisecond)))
	report = append(report, fmt.Sprintf("zip entries: %d", len(r.File)))
	usableTimestamps := 0
	for _, f := range r.File {
		usable := f.Modified.Year() > 1980
		if usable {
			usableTimestamps++
		}
		report = append(report, fmt.Sprintf("  entry: name=%q size=%d modified=%s usable=%v",
			f.Name, f.UncompressedSize64, f.Modified.Format(time.RFC3339), usable))
		base := filepath.Base(f.Name)
		if _, known := knownFiles[base]; known {
			knownFiles[base] = usable
		}
	}
	report = append(report, fmt.Sprintf("usable (non-zip-epoch) timestamps: %d/%d", usableTimestamps, len(r.File)))

	var missing, noTimestamp []string
	for name, gotUsable := range knownFiles {
		found := false
		for _, f := range r.File {
			if filepath.Base(f.Name) == name {
				found = true
				break
			}
		}
		if !found {
			missing = append(missing, name)
		} else if !gotUsable {
			noTimestamp = append(noTimestamp, name)
		}
	}
	report = append(report, fmt.Sprintf("known files present with usable timestamp: %d/%d", 15-len(missing)-len(noTimestamp), 15))
	if len(missing) > 0 {
		report = append(report, fmt.Sprintf("MISSING from zip: %v", missing))
	}
	if len(noTimestamp) > 0 {
		report = append(report, fmt.Sprintf("present but NO usable timestamp: %v", noTimestamp))
	}

	out := filepath.Join(tmpDir, "bulkzip-la20-uebungen.txt")
	if err := os.WriteFile(out, []byte(strings.Join(report, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	for _, line := range report {
		t.Log(line)
	}
	t.Logf("report written to %s", out)

	if len(missing) == 0 && len(noTimestamp) == 0 {
		t.Log("RESULT: all 15 known files present with usable timestamps - bulk-ZIP fully covers this Question 45 subset.")
	} else {
		t.Logf("RESULT: partial coverage - %d missing, %d present without a usable timestamp.", len(missing), len(noTimestamp))
	}
}
