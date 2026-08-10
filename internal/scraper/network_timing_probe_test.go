package scraper

// docs/sync-speed-model.md, Question 7 ("Next experiment", written
// 2026-07-31): source-reading (Question 1) found OPAL builds its section
// tree/file table as plain server HTML with no client-side templating - so
// there is nothing for the debounced MutationObserver wait
// (waitForContentSettled) to be waiting *for* in a "JS still assembling the
// DOM" sense. But the settle-wait is real, measured time (sectiontiming.go),
// so if it is not JS work, the leading remaining candidate is network
// transfer: OpenOLAT's tree renderer (MenuTreeRenderer.java, per the
// previous cycle's source read) ships the entire course's .o_tree HTML
// inline on every section-node page, so a bigger course should mean a
// bigger response - and (this experiment's actual question) more of the
// settle-wait actually spent waiting for bytes rather than for anything to
// render.
//
// Prediction (written before this ran, docs/sync-speed-model.md "Next
// experiment"): a section-page document response's size and Playwright-
// measured transfer duration correlate with course size (section count),
// and network time is a non-trivial share of the settle+stability wait
// actually spent inside the same course. Failed if response bodies stay
// small (few KB, well under 50ms transfer) regardless of course size while
// settle/stability wait stays 300ms+ per section - that would mean the time
// is not sitting in network transfer either, and the next step is real
// browser profiling, not more source-reading or network tracing.
//
// Method: crawl the account's smallest and largest content-bearing courses
// in the same run (docs/sync-speed-campaign.md), recording every main-frame
// document request's real transferred body size (Request.Sizes(),
// ResponseBodySize) and Playwright's own requestfinished timing
// (Request.Timing().ResponseEnd), alongside this package's sectionTiming
// aggregate (settle+stable wait actually spent), diffed per course via
// sectionTiming.snapshot.
//
// Sizes() (not the response's content-length header) on purpose. This
// probe's first draft used r.Headers()["content-length"], and a rerun
// against 2026-07-27's saved network-trace-Softwaretechnologie.txt (an
// earlier probe's output, network_trace_probe_test.go) showed why that is
// wrong before this one ever ran live: of 324 main-frame document
// responses in that trace, the recorded content-length total was 0 bytes.
// A Java/Wicket app streaming a dynamically-built page has no reason to
// buffer the whole body just to compute a Content-Length header first - it
// sends chunked transfer-encoding instead, which omits that header
// entirely. Reading it would have silently reported "0 bytes" for every
// section page in both courses and made Candidate A look refuted by an
// instrument that was never measuring anything.
//
// Sizes() does its own channel.Send RPC back into the browser process,
// which is why it is NOT called from inside OnResponse/OnRequestFinished
// here - network_trace_probe_test.go already found (2026-07-27, see its own
// doc comment) that issuing a round-trip call from inside an event handler
// deadlocks the dispatch loop the reply would arrive on (a live 55-minute
// hang). Request.Timing() has no such restriction (a synchronous field read
// populated by an internal handler before OnRequestFinished's callback
// fires), so it is read inside the handler; Sizes() is deferred until after
// collectCourseFiles returns for that course, when the dispatch loop is
// idle and a normal synchronous call is safe - the same reasoning the rest
// of this codebase already relies on for AllHeaders()/HeaderValue() calls
// outside a handler.
//
// Usage: OPAL_SETTLE_TIMING_TRACE=1 go test ./internal/scraper/ -run TestSettleTimingAgainstNetworkTransfer -v -timeout 10m
import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

