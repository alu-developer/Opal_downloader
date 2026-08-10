package scraper

// docs/sync-speed-model.md, Question 19 ("Next experiment", written
// 2026-08-03): when the paginated "Vorlesung" folder (CourseNode/
// 1775615795226691003, Algorithmen und Datenstrukturen) loses its tail under
// course_concurrency=2 contention - the same 6 files, twice out of four runs
// in Question 16/17's archived data - does the click's own Wicket
// AJAX_CALL_DONE signal arrive (Candidate B: the read just happens too
// early) or never arrive at all (Candidate A, reopened: the click's AJAX
// request itself gets dropped under contention even though the earlier
// "control could not be activated" check, which only covers Playwright's own
// Click() call, does not fire)?
//
// This probe does not add new production behaviour. The commit before this
// one added the one thing needed to answer it: expandShowAllInSection
// (crawl.go) now logs expansionSignalled itself via auditLog(--debug-clicks)
// - previously it was only ever visible indirectly, through whether the
// settle wait got skipped. This test just runs the known-failing condition
// with that logging turned on and correlates it with the file count.
//
// Prediction (docs/sync-speed-model.md, written before this file existed):
// the failing run(s) show expansionSignalled=true for the Vorlesung section -
// Wicket says the call finished - and the poll trace right after it
// (showall-expand-poll) reads back the same candidate count for the whole
// budget, i.e. the rows genuinely never arrive rather than arriving after a
// read that already gave up.
//
// Counts as failed at: any run where expansionSignalled=false for a section
// that then loses files - that reopens Candidate A (the click's AJAX request
// itself did not survive contention) and moves the fix to the click/arm
// sequence rather than to the wait after it.
//
// Usage:
//
//	OPAL_SHOWALL_SIGNAL_TRACE=1 go test ./internal/scraper/ \
//	  -run TestShowAllSignalUnderContention -v -timeout 30m
import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/logging"
)

// vorlesungNodeID is the known-paginated folder Question 16/17 traced the
// loss to. Grepping the debug-click trace for this substring isolates its
// lines from the dozens of other sections visited in the same contention run.
const vorlesungNodeID = "1775615795226691003"

// Capture groups: 1=section URL, 2=watchArmed, 3=expansionSignalled,
// 4=signalMs (Question 21 - the elapsed time from arm to resolve, -1 when
// watchArmed=false; do NOT assume this is the timeout budget when
// expansionSignalled=false - live data shows the wait can also error out
// fast, see signalWaitErr), 5=signalWaitErr (classifyWicketWaitError,
// wicket.go - "none"/"timeout"/"context-destroyed"/"navigation"/"closed"/
// "other"), 6=candidates before expansion.
var wicketSignalLineRE = regexp.MustCompile(`\[audit\] .*kind=wicket-expand-signal.*reason="section ([^"]*): watchArmed=(\w+) expansionSignalled=(\w+) signalMs=(-?\d+) signalWaitErr=(\S+) \((\d+) candidates before expansion\)"`)

