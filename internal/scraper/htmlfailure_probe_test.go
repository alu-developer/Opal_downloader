package scraper

// Live diagnostic probe for Question 44 (docs/sync-speed-model.md): a no-op
// sync spends 1097.1s (96% of a 1147.2s run) failing to download 49 files
// that answer with HTML instead of bytes, 33 of them in Softwaretechnologie's
// "Part-3" folder. This is Question 44's own registered "cheapest honest
// first step": discover one real course, then replay exactly what
// DownloadFile does at download time for every Part-3 file, with the full
// click-fallback audit trail on, and watch what the server actually answers
// - before touching any code.
//
// Usage:
//
//	OPAL_HTMLFAILURE_PROBE=1 go test ./internal/scraper/ -run TestHTMLFailureProbe -count=1 -v -timeout 10m
//	OPAL_HTMLFAILURE_PROBE=1 go test ./internal/scraper/ -run TestHTMLFailureProbeConcurrent -count=1 -v -timeout 10m
import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

const htmlFailureProbeCourse = "Softwaretechnologie (SoSe 26)"

// discoverPart3ForProbe logs in, discovers htmlFailureProbeCourse, and
// returns its "Part-3" folder's files - the folder Question 44's own
// measurement named as carrying 33 of the model file's 49 tracked failures,
// the biggest single concentration - plus the live scraper and a tmp dir to
// download into (caller must defer sc.Close() and os.RemoveAll(tmpDir)).
func discoverPart3ForProbe(t *testing.T) (sc *OpalScraper, part3 []RemoteFile, tmpDir string) {
	t.Helper()
	beginLiveProbe(t)

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	sc = New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	sc.SetDeveloperMode(true)
	sc.SetDebugClicks(true) // wires DownloadFile's own auditLog trail through t.Logf via beginLiveProbe's verbose logging
	if serr := sc.ensureSession(false); serr != nil {
		t.Fatalf("ensure session: %v", serr)
	}

	t.Logf("discovering course: %s", htmlFailureProbeCourse)
	remoteFiles, derr := sc.ScrapeWithSavedSession(context.Background(), []string{htmlFailureProbeCourse})
	if derr != nil {
		t.Fatalf("discover course: %v", derr)
	}
	t.Logf("discovered %d files, %d download candidates recorded", len(remoteFiles), len(sc.downloadCandidates))

	for _, f := range remoteFiles {
		if strings.Contains(f.Path, "Part-3") || strings.Contains(f.SectionTitle, "Part-3") {
			part3 = append(part3, f)
		}
	}
	sort.Slice(part3, func(i, j int) bool { return part3[i].Path < part3[j].Path })
	t.Logf("Part-3 files discovered: %d", len(part3))
	if len(part3) == 0 {
		t.Fatal("no Part-3 files discovered - either the folder name changed or the section was empty this run; cannot proceed")
	}

	needsExpansion := 0
	for _, f := range part3 {
		if c, ok := sc.downloadCandidates[f.URL]; ok && (c.ShowAllURL != "" || c.ExpandedPageURL != "") {
			needsExpansion++
		}
	}
	t.Logf("of %d Part-3 candidates, %d were recorded with a non-empty ShowAllURL/ExpandedPageURL (i.e. only found via 'show all' expansion)", len(part3), needsExpansion)

	tmpDir = filepath.Join(repo, "tmp", "q44-probe")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	return sc, part3, tmpDir
}

// TestHTMLFailureProbe downloads every Part-3 file sequentially, one at a
// time. Result 2026-08-13 (autopilot): 48/48 succeeded, 0 failed - flatly
// contradicting the model file's "33/48, stable across two runs a day apart"
// finding. See TestHTMLFailureProbeConcurrent for the next hypothesis this
// forced: not a page/pagination cause at all, but something specific to
// concurrent download load (production's default download_concurrency: 3).
func TestHTMLFailureProbe(t *testing.T) {
	if os.Getenv("OPAL_HTMLFAILURE_PROBE") == "" {
		t.Skip("set OPAL_HTMLFAILURE_PROBE=1 to run")
	}
	sc, part3, tmpDir := discoverPart3ForProbe(t)
	defer sc.Close()
	defer os.RemoveAll(tmpDir)

	succeeded, failed := 0, 0
	for i, f := range part3 {
		localPath := filepath.Join(tmpDir, fmt.Sprintf("part3-%02d-%s", i, f.Name))
		candidate, hasCandidate := sc.downloadCandidates[f.URL]

		dlErr := sc.DownloadFile(f.URL, localPath)
		if dlErr != nil {
			failed++
			t.Logf("--- FAILED %d/%d: %q (url=%s)", i+1, len(part3), f.Name, f.URL)
			if hasCandidate {
				t.Logf("    candidate: SourceURL=%q ShowAllURL=%q ExpandedPageURL=%q ShowAllViaClick=%v",
					candidate.SourceURL, candidate.ShowAllURL, candidate.ExpandedPageURL, candidate.ShowAllViaClick)
			} else {
				t.Logf("    NO CANDIDATE RECORDED for this file's URL - the fallback click path has nothing to try")
			}
			t.Logf("    RESULT: FAILED: %v", dlErr)
		} else {
			succeeded++
		}
	}

	t.Logf("=== SUMMARY (sequential): %d/%d Part-3 files succeeded, %d failed (model file's own count: 33/48 failed) ===", succeeded, len(part3), failed)
	if failed > 0 {
		t.Logf("RESULT: reproduced the failure live on %d/%d files, sequentially - so this is not a concurrency-only effect. "+
			"See the FAILED entries above for exactly which candidate page(s) each one had recorded.", failed, len(part3))
	} else {
		t.Logf("RESULT: all files succeeded sequentially - did not reproduce Question 44's finding this way.")
	}
}

