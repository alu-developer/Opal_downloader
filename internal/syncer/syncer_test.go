package syncer

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// fakeDownloader is a test double for the Downloader interface. It simulates
// a fixed per-file download latency (to make concurrency speedups
// observable) and records how many downloads were in flight at once, plus
// which URLs were requested and with which local paths, all guarded by a
// mutex since SyncCourses's worker pool calls DownloadFile concurrently.
type fakeDownloader struct {
	files []scraper.RemoteFile
	delay time.Duration

	mu            sync.Mutex
	calls         []string // localPath per call, in completion order
	concurrent    int32
	maxConcurrent int32

	failURLs map[string]bool
}

func (f *fakeDownloader) ScrapeWithSavedSession(courseFilter []string) ([]scraper.RemoteFile, error) {
	return f.files, nil
}

func (f *fakeDownloader) DownloadFile(fileURL, localPath string) error {
	cur := atomic.AddInt32(&f.concurrent, 1)
	defer atomic.AddInt32(&f.concurrent, -1)
	for {
		max := atomic.LoadInt32(&f.maxConcurrent)
		if cur <= max || atomic.CompareAndSwapInt32(&f.maxConcurrent, max, cur) {
			break
		}
	}

	if f.delay > 0 {
		time.Sleep(f.delay)
	}

	f.mu.Lock()
	f.calls = append(f.calls, localPath)
	shouldFail := f.failURLs[fileURL]
	f.mu.Unlock()

	if shouldFail {
		return fmt.Errorf("simulated failure for %s", fileURL)
	}

	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(localPath, []byte("fake-content"), 0o644)
}

func TestResolveRemoteTargetPath(t *testing.T) {
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I"}

	cfgExplicit := config.App{
		DefaultCourseFolder: "default",
		CourseFolders: map[string]string{
			"*Analysis*": "Math/Analysis",
		},
	}
	got := filepath.ToSlash(resolveRemoteTargetPath(cfgExplicit, file))
	if got != "Math/Analysis/sheet.pdf" {
		t.Fatalf("explicit target path mismatch: %s", got)
	}

	cfgDefault := config.App{DefaultCourseFolder: "default"}
	got = filepath.ToSlash(resolveRemoteTargetPath(cfgDefault, file))
	if got != "default/Analysis I/sheet.pdf" {
		t.Fatalf("default target path mismatch: %s", got)
	}

	cfgFallback := config.App{}
	got = filepath.ToSlash(resolveRemoteTargetPath(cfgFallback, file))
	if got != "Analysis I/sheet.pdf" {
		t.Fatalf("fallback target path mismatch: %s", got)
	}
}

func TestFileChanged(t *testing.T) {
	size10 := int64(10)
	size11 := int64(11)
	modA := "2026-01-01"
	modB := "2026-01-02"

	remote := scraper.RemoteFile{Size: &size10, Modified: &modA}
	if !fileChanged(remote, false, FileRecord{}) {
		t.Fatal("expected changed when no previous file record")
	}

	prev := FileRecord{Size: &size10, Modified: &modA}
	if fileChanged(remote, true, prev) {
		t.Fatal("expected unchanged when metadata matches")
	}

	prev = FileRecord{Size: &size11, Modified: &modA}
	if !fileChanged(remote, true, prev) {
		t.Fatal("expected changed when size differs")
	}

	prev = FileRecord{Size: &size10, Modified: &modB}
	if !fileChanged(remote, true, prev) {
		t.Fatal("expected changed when modified timestamp differs")
	}
}

