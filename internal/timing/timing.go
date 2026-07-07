// Package timing provides lightweight, dependency-free wall-clock
// instrumentation for the discovery and download phases of an opal-downloader
// run. It is intentionally simple (time.Now/time.Since only) so it can be
// extended by later performance work (parallel crawling, parallel downloads)
// without needing to introduce a profiling dependency.
package timing

import (
	"fmt"
	"sync"
	"time"
)

// Profile controls whether granular per-course / per-file timings are
// printed in addition to the end-of-run summary. It defaults to false; the
// CLI sets it based on the --profile flag.
var Profile bool

// Run aggregates timing data for a single command invocation (list or sync).
type Run struct {
	DiscoveryDuration time.Duration
	CourseCount       int

	FileCollectionDuration time.Duration

	DownloadDuration time.Duration

	FileDownloads   int
	FileDownloadSum time.Duration
	BytesDownloaded int64

	TotalDuration time.Duration
}

// Timer is a minimal start/stop wall-clock stopwatch built on
// time.Now/time.Since, as requested by the perf-01 task: no external
// profiling dependencies.
type Timer struct {
	start time.Time
}

// StartTimer begins a new Timer.
func StartTimer() Timer {
	return Timer{start: time.Now()}
}

// Elapsed returns the duration since the timer started.
func (t Timer) Elapsed() time.Duration {
	return time.Since(t.start)
}

// DownloadTracker accumulates per-file download timings so an
// average/throughput can be computed at the end of a sync run without
// printing per-file spam unless Profile is enabled.
//
// DownloadTracker is safe for concurrent use: Record and all read methods
// take an internal mutex, since perf-02 (parallel downloads) records from
// multiple goroutines into a single shared tracker.
type DownloadTracker struct {
	mu       sync.Mutex
	count    int
	sum      time.Duration
	bytes    int64
	hasBytes bool
}

// Record adds one completed file download's duration (and optionally its
// size in bytes, if known) to the tracker.
func (d *DownloadTracker) Record(elapsed time.Duration, size *int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count++
	d.sum += elapsed
	if size != nil {
		d.bytes += *size
		d.hasBytes = true
	}
}

// Count returns the number of recorded downloads.
func (d *DownloadTracker) Count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.count
}

// Sum returns the total time spent downloading across all recorded files.
func (d *DownloadTracker) Sum() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.sum
}

// Bytes returns the total bytes downloaded across all recorded files (0 if
// sizes were never provided).
func (d *DownloadTracker) Bytes() int64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.bytes
}

// AverageDuration returns the mean per-file download duration, or 0 if no
// downloads were recorded.
func (d *DownloadTracker) AverageDuration() time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.count == 0 {
		return 0
	}
	return d.sum / time.Duration(d.count)
}

// FilesPerSecond returns throughput in files/sec based on wall-clock sum, or
// 0 if no downloads were recorded or the sum is zero.
func (d *DownloadTracker) FilesPerSecond() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	seconds := d.sum.Seconds()
	if d.count == 0 || seconds <= 0 {
		return 0
	}
	return float64(d.count) / seconds
}

// MBPerSecond returns throughput in MB/sec based on wall-clock sum and the
// total bytes recorded, or 0 if no sizes were ever provided or the sum is
// zero.
func (d *DownloadTracker) MBPerSecond() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	seconds := d.sum.Seconds()
	if !d.hasBytes || seconds <= 0 {
		return 0
	}
	const bytesPerMB = 1024 * 1024
	return (float64(d.bytes) / bytesPerMB) / seconds
}

// formatDuration renders a duration with one decimal place of seconds, e.g.
// "12.3s", matching the style requested in the perf-01 task's example
// summary output.
func formatDuration(d time.Duration) string {
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// PrintDiscoverySummary prints a one-line discovery timing summary, e.g.:
//
//	Discovery: 12.3s (18 courses)
func PrintDiscoverySummary(elapsed time.Duration, courseCount int) {
	fmt.Printf("Discovery: %s (%d courses)\n", formatDuration(elapsed), courseCount)
}

// PrintDownloadSummary prints a one-line download timing summary, e.g.:
//
//	Download:  45.1s (132 files, 2.9 files/sec)
//
// or, when file sizes were tracked:
//
//	Download:  45.1s (132 files, 2.9 files/sec, 12.4 MB/sec)
func PrintDownloadSummary(elapsed time.Duration, tracker *DownloadTracker) {
	if tracker == nil || tracker.Count() == 0 {
		fmt.Printf("Download:  %s (0 files)\n", formatDuration(elapsed))
		return
	}

	if mbps := tracker.MBPerSecond(); mbps > 0 {
		fmt.Printf("Download:  %s (%d files, %.1f files/sec, %.1f MB/sec)\n",
			formatDuration(elapsed), tracker.Count(), tracker.FilesPerSecond(), mbps)
		return
	}

	fmt.Printf("Download:  %s (%d files, %.1f files/sec)\n",
		formatDuration(elapsed), tracker.Count(), tracker.FilesPerSecond())
}

// PrintTotalSummary prints the final total wall-clock line, e.g.:
//
//	Total:     57.4s
func PrintTotalSummary(elapsed time.Duration) {
	fmt.Printf("Total:     %s\n", formatDuration(elapsed))
}

// PrintProfileLine prints a granular per-course/per-file timing line when
// Profile is enabled; it is a no-op otherwise. Kept as a small helper so
// call sites don't need to repeat the `if timing.Profile` check everywhere.
func PrintProfileLine(format string, args ...interface{}) {
	if !Profile {
		return
	}
	fmt.Printf("  [profile] "+format+"\n", args...)
}