// TestHTMLFailureProbeConcurrent downloads every Part-3 file through a
// worker pool sized at htmlFailureProbeConcurrency (matching production
// config.yaml's download_concurrency: 3), instead of one at a time. Written
// because TestHTMLFailureProbe's sequential run found 0/48 failures where
// the model file recorded 33/48 failing, stable across two runs a day apart
// - and this campaign has already established, independently, that "OPAL
// serialises the session server-side" (docs/sync-speed-model.md's discovery
// numbers: parallel HTTP fetch "corrupted in parallel"). DownloadFile's fast
// path (refreshCounterURL, download_refresh.go) re-fetches the file's
// section page fresh and extracts a one-time Wicket AJAX click-behavior URL
// from it on every call - if concurrent workers do that against the *same*
// section page at the same time, a session that only tracks one page
// instance server-side could hand back a stale/mismatched token to whichever
// request loses the race, which would show up exactly as "response is HTML"
// on the retry. Sequential calls never exercise that race at all.
//
// Prediction, written before running: if the concurrency hypothesis is
// right, this run reproduces some of the 33 failures the sequential run
// didn't; if it doesn't, the sequential result already refuted the model
// file's "stable" characterization on its own and this is a second data
// point toward "no longer reproducible", not a concurrency finding.
func TestHTMLFailureProbeConcurrent(t *testing.T) {
	if os.Getenv("OPAL_HTMLFAILURE_PROBE") == "" {
		t.Skip("set OPAL_HTMLFAILURE_PROBE=1 to run")
	}
	sc, part3, tmpDir := discoverPart3ForProbe(t)
	defer sc.Close()
	defer os.RemoveAll(tmpDir)

	const htmlFailureProbeConcurrency = 3 // matches config.yaml's download_concurrency

	type result struct {
		index     int
		file      RemoteFile
		err       error
		candidate downloadCandidate
		hasCand   bool
	}

	jobs := make(chan int)
	results := make(chan result)
	var wg sync.WaitGroup
	wg.Add(htmlFailureProbeConcurrency)
	for w := 0; w < htmlFailureProbeConcurrency; w++ {
		go func() {
			defer wg.Done()
			for i := range jobs {
				f := part3[i]
				localPath := filepath.Join(tmpDir, fmt.Sprintf("concurrent-part3-%02d-%s", i, f.Name))
				candidate, hasCandidate := sc.downloadCandidates[f.URL]
				err := sc.DownloadFile(f.URL, localPath)
				results <- result{index: i, file: f, err: err, candidate: candidate, hasCand: hasCandidate}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range part3 {
			jobs <- i
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	succeeded, failed := 0, 0
	for r := range results {
		if r.err != nil {
			failed++
			t.Logf("--- FAILED %q (url=%s)", r.file.Name, r.file.URL)
			if r.hasCand {
				t.Logf("    candidate: SourceURL=%q ShowAllURL=%q ExpandedPageURL=%q ShowAllViaClick=%v",
					r.candidate.SourceURL, r.candidate.ShowAllURL, r.candidate.ExpandedPageURL, r.candidate.ShowAllViaClick)
			} else {
				t.Logf("    NO CANDIDATE RECORDED for this file's URL")
			}
			t.Logf("    RESULT: FAILED: %v", r.err)
		} else {
			succeeded++
		}
	}

	t.Logf("=== SUMMARY (concurrency=%d): %d/%d Part-3 files succeeded, %d failed (model file's own count: 33/48 failed) ===",
		htmlFailureProbeConcurrency, succeeded, len(part3), failed)
	if failed > 0 {
		t.Logf("RESULT: reproduced %d/%d failures under concurrent download load, where the sequential run (see "+
			"TestHTMLFailureProbe) found 0/48 - CONFIRMS the concurrency hypothesis: this failure class needs concurrent "+
			"access to the same section's Wicket page instance to trigger, matching this campaign's established "+
			"\"OPAL serialises the session server-side\" finding elsewhere.", failed, len(part3))
	} else {
		t.Logf("RESULT: still 0 failures under concurrency=%d - REFUTES the concurrency hypothesis. The model file's 33/48 "+
			"finding is not reproducible today by either sequential or concurrent replay of the same download mechanism; "+
			"something else (server-side content change, session/cookie state, timing on a much longer real sync) must "+
			"explain the gap.", htmlFailureProbeConcurrency)
	}
}
