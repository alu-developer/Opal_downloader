package scraper

import (
	"context"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// A crawl that stops making progress leaves no evidence of where it stopped.
//
// The maintainer hit this once (2026-07-26): a sync sat on
// "Running: skipping - .../Woche 07/hybrid_quicksort.ipynb" and never moved.
// The /sync page now notices silence and says so, but that only helps somebody
// who is watching a browser - and it says how long, never where. A scheduled
// 06:00 run that hangs produces nothing at all: no status file, no new files,
// no explanation the next morning.
//
// This watches from inside the crawl, so it covers CLI, scheduled and GUI runs
// alike, and it records the thing that was actually missing: which section was
// being visited when progress stopped. The next time this happens there will
// be a line in the diagnostic log naming the page to go and look at.
//
// It only ever logs. Cancelling a crawl on suspicion of a stall would risk
// killing a slow-but-healthy run - a single OPAL section can genuinely take a
// long time - and losing work to a false positive is worse than the stall.

// Vars rather than consts so a test can drive the real goroutine instead of
// waiting three minutes for it. The alternative - a test that reimplements the
// warning and asserts its own copy is well-formed - checks the arithmetic and
// silently passes if the watchdog is never started, which is the wiring most
// likely to be wrong.
var (
	// stallWarnAfter is how long a crawl may go without progress before it is
	// worth saying something. Sections normally complete in about a second, so
	// three minutes is far outside anything observed while running normally,
	// including the slowest courses on the real account.
	stallWarnAfter = 3 * time.Minute

	// stallCheckEvery is how often the watchdog looks. Cheap enough to be
	// frequent, coarse enough not to matter.
	stallCheckEvery = 30 * time.Second
)

// stallWatch records when the crawl last made progress, and what it was doing.
type stallWatch struct {
	mu       sync.Mutex
	lastAt   time.Time
	course   string
	section  string
	url      string
	warnings int

	// started records that a watcher was launched at all. The watchdog is
	// stopped by a deferred call when the scrape returns, so by the time a
	// test can look, there is nothing running to observe - and "nobody ever
	// started it" is the way this feature is most likely to break. A mutation
	// that removed the call from the orchestrator passed every other test
	// here, which is what put this field in.
	started bool
}

// note records progress. Called from publishProgress, so every kind of crawl
// progress counts - not just section visits - and a course that is slow to
// start does not read as a stall.
func (w *stallWatch) note(course, section, url string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastAt = time.Now()
	if course != "" {
		w.course = course
	}
	if section != "" {
		w.section = section
	}
	if url != "" {
		w.url = url
	}
}

// quietFor reports how long since the last progress, and what was happening
// then. A zero lastAt means nothing has happened yet, which is reported as no
// elapsed time at all: a crawl that has not started cannot have stalled.
func (w *stallWatch) quietFor(now time.Time) (time.Duration, string, string, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.lastAt.IsZero() {
		return 0, w.course, w.section, w.url
	}
	return now.Sub(w.lastAt), w.course, w.section, w.url
}

// everStarted reports whether a watcher was ever launched for this scrape.
func (w *stallWatch) everStarted() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.started
}

func (w *stallWatch) countWarning() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.warnings++
	return w.warnings
}

// watchForStall runs until ctx is cancelled, logging when the crawl goes quiet.
// The returned function stops it; callers should defer it.
//
// Started per scrape rather than per course: with course concurrency the
// courses share one browser, and a wedged browser stops all of them, so "is
// anything at all still moving" is the question worth asking.
func (s *OpalScraper) watchForStall(ctx context.Context) func() {
	if s == nil || s.stall == nil {
		return func() {}
	}

	s.stall.mu.Lock()
	s.stall.started = true
	s.stall.mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})

	go func() {
		defer close(done)
		ticker := time.NewTicker(stallCheckEvery)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				quiet, course, section, url := s.stall.quietFor(time.Now())
				if quiet < stallWarnAfter {
					continue
				}
				n := s.stall.countWarning()
				// Warned once per check while it stays quiet, which is what
				// makes the duration in the log usable: the last such line
				// says how long it really went on for.
				logging.Warn("crawl has made no progress for %s (warning %d) - last section was %q in course %q (%s)",
					quiet.Round(time.Second), n, section, course, url)
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}
