package scraper

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

func TestAFreshWatchHasNotStalled(t *testing.T) {
	w := &stallWatch{}

	// Nothing has happened yet. A crawl that has not started cannot have
	// stalled, and reporting it as quiet "since the zero time" would fire a
	// warning on every single run before the first section is visited.
	quiet, _, _, _ := w.quietFor(time.Now())
	if quiet != 0 {
		t.Fatalf("a watch with no recorded progress reported %s of silence", quiet)
	}
}

func TestQuietForMeasuresFromTheLastProgress(t *testing.T) {
	w := &stallWatch{}
	w.note("Analysis I", "Woche 07", "https://example.test/opal/auth/CourseNode/1")

	start := time.Now()
	quiet, course, section, url := w.quietFor(start.Add(5 * time.Minute))

	if quiet < 4*time.Minute || quiet > 6*time.Minute {
		t.Errorf("expected roughly five minutes of silence, got %s", quiet)
	}
	if course != "Analysis I" || section != "Woche 07" {
		t.Errorf("the watch lost track of what was happening: course=%q section=%q", course, section)
	}
	if url == "" {
		t.Error("the section URL is the whole point - it is what makes a stall warning something you can act on")
	}
}

// Fields are only overwritten by events that carry them, so a progress event
// with no section (a course starting, say) does not erase the last section
// known - which is exactly the information wanted if it stalls right there.
func TestNoteKeepsTheLastKnownSection(t *testing.T) {
	w := &stallWatch{}
	w.note("Analysis I", "Woche 07", "https://example.test/opal/auth/CourseNode/1")
	w.note("Analysis I", "", "")

	_, _, section, url := w.quietFor(time.Now())
	if section != "Woche 07" {
		t.Errorf("a later event without a section erased the last one known, got %q", section)
	}
	if url == "" {
		t.Error("a later event without a URL erased the last one known")
	}
}

// The watchdog must survive a scraper that has none, since tests and any
// zero-value construction produce exactly that.
func TestWatchForStallToleratesAScraperWithoutOne(t *testing.T) {
	s := &OpalScraper{}
	stop := s.watchForStall(context.Background())
	stop() // must not panic or block
}

// Stopping must actually stop it, not leak a goroutine that keeps logging
// after the sync it was watching has finished.
func TestWatchForStallStopsWhenTold(t *testing.T) {
	s := New("", "")
	done := make(chan struct{})
	go func() {
		stop := s.watchForStall(context.Background())
		stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("stopping the stall watchdog blocked")
	}
}

// A CLI run registers no progress callback at all. If activity were only
// recorded alongside a callback, every CLI sync would look permanently
// stalled and log a warning every 30 seconds.
func TestProgressIsRecordedEvenWithNoCallbackRegistered(t *testing.T) {
	s := New("", "")

	s.publishProgress(DiscoveryProgress{
		Phase:      PhaseSection,
		Course:     "Analysis I",
		Section:    "Woche 07",
		SectionURL: "https://example.test/opal/auth/CourseNode/1",
	})

	quiet, course, section, _ := s.stall.quietFor(time.Now())
	if quiet > time.Second {
		t.Fatalf("progress was not recorded without a callback: %s of silence right after an event", quiet)
	}
	if course != "Analysis I" || section != "Woche 07" {
		t.Errorf("progress recorded but not the detail: course=%q section=%q", course, section)
	}
}

// The point of the whole thing, driven through the real goroutine: a crawl
// that stops making progress produces a log line naming the section it was on.
//
// The thresholds are shortened rather than reimplemented, so this exercises
// watchForStall itself. A test that built the warning string by hand would
// check the arithmetic and pass happily even if the watchdog were never
// started - and "never started" is the likeliest way for this to be wrong.
func TestTheWatchdogItselfReportsAStall(t *testing.T) {
	restore := shortenStallThresholds(t, 40*time.Millisecond, 10*time.Millisecond)
	defer restore()

	var console bytes.Buffer
	closeFn, err := logging.Setup(logging.Options{Console: &console, Verbose: true})
	if err != nil {
		t.Fatalf("logging.Setup: %v", err)
	}
	t.Cleanup(func() {
		closeFn()
		restoreLog, _ := logging.Setup(logging.Options{Console: os.Stdout})
		t.Cleanup(restoreLog)
	})

	s := New("", "")
	s.publishProgress(DiscoveryProgress{
		Phase:      PhaseSection,
		Course:     "Softwaretechnologie",
		Section:    "Woche 07",
		SectionURL: "https://example.test/opal/auth/CourseNode/42",
	})

	stop := s.watchForStall(context.Background())
	// Long enough for several checks to run against a crawl that publishes
	// nothing further.
	time.Sleep(300 * time.Millisecond)
	stop()

	out := console.String()
	for _, want := range []string{"no progress", "Woche 07", "Softwaretechnologie", "CourseNode/42"} {
		if !strings.Contains(out, want) {
			t.Errorf("the watchdog's own warning is missing %q, which is what makes it actionable:\n%s", want, out)
		}
	}
}

