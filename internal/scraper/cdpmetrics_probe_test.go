package scraper

// docs/sync-speed-model.md, Question 13 ("Next experiment", written
// 2026-08-02): three independently measured candidates for the settle+stable
// wait (network transfer 24-31%, section file count linear r=0.40-0.46,
// section file count quadratic r=0.54) all land in the same "real but
// minority" bucket - none dominant. This is the source-predicted next step
// (Question 7, Question 10): real browser profiling instead of more source-reading
// or network tracing.
//
// Full CDP Tracing.start (Chrome-Trace-JSON via Tracing.dataCollected/
// tracingComplete stream events) would need an offline trace parser this
// project doesn't have. Performance.enable + Performance.getMetrics() is a
// lighter member of the same CDP domain family: a single synchronous call
// (no stream) returning cumulative-since-enable counters for ScriptDuration,
// LayoutDuration, RecalcStyleDuration, TaskDuration - enough to answer the
// same qualitative question ("does the time sit in script, layout, style
// recalc, or none of those") without new parsing infrastructure.
//
// Targets the small course's already-known slowest section ("Vorlesung",
// Algorithmen und Datenstrukturen, 44 candidates / 533 mutations per Question
// 11) rather than a third big-course crawl today - no new server load beyond
// what a 6-section BFS walk already costs (docs/server-load.md).
//
// Prediction (written before this ran, docs/sync-speed-model.md "Next
// experiment"): LayoutDuration+RecalcStyleDuration+ScriptDuration together
// make up a real but non-dominant 20-40% of settle+stable time for the
// slowest section, in the same range as the already-measured network and
// file-count candidates. Qualitative failure criterion (not just a
// threshold, per Question 12's lesson): >50% = CPU work is the previously
// missed dominant driver; ~20-40% = confirmed as another minority
// explanation, pointing at the 300ms debounce constant itself as the
// remaining suspect; <10% = strong case the time is the debounce constant,
// not measurable browser work at all.
//
// Usage: OPAL_CDP_METRICS_TRACE=1 go test ./internal/scraper/ -run TestCDPPerformanceMetricsAcrossSections -v -timeout 5m
import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

// cdpPerformanceMetrics reads back Performance.getMetrics() as a name->value
// map. CDPSession.Send returns `any` (a generic decoded JSON value), not a
// typed struct - there is no generated binding for this raw protocol method,
// same situation Question 3 already found for Fetch.enable.
func cdpPerformanceMetrics(session playwright.CDPSession) (map[string]float64, error) {
	raw, err := session.Send("Performance.getMetrics", map[string]any{})
	if err != nil {
		return nil, err
	}
	result := map[string]float64{}
	top, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("unexpected Performance.getMetrics reply shape: %T", raw)
	}
	list, ok := top["metrics"].([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected Performance.getMetrics 'metrics' field shape: %T", top["metrics"])
	}
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		value, _ := entry["value"].(float64)
		if name != "" {
			result[name] = value
		}
	}
	return result, nil
}

