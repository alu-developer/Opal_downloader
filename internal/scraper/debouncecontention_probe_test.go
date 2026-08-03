package scraper

// docs/sync-speed-model.md, Frage 16 (prediction written 2026-08-03, before
// this file existed - see that document's "Nächstes Experiment").
//
// Frage 14 and 15 validated a 150ms mutationObserverDebounceMs against the
// small and then the large course, both at course_concurrency=1, with
// byte-identical file sets. Neither tested contention, and contention is
// where every real data loss in this campaign happened
// (docs/sync-speed-campaign.md: course_concurrency=2 lost 9 files on
// 2026-07-26).
//
// Frage 15 could not test it, and the reason is a property of the override
// rather than of the probe: contentSettleWaitBudget() (navigation.go line
// 397) checks OPAL_DEBOUNCE_MS_OVERRIDE *before* the
// effectiveCourseConcurrency() > 1 branch, so setting SetCourseConcurrency(2)
// on a single-course run only changes which unused branch would have been
// taken - no second tab ever renders alongside. Real contention needs two
// courses actually crawled at the same time, which is why this probe drives
// collectCourseFilesConcurrently (orchestrator.go line 437) directly rather
// than reusing debounceoverride_probe_test.go's single-course
// sc.collectCourseFiles call.
//
// What this compares is therefore NOT 150ms against 300ms. With the override
// set, contentSettleWaitBudget returns (ms, mutationObserverHardCapMs) - the
// *serial* 4000ms hard cap, not the concurrent 6000ms one. So an override run
// under concurrency=2 runs 150ms/4000ms against a baseline of 500ms/6000ms:
// two budgets lowered at once. If a difference shows up, that is the first
// thing to separate (rerun with 150ms debounce and an explicitly restored
// 6000ms cap), because "150ms is too short" and "4000ms is too short" have
// different fixes.
//
// Prediction (docs/sync-speed-model.md, written before this ran): no file
// difference in any direction - not between the two runs of a condition, not
// between conditions - because the debounce measures quiet *after the last
// mutation* rather than absolute time, so contention shifts the window later
// instead of truncating it. Failed by any file difference at all; under
// contention a single missing file is an answer, not measurement noise.
//
// Usage:
//
//	OPAL_DEBOUNCE_CONTENTION_TRACE=1 go test ./internal/scraper/ \
//	  -run TestDebounceOverrideUnderContention -v -timeout 60m

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
)

