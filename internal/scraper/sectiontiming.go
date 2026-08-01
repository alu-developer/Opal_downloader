package scraper

import (
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// Where a section's ~1 second actually goes.
//
// docs/sync-speed-campaign.md concluded the target was unreachable, and that
// conclusion was accepted twice without anyone asking what the second is made
// of. Reading the constants (2026-07-27) suggested most of it may be this
// tool's own waiting rather than OPAL's rendering:
//
//   - contentFallbackWaitMs is 1100ms of settle wait before extraction starts
//   - the stability poll is 150ms x 4 required stable reads = 600ms minimum
//
// 284 sections x that is a large fraction of a ~227s run. But those are
// ceilings, not measurements: the settle wait exits early when the page goes
// calm, and the poll stops as soon as four reads agree. What actually gets
// spent has never been recorded, so this records it.
//
// Deliberately measurement-only. The obvious change - lower the constants - is
// known to be wrong: a version requiring one stable read instead of four was
// live A/B tested and lost files byte-for-byte identically to the unfixed code
// (see sectionContentRequiredStableReads in crawl.go). The useful question is
// not "is four too many" but "how much is this costing, and is a time-based
// poll the right mechanism at all". Answering the first half is this file's
// whole job.
type sectionTiming struct {
	mu sync.Mutex

	sections int
	settle   time.Duration
	stable   time.Duration
	total    time.Duration
}

func (t *sectionTiming) record(settle, stable, total time.Duration) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sections++
	t.settle += settle
	t.stable += stable
	t.total += total
}

// log writes one summary line per run. Diagnostic audience: this is for
// whoever is working on sync speed, not for someone waiting on their files.
func (t *sectionTiming) log() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.sections == 0 {
		return
	}
	n := time.Duration(t.sections)
	other := t.total - t.settle - t.stable

	logging.Detail(
		"section timing over %d sections: total %s (avg %s) | settle wait %s (avg %s, %d%%) | stability poll %s (avg %s, %d%%) | everything else %s (%d%%)",
		t.sections,
		t.total.Round(time.Millisecond), (t.total / n).Round(time.Millisecond),
		t.settle.Round(time.Millisecond), (t.settle / n).Round(time.Millisecond), percent(t.settle, t.total),
		t.stable.Round(time.Millisecond), (t.stable / n).Round(time.Millisecond), percent(t.stable, t.total),
		other.Round(time.Millisecond), percent(other, t.total),
	)
}

// snapshot lets a caller diff a bounded span (e.g. one course within a
// probe run) instead of reading the lifetime total log() prints.
func (t *sectionTiming) snapshot() (sections int, settle, stable, total time.Duration) {
	if t == nil {
		return 0, 0, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sections, t.settle, t.stable, t.total
}

func percent(part, whole time.Duration) int {
	if whole <= 0 {
		return 0
	}
	return int(part * 100 / whole)
}
