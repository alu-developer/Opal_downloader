package scraper

// docs/sync-speed-model.md, Frage 14 ("Nächstes Experiment", written
// 2026-08-02, after Frage 13 found real browser CPU work explains at most
// ~24% of settle+stable time and the measured settle-wait average (326ms)
// sits almost exactly on the mutationObserverDebounceMs constant (300ms,
// navigation.go)): can that constant be lowered - the same shape change
// sectionContentPollIntervalMs already went through safely (400->150ms,
// crawl.go, 2026-07-21) - without losing files?
//
// That prior change's own doc comment carries an explicit, still-unresolved
// warning this probe takes seriously: "1 of 3 runs at the OLD 400ms setting
// silently lost 15 files (...). That intermittent loss is NOT proven fixed
// by this change; three clean runs are not enough to prove absence." A
// single clean comparison here would repeat exactly the kind of
// under-tested claim that warning is about. This probe instead runs the
// small course's full crawl twice at each setting (baseline/baseline and
// 150ms/150ms, not just baseline/150ms) so a flaky section shows up as
// self-inconsistency within a setting, not only as a baseline-vs-override
// difference.
//
// Uses OPAL_DEBOUNCE_MS_OVERRIDE (navigation.go, contentSettleWaitBudget) -
// off by default in production, so this probe is the only thing that sets
// it.
//
// Usage: OPAL_DEBOUNCE_OVERRIDE_TRACE=1 go test ./internal/scraper/ -run TestDebounceOverrideCorrectness -v -timeout 10m
import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
)