func TestShowAllSignalUnderContention(t *testing.T) {
	if os.Getenv("OPAL_SHOWALL_SIGNAL_TRACE") == "" {
		t.Skip("set OPAL_SHOWALL_SIGNAL_TRACE=1 to run the real-account show-all signal probe (docs/sync-speed-model.md Question 19)")
	}
	// This probe sets up its own logging rather than calling beginLiveProbe,
	// so it has to take the overlap guard itself. See probeoverlap_test.go.
	requireQuietAccount(t)

	// Capture into a buffer (for grepping) in addition to t.Logf (for
	// visibility while the run is in progress) - beginLiveProbe
	// (probelogging_test.go) only does the latter.
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

	courseNames := []string{"Algorithmen und Datenstrukturen", "Softwaretechnologie (SoSe 26)"}
	if v := os.Getenv("OPAL_SHOWALL_SIGNAL_COURSES"); v != "" {
		courseNames = strings.Split(v, ",")
		for i := range courseNames {
			courseNames[i] = strings.TrimSpace(courseNames[i])
		}
	}
	runs := 3
	if v := os.Getenv("OPAL_SHOWALL_SIGNAL_RUNS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			runs = n
		}
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()
	sc.SetDebugClicks(true)
	sc.SetCourseConcurrency(2)

	if err := sc.ensureSession(false); err != nil {
		t.Fatalf("ensureSession: %v", err)
	}

	courses, err := sc.discoverCourseLinks(courseNames)
	if err != nil {
		t.Fatalf("discoverCourseLinks: %v", err)
	}
	if len(courses) < 2 {
		var got []string
		for _, c := range courses {
			got = append(got, c.Title)
		}
		t.Fatalf("need 2 courses for a contention run, found %d (%v) for filter %v", len(courses), got, courseNames)
	}

	var report []string
	say := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		report = append(report, line)
		t.Log(line)
	}

	say("show-all signal probe (Question 19): %d courses at course_concurrency=2, %d run(s)", len(courses), runs)
	for _, c := range courses {
		say("  course: %s", c.Title)
	}

	reproduced := 0
	for i := 1; i <= runs; i++ {
		logBuf.Reset()
		say("")
		say("--- run %d/%d ---", i, runs)

		sc.suspendPageTracking()
		files := collectCourseFilesConcurrently(
			context.Background(),
			courses,
			2,
			sc.newCourseFileCollector(len(courses)),
			nil,
		)
		sc.resumePageTracking()

		say("run %d: %d files", i, len(files))

		captured := logBuf.String()
		signalLines := wicketSignalLineRE.FindAllStringSubmatch(captured, -1)
		var vorlesungSignal string
		for _, m := range signalLines {
			if strings.Contains(m[1], vorlesungNodeID) {
				vorlesungSignal = fmt.Sprintf("watchArmed=%s expansionSignalled=%s signalMs=%s signalWaitErr=%s (%s candidates before expansion)", m[2], m[3], m[4], m[5], m[6])
			}
		}

		truncatedVorlesung := false
		for _, line := range strings.Split(captured, "\n") {
			if strings.Contains(line, vorlesungNodeID) && strings.Contains(line, "expansion completed but added no files") {
				truncatedVorlesung = true
				say("  warnShowAllTruncated fired for the Vorlesung node: %s", strings.TrimSpace(line))
			}
		}

		if vorlesungSignal == "" {
			say("  Vorlesung node (%s) did not show a wicket-expand-signal line this run (section may not have needed expansion, or was reached via direct navigation instead of a click)", vorlesungNodeID)
		} else {
			say("  Vorlesung node (%s) signal: %s", vorlesungNodeID, vorlesungSignal)
		}

		if truncatedVorlesung {
			reproduced++
			say("  REPRODUCED the loss this run.")
			if vorlesungSignal == "" {
				say("  -> no signal line captured for this section at all - inspect the raw log below.")
			} else if strings.Contains(vorlesungSignal, "expansionSignalled=true") {
				say("  -> Candidate B confirmed this run: Wicket said the call finished, but the row count never grew afterward.")
			} else {
				say("  -> Candidate A reopened this run: the signal never arrived (expansionSignalled=false) even though the click was dispatched.")
			}
		} else {
			say("  no loss this run (or a different section was affected - see the raw log if the total file count looks off).")
		}
	}

	say("")
	say("VERDICT: reproduced the Vorlesung-node loss in %d of %d runs.", reproduced, runs)
	if reproduced == 0 {
		say("  Could not reproduce this cycle - the failure is intermittent by the campaign's own prior data (2 of 4 in Question 16/17); rerun with OPAL_SHOWALL_SIGNAL_RUNS set higher another cycle rather than concluding it is fixed.")
	}

	outDir := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else {
		outPath := filepath.Join(outDir, "showall-signal-probe.txt")
		header := "show-all signal probe (Question 19) recorded " + time.Now().Format(time.RFC3339) + "\n\n"
		if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
			t.Logf("could not write %s, result is in this log only: %v", outPath, err)
		} else {
			t.Logf("findings written to %s", outPath)
		}
	}
}

// multiWriter fans logging output out to both t.Logf (so the run is visible
// live under -v) and an in-memory buffer (so this test can grep its own
// audit lines back out afterward - beginLiveProbe, probelogging_test.go,
// only does the former).
type multiWriter struct {
	t   *testing.T
	buf *bytes.Buffer
}

func (w *multiWriter) Write(p []byte) (int, error) {
	w.t.Helper()
	w.t.Logf("%s", bytes.TrimRight(p, "\r\n"))
	return w.buf.Write(p)
}
