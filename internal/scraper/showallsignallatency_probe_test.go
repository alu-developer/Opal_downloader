package scraper

// docs/sync-speed-model.md, Question 21 ("Next experiment", written
// 2026-08-04): Question 20 raised the Wicket expansion signal timeout to
// 15000ms and got 3 clean runs in a row - not proof of "pure delay" at this
// condition's own ~33-50% historical failure rate, because a boolean
// (signalled within N ms, yes/no) cannot distinguish "always fast, this run
// was lucky" from "usually fast, occasionally spikes past 4000ms, and
// today's spike (if any) happened to land under 15000ms".
//
// The commit before this one made expandShowAllInSection (crawl.go) log the
// actual elapsed time from arming the watch to the watch resolving
// (signalMs, wicket-expand-signal audit line) rather than just the boolean.
// This probe collects that number across repeated contention runs of the
// Vorlesung node (CourseNode/1775615795226691003, the same paginated folder
// Questions 16-20 all used) to see whether the distribution is bimodal (a
// tight cluster near the previously-measured 156-184ms plus a handful of
// outliers running into the seconds - something occasionally blocks
// outright) or a smooth, unimodal spread (ordinary queueing delay under
// load).
//
// Prediction (docs/sync-speed-model.md, written before this file existed):
// bimodal - most runs cluster near 150-200ms, a minority stretch well past
// 500ms-1s, with no smooth gradient in between.
//
// Counts as failed at: a smooth, unimodal spread with no outliers beyond
// ~500ms-1s - that refutes the "something drops/blocks outright" hypothesis
// and points back to ordinary delay under load as the mechanism, now with a
// number instead of a guess.
//
// Deliberately cheap and deliberately small per invocation: this sub-thread
// (Questions 19-21) had already spent 6 two-course contention crawls against
// the real account earlier the same day this file was written
// (docs/server-load.md). Defaults to 2 runs per invocation and appends to
// the same tmp file rather than overwriting it, so this is meant to be
// re-run across more than one cycle and accumulate a real distribution
// instead of one large batch.
//
// Usage:
//
//	OPAL_SIGNAL_LATENCY_TRACE=1 go test ./internal/scraper/ \
//	  -run TestWicketExpandSignalLatencyDistribution -v -timeout 30m
import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/logging"
)

func TestWicketExpandSignalLatencyDistribution(t *testing.T) {
	if os.Getenv("OPAL_SIGNAL_LATENCY_TRACE") == "" {
		t.Skip("set OPAL_SIGNAL_LATENCY_TRACE=1 to run the real-account signal-latency probe (docs/sync-speed-model.md Question 21)")
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

	courseNames := []string{"Algorithmen und Datenstrukturen", "Softwaretechnologie (SoSe 26)"}
	if v := os.Getenv("OPAL_SIGNAL_LATENCY_COURSES"); v != "" {
		courseNames = strings.Split(v, ",")
		for i := range courseNames {
			courseNames[i] = strings.TrimSpace(courseNames[i])
		}
	}
	runs := 2
	if v := os.Getenv("OPAL_SIGNAL_LATENCY_RUNS"); v != "" {
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

	say("wicket expand-signal latency probe (Question 21): %d courses at course_concurrency=2, %d run(s)", len(courses), runs)
	for _, c := range courses {
		say("  course: %s", c.Title)
	}

	type sample struct {
		ms        int64
		signalled bool
		waitErr   string
		truncated bool
	}
	var samples []sample

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
		found := false
		for _, m := range signalLines {
			if !strings.Contains(m[1], vorlesungNodeID) {
				continue
			}
			found = true
			ms, convErr := strconv.ParseInt(m[4], 10, 64)
			if convErr != nil {
				say("  could not parse signalMs from %q: %v", m[4], convErr)
				continue
			}
			signalled := m[3] == "true"
			waitErr := m[5]

			truncated := false
			for _, line := range strings.Split(captured, "\n") {
				if strings.Contains(line, vorlesungNodeID) && strings.Contains(line, "expansion completed but added no files") {
					truncated = true
				}
			}

			samples = append(samples, sample{ms: ms, signalled: signalled, waitErr: waitErr, truncated: truncated})
			say("  Vorlesung node signal: watchArmed=%s expansionSignalled=%s signalMs=%d signalWaitErr=%s truncated=%v", m[2], m[3], ms, waitErr, truncated)
		}
		if !found {
			say("  Vorlesung node (%s) did not show a wicket-expand-signal line this run", vorlesungNodeID)
		}
	}

	say("")
	say("--- this run's samples, sorted ---")
	sorted := make([]int64, len(samples))
	for i, s := range samples {
		sorted[i] = s.ms
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	for _, ms := range sorted {
		say("  %dms", ms)
	}
	outlierCount := 0
	for _, ms := range sorted {
		if ms > 500 {
			outlierCount++
		}
	}
	say("this run: %d/%d samples above 500ms.", outlierCount, len(samples))
	say("Accumulate across cycles before reading bimodal vs. smooth - this run alone is too few points for a real distribution (docs/sync-speed-model.md Question 21's own cost note); see tmp/signal-latency-probe.log for everything gathered so far.")

	outDir := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else {
		outPath := filepath.Join(outDir, "signal-latency-probe.log")
		f, err := os.OpenFile(outPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			t.Logf("could not open %s, result is in this log only: %v", outPath, err)
		} else {
			defer f.Close()
			header := "\n=== wicket expand-signal latency probe (Question 21) recorded " + time.Now().Format(time.RFC3339) + " ===\n\n"
			if _, err := f.WriteString(header + strings.Join(report, "\n") + "\n"); err != nil {
				t.Logf("could not write %s, result is in this log only: %v", outPath, err)
			} else {
				t.Logf("findings appended to %s", outPath)
			}
		}
	}
}