func TestDebounceOverrideCorrectness(t *testing.T) {
	if os.Getenv("OPAL_DEBOUNCE_OVERRIDE_TRACE") == "" {
		t.Skip("set OPAL_DEBOUNCE_OVERRIDE_TRACE=1 to run the real-account debounce-override probe (docs/sync-speed-model.md Frage 14)")
	}
	captureProbeLogs(t) // see probelogging_test.go for the day a suppressed Warn cost

	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	courseName := os.Getenv("OPAL_DEBOUNCE_OVERRIDE_COURSE")
	if courseName == "" {
		courseName = "Algorithmen und Datenstrukturen"
	}
	overrideMs := os.Getenv("OPAL_DEBOUNCE_OVERRIDE_VALUE")
	if overrideMs == "" {
		overrideMs = "150"
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()

	if err := sc.ensureSession(false); err != nil {
		t.Fatalf("ensureSession: %v", err)
	}

	courses, err := sc.discoverCourseLinks([]string{courseName})
	if err != nil {
		t.Fatalf("discoverCourseLinks: %v", err)
	}
	if len(courses) == 0 {
		t.Fatalf("course %q not found", courseName)
	}
	course := courses[0]
	t.Logf("probing course %q (%s)", course.Title, course.URL)

	page := sc.getPage()
	if page == nil {
		t.Fatalf("no page available after ensureSession")
	}

	var report []string
	say := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		report = append(report, line)
		t.Log(line)
	}

	type run struct {
		label          string
		urls           map[string]bool
		settleStableMs float64
		fileCount      int
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
		newPage, files, _, err := sc.collectCourseFiles(page, course)
		page = newPage
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("collectCourseFiles(%s): %v", label, err)
		}
		_, afterSettle, afterStable, _ := sc.sectionTiming.snapshot()
		settleStable := (afterSettle - beforeSettle) + (afterStable - beforeStable)

		urls := map[string]bool{}
		for _, f := range files {
			urls[f.URL] = true
		}
		say("%s: %d files, wall clock %.1fs, settle+stable %.0fms", label, len(files), elapsed.Seconds(), float64(settleStable.Milliseconds()))
		return run{label: label, urls: urls, settleStableMs: float64(settleStable.Milliseconds()), fileCount: len(files)}
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

	// OPAL_DEBOUNCE_OVERRIDE_SKIP_BASELINE: docs/sync-speed-model.md Frage 15
	// (2026-08-02) - re-running the 300ms baseline on a course that already
	// has a historical live-verified baseline (navigation.go's 2026-07-16
	// doc comment: 344/344 across all 9 courses including this one) is a
	// third confirmation of an already-established number, not new evidence.
	// Set to skip both baseline runs and compare override self-consistency
	// plus the historical file count instead - use only for a course whose
	// 300ms baseline is already separately documented as live-tested.
	skipBaseline := os.Getenv("OPAL_DEBOUNCE_OVERRIDE_SKIP_BASELINE") != ""

	var base1, base2 run
	if !skipBaseline {
		say("--- baseline (default %.0fms debounce), run 1 ---", mutationObserverDebounceMs)
		base1 = doRun("baseline-1", "")
		say("--- baseline, run 2 (self-consistency check) ---")
		base2 = doRun("baseline-2", "")
	} else {
		say("--- baseline SKIPPED (OPAL_DEBOUNCE_OVERRIDE_SKIP_BASELINE set) - relying on this course's already-documented 300ms baseline instead of re-measuring it ---")
	}
	say("--- override (%sms debounce), run 1 ---", overrideMs)
	over1 := doRun("override-1", overrideMs)
	say("--- override, run 2 (self-consistency check) ---")
	over2 := doRun("override-2", overrideMs)

	say("--- comparisons ---")
	var baseSelfDiff []string
	if !skipBaseline {
		baseSelfDiff = diff(base1, base2)
		if len(baseSelfDiff) == 0 {
			say("baseline self-consistency: IDENTICAL across both runs (%d files)", base1.fileCount)
		} else {
			say("baseline self-consistency: %d difference(s) between the two default-setting runs (flaky regardless of the override):", len(baseSelfDiff))
			report = append(report, baseSelfDiff...)
		}
	}

	overSelfDiff := diff(over1, over2)
	if len(overSelfDiff) == 0 {
		say("override self-consistency: IDENTICAL across both runs (%d files)", over1.fileCount)
	} else {
		say("override self-consistency: %d difference(s) between the two override-setting runs:", len(overSelfDiff))
		report = append(report, overSelfDiff...)
	}

	var crossDiff []string
	historicalMismatch := false
	if !skipBaseline {
		crossDiff = diff(base1, over1)
		if len(crossDiff) == 0 {
			say("baseline vs override (run 1 vs run 1): IDENTICAL file sets (%d files)", base1.fileCount)
		} else {
			say("baseline vs override (run 1 vs run 1): %d difference(s):", len(crossDiff))
			report = append(report, crossDiff...)
		}
	} else if historicalCount := os.Getenv("OPAL_DEBOUNCE_OVERRIDE_HISTORICAL_COUNT"); historicalCount != "" {
		if over1.fileCount != over2.fileCount {
			say("override file count differs between the two runs (%d vs %d) - already caught by override self-consistency above", over1.fileCount, over2.fileCount)
		}
		say("comparing against documented historical baseline count (%s files) instead of a fresh baseline run - see OPAL_DEBOUNCE_OVERRIDE_SKIP_BASELINE", historicalCount)
		var wantCount int
		fmt.Sscanf(historicalCount, "%d", &wantCount)
		if over1.fileCount != wantCount {
			say("MISMATCH: override run found %d files, historical baseline documents %d", over1.fileCount, wantCount)
			historicalMismatch = true
		} else {
			say("override file count matches documented historical baseline (%d)", wantCount)
		}
	}

	if !skipBaseline {
		avgBaseMs := (base1.settleStableMs + base2.settleStableMs) / 2
		avgOverMs := (over1.settleStableMs + over2.settleStableMs) / 2
		say("avg settle+stable: baseline=%.0fms, override=%.0fms", avgBaseMs, avgOverMs)
		if avgBaseMs > 0 {
			say("savings: %.0fms (%.1f%% of baseline settle+stable)", avgBaseMs-avgOverMs, (avgBaseMs-avgOverMs)/avgBaseMs*100)
		}
	} else {
		avgOverMs := (over1.settleStableMs + over2.settleStableMs) / 2
		say("avg settle+stable: override=%.0fms (no fresh baseline run this pass)", avgOverMs)
	}

	if len(baseSelfDiff) == 0 && len(overSelfDiff) == 0 && len(crossDiff) == 0 && !historicalMismatch {
		say("VERDICT: no file-count or file-identity regression found - correctness prediction holds for this course on this run. NOT sufficient alone to change the production default (single course, single day, per this project's own repeated-loss history) - see docs/sync-speed-model.md Frage 14/15 for what would be.")
	} else {
		say("VERDICT: at least one difference found somewhere above - do not treat the override as safe without reading exactly which section(s) differed and why.")
	}

	outDir := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else {
		outPath := filepath.Join(outDir, "debounce-override-probe.txt")
		header := "debounce override probe recorded " + time.Now().Format(time.RFC3339) + "\n\n"
		if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
			t.Logf("could not write %s, result is in this log only: %v", outPath, err)
		} else {
			t.Logf("findings written to %s", outPath)
		}
	}
}