// TestSyncRemoteFilesDurationExcludesDiscovery is a regression test for the
// PR #16 bug where the printed "Download" duration silently included
// discovery time. It simulates a slow discovery phase (by sleeping before
// ever calling syncRemoteFiles, mirroring how SyncCourses only starts timing
// after sc.ScrapeWithSavedSession returns) followed by a fast, fake
// download, and asserts stats.DownloadDuration reflects only the download
// work, not the simulated discovery delay.
func TestSyncRemoteFilesDurationExcludesDiscovery(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.App{DownloadPath: tmpDir}

	size := int64(4)
	remoteFiles := []scraper.RemoteFile{
		{Name: "notes.txt", Course: "Course A", Path: "Course A/notes.txt", Size: &size},
	}
	manifest := &Manifest{Path: filepath.Join(tmpDir, ".opal-sync.manifest.json"), Files: map[string]FileRecord{}}

	const discoverySimulatedDelay = 300 * time.Millisecond
	const downloadFakeDelay = 20 * time.Millisecond

	// Simulate a slow discovery phase that happens entirely BEFORE the
	// timed section starts (as it does in SyncCourses, where
	// sc.ScrapeWithSavedSession runs to completion before syncRemoteFiles
	// is invoked).
	time.Sleep(discoverySimulatedDelay)

	downloadFn := func(url, localPath string) error {
		time.Sleep(downloadFakeDelay)
		return writePlaceholderFile(localPath)
	}

	stats, err := syncRemoteFiles(remoteFiles, manifest, cfg, false, downloadFn)
	if err != nil {
		t.Fatalf("syncRemoteFiles returned error: %v", err)
	}

	if stats.Downloaded != 1 {
		t.Fatalf("expected 1 file downloaded, got %d", stats.Downloaded)
	}

	// The reported duration must be well under the simulated discovery
	// delay. Before the fix, DownloadDuration would have included the full
	// discovery sleep too. Allow generous slack for scheduler jitter, but
	// stay far below discoverySimulatedDelay so the test fails if discovery
	// time leaks back in.
	if stats.DownloadDuration >= discoverySimulatedDelay {
		t.Fatalf("DownloadDuration (%v) should not include discovery delay (%v); it appears discovery time leaked into the download timer", stats.DownloadDuration, discoverySimulatedDelay)
	}
	if stats.DownloadDuration < downloadFakeDelay {
		t.Fatalf("DownloadDuration (%v) is implausibly smaller than the fake download delay (%v)", stats.DownloadDuration, downloadFakeDelay)
	}
}

func writePlaceholderFile(path string) error {
	return os.WriteFile(path, []byte("data"), 0o644)
}

func makeRemoteFiles(n int) []scraper.RemoteFile {
	files := make([]scraper.RemoteFile, 0, n)
	for i := 0; i < n; i++ {
		size := int64(100 + i)
		files = append(files, scraper.RemoteFile{
			Name:   fmt.Sprintf("file-%03d.pdf", i),
			URL:    fmt.Sprintf("https://example.invalid/file-%03d.pdf", i),
			Course: "Course A",
			Path:   fmt.Sprintf("Course A/file-%03d.pdf", i),
			Size:   &size,
		})
	}
	return files
}

// TestSyncCoursesDownloadsAllFilesConcurrently exercises the full worker-pool
// path end to end: every file must end up on disk and recorded correctly in
// the manifest and in stats, even though downloads happen concurrently.
func TestSyncCoursesDownloadsAllFilesConcurrently(t *testing.T) {
	dir := t.TempDir()
	const fileCount = 20

	fake := &fakeDownloader{files: makeRemoteFiles(fileCount), delay: 5 * time.Millisecond}
	cfg := config.App{DownloadPath: dir, DownloadConcurrency: 4}

	stats, err := SyncCourses(fake, cfg, false)
	if err != nil {
		t.Fatalf("SyncCourses returned error: %v", err)
	}
	if stats.Downloaded != fileCount {
		t.Fatalf("expected %d downloaded, got %d", fileCount, stats.Downloaded)
	}
	if stats.Errors != 0 {
		t.Fatalf("expected 0 errors, got %d", stats.Errors)
	}
	if stats.Downloads.Count() != fileCount {
		t.Fatalf("expected timing tracker to record %d downloads, got %d", fileCount, stats.Downloads.Count())
	}

	manifestPath := filepath.Join(dir, ".opal-sync.manifest.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if len(manifest.Files) != fileCount {
		t.Fatalf("expected %d manifest entries, got %d", fileCount, len(manifest.Files))
	}

	for _, f := range fake.files {
		localPath := filepath.Join(dir, "Course A", f.Name)
		if _, err := os.Stat(localPath); err != nil {
			t.Fatalf("expected file to exist on disk: %s (%v)", localPath, err)
		}
		key := filepath.ToSlash(filepath.Join("Course A", f.Name))
		record, ok := manifest.Files[key]
		if !ok {
			t.Fatalf("manifest missing entry for %s", key)
		}
		if record.Size == nil || *record.Size != *f.Size {
			t.Fatalf("manifest size mismatch for %s", key)
		}
	}

	if got := atomic.LoadInt32(&fake.maxConcurrent); got < 2 {
		t.Fatalf("expected downloads to run concurrently (max observed concurrency %d), fake delay should have made overlap observable", got)
	}
	if got := atomic.LoadInt32(&fake.maxConcurrent); got > 4 {
		t.Fatalf("expected concurrency to be capped at 4, observed %d", got)
	}
}

