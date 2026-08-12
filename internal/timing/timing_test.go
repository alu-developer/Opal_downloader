package timing

import (
	"io"
	"math"
	"os"
	"testing"
	"time"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("closing pipe writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading pipe: %v", err)
	}
	return string(out)
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestDownloadTrackerAggregation(t *testing.T) {
	var d DownloadTracker

	d.Record(1*time.Second, int64Ptr(1024*1024)) // 1 MB in 1s
	d.Record(1*time.Second, int64Ptr(1024*1024)) // 1 MB in 1s

	// Wall-clock elapsed for the download phase; matches the sum here
	// because these two "jobs" ran back-to-back (not serialized/queued).
	elapsed := 2 * time.Second

	if d.Count() != 2 {
		t.Fatalf("expected count 2, got %d", d.Count())
	}
	if d.Sum() != 2*time.Second {
		t.Fatalf("expected sum 2s, got %v", d.Sum())
	}
	if d.Bytes() != 2*1024*1024 {
		t.Fatalf("expected 2MB, got %d bytes", d.Bytes())
	}
	if d.AverageDuration() != 1*time.Second {
		t.Fatalf("expected average 1s, got %v", d.AverageDuration())
	}
	if !almostEqual(d.FilesPerSecond(elapsed), 1.0) {
		t.Fatalf("expected 1.0 files/sec, got %f", d.FilesPerSecond(elapsed))
	}
	if !almostEqual(d.MBPerSecond(elapsed), 1.0) {
		t.Fatalf("expected 1.0 MB/sec, got %f", d.MBPerSecond(elapsed))
	}
}

func TestDownloadTrackerNoRecords(t *testing.T) {
	var d DownloadTracker
	elapsed := 1 * time.Second

	if d.Count() != 0 {
		t.Fatalf("expected count 0, got %d", d.Count())
	}
	if d.AverageDuration() != 0 {
		t.Fatalf("expected average 0, got %v", d.AverageDuration())
	}
	if d.FilesPerSecond(elapsed) != 0 {
		t.Fatalf("expected 0 files/sec, got %f", d.FilesPerSecond(elapsed))
	}
	if d.MBPerSecond(elapsed) != 0 {
		t.Fatalf("expected 0 MB/sec, got %f", d.MBPerSecond(elapsed))
	}
}

func TestDownloadTrackerWithoutSizes(t *testing.T) {
	var d DownloadTracker

	d.Record(2*time.Second, nil)
	d.Record(2*time.Second, nil)
	elapsed := 4 * time.Second

	if d.Count() != 2 {
		t.Fatalf("expected count 2, got %d", d.Count())
	}
	if d.Bytes() != 0 {
		t.Fatalf("expected 0 bytes when sizes are unknown, got %d", d.Bytes())
	}
	// MB/sec must be 0 (not NaN/Inf) when no sizes were ever recorded.
	if d.MBPerSecond(elapsed) != 0 {
		t.Fatalf("expected 0 MB/sec without size data, got %f", d.MBPerSecond(elapsed))
	}
	if !almostEqual(d.FilesPerSecond(elapsed), 0.5) {
		t.Fatalf("expected 0.5 files/sec, got %f", d.FilesPerSecond(elapsed))
	}
}

func TestDownloadTrackerMixedSizes(t *testing.T) {
	var d DownloadTracker

	d.Record(1*time.Second, int64Ptr(1024*1024))
	d.Record(1*time.Second, nil) // size unknown for this one
	elapsed := 2 * time.Second

	if d.Bytes() != 1024*1024 {
		t.Fatalf("expected 1MB accounted for, got %d", d.Bytes())
	}
	if !almostEqual(d.MBPerSecond(elapsed), 0.5) {
		t.Fatalf("expected 0.5 MB/sec over 2s wall-clock elapsed, got %f", d.MBPerSecond(elapsed))
	}
}

// TestDownloadTrackerSerializedJobsUseWallClock simulates the bug scenario
// from the original report: downloads are serialized behind a mutex
// (e.g. scraper.go's browserDownloadMu), so the sum of individual per-job
// durations (2s here) far exceeds the actual wall-clock elapsed time of the
// download phase (0.5s here, because the jobs were queued/overlapping and
// only the serialized portion counted once against wall-clock). Throughput
// must be computed against wall-clock elapsed, not the summed durations,
// or it drastically underreports (0.5 files/sec here vs. the true 4
// files/sec).
func TestDownloadTrackerSerializedJobsUseWallClock(t *testing.T) {
	var d DownloadTracker

	// Two jobs, each individually timed at 1s (e.g. queued waiting on a
	// mutex plus the actual transfer), but the download phase as a whole
	// only took 0.5s of wall-clock time.
	d.Record(1*time.Second, int64Ptr(1*1024*1024))
	d.Record(1*time.Second, int64Ptr(1*1024*1024))
	elapsed := 500 * time.Millisecond

	if d.Sum() != 2*time.Second {
		t.Fatalf("expected summed duration 2s, got %v", d.Sum())
	}

	// Sum-based throughput would report 2 files / 2s = 1.0 files/sec, which
	// is what the bug produced. Wall-clock-based throughput must report
	// 2 files / 0.5s = 4.0 files/sec instead.
	if !almostEqual(d.FilesPerSecond(elapsed), 4.0) {
		t.Fatalf("expected 4.0 files/sec (wall-clock based), got %f", d.FilesPerSecond(elapsed))
	}
	// Sum-based would report 2MB / 2s = 1.0 MB/sec; wall-clock-based must
	// report 2MB / 0.5s = 4.0 MB/sec.
	if !almostEqual(d.MBPerSecond(elapsed), 4.0) {
		t.Fatalf("expected 4.0 MB/sec (wall-clock based), got %f", d.MBPerSecond(elapsed))
	}
}

func TestTimerElapsed(t *testing.T) {
	timer := StartTimer()
	time.Sleep(5 * time.Millisecond)
	elapsed := timer.Elapsed()
	if elapsed <= 0 {
		t.Fatalf("expected positive elapsed duration, got %v", elapsed)
	}
}

// TestPrintCourseProgress_AlwaysOn is a regression test for the
// friction-campaign Walk 3 finding (docs/friction-campaign.md, 2026-08-11):
// a real `list` run sat silent for 2m44s during discovery because per-course
// completion only ever reached PrintProfileLine, which is a no-op without
// --profile. PrintCourseProgress must print regardless of Profile.
func TestPrintCourseProgress_AlwaysOn(t *testing.T) {
	orig := Profile
	Profile = false
	defer func() { Profile = orig }()

	out := captureStdout(t, func() {
		PrintCourseProgress("Analysis", 30, 4200*time.Millisecond)
	})

	want := "  Analysis: 30 files (4.2s)\n"
	if out != want {
		t.Fatalf("PrintCourseProgress output = %q, want %q", out, want)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
