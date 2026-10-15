package scraper

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// downloadPhaseStats accumulates where the download phase's wall-clock time
// actually goes, split by the step that spent it.
//
// Why this exists: before it, "the downloads are slow" could only be answered
// by per-file totals (timing.PrintProfileLine's "downloaded X in Y"), which
// say nothing about *which* of the four HTTP round trips a single file makes
// cost the time - the always-missing fast-path GET, the section-page
// re-fetch, the Wicket AJAX click, or the actual file transfer. Three past
// speed investigations (docs/sync-speed-model.md) only ever measured the
// discovery phase for exactly this reason: the download phase had no
// breakdown to look at. Every number in the report that motivated the
// section-HTML memo came out of this.
//
// Safe for concurrent use: download workers (download_concurrency, default 3)
// all record into one of these.
type downloadPhaseStats struct {
	mu    sync.Mutex
	steps map[string]*downloadStepStat
}

type downloadStepStat struct {
	count int
	total time.Duration
	// wait is the share of total that was spent asleep in the politeness
	// limiter rather than waiting on OPAL. Kept apart because the two have
	// completely different remedies: one is our own ceiling, the other is
	// the server.
	wait time.Duration
}

func newDownloadPhaseStats() *downloadPhaseStats {
	return &downloadPhaseStats{steps: map[string]*downloadStepStat{}}
}

// record adds one occurrence of step, which took total wall-clock time of
// which wait was limiter sleep.
func (d *downloadPhaseStats) record(step string, total, wait time.Duration) {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	s := d.steps[step]
	if s == nil {
		s = &downloadStepStat{}
		d.steps[step] = s
	}
	s.count++
	s.total += total
	s.wait += wait
}

// log writes the breakdown to the diagnostic log, busiest step first. It is
// a no-op when nothing was recorded, so a run that downloaded nothing (the
// common "everything already up to date" sync) stays silent.
func (d *downloadPhaseStats) log() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.steps) == 0 {
		return
	}

	type row struct {
		name string
		stat downloadStepStat
	}
	rows := make([]row, 0, len(d.steps))
	var grand time.Duration
	for name, s := range d.steps {
		rows = append(rows, row{name: name, stat: *s})
		grand += s.total
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].stat.total > rows[j].stat.total })

	logging.Detail("download phase breakdown (sum of per-worker time, %s across %d steps):", grand, len(rows))
	for _, r := range rows {
		share := 0.0
		if grand > 0 {
			share = 100 * float64(r.stat.total) / float64(grand)
		}
		avg := time.Duration(0)
		if r.stat.count > 0 {
			avg = r.stat.total / time.Duration(r.stat.count)
		}
		logging.Detail("  %-28s %5d x, %10s total (%4.1f%%), %8s avg, %s of it limiter wait",
			r.name, r.stat.count, r.stat.total.Round(time.Millisecond), share,
			avg.Round(time.Millisecond), r.stat.wait.Round(time.Millisecond))
	}
}

// summary renders the same breakdown as a single string, for the profile
// output on the console (--profile), where the diagnostic log is not where
// someone measuring a change is looking.
func (d *downloadPhaseStats) summary() string {
	if d == nil {
		return ""
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.steps) == 0 {
		return ""
	}
	names := make([]string, 0, len(d.steps))
	for name := range d.steps {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return d.steps[names[i]].total > d.steps[names[j]].total })
	out := ""
	for _, name := range names {
		s := d.steps[name]
		out += fmt.Sprintf("  %-28s %5d x %10s (limiter %s)\n", name, s.count,
			s.total.Round(time.Millisecond), s.wait.Round(time.Millisecond))
	}
	return out
}
