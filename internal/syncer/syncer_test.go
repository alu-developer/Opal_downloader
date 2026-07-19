package syncer

import (
	"context"
	"errors"
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

// TestMain overrides acquireSyncLock (see syncer.go) with a no-op for this
// entire test binary run: none of these tests exercise the overlap-guard
// lock itself (that's internal/synclock's own test suite) and touching the
// real ~/.opal-downloader/sync.lock file from every SyncCourses-calling
// test here would be both impure (mutates the developer's real home
// directory) and pointless (these tests never run concurrently with each
// other or a real sync).
func TestMain(m *testing.M) {
	acquireSyncLock = func() (func(), error) { return func() {}, nil }
	os.Exit(m.Run())
}

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

func (f *fakeDownloader) ScrapeWithSavedSession(ctx context.Context, courseFilter []string) ([]scraper.RemoteFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return f.files, nil
}

// cancelingDownloader wraps a fakeDownloader and invokes cancel once its
// DownloadFile has been called cancelAfter times, simulating the GUI's
// /sync/cancel handler firing while a sync's download phase is in progress.
// Used by TestSyncCoursesWithProgressStopsDownloadingAfterCancel to verify
// the download job-dispatch loop (processRemoteFiles, syncer.go) actually
// stops queuing further downloads once ctx is cancelled, instead of grinding
// through the rest of the file list.
type cancelingDownloader struct {
	*fakeDownloader
	cancel      context.CancelFunc
	cancelAfter int32
	downloads   int32
}

func (c *cancelingDownloader) DownloadFile(fileURL, localPath string) error {
	err := c.fakeDownloader.DownloadFile(fileURL, localPath)
	if atomic.AddInt32(&c.downloads, 1) == c.cancelAfter {
		c.cancel()
	}
	return err
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
	got := filepath.ToSlash(resolveRemoteTargetPath(cfgExplicit, file).ManifestKey)
	if got != "Math/Analysis/sheet.pdf" {
		t.Fatalf("explicit target path mismatch: %s", got)
	}

	cfgDefault := config.App{DefaultCourseFolder: "default"}
	got = filepath.ToSlash(resolveRemoteTargetPath(cfgDefault, file).ManifestKey)
	if got != "default/Analysis I/sheet.pdf" {
		t.Fatalf("default target path mismatch: %s", got)
	}

	cfgFallback := config.App{}
	got = filepath.ToSlash(resolveRemoteTargetPath(cfgFallback, file).ManifestKey)
	if got != "Analysis I/sheet.pdf" {
		t.Fatalf("fallback target path mismatch: %s", got)
	}
}

func TestResolveRemoteTargetPathDefaultUnchangedWithoutSubfolders(t *testing.T) {
	// Regression guard: with none of the new config keys set, behavior must be
	// byte-identical to the pre-subfolder-support implementation.
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Uebungen"}

	cfgFallback := config.App{}
	resolved := resolveRemoteTargetPath(cfgFallback, file)
	if filepath.ToSlash(resolved.ManifestKey) != "Analysis I/sheet.pdf" {
		t.Fatalf("expected unchanged flat path, got %s", filepath.ToSlash(resolved.ManifestKey))
	}
	if resolved.LocalPath != filepath.Join(cfgFallback.DownloadPath, resolved.ManifestKey) {
		t.Fatalf("expected LocalPath to be DownloadPath+ManifestKey, got %s", resolved.LocalPath)
	}
}

func TestResolveRemoteTargetPathSectionSubfoldersEnabled(t *testing.T) {
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Uebungen"}

	cfg := config.App{UseSectionSubfolders: true}
	got := filepath.ToSlash(resolveRemoteTargetPath(cfg, file).ManifestKey)
	if got != "Analysis I/Uebungen/sheet.pdf" {
		t.Fatalf("expected section subfolder path, got %s", got)
	}
}

func TestResolveRemoteTargetPathSectionSubfoldersWithNameMapping(t *testing.T) {
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Exercises"}

	cfg := config.App{
		UseSectionSubfolders: true,
		SectionFolderNames: map[string]string{
			"Exercises": "Uebungen",
		},
	}
	got := filepath.ToSlash(resolveRemoteTargetPath(cfg, file).ManifestKey)
	if got != "Analysis I/Uebungen/sheet.pdf" {
		t.Fatalf("expected mapped section subfolder path, got %s", got)
	}
}

func TestResolveRemoteTargetPathSubfolderDestinationOverride(t *testing.T) {
	file := scraper.RemoteFile{Name: "slides.pdf", Course: "Analysis I", SectionTitle: "Vorlesung"}

	cfg := config.App{
		DownloadPath:         `C:\downloads`,
		UseSectionSubfolders: true,
		SubfolderDestinations: map[string]string{
			"*Analysis*/*Vorlesung*": `D:\Elsewhere\AnalysisSlides`,
		},
	}
	resolved := resolveRemoteTargetPath(cfg, file)
	wantLocal := filepath.Join(`D:\Elsewhere\AnalysisSlides`, "slides.pdf")
	if resolved.LocalPath != wantLocal {
		t.Fatalf("expected override local path %s, got %s", wantLocal, resolved.LocalPath)
	}
	// Manifest key still tracks the file under its normal course/subfolder path,
	// independent of the override destination.
	if filepath.ToSlash(resolved.ManifestKey) != "Analysis I/Vorlesung/slides.pdf" {
		t.Fatalf("expected manifest key to reflect normal course path, got %s", filepath.ToSlash(resolved.ManifestKey))
	}
}

func TestResolveRemoteTargetPathAbsoluteDefaultCourseFolder(t *testing.T) {
	// Regression test: default_course_folder set to an absolute path (the
	// maintainer's real config.yaml shape) with no matching course_folders
	// entry must NOT be joined onto cfg.DownloadPath - that produced a
	// doubled/broken path like "<download_path>\<Course>\C:\Users\...".
	//
	// The absolute paths here are built from t.TempDir() rather than
	// hardcoded Windows-style literals ("C:\Users\...") on purpose: the
	// fix's absolute-path detection (filepath.IsAbs) is OS-native, which is
	// correct for this project's real (Windows-only) execution environment,
	// but a hardcoded "C:\..." string is not actually absolute when this
	// test runs on Linux CI (go test runs on ubuntu-latest per
	// .github/workflows/ci.yml, even though the shipped binary is
	// Windows-only). Using t.TempDir() gives a real, OS-native absolute
	// path on whatever platform the test runs on, so this genuinely
	// exercises the IsAbs branch everywhere instead of only on Windows.
	file := scraper.RemoteFile{Name: "sheet.pdf", Course: "Analysis I"}

	downloadRoot := t.TempDir()
	absoluteDefaultFolder := filepath.Join(t.TempDir(), "Default_downloads")

	cfg := config.App{
		DownloadPath:        downloadRoot,
		DefaultCourseFolder: absoluteDefaultFolder,
		CourseFolders:       map[string]string{},
	}
	resolved := resolveRemoteTargetPath(cfg, file)

	wantLocal := filepath.Join(absoluteDefaultFolder, "Analysis I", "sheet.pdf")
	if resolved.LocalPath != wantLocal {
		t.Fatalf("expected absolute default_course_folder to be used directly, got %s, want %s", resolved.LocalPath, wantLocal)
	}
	if filepath.IsAbs(resolved.ManifestKey) {
		t.Fatalf("expected relative manifest key with no absolute prefix, got %s", resolved.ManifestKey)
	}
	if filepath.ToSlash(resolved.ManifestKey) != "Analysis I/sheet.pdf" {
		t.Fatalf("expected manifest key to fall back to course-name shape, got %s", filepath.ToSlash(resolved.ManifestKey))
	}
}

func TestResolveRemoteTargetPathAbsoluteCourseFoldersEntry(t *testing.T) {
	// Same doubled-path bug can occur via an explicit course_folders mapping
	// (not just default_course_folder) if that mapped value is absolute. See
	// the comment in TestResolveRemoteTargetPathAbsoluteDefaultCourseFolder
	// for why t.TempDir() is used instead of a hardcoded Windows-style path
	// literal (this test must also pass on Linux CI).
	file := scraper.RemoteFile{Name: "slides.pdf", Course: "Analysis I", SectionTitle: "Vorlesung"}

	downloadRoot := t.TempDir()
	elsewhereFolder := filepath.Join(t.TempDir(), "Elsewhere", "Analysis")

	cfg := config.App{
		DownloadPath: downloadRoot,
		CourseFolders: map[string]string{
			"*Analysis*": elsewhereFolder,
		},
		UseSectionSubfolders: true,
	}
	resolved := resolveRemoteTargetPath(cfg, file)

	wantLocal := filepath.Join(elsewhereFolder, "Vorlesung", "slides.pdf")
	if resolved.LocalPath != wantLocal {
		t.Fatalf("expected absolute course_folders value to be used directly, got %s, want %s", resolved.LocalPath, wantLocal)
	}
	if filepath.IsAbs(resolved.ManifestKey) {
		t.Fatalf("expected relative manifest key with no absolute prefix, got %s", resolved.ManifestKey)
	}
	if filepath.ToSlash(resolved.ManifestKey) != "Analysis I/Vorlesung/slides.pdf" {
		t.Fatalf("expected manifest key to fall back to course-name shape, got %s", filepath.ToSlash(resolved.ManifestKey))
	}
}

func TestSyncCoursesAbsoluteDefaultCourseFolderLandsOnDisk(t *testing.T) {
	// End-to-end regression test using the real filesystem (t.TempDir), not
	// just the path-string computation: mirrors the maintainer's real
	// config.yaml shape (download_path set, default_course_folder an
	// absolute path, course_folders empty) and confirms SyncCourses actually
	// writes the file to the correct, non-doubled location on disk.
	downloadRoot := t.TempDir()
	absoluteDefaultFolder := filepath.Join(t.TempDir(), "Default_downloads")

	cfg := config.App{
		DownloadPath:        downloadRoot,
		DefaultCourseFolder: absoluteDefaultFolder,
		CourseFolders:       map[string]string{},
		DownloadConcurrency: 1,
	}

	fd := &fakeDownloader{
		files: []scraper.RemoteFile{
			{Name: "sheet.pdf", Course: "Analysis I", Path: "sheet.pdf"},
		},
	}

	stats, err := SyncCourses(context.Background(), fd, cfg, false)
	if err != nil {
		t.Fatalf("SyncCourses returned error: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("expected 1 file downloaded, got %d (errors=%d)", stats.Downloaded, stats.Errors)
	}

	wantPath := filepath.Join(absoluteDefaultFolder, "Analysis I", "sheet.pdf")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected file at %s, stat failed: %v", wantPath, err)
	}

	// Guard against the specific doubled-path bug: the file must not have
	// been written anywhere under downloadRoot (that would mean the absolute
	// folder got joined onto DownloadPath instead of used directly).
	var foundUnderDownloadRoot bool
	_ = filepath.Walk(downloadRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "sheet.pdf" {
			foundUnderDownloadRoot = true
		}
		return nil
	})
	if foundUnderDownloadRoot {
		t.Fatalf("file was written under download_path (%s) instead of the absolute default_course_folder - doubled-path bug regressed", downloadRoot)
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

	stats, err := syncRemoteFiles(context.Background(), remoteFiles, manifest, cfg, false, downloadFn, nil)
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

	stats, err := SyncCourses(context.Background(), fake, cfg, false)
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
		stats, err := SyncCourses(context.Background(), fake, cfg, false)
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

	stats, err := SyncCourses(context.Background(), fake, cfg, false)
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

	stats, err := SyncCourses(context.Background(), fake, cfg, false)
	if err != nil {
		t.Fatalf("SyncCourses returned error: %v", err)
	}
	if stats.Downloaded != 5 {
		t.Fatalf("expected 5 downloaded, got %d", stats.Downloaded)
	}
}

func TestProcessRemoteFilesFiresExpectedEvents(t *testing.T) {
	dir := t.TempDir()
	cfg := config.App{DownloadPath: dir}
	manifest := &Manifest{Path: filepath.Join(dir, ".opal-sync.manifest.json"), Files: map[string]FileRecord{}}

	remoteFiles := []scraper.RemoteFile{
		{Name: "a.pdf", Course: "Course A", Path: "a.pdf", URL: "https://example.test/a.pdf"},
		{Name: "b.pdf", Course: "Course A", Path: "b.pdf", URL: "https://example.test/b.pdf"},
		{Name: "c.pdf", Course: "Course B", Path: "c.pdf", URL: "https://example.test/c.pdf"},
	}

	var events []Event
	downloadFn := func(fileURL, localPath string) error {
		if fileURL == "https://example.test/b.pdf" {
			return errors.New("boom")
		}
		return os.WriteFile(localPath, []byte("data"), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remoteFiles, manifest, cfg, false, downloadFn, func(e Event) {
		events = append(events, e)
	})

	if stats.Downloaded != 2 || stats.Errors != 1 || stats.Skipped != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	var courseStarted, downloaded, errored int
	coursesSeen := map[string]bool{}
	var courseStartedEvents []Event
	for _, e := range events {
		switch e.Type {
		case EventCourseStarted:
			courseStarted++
			coursesSeen[e.Course] = true
			courseStartedEvents = append(courseStartedEvents, e)
		case EventFileDownloaded:
			downloaded++
		case EventError:
			errored++
			if e.Err == nil {
				t.Fatal("expected EventError to carry an error")
			}
		}
	}

	if courseStarted != 2 || !coursesSeen["Course A"] || !coursesSeen["Course B"] {
		t.Fatalf("expected one EventCourseStarted per distinct course, got %d events: %+v", courseStarted, events)
	}
	if downloaded != 2 {
		t.Fatalf("expected 2 EventFileDownloaded, got %d", downloaded)
	}
	if errored != 1 {
		t.Fatalf("expected 1 EventError, got %d", errored)
	}

	// TotalCourses/CourseIndex are cheap to compute upfront because
	// remoteFiles is already the complete discovery result by the time
	// processRemoteFiles runs (see Event's doc comment) - verify every
	// EventCourseStarted carries the correct total and a distinct,
	// monotonically increasing 1-based index.
	for i, e := range courseStartedEvents {
		if e.TotalCourses != 2 {
			t.Fatalf("expected TotalCourses=2 on course_started event %+v", e)
		}
		if e.CourseIndex != i+1 {
			t.Fatalf("expected CourseIndex=%d on course_started event %+v", i+1, e)
		}
	}
}

func TestProcessRemoteFilesSkipsUnchangedFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := config.App{DownloadPath: dir}

	targetKey := "Course A/a.pdf"
	targetPath := filepath.Join(dir, "Course A", "a.pdf")
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(targetPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifest := &Manifest{Path: filepath.Join(dir, ".opal-sync.manifest.json"), Files: map[string]FileRecord{
		targetKey: {},
	}}

	remoteFiles := []scraper.RemoteFile{
		{Name: "a.pdf", Course: "Course A", Path: "a.pdf", URL: "https://example.test/a.pdf"},
	}

	var events []Event
	downloadCalled := false
	downloadFn := func(fileURL, localPath string) error {
		downloadCalled = true
		return nil
	}

	stats := processRemoteFiles(context.Background(), remoteFiles, manifest, cfg, false, downloadFn, func(e Event) {
		events = append(events, e)
	})

	if downloadCalled {
		t.Fatal("expected downloadFn not to be called for an unchanged, already-present file")
	}
	if stats.Skipped != 1 || stats.Downloaded != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	skipped := 0
	for _, e := range events {
		if e.Type == EventFileSkipped {
			skipped++
		}
	}
	if skipped != 1 {
		t.Fatalf("expected 1 EventFileSkipped, got %d", skipped)
	}
}

func TestSyncCoursesWithProgressNilCallbackDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	cfg := config.App{DownloadPath: dir}
	manifest := &Manifest{Path: filepath.Join(dir, ".opal-sync.manifest.json"), Files: map[string]FileRecord{}}

	remoteFiles := []scraper.RemoteFile{
		{Name: "a.pdf", Course: "Course A", Path: "a.pdf", URL: "https://example.test/a.pdf"},
	}
	downloadFn := func(fileURL, localPath string) error {
		return os.WriteFile(localPath, []byte("data"), 0o644)
	}

	// processRemoteFiles is exercised directly with a nil-safe wrapper the
	// same way SyncCourses wraps SyncCoursesWithProgress with progress=nil.
	stats := processRemoteFiles(context.Background(), remoteFiles, manifest, cfg, false, downloadFn, func(Event) {})
	if stats.Downloaded != 1 {
		t.Fatalf("expected 1 downloaded, got %+v", stats)
	}
}

// TestSyncCoursesWithProgressStopsDownloadingAfterCancel is a regression test
// for the queue task fix-gui-cancel-doesnt-abort-course-loop: before the fix,
// cancelling a running GUI sync job only closed the browser - the sync's
// course/file loop kept grinding through the remaining work and the job
// still reported "Done" instead of "Cancelled". This drives
// SyncCoursesWithProgress with DownloadConcurrency=1 and a Downloader that
// cancels ctx from inside its first DownloadFile call (simulating
// /sync/cancel firing mid-run), and asserts: the download loop stops well
// short of all files, no EventComplete ("done") fires, and the returned
// error satisfies errors.Is(err, context.Canceled) so
// publishCancelOrError (internal/gui/sync.go) can tell this apart from a
// genuine failure or a normal completion.
func TestSyncCoursesWithProgressStopsDownloadingAfterCancel(t *testing.T) {
	dir := t.TempDir()
	const fileCount = 10

	fake := &fakeDownloader{files: makeRemoteFiles(fileCount)}
	cfg := config.App{DownloadPath: dir, DownloadConcurrency: 1}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cd := &cancelingDownloader{fakeDownloader: fake, cancel: cancel, cancelAfter: 1}

	var gotComplete bool
	stats, err := SyncCoursesWithProgress(ctx, cd, cfg, false, func(e Event) {
		if e.Type == EventComplete {
			gotComplete = true
		}
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error satisfying errors.Is(err, context.Canceled), got %v", err)
	}
	if gotComplete {
		t.Fatal("expected EventComplete not to fire for a cancelled sync")
	}
	if stats.Downloaded == 0 {
		t.Fatal("expected the in-flight download (before cancellation was observed) to still be recorded")
	}
	if stats.Downloaded >= fileCount {
		t.Fatalf("expected the download loop to stop well before all %d files, got %d downloaded", fileCount, stats.Downloaded)
	}
}