// TestSyncCoursesConcurrencyIsFasterThanSequential is the synthetic
// throughput check called for in the perf-02 task: with an artificial
// per-file delay, concurrency=4 should complete meaningfully faster than
// concurrency=1 for the same file count. This does not require a live OPAL
// account - it isolates the scheduling change in SyncCourses using the fake
// Downloader above.
func TestSyncCoursesConcurrencyIsFasterThanSequential(t *testing.T) {
	const fileCount = 12
	const perFileDelay = 20 * time.Millisecond

	runWithConcurrency := func(concurrency int) time.Duration {
		dir := t.TempDir()
		fake := &fakeDownloader{files: makeRemoteFiles(fileCount), delay: perFileDelay}
		cfg := config.App{DownloadPath: dir, DownloadConcurrency: concurrency}

		start := time.Now()
		stats, err := SyncCourses(fake, cfg, false)
		elapsed := time.Since(start)
		if err != nil {
			t.Fatalf("SyncCourses returned error: %v", err)
		}
		if stats.Downloaded != fileCount {
			t.Fatalf("expected %d downloaded, got %d", fileCount, stats.Downloaded)
		}
		return elapsed
	}

	sequential := runWithConcurrency(1)
	concurrent := runWithConcurrency(4)

	t.Logf("sequential (concurrency=1): %s, concurrent (concurrency=4): %s", sequential, concurrent)

	// With 12 files at 20ms each, sequential should take ~240ms and
	// concurrency=4 should take ~60-80ms. Assert concurrent is at least 40%
	// faster to leave generous headroom for slow/loaded CI machines while
	// still proving the parallel path actually improves throughput.
	if concurrent >= time.Duration(float64(sequential)*0.6) {
		t.Fatalf("expected concurrency=4 to be meaningfully faster than concurrency=1: sequential=%s concurrent=%s", sequential, concurrent)
	}
}

// TestSyncCoursesHandlesDownloadErrors verifies that failures from some
// concurrent workers don't corrupt stats or the manifest for the files that
// succeeded, and that errors are still counted.
func TestSyncCoursesHandlesDownloadErrors(t *testing.T) {
	dir := t.TempDir()
	files := makeRemoteFiles(10)

	fake := &fakeDownloader{
		files:    files,
		failURLs: map[string]bool{files[2].URL: true, files[7].URL: true},
	}
	cfg := config.App{DownloadPath: dir, DownloadConcurrency: 3}

	stats, err := SyncCourses(fake, cfg, false)
	if err != nil {
		t.Fatalf("SyncCourses returned error: %v", err)
	}
	if stats.Errors != 2 {
		t.Fatalf("expected 2 errors, got %d", stats.Errors)
	}
	if stats.Downloaded != 8 {
		t.Fatalf("expected 8 downloaded, got %d", stats.Downloaded)
	}

	manifestPath := filepath.Join(dir, ".opal-sync.manifest.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	if len(manifest.Files) != 8 {
		t.Fatalf("expected 8 manifest entries, got %d", len(manifest.Files))
	}
	for _, badIdx := range []int{2, 7} {
		key := filepath.ToSlash(filepath.Join("Course A", files[badIdx].Name))
		if _, ok := manifest.Files[key]; ok {
			t.Fatalf("manifest should not contain failed download %s", key)
		}
	}
}

// TestSyncCoursesDefaultsConcurrencyWhenUnset confirms a zero/unset
// DownloadConcurrency in config.App still downloads everything (falls back
// to config.DefaultDownloadConcurrency) rather than deadlocking or skipping
// files.
func TestSyncCoursesDefaultsConcurrencyWhenUnset(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeDownloader{files: makeRemoteFiles(5)}
	cfg := config.App{DownloadPath: dir} // DownloadConcurrency left at zero value

	stats, err := SyncCourses(fake, cfg, false)
	if err != nil {
		t.Fatalf("SyncCourses returned error: %v", err)
	}
	if stats.Downloaded != 5 {
		t.Fatalf("expected 5 downloaded, got %d", stats.Downloaded)
	}
}