// And it must stay quiet while the crawl is actually working, or it becomes
// noise that gets filtered out before the one time it matters.
func TestTheWatchdogStaysQuietWhileWorkIsHappening(t *testing.T) {
	restore := shortenStallThresholds(t, 40*time.Millisecond, 10*time.Millisecond)
	defer restore()

	var console bytes.Buffer
	closeFn, err := logging.Setup(logging.Options{Console: &console, Verbose: true})
	if err != nil {
		t.Fatalf("logging.Setup: %v", err)
	}
	t.Cleanup(func() {
		closeFn()
		restoreLog, _ := logging.Setup(logging.Options{Console: os.Stdout})
		t.Cleanup(restoreLog)
	})

	s := New("", "")
	stop := s.watchForStall(context.Background())

	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.publishProgress(DiscoveryProgress{Phase: PhaseSection, Course: "Analysis", Section: "Woche 01"})
		time.Sleep(5 * time.Millisecond)
	}
	stop()

	if out := console.String(); strings.Contains(out, "no progress") {
		t.Errorf("the watchdog accused a crawl that was visibly working:\n%s", out)
	}
}

func shortenStallThresholds(t *testing.T, warnAfter, checkEvery time.Duration) func() {
	t.Helper()
	oldWarn, oldCheck := stallWarnAfter, stallCheckEvery
	stallWarnAfter, stallCheckEvery = warnAfter, checkEvery
	return func() { stallWarnAfter, stallCheckEvery = oldWarn, oldCheck }
}

// The fields the warning is built from, checked directly.
func TestAStalledCrawlLogsWhereItStopped(t *testing.T) {
	var console bytes.Buffer
	closeFn, err := logging.Setup(logging.Options{Console: &console, Verbose: true})
	if err != nil {
		t.Fatalf("logging.Setup: %v", err)
	}
	t.Cleanup(func() {
		closeFn()
		restore, _ := logging.Setup(logging.Options{Console: os.Stdout})
		t.Cleanup(restore)
	})

	s := New("", "")
	// Backdate the last progress so the very next check sees a stall, rather
	// than making the test sit through the real three minutes.
	s.stall.note("Softwaretechnologie", "Woche 07", "https://example.test/opal/auth/CourseNode/42")
	s.stall.mu.Lock()
	s.stall.lastAt = time.Now().Add(-stallWarnAfter - time.Minute)
	s.stall.mu.Unlock()

	quiet, course, section, url := s.stall.quietFor(time.Now())
	if quiet < stallWarnAfter {
		t.Fatalf("backdating did not produce a stall: %s", quiet)
	}
	logging.Warn("crawl has made no progress for %s (warning %d) - last section was %q in course %q (%s)",
		quiet.Round(time.Second), s.stall.countWarning(), section, course, url)

	out := console.String()
	for _, want := range []string{"Woche 07", "Softwaretechnologie", "CourseNode/42", "no progress"} {
		if !strings.Contains(out, want) {
			t.Errorf("the stall warning is missing %q, which is what makes it actionable:\n%s", want, out)
		}
	}
}

// The thresholds have to be far enough apart that a stall is actually noticed,
// and far enough above a normal section that a slow-but-healthy crawl is not
// accused of hanging.
func TestStallThresholdsAreSane(t *testing.T) {
	if stallCheckEvery >= stallWarnAfter {
		t.Fatal("checking less often than the warning threshold means a stall can go unreported indefinitely")
	}
	// Sections complete in about a second on the real account; anything near
	// that would fire constantly on a healthy run.
	if stallWarnAfter < time.Minute {
		t.Fatalf("warning after %s would accuse a slow-but-healthy crawl of hanging", stallWarnAfter)
	}
}

// The watchdog has to actually be started by the scrape, not merely exist.
//
// This test was added because a mutation that deleted the call from
// scrapeCoursesBrowser passed every other test in this file: they all invoke
// watchForStall directly, which checks the machinery and not the wiring.
//
// The scrape is run with no session and an unreachable URL, so it fails almost
// immediately - which is fine and is the point. The watchdog is started before
// anything can fail, so whether the crawl then succeeds is irrelevant.
func TestTheScrapeStartsTheWatchdog(t *testing.T) {
	s := New("https://127.0.0.1:1/opal/", "")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Expected to fail: there is no browser session and nothing to reach.
	_, _ = s.scrapeCoursesBrowser(ctx, []string{"*"})

	if !s.stall.everStarted() {
		t.Fatal("a scrape ran without ever starting the stall watchdog, so a hang would leave no evidence again")
	}
}
