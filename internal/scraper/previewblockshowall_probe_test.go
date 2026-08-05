package scraper

// docs/sync-speed-model.md, Question 23: the real-account byte-diff safety
// bar (filelist_probe_test.go, 2026-08-05) found the raw-CDP preview blocker
// is NOT safe as-is. OPAL_FILELIST=after OPAL_BLOCK_FILE_PREVIEWS=1 lost 33
// files against the OPAL_FILELIST=before baseline (349 -> 316), all of them
// in "Softwaretechnologie (SoSe 26)" / Part-3 (CourseNode/
// 1615865126729195011) - the same section a much older bug (the one
// wicket.go's AJAX_CALL_DONE work exists to prevent) already lost 52 files
// from once. The run logged warnShowAllTruncated for exactly that section:
// "expansion completed but added no files (18 file rows before, 18 after; 72
// raw rows before, 72 after)". The OPAL_FILELIST=before run (previews not
// blocked) logged no such warning anywhere.
//
// course_concurrency=1 and section_concurrency=1 in config.yaml, so this is
// NOT the known course/section-contention loss mode - whatever is happening
// here is single-section, single-course. The candidate mechanism: Part-3 is
// the section with by far the most inline-preview-bearing files (the 30+
// PDF variants that expand from 18 to 51 rows), so its "show all" click
// creates a burst of near-simultaneous Fetch.requestPaused events - each
// answered from its own goroutine (previews.go's doc comment already flags
// this as reentrancy-sensitive, found while building the Question 8 probe).
// This probe does not change production behaviour; it re-runs the known-losing
// section alone, with --debug-clicks-equivalent auditing on, to read
// expansionSignalled/signalWaitErr and the showall-expand-poll trace instead
// of guessing from the file count.
//
// Prediction (written before running): expansionSignalled=true (Wicket's own
// AJAX_CALL_DONE fires fine - the click itself is not the problem) and the
// showall-expand-poll trace shows the candidate count flat at 18 for the
// whole poll budget, i.e. the DOM patch that show-all's response should have
// applied never happened at all rather than arriving late. That would point
// at the preview blocker's Fetch domain interfering with how Chrome applies
// the AJAX response's DOM mutation (plausibly the burst of paused/failed
// subframe navigations), not at Wicket's signal.
//
// Counts as refuted if expansionSignalled=false or signalWaitErr != "none":
// that would mean the click/signal path itself broke, not the DOM update
// after it - a different and more familiar failure shape (Question 19-22).
//
// Usage:
//
//	OPAL_PREVIEWBLOCK_SHOWALL_TRACE=1 go test ./internal/scraper/ \
//	  -run TestPreviewBlockShowAllRegression -v -timeout 10m

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/logging"
)

const softwaretechnologiePart3NodeID = "1615865126729195011"

var previewBlockSignalLineRE = regexp.MustCompile(`\[audit\] .*kind=wicket-expand-signal.*reason="section ([^"]*): watchArmed=(\w+) expansionSignalled=(\w+) signalMs=(-?\d+) signalWaitErr=(\S+) \((\d+) candidates before expansion\)"`)
var previewBlockPollLineRE = regexp.MustCompile(`\[audit\] .*kind=showall-expand-poll.*reason="poll #(\d+): (\d+) candidates, err=(\S*)"`)

func TestPreviewBlockShowAllRegression(t *testing.T) {
	if os.Getenv("OPAL_PREVIEWBLOCK_SHOWALL_TRACE") == "" {
		t.Skip("set OPAL_PREVIEWBLOCK_SHOWALL_TRACE=1 to run the real-account Question 23 regression probe (docs/sync-speed-model.md Question 23)")
	}
	if os.Getenv(blockPreviewsEnv) == "" {
		t.Fatalf("set %s=1 too - this probe traces the regression, it does not reproduce a baseline", blockPreviewsEnv)
	}

	var logBuf bytes.Buffer
	closeLogs, err := logging.Setup(probeLogOptions(&multiWriter{t: t, buf: &logBuf}))
	if err != nil {
		t.Logf("logging setup reported: %v", err)
	}
	t.Cleanup(func() {
		if closeLogs != nil {
			closeLogs()
		}
		if restore, err := logging.Setup(logging.Options{}); err == nil && restore != nil {
			defer restore()
		}
	})

	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()
	sc.SetDebugClicks(true)
	sc.SetCourseConcurrency(1)
	sc.SetSectionConcurrency(1)
	sc.SetSkipEnrollmentSections(loaded.App.SkipEnrollmentSections)

	files, err := sc.ScrapeWithSavedSession(context.Background(), []string{"Softwaretechnologie (SoSe 26)"})
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}
	t.Logf("%d files found", len(files))

	captured := logBuf.String()

	var signalLine string
	for _, m := range previewBlockSignalLineRE.FindAllStringSubmatch(captured, -1) {
		if strings.Contains(m[1], softwaretechnologiePart3NodeID) {
			signalLine = fmt.Sprintf("watchArmed=%s expansionSignalled=%s signalMs=%s signalWaitErr=%s (%s candidates before expansion)", m[2], m[3], m[4], m[5], m[6])
		}
	}
	if signalLine == "" {
		t.Logf("no wicket-expand-signal line found for Part-3 (node %s) - the click path may not have been reached", softwaretechnologiePart3NodeID)
	} else {
		t.Logf("Part-3 wicket-expand-signal: %s", signalLine)
	}

	var pollLines []string
	for _, m := range previewBlockPollLineRE.FindAllStringSubmatch(captured, -1) {
		pollLines = append(pollLines, fmt.Sprintf("poll #%s: %s candidates, err=%s", m[1], m[2], m[3]))
	}
	t.Logf("showall-expand-poll trace (%d lines, last 10 shown):", len(pollLines))
	start := 0
	if len(pollLines) > 10 {
		start = len(pollLines) - 10
	}
	for _, l := range pollLines[start:] {
		t.Logf("  %s", l)
	}

	truncatedPart3 := false
	for _, line := range strings.Split(captured, "\n") {
		if strings.Contains(line, softwaretechnologiePart3NodeID) && strings.Contains(line, "did not add any files") {
			truncatedPart3 = true
			t.Logf("warning line: %s", line)
		}
	}
	t.Logf("Part-3 truncation reproduced: %v", truncatedPart3)
}