func TestCDPPerformanceMetricsAcrossSections(t *testing.T) {
	if os.Getenv("OPAL_CDP_METRICS_TRACE") == "" {
		t.Skip("set OPAL_CDP_METRICS_TRACE=1 to run the real-account CDP performance-metrics probe (docs/sync-speed-model.md Question 13)")
	}
	captureProbeLogs(t) // see probelogging_test.go for the day a suppressed Warn cost

	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	courseName := os.Getenv("OPAL_CDP_METRICS_COURSE")
	if courseName == "" {
		courseName = "Algorithmen und Datenstrukturen"
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

	newSession := func(p playwright.Page) playwright.CDPSession {
		session, err := sc.getContext().NewCDPSession(p)
		if err != nil {
			t.Fatalf("NewCDPSession: %v", err)
		}
		if _, err := session.Send("Performance.enable", map[string]any{}); err != nil {
			t.Fatalf("Performance.enable: %v", err)
		}
		return session
	}
	session := newSession(page)

	var report []string
	say := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		report = append(report, line)
		t.Log(line)
	}

	type sectionResult struct {
		title          string
		candidates     int
		settleStableMs float64
		scriptMs       float64
		layoutMs       float64
		recalcStyleMs  float64
		taskMs         float64
		cpuMs          float64 // script+layout+recalcStyle, the metrics named in the prediction
		cpuPctOfSettle float64
	}
	var results []sectionResult

	rootKey := sectionKey(course.URL, course.RepoID)
	queue := []string{course.URL}
	queued := map[string]struct{}{rootKey: {}}
	visited := map[string]struct{}{}
	sectionTitles := map[string]string{rootKey: course.Title}
	const maxSections = 20 // guard, not the primary bound - small course has 6 known sections
	sectionsWalked := 0

	for len(queue) > 0 && sectionsWalked < maxSections {
		currentURL := queue[0]
		queue = queue[1:]
		currentKey := sectionKey(currentURL, course.RepoID)
		delete(queued, currentKey)
		if _, ok := visited[currentKey]; ok {
			continue
		}
		visited[currentKey] = struct{}{}
		title := sectionTitles[currentKey]

		before, err := cdpPerformanceMetrics(session)
		if err != nil {
			say("section %q: Performance.getMetrics (before) failed: %v - CDP session likely detached, recreating", title, err)
			session = newSession(page)
			before, err = cdpPerformanceMetrics(session)
			if err != nil {
				say("section %q: still failing after session recreate: %v - skipping this section", title, err)
				continue
			}
		}
		_, beforeSettle, beforeStable, _ := sc.sectionTiming.snapshot()

		newPage, visit := sc.visitSection(page, currentURL, title)
		sectionsWalked++
		if newPage != page {
			// visitSection recovered from a page crash - the CDP session is
			// bound to the old target and is now dead. Rare on this small,
			// already-healthy course, but silently keeping a stale session
			// would make every metric after this point read as flat/zero
			// instead of failing loudly.
			say("section %q: page changed (crash recovery) - recreating CDP session on the new page", title)
			page = newPage
			session = newSession(page)
		}
		if visit.failed {
			say("section %q (%s): visitSection FAILED - skipped", title, currentURL)
			queue, _ = appendSectionFolderTargets(queue, queued, visited, visit.candidates, sc.opalURL, course.RepoID, currentURL, course.URL, course.Title, sectionTitles, false)
			continue
		}

		_, afterSettle, afterStable, _ := sc.sectionTiming.snapshot()
		settleStable := (afterSettle - beforeSettle) + (afterStable - beforeStable)

		after, err := cdpPerformanceMetrics(session)
		if err != nil {
			say("section %q: Performance.getMetrics (after) failed: %v - skipping this section's CPU numbers", title, err)
			queue, _ = appendSectionFolderTargets(queue, queued, visited, visit.candidates, sc.opalURL, course.RepoID, currentURL, course.URL, course.Title, sectionTitles, false)
			continue
		}

		// CDP durations are in seconds; convert to ms to match settleStable.
		deltaMs := func(name string) float64 {
			return (after[name] - before[name]) * 1000
		}
		scriptMs := deltaMs("ScriptDuration")
		layoutMs := deltaMs("LayoutDuration")
		recalcMs := deltaMs("RecalcStyleDuration")
		taskMs := deltaMs("TaskDuration")
		cpuMs := scriptMs + layoutMs + recalcMs

		r := sectionResult{
			title:          title,
			candidates:     len(visit.candidates),
			settleStableMs: float64(settleStable.Milliseconds()),
			scriptMs:       scriptMs,
			layoutMs:       layoutMs,
			recalcStyleMs:  recalcMs,
			taskMs:         taskMs,
			cpuMs:          cpuMs,
		}
		if r.settleStableMs > 0 {
			r.cpuPctOfSettle = cpuMs / r.settleStableMs * 100
		}
		results = append(results, r)

		say("section %q (%s): %d candidates, settle+stable=%.0fms, Script=%.1fms Layout=%.1fms RecalcStyle=%.1fms Task=%.1fms -> CPU(Script+Layout+RecalcStyle)=%.1fms (%.1f%% of settle+stable)",
			title, currentURL, r.candidates, r.settleStableMs, scriptMs, layoutMs, recalcMs, taskMs, cpuMs, r.cpuPctOfSettle)

		queue, _ = appendSectionFolderTargets(queue, queued, visited, visit.candidates, sc.opalURL, course.RepoID, currentURL, course.URL, course.Title, sectionTitles, false)
	}

	say("--- %d sections walked ---", sectionsWalked)

	if len(results) == 0 {
		say("RESULT: no sections produced usable metrics - inconclusive.")
	} else {
		// The slowest section by settle+stable time is the one the
		// prediction was written against ("Vorlesung", per Question 11 the
		// section with the most candidates/mutations in this course).
		slowest := results[0]
		for _, r := range results[1:] {
			if r.settleStableMs > slowest.settleStableMs {
				slowest = r
			}
		}
		say("SLOWEST SECTION: %q, %d candidates, settle+stable=%.0fms, CPU(Script+Layout+RecalcStyle)=%.1fms = %.1f%% of settle+stable",
			slowest.title, slowest.candidates, slowest.settleStableMs, slowest.cpuMs, slowest.cpuPctOfSettle)

		var totalSettle, totalCPU float64
		for _, r := range results {
			totalSettle += r.settleStableMs
			totalCPU += r.cpuMs
		}
		var aggPct float64
		if totalSettle > 0 {
			aggPct = totalCPU / totalSettle * 100
		}
		say("AGGREGATE ACROSS ALL SECTIONS: CPU(Script+Layout+RecalcStyle)=%.1fms of %.1fms settle+stable = %.1f%%", totalCPU, totalSettle, aggPct)

		verdictFor := func(pct float64, label string) {
			switch {
			case pct > 50:
				say("VERDICT (%s): >50%% - prediction refuted in the informative direction. CPU work (script/layout/style-recalc) is the previously-missed dominant driver.", label)
			case pct >= 20 && pct <= 40:
				say("VERDICT (%s): ~20-40%% - prediction confirmed. Another real-but-minority explanation; the 300ms debounce constant itself becomes the leading suspect for the remaining majority.", label)
			case pct < 10:
				say("VERDICT (%s): <10%% - strong case the time is the debounce constant, not measurable browser work.", label)
			default:
				say("VERDICT (%s): %.1f%% - outside the predicted 20-40%% band but not >50%% or <10%% either; read as a partial/ambiguous result, not a clean confirm or refute.", label, pct)
			}
		}
		verdictFor(slowest.cpuPctOfSettle, "slowest section")
		verdictFor(aggPct, "aggregate")
	}

	outDir := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else {
		outPath := filepath.Join(outDir, "cdp-performance-metrics-probe.txt")
		header := "CDP performance metrics probe recorded " + time.Now().Format(time.RFC3339) + "\n\n"
		if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
			t.Logf("could not write %s, result is in this log only: %v", outPath, err)
		} else {
			t.Logf("findings written to %s", outPath)
		}
	}
}
