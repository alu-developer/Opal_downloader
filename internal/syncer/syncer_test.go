package syncer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	// barrier, when non-nil, makes DownloadFile block until barrierWidth
	// calls are in flight *at the same time* (or until barrierTimeout
	// elapses). That turns "did downloads really run in parallel?" into a
	// deterministic question instead of a wall-clock race: if the worker
	// pool genuinely runs barrierWidth workers, the barrier is reached and
	// released no matter how loaded the machine is; if it secretly
	// serialises, the barrier can never be reached and the timeout fires.
	// Use newBarrierDownloader to construct one.
	barrier         chan struct{}
	barrierWidth    int32
	barrierOnce     sync.Once
	barrierTimeout  time.Duration
	barrierTimeouts int32

	failURLs map[string]bool
}

// newBarrierDownloader builds a fakeDownloader whose DownloadFile blocks
// until `width` downloads are simultaneously in flight. The timeout is the
// failure path only: it is never hit when concurrency works, so it can be
// generous without slowing the passing case. Once it does fire the barrier
// is released for everyone, so a broken (serialised) implementation fails
// after one timeout rather than one per file.
func newBarrierDownloader(files []scraper.RemoteFile, width int32) *fakeDownloader {
	return &fakeDownloader{
		files:          files,
		barrier:        make(chan struct{}),
		barrierWidth:   width,
		barrierTimeout: 10 * time.Second,
	}
}