func TestDebounceOverrideUnderContention(t *testing.T) {
	if os.Getenv("OPAL_DEBOUNCE_CONTENTION_TRACE") == "" {
		t.Skip("set OPAL_DEBOUNCE_CONTENTION_TRACE=1 to run the real-account contention probe (docs/sync-speed-model.md Frage 16)")
	}
	captureProbeLogs(t) // see probelogging_test.go for the day a suppressed Warn cost

	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Small + large on purpose, the same pair Frage 15 used: two courses of
	// very different section counts keep both workers busy for an overlapping
	// stretch instead of one finishing before the other has opened its tab,
	// which is what makes the contention real rather than nominal.
	courseNames := []string{"Algorithmen und Datenstrukturen", "Softwaretechnologie (SoSe 26)"}
	if v := os.Getenv("OPAL_DEBOUNCE_CONTENTION_COURSES"); v != "" {
		courseNames = strings.Split(v, ",")
		for i := range courseNames {
			courseNames[i] = strings.TrimSpace(courseNames[i])
		}
	}
	overrideMs := os.Getenv("OPAL_DEBOUNCE_CONTENTION_VALUE")
	if overrideMs == "" {
		overrideMs = "150"
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()

	// The condition under test. Set before ensureSession so every code path
	// below sees the same concurrency the production crawl would.
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

	say("contention probe: %d courses at course_concurrency=2, override value %sms", len(courses), overrideMs)
	for _, c := range courses {
		say("  course: %s", c.Title)
	}

	type run struct {
		label          string
		urls           map[string]bool
		settleStableMs float64
		fileCount      int
		wallSeconds    float64
	}

	doRun := func(label, envValue string) run {
		if envValue == "" {
			os.Unsetenv("OPAL_DEBOUNCE_MS_OVERRIDE")
		} else {
			os.Setenv("OPAL_DEBOUNCE_MS_OVERRIDE", envValue)
		}
		defer os.Unsetenv("OPAL_DEBOUNCE_MS_OVERRIDE")

		_, beforeSettle, beforeStable, _ := sc.sectionTiming.snapshot()
		start := time.Now()

		// Mirrors the production concurrent path (orchestrator.go line 85):
		// page tracking must be suspended, because each course gets its own
		// throwaway tab and trackActivePage would otherwise retarget the
		// shared s.page at whichever tab opened last - already closed by the
		// time anything else uses it.
		sc.suspendPageTracking()
		files := collectCourseFilesConcurrently(
			context.Background(),
			courses,
			2,
			sc.newCourseFileCollector(len(courses)),
			nil,
		)
		sc.resumePageTracking()

		elapsed := time.Since(start)
		_, afterSettle, afterStable, _ := sc.sectionTiming.snapshot()
		settleStable := (afterSettle - beforeSettle) + (afterStable - beforeStable)

		urls := map[string]bool{}
		for _, f := range files {
			urls[f.URL] = true
		}
		say("%s: %d files, wall clock %.1fs, settle+stable %.0fms",
			label, len(files), elapsed.Seconds(), float64(settleStable.Milliseconds()))
		return run{
			label:          label,
			urls:           urls,
			settleStableMs: float64(settleStable.Milliseconds()),
			fileCount:      len(files),
			wallSeconds:    elapsed.Seconds(),
		}
	}

	diff := func(a, b run) []string {
		var lines []string
		for u := range a.urls {
			if !b.urls[u] {
				lines = append(lines, fmt.Sprintf("  only in %s: %s", a.label, u))
			}
		}
		for u := range b.urls {
			if !a.urls[u] {
				lines = append(lines, fmt.Sprintf("  only in %s: %s", b.label, u))
			}
		}
		sort.Strings(lines)
		return lines
	}

	// Order matters: baseline first, so a session that dies partway still
	// leaves the *unchanged* configuration measured rather than only the
	// experimental one.
	base1 := doRun("baseline-1 (500ms/6000ms, concurrency=2)", "")
	base2 := doRun("baseline-2 (500ms/6000ms, concurrency=2)", "")
	over1 := doRun("override-1 ("+overrideMs+"ms/4000ms, concurrency=2)", overrideMs)
	over2 := doRun("override-2 ("+overrideMs+"ms/4000ms, concurrency=2)", overrideMs)

	baseSelfDiff := diff(base1, base2)
	if len(baseSelfDiff) == 0 {
		say("baseline self-consistency: identical file sets across both runs (%d files)", base1.fileCount)
	} else {
		say("baseline self-consistency: %d difference(s):", len(baseSelfDiff))
		report = append(report, baseSelfDiff...)
	}

	overSelfDiff := diff(over1, over2)
	if len(overSelfDiff) == 0 {
		say("override self-consistency: identical file sets across both runs (%d files)", over1.fileCount)
	} else {
		say("override self-consistency: %d difference(s):", len(overSelfDiff))
		report = append(report, overSelfDiff...)
	}

	crossDiff := diff(base1, over1)
	if len(crossDiff) == 0 {
		say("baseline vs override: identical file sets (%d files)", base1.fileCount)
	} else {
		say("baseline vs override: %d difference(s):", len(crossDiff))
		report = append(report, crossDiff...)
	}

	avgBaseMs := (base1.settleStableMs + base2.settleStableMs) / 2
	avgOverMs := (over1.settleStableMs + over2.settleStableMs) / 2
	say("avg settle+stable: baseline=%.0fms, override=%.0fms", avgBaseMs, avgOverMs)
	if avgBaseMs > 0 {
		say("savings: %.0fms (%.1f%% of baseline settle+stable)", avgBaseMs-avgOverMs, (avgBaseMs-avgOverMs)/avgBaseMs*100)
	}
	avgBaseWall := (base1.wallSeconds + base2.wallSeconds) / 2
	avgOverWall := (over1.wallSeconds + over2.wallSeconds) / 2
	say("avg wall clock: baseline=%.1fs, override=%.1fs", avgBaseWall, avgOverWall)

	if len(baseSelfDiff) == 0 && len(overSelfDiff) == 0 && len(crossDiff) == 0 {
		say("VERDICT: no difference under real contention - Frage 16's correctness prediction holds for this course pair on this run.")
	} else {
		say("VERDICT: at least one difference found. Before concluding '150ms is too short', rerun with the debounce lowered but the hard cap restored to 6000ms - the override lowers both at once (see this file's header comment), and the two have different fixes.")
	}

	outDir := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else {
		outPath := filepath.Join(outDir, "debounce-contention-probe.txt")
		header := "debounce contention probe (Frage 16) recorded " + time.Now().Format(time.RFC3339) + "\n\n"
		if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
			t.Logf("could not write %s, result is in this log only: %v", outPath, err)
		} else {
			t.Logf("findings written to %s", outPath)
		}
	}
}
