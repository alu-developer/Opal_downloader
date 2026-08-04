package scraper

// docs/sync-speed-model.md, Question 20 ("Next experiment", written
// 2026-08-04): Question 19 found expansionSignalled=false in both runs that
// lost the Vorlesung folder's tail under course_concurrency=2 contention -
// Wicket's AJAX_CALL_DONE never arrived within the current 4000ms budget.
// Two mechanisms are compatible with that one fact:
//
//   - Candidate A1 (pure delay): the call completes, just later than 4000ms
//     under contention. Fix: raise wicketExpansionSignalTimeoutMs.
//   - Candidate A2 (never issued/received): no amount of extra waiting helps,
//     because the call was never made or its response never reached this
//     page's Wicket runtime. Fix: at the click/arm sequence, not the wait.
//
// This probe raises the ceiling via the previous commit's
// OPAL_WICKET_SIGNAL_TIMEOUT_MS_OVERRIDE (crawl.go,
// effectiveWicketExpansionSignalTimeoutMs) for one diagnostic run and reuses
// Question 19's contention condition and signal-line grep.
//
// Prediction (docs/sync-speed-model.md, written before this file existed):
// a failing run under the raised ceiling still shows expansionSignalled=false
// at the moment warnShowAllTruncated would have fired at 4000ms, but flips to
// true before the raised ceiling - i.e. the call does complete, it is just
// slow under contention (Candidate A1).
//
// Counts as failed at: a run that reproduces the loss (warnShowAllTruncated
// fires for the Vorlesung node) while still showing expansionSignalled=false
// even against the raised ceiling - that refutes Candidate A1 and moves the
// mechanism to Candidate A2 (never issued/received), where the next
// instrumentation point is page.on("request")/page.on("response") around the
// click, not more timeout tuning.
//
// Usage:
//
//	OPAL_SHOWALL_SIGNAL_TIMEOUT_TRACE=1 go test ./internal/scraper/ \
//	  -run TestShowAllSignalTimeoutOverride -v -timeout 30m
import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/logging"
)

func TestShowAllSignalTimeoutOverride(t *testing.T) {
	if os.Getenv("OPAL_SHOWALL_SIGNAL_TIMEOUT_TRACE") == "" {
		t.Skip("set OPAL_SHOWALL_SIGNAL_TIMEOUT_TRACE=1 to run the real-account show-all signal-timeout probe (docs/sync-speed-model.md Question 20)")
	}

	overrideMs := os.Getenv("OPAL_SHOWALL_SIGNAL_TIMEOUT_VALUE")
	if overrideMs == "" {
		overrideMs = "15000"
	}
	os.Setenv("OPAL_WICKET_SIGNAL_TIMEOUT_MS_OVERRIDE", overrideMs)
	t.Cleanup(func() { os.Unsetenv("OPAL_WICKET_SIGNAL_TIMEOUT_MS_OVERRIDE") })

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

	say("show-all signal-timeout probe (Question 20): %d courses at course_concurrency=2, %d run(s), timeout override=%sms", len(courses), runs, overrideMs)
	for _, c := range courses {
		say("  course: %s", c.Title)
	}

	reproduced, delayConfirmed, delayRefuted := 0, 0, 0
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
				vorlesungSignal = fmt.Sprintf("watchArmed=%s expansionSignalled=%s signalMs=%s (%s candidates before expansion)", m[2], m[3], m[4], m[5])
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
			say("  Vorlesung node (%s) did not show a wicket-expand-signal line this run", vorlesungNodeID)
		} else {
			say("  Vorlesung node (%s) signal (%sms budget): %s", vorlesungNodeID, overrideMs, vorlesungSignal)
		}

		if truncatedVorlesung {
			reproduced++
			say("  REPRODUCED the loss this run, even with the %sms budget.", overrideMs)
			if strings.Contains(vorlesungSignal, "expansionSignalled=true") {
				say("  -> unexpected: signal arrived but the row count still didn't grow - a THIRD shape, downstream of the signal after all.")
			} else {
				delayRefuted++
				say("  -> Candidate A1 (pure delay) REFUTED this run: no signal even at %sms - the call is not merely slow, it never completes.", overrideMs)
			}
		} else if vorlesungSignal != "" && strings.Contains(vorlesungSignal, "expansionSignalled=true") {
			delayConfirmed++
			say("  no loss this run, signal arrived - consistent with Candidate A1 if this run would otherwise have failed, but not proof by itself (this run may simply have been a clean one, same as Question 19 run 1).")
		} else {
			say("  no loss this run (or a different section was affected).")
		}
	}

	say("")
	say("VERDICT: reproduced the loss in %d of %d runs even at the raised %sms budget; of those, %d showed no signal at all (Candidate A1 refuted), %d showed a signal with the loss still happening (a third shape).", reproduced, runs, overrideMs, delayRefuted, reproduced-delayRefuted)
	if reproduced == 0 {
		say("  Could not reproduce this cycle - consistent with Candidate A1 (pure delay, the raised budget masks it) but not proof on its own; the failure is intermittent even at the original budget (2 of 3 in Question 19), so a clean run here is expected some fraction of the time regardless of which candidate is true. Rerun with OPAL_SHOWALL_SIGNAL_RUNS raised, or cross-check timing: does this run's Vorlesung expansion take noticeably longer than 4000ms end-to-end?")
	} else if delayRefuted == reproduced {
		say("  Candidate A1 (pure delay) is refuted: even generous extra time does not produce the signal. Move to Candidate A2 - instrument page.on(\"request\")/page.on(\"response\") around the click to see whether the AJAX request is made at all.")
	}

	outDir := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else {
		outPath := filepath.Join(outDir, "showall-signal-timeout-probe.txt")
		header := "show-all signal-timeout probe (Question 20) recorded " + time.Now().Format(time.RFC3339) + "\n\n"
		if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
			t.Logf("could not write %s, result is in this log only: %v", outPath, err)
		} else {
			t.Logf("findings written to %s", outPath)
		}
	}
}