// barrierTimedOut reports whether the barrier was released by its timeout
// (i.e. the expected number of parallel downloads never materialised)
// rather than by enough downloads actually arriving together.
func (f *fakeDownloader) barrierTimedOut() bool {
	return atomic.LoadInt32(&f.barrierTimeouts) > 0
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

	if f.barrier != nil {
		if cur >= f.barrierWidth {
			f.barrierOnce.Do(func() { close(f.barrier) })
		}
		select {
		case <-f.barrier:
		case <-time.After(f.barrierTimeout):
			atomic.AddInt32(&f.barrierTimeouts, 1)
			// Release everyone else too, so a serialised implementation
			// costs one timeout total instead of one per file.
			f.barrierOnce.Do(func() { close(f.barrier) })
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

// pathGoesUnwritableAfterFirstDownloader wraps a fakeDownloader to answer
// the open backlog question ("what does a sync do with an unwritable
// download_path?") for the half that question left unmeasured: not a path
// that is already broken before the sync starts (SyncCoursesWithProgress's
// own os.MkdirAll(cfg.DownloadPath, ...) already fails loudly for that, and
// `status` catches it earlier still - see TestRunStatus_FlagsUnwritableDownloadPath),
// but one that goes bad *mid-sync* - a removable drive unmounted, a OneDrive
// folder renamed, mid-run. After the first successful download it places a
// regular file where every subsequent job's course subfolder needs to be a
// directory, so each later os.MkdirAll (syncer.go's per-file one) fails with
// a real OS error rather than an injected one.
type pathGoesUnwritableAfterFirstDownloader struct {
	*fakeDownloader
	succeeded int32
}

func (d *pathGoesUnwritableAfterFirstDownloader) DownloadFile(fileURL, localPath string) error {
	if atomic.LoadInt32(&d.succeeded) >= 1 {
		// syncer.go's own per-job os.MkdirAll (syncRemoteFiles's job-building
		// loop) already ran for every job, including this one, before any
		// download started - so the directory this file needs already
		// exists. To simulate the path going bad *mid-sync* rather than
		// before the sync even began, replace that already-created
		// directory with a blocking file right before the write, the same
		// way TestRunStatus_FlagsUnwritableDownloadPath blocks a directory
		// that never existed - a directory genuinely cannot be created (or,
		// here, written into) where a regular file sits.
		blockerDir := filepath.Dir(localPath)
		_ = os.RemoveAll(blockerDir)
		_ = os.WriteFile(blockerDir, []byte("blocked"), 0o644)
	}
	err := d.fakeDownloader.DownloadFile(fileURL, localPath)
	if err == nil {
		atomic.AddInt32(&d.succeeded, 1)
	}
	return err
}

// TestSyncCoursesWithProgress_DownloadPathGoesUnwritableMidSync answers
// docs/BACKLOG.md's open finding ("What a *sync* does with an unwritable
// download_path" - does it fail clearly or appear to succeed, for a path
// that goes bad *between* the pre-sync check and the sync finishing).
//
// Result: neither, cleanly. SyncCoursesWithProgress's top-level return is
// nil - it does not abort the run - but it is not silent either: every
// blocked file is individually counted into stats.Errors and reported via
// printSyncError/EventError, so the caller sees exactly how many and which.
// The remaining, once-broken course folders are retried file-by-file rather
// than the run recognising "the whole path just died" and stopping early -
// each retry fails the same way and costs one more MkdirAll/EventError, not
// a hang or a crash. This is the same shape cmd/opal-downloader/root.go
// already gives every per-file failure (a flaky single download included),
// which callers turn into distinct outcomes: `sync --scheduled` classifies
// stats.Errors>0 as statuslog.OutcomePartial (never a failure toast - see
// runSync's "notification fatigue" comment), and the last-sync status file
// is written unconditionally for *every* run, scheduled or not, so a plain
// interactive `sync`'s GUI/status-file record shows "Synced with N file
// error(s)" too. What does NOT reflect it: the plain interactive `sync`
// command's own process exit code, which stays 0 - unlike `--full-sync`,
// which does return an error (and thus a non-zero exit) when stats.Errors>0.
func TestSyncCoursesWithProgress_DownloadPathGoesUnwritableMidSync(t *testing.T) {
	downloadRoot := t.TempDir()
	cfg := config.App{
		DownloadPath:        downloadRoot,
		DownloadConcurrency: 1, // deterministic processing order
	}

	fd := &pathGoesUnwritableAfterFirstDownloader{
		fakeDownloader: &fakeDownloader{
			files: []scraper.RemoteFile{
				{Name: "a.pdf", Course: "Course A", Path: "Course A/a.pdf"},
				{Name: "b.pdf", Course: "Course B", Path: "Course B/b.pdf"},
				{Name: "c.pdf", Course: "Course C", Path: "Course C/c.pdf"},
			},
		},
	}

	stats, err := SyncCoursesWithProgress(context.Background(), fd, cfg, false, nil)
	if err != nil {
		t.Fatalf("expected SyncCoursesWithProgress to return nil despite mid-sync per-file failures (it does not treat this as fatal), got: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("expected exactly the first file to succeed before the path broke, got Downloaded=%d", stats.Downloaded)
	}
	if stats.Errors != 2 {
		t.Fatalf("expected the two files behind the now-blocked path to be individually counted as errors, got Errors=%d", stats.Errors)
	}
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

// TestSyncCoursesForwardSlashAbsoluteDefaultCourseFolderLandsOnDisk is the
// specific interaction walk 1's open question 4 named but left unchecked:
// does the doubled-path bug TestSyncCoursesAbsoluteDefaultCourseFolderLandsOnDisk
// guards against (default_course_folder joined onto download_path instead of
// used directly) resurface for a forward-slash-form absolute
// default_course_folder specifically, the one convention that test above does
// not cover. Windows-only for the same reason
// TestSyncCoursesForwardSlashAbsoluteDownloadPathBehavesLikeBackslash is.
func TestSyncCoursesForwardSlashAbsoluteDefaultCourseFolderLandsOnDisk(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("forward-slash-vs-backslash is a Windows-only path ambiguity")
	}

	downloadRoot := t.TempDir()
	absoluteDefaultFolderBackslash := filepath.Join(t.TempDir(), "Default_downloads")
	absoluteDefaultFolderForwardSlash := strings.ReplaceAll(absoluteDefaultFolderBackslash, `\`, "/")

	cfg := config.App{
		DownloadPath:        downloadRoot,
		DefaultCourseFolder: absoluteDefaultFolderForwardSlash,
		CourseFolders:       map[string]string{},
		DownloadConcurrency: 1,
	}
	fd := &fakeDownloader{
		files: []scraper.RemoteFile{{Name: "sheet.pdf", Course: "Analysis I", Path: "sheet.pdf"}},
	}

	stats, err := SyncCourses(context.Background(), fd, cfg, false)
	if err != nil {
		t.Fatalf("SyncCourses returned error: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("expected 1 file downloaded, got %d (errors=%d)", stats.Downloaded, stats.Errors)
	}

	wantPath := filepath.Join(absoluteDefaultFolderBackslash, "Analysis I", "sheet.pdf")
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Fatalf("expected file at %s, stat failed: %v", wantPath, statErr)
	}

	var foundUnderDownloadRoot bool
	_ = filepath.Walk(downloadRoot, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.Name() == "sheet.pdf" {
			foundUnderDownloadRoot = true
		}
		return nil
	})
	if foundUnderDownloadRoot {
		t.Fatalf("file was written under download_path (%s) instead of the forward-slash absolute default_course_folder - doubled-path bug regressed for this path convention", downloadRoot)
	}
}

// TestSyncCoursesForwardSlashAbsoluteDownloadPathBehavesLikeBackslash answers
// half of docs/friction-campaign.md walk 1's open question 4: three path
// conventions (forward-slash absolute, backslash absolute, relative) coexist
// unremarked-on in the Settings form, and only backslash absolute had been
// live spot-checked (walk 4). handleSettings (internal/gui/settings.go) does
// no path normalization at all - whatever string is typed is written to
// config.yaml verbatim - so the real question is whether the sync machinery
// downstream (os.MkdirAll, filepath.Join/Clean) treats "C:/a/b" the same as
// "C:\a\b" on Windows. It does: filepath.Join always cleans through
// filepath.Separator regardless of the slash direction in its inputs, and
// Windows' own file APIs accept forward slashes directly, so this is a
// Windows-specific path-separator question with no Linux equivalent (a
// forward-slash string is already the native, and only, absolute form
// there) - hence windows-only rather than skipped-on-CI.
func TestSyncCoursesForwardSlashAbsoluteDownloadPathBehavesLikeBackslash(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("forward-slash-vs-backslash is a Windows-only path ambiguity")
	}

	backslashRoot := t.TempDir() // t.TempDir() returns the OS-native (backslash) form on Windows
	forwardSlashRoot := strings.ReplaceAll(backslashRoot, `\`, "/")

	file := scraper.RemoteFile{Name: "slides.pdf", Course: "Analysis I", Path: "Analysis I/slides.pdf"}

	cfg := config.App{DownloadPath: forwardSlashRoot, DownloadConcurrency: 1}
	fd := &fakeDownloader{files: []scraper.RemoteFile{file}}

	stats, err := SyncCoursesWithProgress(context.Background(), fd, cfg, false, nil)
	if err != nil {
		t.Fatalf("SyncCoursesWithProgress returned error: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("expected 1 file downloaded, got %d (errors=%d)", stats.Downloaded, stats.Errors)
	}

	wantPath := filepath.Join(backslashRoot, "Analysis I", "slides.pdf")
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Fatalf("expected file at the same location a backslash-form download_path would have used (%s), stat failed: %v", wantPath, statErr)
	}
}

// TestSyncCoursesRelativeDownloadPathResolvesAgainstCWD answers the other
// half of walk 1's open question 4: a relative download_path (the third of
// the three coexisting conventions, and per internal/syncer/migrate.go's own
// displayPath comment "usually" what a real config.yaml uses) is resolved by
// filepath.Abs/os.MkdirAll against the process's current working directory
// at invocation time - not against the config file's own directory. That
// matters because CWD differs across this project's three real launch paths
// (manual CLI from wherever the user `cd`'d, the GUI's shortcut "Start in"
// setting, and the scheduled task's own working-directory field) in a way
// none of the other two conventions are sensitive to.
func TestSyncCoursesRelativeDownloadPathResolvesAgainstCWD(t *testing.T) {
	cwd := t.TempDir()
	prevWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	defer func() {
		if err := os.Chdir(prevWD); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	}()

	file := scraper.RemoteFile{Name: "slides.pdf", Course: "Analysis I", Path: "Analysis I/slides.pdf"}
	cfg := config.App{DownloadPath: "scratch-downloads", DownloadConcurrency: 1}
	fd := &fakeDownloader{files: []scraper.RemoteFile{file}}

	stats, err := SyncCoursesWithProgress(context.Background(), fd, cfg, false, nil)
	if err != nil {
		t.Fatalf("SyncCoursesWithProgress returned error: %v", err)
	}
	if stats.Downloaded != 1 {
		t.Fatalf("expected 1 file downloaded, got %d (errors=%d)", stats.Downloaded, stats.Errors)
	}

	wantPath := filepath.Join(cwd, "scratch-downloads", "Analysis I", "slides.pdf")
	if _, statErr := os.Stat(wantPath); statErr != nil {
		t.Fatalf("expected a relative download_path to resolve against the process CWD (%s), stat failed: %v", wantPath, statErr)
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

// TestFileChangedHealsSignallessManifestEntry pins the fix for the bug where
// a manifest entry with no recorded size/modified could never be detected as
// changed, because the comparison required a non-nil value on both sides -
// and the only way to record one was a download that this very check
// prevented. See fileChanged's doc comment for the live evidence
// (Analysis/Material/AnalysisSkriptChill.pdf, 122 of 370 real entries
// affected).
func TestFileChangedHealsSignallessManifestEntry(t *testing.T) {
	size := int64(643686)
	mod := "15.07.2026 um 12:52 Uhr"

	// The exact real-world shape: remote reports both values, the manifest
	// entry (written before size/modified parsing worked) has neither.
	remote := scraper.RemoteFile{Size: &size, Modified: &mod}
	if !fileChanged(remote, true, FileRecord{}) {
		t.Fatal("expected changed when the manifest entry carries no size/modified but the remote does - such an entry can otherwise never heal")
	}

	// Each signal on its own is enough to trigger the heal.
	if !fileChanged(scraper.RemoteFile{Size: &size}, true, FileRecord{}) {
		t.Fatal("expected changed when only the remote size is newly available")
	}
	if !fileChanged(scraper.RemoteFile{Modified: &mod}, true, FileRecord{}) {
		t.Fatal("expected changed when only the remote modified date is newly available")
	}

	// Once healed, an unchanged file must go back to being skipped - the fix
	// must not turn every sync into a full re-download.
	healed := FileRecord{Size: &size, Modified: &mod}
	if fileChanged(remote, true, healed) {
		t.Fatal("expected unchanged after the entry has been healed; the heal must be one-time, not permanent re-downloading")
	}

	// Neither side has a signal: nothing to compare, and re-downloading every
	// such file on every sync would be a permanent cost. Documented as the
	// deliberate residual gap - it did not occur in either live course probe.
	if fileChanged(scraper.RemoteFile{}, true, FileRecord{}) {
		t.Fatal("expected unchanged when neither side carries any signal")
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

	// Barrier rather than a sleep delay: the concurrency assertion at the
	// end of this test used to rely on a 5ms per-file sleep making overlap
	// "likely enough" to observe, which is the same load-sensitive gamble
	// that made the old throughput test flaky.
	fake := newBarrierDownloader(makeRemoteFiles(fileCount), 4)
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

	if fake.barrierTimedOut() {
		t.Fatalf("downloads never reached 4 in flight at once - the worker pool is not running them concurrently")
	}
	if got := atomic.LoadInt32(&fake.maxConcurrent); got != 4 {
		t.Fatalf("expected exactly 4 downloads in flight at peak (concurrency limit), observed %d", got)
	}
}

// TestSyncCoursesHonoursDownloadConcurrency is the synthetic scheduling
// check originally called for by the perf-02 task. It used to compare
// wall-clock time for concurrency=1 vs concurrency=4 and assert the latter
// was meaningfully faster, which was inherently flaky: on a loaded machine
// the "concurrent" run could measure slower than the sequential one (seen
// at 3.7x slower in the task that added it). It now asserts the thing the
// timing comparison was only a proxy for - how many downloads were actually
// in flight at once - which is deterministic under any system load:
//
//   - concurrency=1 must never observe more than 1 download in flight
//     (the limit is respected), and
//   - concurrency=4 must observe exactly 4 in flight (real parallelism, and
//     still capped at the configured limit).
//
// The concurrency=4 case uses a barrier: each simulated download blocks
// until 4 of them are in flight together. A genuinely parallel worker pool
// always reaches that, however slow the machine; a serialised one never
// does and trips the barrier timeout, so the test still fails loudly if
// concurrency regresses.
func TestSyncCoursesHonoursDownloadConcurrency(t *testing.T) {
	const fileCount = 12

	t.Run("concurrency=1 stays serial", func(t *testing.T) {
		dir := t.TempDir()
		fake := &fakeDownloader{files: makeRemoteFiles(fileCount), delay: time.Millisecond}
		cfg := config.App{DownloadPath: dir, DownloadConcurrency: 1}

		stats, err := SyncCourses(context.Background(), fake, cfg, false)
		if err != nil {
			t.Fatalf("SyncCourses returned error: %v", err)
		}
		if stats.Downloaded != fileCount {
			t.Fatalf("expected %d downloaded, got %d", fileCount, stats.Downloaded)
		}
		if got := atomic.LoadInt32(&fake.maxConcurrent); got != 1 {
			t.Fatalf("concurrency=1 should never run more than 1 download at a time, observed max %d", got)
		}
	})

	t.Run("concurrency=4 runs four at once", func(t *testing.T) {
		const want int32 = 4

		dir := t.TempDir()
		fake := newBarrierDownloader(makeRemoteFiles(fileCount), want)
		cfg := config.App{DownloadPath: dir, DownloadConcurrency: int(want)}

		stats, err := SyncCourses(context.Background(), fake, cfg, false)
		if err != nil {
			t.Fatalf("SyncCourses returned error: %v", err)
		}
		if stats.Downloaded != fileCount {
			t.Fatalf("expected %d downloaded, got %d", fileCount, stats.Downloaded)
		}
		if fake.barrierTimedOut() {
			t.Fatalf("downloads never reached %d in flight at once - the worker pool is not running them concurrently", want)
		}
		if got := atomic.LoadInt32(&fake.maxConcurrent); got != want {
			t.Fatalf("expected exactly %d downloads in flight at peak, observed %d", want, got)
		}
	})
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
	// 10, not 8: a failed download now writes a negative manifest entry
	// (FailCount/FailedAt, no Size/Modified) rather than nothing at all -
	// Question 44's policy half, docs/sync-speed-model.md. That entry is
	// what lets the *next* sync back off instead of retrying at full cost.
	if len(manifest.Files) != 10 {
		t.Fatalf("expected 10 manifest entries (8 downloaded + 2 failure records), got %d", len(manifest.Files))
	}
	for _, badIdx := range []int{2, 7} {
		key := filepath.ToSlash(filepath.Join("Course A", files[badIdx].Name))
		rec, ok := manifest.Files[key]
		if !ok {
			t.Fatalf("expected a negative manifest entry for failed download %s", key)
		}
		if rec.Size != nil || rec.Modified != nil {
			t.Fatalf("failed download %s should carry no Size/Modified, got %+v", key, rec)
		}
		if rec.FailCount != 1 {
			t.Fatalf("expected FailCount=1 for %s, got %d", key, rec.FailCount)
		}
		if rec.FailedAt == nil || *rec.FailedAt == "" {
			t.Fatalf("expected FailedAt to be set for %s", key)
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

// TestProcessRemoteFilesBacksOffAfterRepeatedFailure is Question 44's policy
// half (docs/sync-speed-model.md): a file with a recent failure record must
// not be retried at full cost on the very next sync.
func TestProcessRemoteFilesBacksOffAfterRepeatedFailure(t *testing.T) {
	dir := t.TempDir()
	cfg := config.App{DownloadPath: dir}

	targetKey := "Course A/a.pdf"
	failedAt := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339Nano)
	manifest := &Manifest{Path: filepath.Join(dir, ".opal-sync.manifest.json"), Files: map[string]FileRecord{
		targetKey: {FailCount: 1, FailedAt: &failedAt},
	}}

	size := int64(123)
	remoteFiles := []scraper.RemoteFile{
		{Name: "a.pdf", Course: "Course A", Path: "a.pdf", URL: "https://example.test/a.pdf", Size: &size},
	}

	downloadCalled := false
	downloadFn := func(fileURL, localPath string) error {
		downloadCalled = true
		return nil
	}

	stats := processRemoteFiles(context.Background(), remoteFiles, manifest, cfg, false, downloadFn, func(Event) {})

	if downloadCalled {
		t.Fatal("expected downloadFn not to be called while a recent failure is still backing off")
	}
	if stats.Skipped != 1 || stats.SkippedFailing != 1 {
		t.Fatalf("expected 1 Skipped and 1 SkippedFailing, got %+v", stats)
	}
	if rec := manifest.Files[targetKey]; rec.FailCount != 1 {
		t.Fatalf("expected the backed-off entry's FailCount to stay 1 (no attempt made), got %+v", rec)
	}
}

// TestProcessRemoteFilesRetriesOnceBackoffExpires confirms the flip side: once
// enough time has passed, the file is attempted again like any other.
func TestProcessRemoteFilesRetriesOnceBackoffExpires(t *testing.T) {
	dir := t.TempDir()
	cfg := config.App{DownloadPath: dir}

	targetKey := "Course A/a.pdf"
	failedAt := time.Now().UTC().Add(-48 * time.Hour).Format(time.RFC3339Nano)
	manifest := &Manifest{Path: filepath.Join(dir, ".opal-sync.manifest.json"), Files: map[string]FileRecord{
		targetKey: {FailCount: 1, FailedAt: &failedAt}, // 1st-failure backoff is 6h, long expired
	}}

	size := int64(123)
	remoteFiles := []scraper.RemoteFile{
		{Name: "a.pdf", Course: "Course A", Path: "a.pdf", URL: "https://example.test/a.pdf", Size: &size},
	}

	downloadFn := func(fileURL, localPath string) error {
		return os.WriteFile(localPath, []byte("data"), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remoteFiles, manifest, cfg, false, downloadFn, func(Event) {})

	if stats.Downloaded != 1 || stats.SkippedFailing != 0 {
		t.Fatalf("expected the file to be retried and succeed once backoff expired, got %+v", stats)
	}
	rec := manifest.Files[targetKey]
	if rec.FailCount != 0 || rec.FailedAt != nil {
		t.Fatalf("expected a successful retry to clear the failure record, got %+v", rec)
	}
}

// TestProcessRemoteFilesForceBypassesBackoff confirms force (the escape
// hatch a maintainer already reaches for to ignore manifest state) also
// ignores an active backoff, rather than adding a second, separate override.
func TestProcessRemoteFilesForceBypassesBackoff(t *testing.T) {
	dir := t.TempDir()
	cfg := config.App{DownloadPath: dir}

	targetKey := "Course A/a.pdf"
	failedAt := time.Now().UTC().Add(-1 * time.Minute).Format(time.RFC3339Nano)
	manifest := &Manifest{Path: filepath.Join(dir, ".opal-sync.manifest.json"), Files: map[string]FileRecord{
		targetKey: {FailCount: 1, FailedAt: &failedAt},
	}}

	size := int64(123)
	remoteFiles := []scraper.RemoteFile{
		{Name: "a.pdf", Course: "Course A", Path: "a.pdf", URL: "https://example.test/a.pdf", Size: &size},
	}

	downloadCalled := false
	downloadFn := func(fileURL, localPath string) error {
		downloadCalled = true
		return os.WriteFile(localPath, []byte("data"), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remoteFiles, manifest, cfg, true /* force */, downloadFn, func(Event) {})

	if !downloadCalled {
		t.Fatal("expected force to bypass an active backoff and call downloadFn")
	}
	if stats.Downloaded != 1 {
		t.Fatalf("expected 1 downloaded, got %+v", stats)
	}
}

// TestDownloadBackoffForEscalates locks down the step schedule itself so a
// future edit notices if it accidentally flattens or reorders it.
func TestDownloadBackoffForEscalates(t *testing.T) {
	prev := time.Duration(0)
	for failCount := 1; failCount <= 6; failCount++ {
		got := downloadBackoffFor(failCount)
		if got < prev {
			t.Fatalf("expected downloadBackoffFor to never decrease, failCount=%d got %s after %s", failCount, got, prev)
		}
		prev = got
	}
	if got := downloadBackoffFor(0); got != 0 {
		t.Fatalf("expected downloadBackoffFor(0) == 0, got %s", got)
	}
	// Caps at the last step rather than growing unbounded.
	if downloadBackoffFor(4) != downloadBackoffFor(100) {
		t.Fatalf("expected backoff to cap at the last step: failCount=4 -> %s, failCount=100 -> %s",
			downloadBackoffFor(4), downloadBackoffFor(100))
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

	// The file must carry a real size/date on both sides: a file with NO
	// remote signal is deliberately re-fetched to compare bytes now (see
	// needsContentVerification), because "unchanged" would otherwise be an
	// assumption rather than a finding. That case is covered by
	// TestProcessRemoteFilesVerifiesSignallessFiles below.
	unchangedSize := int64(4)
	unchangedModified := "01.07.2026 um 09:06 Uhr"
	manifest := &Manifest{Path: filepath.Join(dir, ".opal-sync.manifest.json"), Files: map[string]FileRecord{
		targetKey: {Size: &unchangedSize, Modified: &unchangedModified},
	}}

	remoteFiles := []scraper.RemoteFile{
		{Name: "a.pdf", Course: "Course A", Path: "a.pdf", URL: "https://example.test/a.pdf", Size: &unchangedSize, Modified: &unchangedModified},
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

// TestSplitTechnicalDetailSeparatesShortClauseFromDetail locks in the split
// internal/scraper/download.go's browser-fallback error and
// internal/netcheck both format their errors for: a short, user-facing
// clause followed by "(technical detail: ...)". This is what keeps a real
// per-file failure's Playwright locator/timeout internals out of the CLI's
// stdout and the GUI's main log line - see printSyncError and
// docs/BACKLOG-archive.md's friction-campaign finding this fixed.
func TestSplitTechnicalDetailSeparatesShortClauseFromDetail(t *testing.T) {
	err := fmt.Errorf("response is HTML, browser fallback click did not find a downloadable link after 2 attempts (technical detail: %w)",
		errors.New("on https://example.test/section: locator.click: Timeout 5000ms exceeded"))

	short, detail := splitTechnicalDetail(err)

	wantShort := "response is HTML, browser fallback click did not find a downloadable link after 2 attempts"
	if short != wantShort {
		t.Fatalf("short clause = %q, want %q", short, wantShort)
	}
	wantDetail := "on https://example.test/section: locator.click: Timeout 5000ms exceeded"
	if detail != wantDetail {
		t.Fatalf("detail = %q, want %q", detail, wantDetail)
	}
}

// TestSplitTechnicalDetailPassesThroughAnOrdinaryError guards the common
// case - most errors (a plain os.MkdirAll failure, for instance) carry no
// marker at all, and must come back unchanged rather than truncated or
// mangled by a marker search that assumes one is always present.
func TestSplitTechnicalDetailPassesThroughAnOrdinaryError(t *testing.T) {
	err := errors.New("mkdir /no/such/place: permission denied")

	short, detail := splitTechnicalDetail(err)

	if short != err.Error() {
		t.Fatalf("short clause = %q, want the whole message %q", short, err.Error())
	}
	if detail != "" {
		t.Fatalf("expected no detail for a marker-free error, got %q", detail)
	}
}
