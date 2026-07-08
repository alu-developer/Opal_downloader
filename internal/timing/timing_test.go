package timing

import (
	"math"
	"testing"
	"time"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestDownloadTrackerAggregation(t *testing.T) {
	var d DownloadTracker

	d.Record(1*time.Second, int64Ptr(1024*1024)) // 1 MB in 1s
	d.Record(1*time.Second, int64Ptr(1024*1024)) // 1 MB in 1s

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
	if !almostEqual(d.FilesPerSecond(), 1.0) {
		t.Fatalf("expected 1.0 files/sec, got %f", d.FilesPerSecond())
	}
	if !almostEqual(d.MBPerSecond(), 1.0) {
		t.Fatalf("expected 1.0 MB/sec, got %f", d.MBPerSecond())
	}
}

func TestDownloadTrackerNoRecords(t *testing.T) {
	var d DownloadTracker

	if d.Count() != 0 {
		t.Fatalf("expected count 0, got %d", d.Count())
	}
	if d.AverageDuration() != 0 {
		t.Fatalf("expected average 0, got %v", d.AverageDuration())
	}
	if d.FilesPerSecond() != 0 {
		t.Fatalf("expected 0 files/sec, got %f", d.FilesPerSecond())
	}
	if d.MBPerSecond() != 0 {
		t.Fatalf("expected 0 MB/sec, got %f", d.MBPerSecond())
	}
}

func TestDownloadTrackerWithoutSizes(t *testing.T) {
	var d DownloadTracker

	d.Record(2*time.Second, nil)
	d.Record(2*time.Second, nil)

	if d.Count() != 2 {
		t.Fatalf("expected count 2, got %d", d.Count())
	}
	if d.Bytes() != 0 {
		t.Fatalf("expected 0 bytes when sizes are unknown, got %d", d.Bytes())
	}
	// MB/sec must be 0 (not NaN/Inf) when no sizes were ever recorded.
	if d.MBPerSecond() != 0 {
		t.Fatalf("expected 0 MB/sec without size data, got %f", d.MBPerSecond())
	}
	if !almostEqual(d.FilesPerSecond(), 0.5) {
		t.Fatalf("expected 0.5 files/sec, got %f", d.FilesPerSecond())
	}
}

func TestDownloadTrackerMixedSizes(t *testing.T) {
	var d DownloadTracker

	d.Record(1*time.Second, int64Ptr(1024*1024))
	d.Record(1*time.Second, nil) // size unknown for this one

	if d.Bytes() != 1024*1024 {
		t.Fatalf("expected 1MB accounted for, got %d", d.Bytes())
	}
	if !almostEqual(d.MBPerSecond(), 0.5) {
		t.Fatalf("expected 0.5 MB/sec over 2s sum, got %f", d.MBPerSecond())
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

func int64Ptr(v int64) *int64 {
	return &v
}