func TestSettleTimingAgainstNetworkTransfer(t *testing.T) {
	if os.Getenv("OPAL_SETTLE_TIMING_TRACE") == "" {
		t.Skip("set OPAL_SETTLE_TIMING_TRACE=1 to run the real-account network-timing probe")
	}
	beginLiveProbe(t) // see probelogging_test.go for the day a suppressed Warn cost

	// The package's working directory during `go test` is internal/scraper, so
	// the repo's own config.yaml is two levels up.
	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	// Deliberately the account's smallest and largest content-bearing courses
	// (docs/sync-speed-campaign.md), so a size effect on response bytes/
	// duration has room to show up. Override via env for a cheaper or
	// different pair.
	courseNames := []string{"Algorithmen und Datenstrukturen", "Softwaretechnologie (SoSe 26)"}
	if v := os.Getenv("OPAL_SETTLE_TIMING_COURSES"); v != "" {
		courseNames = strings.Split(v, ",")
		for i := range courseNames {
			courseNames[i] = strings.TrimSpace(courseNames[i])
		}
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()

	// sectionSamples collects one (settle+stable, candidateCount) tuple per
	// section across the whole run - docs/sync-speed-model.md Question 10,
	// added after Question 9 (2026-08-01) showed MenuTreeRenderer only
	// serializes open/selected-path tree nodes, not the whole course, so
	// "does settle-wait scale with course size" was answered (no) but
	// "does it scale with the file count of the section actually visited"
	// was not. sectionProbe (scraper.go) is nil in production; this test is
	// the only caller.
	type sectionSample struct {
		settleStable time.Duration
		candidates   int
	}
	var sectionSamples []sectionSample
	sc.sectionProbe = func(settle, stable time.Duration, candidates int) {
		sectionSamples = append(sectionSamples, sectionSample{settleStable: settle + stable, candidates: candidates})
	}

	if err := sc.ensureSession(false); err != nil {
		t.Fatalf("ensureSession: %v", err)
	}

	all, err := sc.discoverCourseLinks(nil)
	if err != nil {
		t.Fatalf("discoverCourseLinks: %v", err)
	}
	byName := map[string]CourseRef{}
	var allTitles []string
	for _, c := range all {
		byName[c.Title] = c
		allTitles = append(allTitles, c.Title)
	}

	page := sc.getPage()
	if page == nil {
		t.Fatalf("no page available after ensureSession")
	}

	// finishedMainDocs collects, in order, every main-frame document request
	// that finished - across the whole run, not one course. Each course's
	// slice is carved out afterwards via a start-index snapshot, the same
	// pattern network_trace_probe_test.go uses for its response log.
	var finishedMainDocs []playwright.Request

	mainFrame := page.MainFrame()
	page.OnRequestFinished(func(req playwright.Request) {
		if req.ResourceType() != "document" {
			return
		}
		if frame := req.Frame(); frame == nil || frame != mainFrame {
			return
		}
		finishedMainDocs = append(finishedMainDocs, req)
	})

	var report []string
	say := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		report = append(report, line)
		t.Log(line)
	}

	type courseSummary struct {
		title           string
		sections        int
		files           int
		docCount        int
		sizeErrors      int
		avgBytes        int64
		avgDurationMs   float64
		totalDurationMs float64
		settleWait      time.Duration
		stableWait      time.Duration
	}
	var summaries []courseSummary

	for _, name := range courseNames {
		course, ok := byName[name]
		if !ok {
			t.Fatalf("course %q not found; discovered courses are: %q", name, allTitles)
		}
		say("--- course %q (%s) ---", course.Title, course.URL)

		startIdx := len(finishedMainDocs)
		sectionStartIdx := len(sectionSamples)
		beforeSections, beforeSettle, beforeStable, _ := sc.sectionTiming.snapshot()

		_, files, _, err := sc.collectCourseFiles(page, course)
		if err != nil {
			t.Fatalf("collectCourseFiles(%q): %v", course.Title, err)
		}

		afterSections, afterSettle, afterStable, _ := sc.sectionTiming.snapshot()
		courseSections := sectionSamples[sectionStartIdx:]
		if len(courseSections) >= 2 {
			var minC, maxC int = -1, -1
			var xs, xsSquared, ys []float64
			for _, sample := range courseSections {
				if minC == -1 || sample.candidates < minC {
					minC = sample.candidates
				}
				if sample.candidates > maxC {
					maxC = sample.candidates
				}
				xs = append(xs, float64(sample.candidates))
				xsSquared = append(xsSquared, float64(sample.candidates*sample.candidates))
				ys = append(ys, float64(sample.settleStable.Milliseconds()))
			}
			say("  Question 10: %d sections, candidate count range %d-%d, settle+stable-vs-candidates Pearson r=%.2f",
				len(courseSections), minC, maxC, pearsonR(xs, ys))
			// Question 12 (docs/sync-speed-model.md, born from Question 11's finding
			// that mutation COUNT scales roughly with candidates^2, not
			// candidates): does settle+stable TIME follow the same quadratic
			// shape, which would explain Question 10's own weak linear r=0.40 as a
			// linear model underfitting a real quadratic relationship?
			say("  Question 12: settle+stable-vs-candidates^2 Pearson r=%.2f", pearsonR(xsSquared, ys))
		}
		// The dispatch loop is idle here (collectCourseFiles has returned), so
		// the Sizes() round trip below is safe - see the doc comment above.
		docs := finishedMainDocs[startIdx:]

		var totalBytes, totalDuration float64
		var byteN, durN int64
		var sizeErrors int
		for _, req := range docs {
			if timing := req.Timing(); timing != nil {
				totalDuration += timing.ResponseEnd
				durN++
			}
			sizes, err := req.Sizes()
			if err != nil {
				sizeErrors++
				continue
			}
			totalBytes += float64(sizes.ResponseBodySize)
			byteN++
		}
		var avgBytes int64
		if byteN > 0 {
			avgBytes = int64(totalBytes / float64(byteN))
		}
		var avgDur float64
		if durN > 0 {
			avgDur = totalDuration / float64(durN)
		}

		s := courseSummary{
			title:           course.Title,
			sections:        afterSections - beforeSections,
			files:           len(files),
			docCount:        len(docs),
			sizeErrors:      sizeErrors,
			avgBytes:        avgBytes,
			avgDurationMs:   avgDur,
			totalDurationMs: totalDuration,
			settleWait:      afterSettle - beforeSettle,
			stableWait:      afterStable - beforeStable,
		}
		summaries = append(summaries, s)

		say("  sections=%d files=%d document-responses=%d (Sizes() errors: %d)", s.sections, s.files, s.docCount, s.sizeErrors)
		say("  avg document response: %d bytes (real transferred body, Request.Sizes), %.0fms transfer (requestfinished timing)", s.avgBytes, s.avgDurationMs)
		say("  sum document transfer time: %.0fms", s.totalDurationMs)
		say("  section timing (this course, this package's own aggregate): settle=%s stable=%s", s.settleWait.Round(time.Millisecond), s.stableWait.Round(time.Millisecond))
		if denom := s.settleWait + s.stableWait; denom > 0 {
			say("  network transfer as %% of settle+stable wait: %.0f%%", s.totalDurationMs/float64(denom.Milliseconds())*100)
		}
	}

	say("--- comparison ---")
	if len(summaries) >= 2 {
		small, large := summaries[0], summaries[1]
		if large.sections < small.sections {
			small, large = large, small
		}
		say("smaller: %q (%d sections), avg doc %d bytes / %.0fms", small.title, small.sections, small.avgBytes, small.avgDurationMs)
		say("larger:  %q (%d sections), avg doc %d bytes / %.0fms", large.title, large.sections, large.avgBytes, large.avgDurationMs)
		if small.avgBytes > 0 {
			say("byte ratio (larger/smaller): %.1fx, section-count ratio: %.1fx", float64(large.avgBytes)/float64(small.avgBytes), float64(large.sections)/float64(small.sections))
		}
		if small.avgDurationMs > 0 {
			say("duration ratio (larger/smaller): %.1fx", large.avgDurationMs/small.avgDurationMs)
		}
	}

	// A failure to save is not a failure of the probe - the result is already
	// in the test log, and losing the copy is only as bad as the situation
	// before this existed. Say so rather than turning a good run red.
	outDir := filepath.Join("..", "..", "tmp")
	outPath := filepath.Join(outDir, "settle-timing-network-trace.txt")
	header := "settle-timing network trace recorded " + time.Now().Format(time.RFC3339) + "\n\n"
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
		t.Logf("could not write %s, result is in this log only: %v", outPath, err)
	} else {
		t.Logf("findings written to %s", outPath)
	}
}

// pearsonR is the sample Pearson correlation coefficient between xs and ys
// (equal length). Returns 0 for fewer than 2 points or zero variance in
// either series - this is a diagnostic probe, not a stats library, so
// "no signal" is reported as 0 rather than NaN/panic.
func pearsonR(xs, ys []float64) float64 {
	n := len(xs)
	if n < 2 || n != len(ys) {
		return 0
	}
	var sumX, sumY float64
	for i := 0; i < n; i++ {
		sumX += xs[i]
		sumY += ys[i]
	}
	meanX, meanY := sumX/float64(n), sumY/float64(n)

	var cov, varX, varY float64
	for i := 0; i < n; i++ {
		dx, dy := xs[i]-meanX, ys[i]-meanY
		cov += dx * dy
		varX += dx * dx
		varY += dy * dy
	}
	if varX == 0 || varY == 0 {
		return 0
	}
	return cov / math.Sqrt(varX*varY)
}
