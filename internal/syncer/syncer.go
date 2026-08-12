package syncer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/synclock"
	"github.com/alu-developer/opal-downloader/internal/timing"
)

// acquireSyncLock is a package-level indirection over
// synclock.AcquireDefault - a cross-process, PID-stamped single-instance
// lock at ~/.opal-downloader/sync.lock (see docs/scheduled-sync-plan.md's
// "Overlap guard" section) - so a Task Scheduler-triggered
// `sync --scheduled` run, a manual CLI `sync`, and a GUI-triggered sync can
// never race each other: whichever one starts first holds the lock for its
// entire run (SyncCoursesWithProgress below), and any other invocation
// started while that's still true fails fast with a clear "already
// running" error instead of contending over the same manifest/downloads.
// Overridden in tests (see syncer_test.go's TestMain) to a no-op so `go
// test` never touches the real ~/.opal-downloader directory or contends
// with an actual sync that might be running on the machine.
var acquireSyncLock = synclock.AcquireDefault

// discoveryProgressReporter is the optional half of Downloader: a downloader
// that can report what its crawl is doing while it does it. *scraper.OpalScraper
// implements it; test fakes generally do not, which is why it is kept
// separate from Downloader rather than folded into it.
type discoveryProgressReporter interface {
	SetDiscoveryProgress(func(scraper.DiscoveryProgress))
}

// formatDiscoveryProgress renders one crawl event as a single display line,
// or "" for an event not worth showing. Kept here rather than in the GUI so
// the CLI and any other front end describe a sync identically.
func formatDiscoveryProgress(p scraper.DiscoveryProgress) string {
	switch p.Phase {
	case scraper.PhaseCoursesFound:
		return fmt.Sprintf("Found %d course(s) - starting to scan them", p.TotalCourses)
	case scraper.PhaseCourseStarted:
		if p.TotalCourses > 0 {
			return fmt.Sprintf("Scanning course %d of %d: %s", p.CourseIndex, p.TotalCourses, p.Course)
		}
		return fmt.Sprintf("Scanning course: %s", p.Course)
	case scraper.PhaseSection:
		section := strings.TrimSpace(p.Section)
		if section == "" {
			return fmt.Sprintf("%s - scanned %d section(s)", p.Course, p.SectionsDone)
		}
		return fmt.Sprintf("%s - section %d: %s", p.Course, p.SectionsDone, section)
	case scraper.PhaseCourseDone:
		return fmt.Sprintf("Finished %s - found %d file(s)", p.Course, p.FileCount)
	}
	return ""
}

// Downloader is the subset of *scraper.OpalScraper's behavior SyncCourses
// depends on. Extracted as an interface so tests can exercise the
// concurrent-download scheduling logic with a fake, without needing a real
// browser/Playwright context.
type Downloader interface {
	ScrapeWithSavedSession(ctx context.Context, courseFilter []string) ([]scraper.RemoteFile, error)
	DownloadFile(fileURL, localPath string) error
}

type Stats struct {
	Downloaded int
	Skipped    int
	Errors     int

	// DownloadDuration is the wall-clock time spent downloading files, i.e.
	// everything in SyncCourses AFTER remote discovery
	// (sc.ScrapeWithSavedSession) has returned: manifest comparison plus
	// every file download. It intentionally excludes discovery/crawl time,
	// which is reported separately via timing.PrintDiscoverySummary inside
	// the scraper. Downloads is the per-file timing tracker used to report
	// throughput at the end of a run (perf-01 instrumentation groundwork for
	// perf-02/03/04).
	// Downloads is a pointer (rather than an embedded value) because
	// timing.DownloadTracker holds a mutex for concurrency-safety (needed by
	// perf-02's parallel downloads); embedding it by value in Stats would
	// make returning Stats by value copy the lock, which go vet flags.
	DownloadDuration time.Duration
	Downloads        *timing.DownloadTracker
}

// EventType identifies the kind of progress event fired during a sync run.
type EventType int

const (
	EventCourseStarted EventType = iota
	EventFileDownloaded
	EventFileSkipped
	EventError
	EventComplete
	// EventMigration reports one line of a manifest key-scheme migration
	// (see migrate.go). Message carries the text; no other field is set.
	EventMigration
	// EventDiscovery relays one crawl-phase progress event from the scraper
	// (scraper.DiscoveryProgress). Message carries a ready-to-display line;
	// Course is set when the event belongs to a specific course.
	//
	// This exists because discovery - the long half of a sync, which walks
	// every section of every course - previously reported nothing at all
	// until it had entirely finished, leaving the UI on a single static
	// "Fetching courses from OPAL..." line for minutes with no way to tell
	// progress from a hang. Every other event type here fires only in the
	// download phase, i.e. after discovery has already returned.
	EventDiscovery
)

// Event is a single incremental progress notification fired during
// SyncCoursesWithProgress. Course/File are populated for the events they're
// relevant to; Err is set for EventError; Stats is set for EventComplete.
// CourseIndex/TotalCourses are set for EventCourseStarted only: remote
// discovery (sc.ScrapeWithSavedSession) already returns the full remote file
// list before the per-course loop begins, so the distinct course count is
// known upfront at effectively no extra cost - CourseIndex is this course's
// 1-based position among distinct courses in discovery order, TotalCourses
// is the total distinct course count.
type Event struct {
	Type         EventType
	Course       string
	File         string
	Err          error
	Stats        Stats
	CourseIndex  int
	TotalCourses int

	// Message carries free-form text for event types that are informational
	// rather than per-file (currently EventMigration only).
	Message string
}

// ProgressFunc receives incremental progress events during a sync run.
type ProgressFunc func(Event)

type FileRecord struct {
	Size     *int64  `json:"size"`
	Modified *string `json:"modified"`
}

// ManifestFileName is the manifest's fixed file name inside download_path.
const ManifestFileName = ".opal-sync.manifest.json"

// ManifestSchemaVersion is the current manifest format/key-scheme version.
// Bump it whenever manifest *key derivation* changes in a way that makes
// previously-stored keys stop matching what resolveRemoteTargetPath now
// produces, so a stale manifest is detectable at load time instead of
// silently presenting as "every file is new".
//
// History:
//   - 1 (implied by a missing schema_version field): every manifest written
//     before this constant existed. Two key-scheme changes shipped under
//     that unversioned format - use_section_subfolders (which inserts a
//     section component into the key) and PR #69's re-basing of absolute
//     course folders onto the sanitized course name - so a version-1
//     manifest's keys may or may not match today's scheme depending on the
//     user's config.
//   - 2: first versioned format. Key scheme is whatever
//     resolveRemoteTargetPath produces today.
const ManifestSchemaVersion = 2

// LegacyManifestSchemaVersion is the version assumed for a manifest that has
// no schema_version field at all (everything written before versioning).
const LegacyManifestSchemaVersion = 1

type Manifest struct {
	Path  string
	Files map[string]FileRecord

	// SchemaVersion is the version the manifest was *loaded* with
	// (LegacyManifestSchemaVersion for a pre-versioning file, 0 for a
	// manifest that does not exist on disk yet). Save always writes
	// ManifestSchemaVersion.
	SchemaVersion int
}

type manifestJSON struct {
	SchemaVersion int                   `json:"schema_version"`
	UpdatedAt     string                `json:"updated_at"`
	Files         map[string]FileRecord `json:"files"`
}

func LoadManifest(path string) (*Manifest, error) {
	manifest := &Manifest{Path: path, Files: map[string]FileRecord{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return manifest, nil
		}
		return nil, err
	}

	var payload manifestJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}

	manifest.SchemaVersion = payload.SchemaVersion
	if manifest.SchemaVersion == 0 && len(payload.Files) > 0 {
		manifest.SchemaVersion = LegacyManifestSchemaVersion
	}

	for key, value := range payload.Files {
		manifest.Files[key] = value
	}
	return manifest, nil
}

func (m *Manifest) Save() error {
	if err := os.MkdirAll(filepath.Dir(m.Path), 0o755); err != nil {
		return err
	}

	sortedKeys := make([]string, 0, len(m.Files))
	for key := range m.Files {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	sortedFiles := make(map[string]FileRecord, len(sortedKeys))
	for _, key := range sortedKeys {
		sortedFiles[key] = m.Files[key]
	}

	payload := manifestJSON{
		SchemaVersion: ManifestSchemaVersion,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		Files:         sortedFiles,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.Path, data, 0o644); err != nil {
		return err
	}
	m.SchemaVersion = ManifestSchemaVersion
	return nil
}

// downloadJob is one file queued for download after the manifest comparison
// pass has decided it needs to be fetched.
//
// verify marks the job as a content check rather than a plain fetch: the
// file is downloaded to a temporary path beside its target and only replaces
// it when the bytes actually differ. See needsContentVerification for why
// some files can only be checked this way.
type downloadJob struct {
	targetKey  string
	localPath  string
	remoteFile scraper.RemoteFile
	verify     bool
}

// needsContentVerification reports whether a file that the manifest
// comparison considered unchanged must nonetheless be re-fetched to find
// out.
//
// It applies to exactly one case: OPAL reports neither a size nor a
// modified date for the file, so there is no signal to compare and
// fileChanged can only ever answer "unchanged". Such a file is invisible to
// change detection forever - an upstream edit is silently never picked up.
// Measured live (2026-07-22): 13 of 14 files in "So26 Programmieren
// (Math-Ba-PR20)" are in this state, every one of them in a Woche 05-13
// section, so it tracks a section layout rather than a file type.
//
// Note this is NOT solvable by recording the downloaded file's own on-disk
// size in the manifest, which is the obvious-looking fix: the local file
// does not change when the remote one does, so a local-size comparison
// still reports "unchanged" for exactly the case that matters. Only the
// bytes themselves settle it.
//
// Deliberately narrow. Files that do carry a size/date are untouched, so
// the normal fast path is unaffected: on the maintainer's account this
// covers 13 of 344 files (~4%).
func needsContentVerification(remote scraper.RemoteFile, hasPrevious bool) bool {
	return hasPrevious && remote.Size == nil && remote.Modified == nil
}

// verificationTempPath returns a scratch path next to target for a verify
// job's download. The original extension is preserved because
// scraper.DownloadFile validates payloads by target extension (a ".pdf"
// download that does not start with %PDF is rejected), and a temp name that
// dropped the suffix would quietly skip that check. The leading dot keeps
// the scratch file out of the way if a crash ever leaves one behind.
func verificationTempPath(target string) string {
	dir := filepath.Dir(target)
	base := filepath.Base(target)
	ext := filepath.Ext(base)
	return filepath.Join(dir, "."+strings.TrimSuffix(base, ext)+".opalverify"+ext)
}

// sameFileContents reports whether the two paths hold identical bytes.
// Any read error returns false, i.e. "assume they differ" - erring toward
// replacing the local copy is the safe direction for a tool whose whole job
// is to keep local files current.
func sameFileContents(a, b string) bool {
	sumA, errA := fileSHA256(a)
	if errA != nil {
		return false
	}
	sumB, errB := fileSHA256(b)
	if errB != nil {
		return false
	}
	return sumA == sumB
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// downloadResult is what a worker reports back for a single job. Manifest
// and stats mutations happen on the main goroutine only (see the result
// collection loop in processRemoteFiles), so no locking is needed there;
// workers only ever write to their own downloadResult value and send it on a
// channel.
type downloadResult struct {
	job     downloadJob
	elapsed time.Duration
	err     error

	// unchanged is set by a verify job that found the freshly-fetched bytes
	// identical to the local copy, so it counts as skipped rather than
	// downloaded and the local file is left completely untouched.
	unchanged bool
}

// runVerifyJob fetches a file to a scratch path beside its target and
// replaces the target only when the bytes differ. It reports unchanged=true
// when they match.
//
// Leaving an identical file alone matters beyond tidiness: the maintainer's
// download_path is inside OneDrive, so rewriting a byte-identical file would
// churn its modification time and trigger a pointless cloud re-upload on
// every single sync, for every file in this category.
func runVerifyJob(job downloadJob, downloadFn func(url, localPath string) error) (unchanged bool, err error) {
	tempPath := verificationTempPath(job.localPath)
	defer func() { _ = os.Remove(tempPath) }()

	if err := downloadFn(job.remoteFile.URL, tempPath); err != nil {
		return false, err
	}
	if sameFileContents(tempPath, job.localPath) {
		return true, nil
	}
	// os.Rename over an existing file is allowed on both Windows and POSIX,
	// but the target may be open/locked (OneDrive, a PDF viewer), so fall
	// back to a copy rather than failing the whole file.
	if err := os.Rename(tempPath, job.localPath); err != nil {
		data, readErr := os.ReadFile(tempPath)
		if readErr != nil {
			return false, readErr
		}
		if writeErr := os.WriteFile(job.localPath, data, 0o644); writeErr != nil {
			return false, writeErr
		}
	}
	return false, nil
}

// SyncCourses runs a sync with no progress callback; CLI output is
// unchanged from before progress reporting was added.
func SyncCourses(ctx context.Context, sc Downloader, cfg config.App, force bool) (Stats, error) {
	// Print the crawl's own progress, which is otherwise entirely silent
	// between "Fetching courses from OPAL..." and the discovery summary -
	// several minutes of nothing on a real account. Only discovery events are
	// echoed; the per-file download lines are already printed by
	// processRemoteFiles, and echoing those here would double them.
	return SyncCoursesWithProgress(ctx, sc, cfg, force, func(e Event) {
		if e.Type == EventDiscovery {
			fmt.Printf("  %s\n", e.Message)
		}
	})
}

// SyncCoursesWithProgress runs a sync, invoking progress (if non-nil) with
// incremental events as courses/files are processed, in addition to the
// existing stdout output. progress may be nil, in which case behavior is
// identical to SyncCourses. ctx is threaded through both phases of the sync -
// discovery (sc.ScrapeWithSavedSession, whose own course-iteration loop stops
// promptly on cancellation - see scraper.OpalScraper.ScrapeWithSavedSession)
// and the download phase (syncRemoteFiles/processRemoteFiles's job-dispatch
// loop) - so a caller can cancel a run in progress (the GUI's /sync/cancel
// handler) and get back an error satisfying errors.Is(err, context.Canceled)
// instead of a silently truncated success.
func SyncCoursesWithProgress(ctx context.Context, sc Downloader, cfg config.App, force bool, progress ProgressFunc) (Stats, error) {
	if progress == nil {
		progress = func(Event) {}
	}

	if err := ctx.Err(); err != nil {
		return Stats{}, err
	}

	// Overlap guard: held for this call's entire duration (discovery +
	// every file download), released via defer whether this returns
	// normally, with an error, or the caller's ctx is cancelled. See
	// acquireSyncLock's doc comment above.
	release, err := acquireSyncLock()
	if err != nil {
		return Stats{}, err
	}
	defer release()

	if err := os.MkdirAll(cfg.DownloadPath, 0o755); err != nil {
		return Stats{}, err
	}

	manifestPath := filepath.Join(cfg.DownloadPath, ManifestFileName)
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return Stats{}, err
	}

	// Relay the scraper's crawl-phase progress, when the downloader supports
	// it. Declared as its own optional interface rather than added to
	// Downloader so the fake downloaders in this package's tests (and any
	// other non-browser implementation) keep satisfying Downloader unchanged.
	if reporter, ok := sc.(discoveryProgressReporter); ok {
		reporter.SetDiscoveryProgress(func(p scraper.DiscoveryProgress) {
			if line := formatDiscoveryProgress(p); line != "" {
				progress(Event{Type: EventDiscovery, Course: p.Course, Message: line})
			}
		})
		// Discovery is over by the time this returns; leaving the callback
		// installed would let a later call publish into a stale closure.
		defer reporter.SetDiscoveryProgress(nil)
	}

	remoteFiles, err := sc.ScrapeWithSavedSession(ctx, cfg.Courses)
	if err != nil {
		return Stats{}, err
	}

	return syncRemoteFiles(ctx, remoteFiles, manifest, cfg, force, sc.DownloadFile, progress)
}

// syncRemoteFiles runs the manifest-diff-and-download phase of a sync given
// an already-discovered list of remote files. It is split out from
// SyncCoursesWithProgress so the download-timing behavior (stats.DownloadDuration
// must exclude discovery/crawl time, which happens before this function is
// ever called) can be exercised in tests with a fake downloadFn, without
// needing a real *scraper.OpalScraper/browser.
func syncRemoteFiles(ctx context.Context, remoteFiles []scraper.RemoteFile, manifest *Manifest, cfg config.App, force bool, downloadFn func(url, localPath string) error, progress ProgressFunc) (Stats, error) {
	if progress == nil {
		progress = func(Event) {}
	}

	// The download timer starts only after discovery has returned (the
	// caller only invokes syncRemoteFiles post-discovery), so
	// stats.DownloadDuration (and the "Download:" line printed from it)
	// measures actual download work, not discovery/crawl time.
	syncTimer := timing.StartTimer()
	fmt.Printf("Discovered %d remote files. Comparing against local manifest...\n", len(remoteFiles))

	// Salvage a manifest written under an older key scheme *before* the
	// diff, so re-keyed entries are seen as already-downloaded instead of
	// silently presenting as "everything is new" (see migrate.go).
	if report := migrateManifestKeys(manifest, cfg, remoteFiles); report.Changed() {
		for _, line := range report.Lines() {
			fmt.Println(line)
			progress(Event{Type: EventMigration, Message: line})
		}
	}

	stats := processRemoteFiles(ctx, remoteFiles, manifest, cfg, force, downloadFn, progress)

	if err := manifest.Save(); err != nil {
		return stats, err
	}

	stats.DownloadDuration = syncTimer.Elapsed()

	if err := ctx.Err(); err != nil {
		// Cancelled mid-download: whatever files finished downloading before
		// cancellation are already recorded in manifest (saved just above),
		// so nothing already-fetched is lost or re-fetched next run - but
		// don't fire EventComplete (that reads as a normal finish) and
		// return ctx.Err() instead, so SyncCoursesWithProgress's caller
		// (ultimately publishCancelOrError in internal/gui/sync.go) reports
		// "cancelled" rather than "done".
		return stats, err
	}

	progress(Event{Type: EventComplete, Stats: stats})

	return stats, nil
}

// processRemoteFiles applies the changed/skip/download decision and
// progress-event firing for a discovered file list. Extracted from
// syncRemoteFiles so it can be unit tested without a real
// *scraper.OpalScraper (downloadFn stands in for sc.DownloadFile). Downloads
// are scheduled across a worker pool (see downloadJob/downloadResult) sized
// by cfg.DownloadConcurrency; manifest and stats mutations only ever happen
// on the goroutine draining the result channel, so no locking is needed for
// them even though DownloadFile calls happen concurrently. ctx cancellation
// stops the job-dispatch goroutine from queuing any further not-yet-started
// download (mirroring collectCourseFilesConcurrently's course-dispatch fix
// in internal/scraper/orchestrator.go) - downloads already in flight are not
// aborted mid-transfer, but no new one is started once cancellation is
// observed.
// distinctCourseCount returns the number of distinct Course values across
// remoteFiles. Used to populate Event.TotalCourses for EventCourseStarted -
// see that field's doc comment for why this is cheap (remoteFiles is already
// the complete, in-memory discovery result by the time it's called).
func distinctCourseCount(remoteFiles []scraper.RemoteFile) int {
	seen := map[string]bool{}
	for _, f := range remoteFiles {
		seen[f.Course] = true
	}
	return len(seen)
}

func processRemoteFiles(ctx context.Context, remoteFiles []scraper.RemoteFile, manifest *Manifest, cfg config.App, force bool, downloadFn func(fileURL, localPath string) error, progress ProgressFunc) Stats {
	// Guarded here as well as in syncRemoteFiles: this is called directly by
	// tests, and an unguarded nil callback panics deep inside the result loop
	// rather than at the call site.
	if progress == nil {
		progress = func(Event) {}
	}

	stats := Stats{Downloads: &timing.DownloadTracker{}}

	sort.Slice(remoteFiles, func(i, j int) bool { return remoteFiles[i].Path < remoteFiles[j].Path })

	// totalCourses is cheap to compute here: remoteFiles is the complete,
	// already-discovered file list (discovery happened before
	// processRemoteFiles was ever called), so counting distinct courses is
	// just one pass over a slice already in memory - no extra scraping or
	// restructuring of the sync flow required. See Event's doc comment.
	totalCourses := distinctCourseCount(remoteFiles)

	jobs := make([]downloadJob, 0, len(remoteFiles))
	seenCourses := map[string]bool{}
	courseIndex := 0
	for _, remoteFile := range remoteFiles {
		if !seenCourses[remoteFile.Course] {
			seenCourses[remoteFile.Course] = true
			courseIndex++
			progress(Event{Type: EventCourseStarted, Course: remoteFile.Course, CourseIndex: courseIndex, TotalCourses: totalCourses})
		}

		resolved := resolveRemoteTargetPath(cfg, remoteFile)
		targetKey := filepath.ToSlash(resolved.ManifestKey)
		localPath := resolved.LocalPath

		previous, ok := manifest.Files[targetKey]
		changed := force || fileChanged(remoteFile, ok, previous)

		verify := false
		if !changed {
			if _, err := os.Stat(localPath); err == nil {
				// A file OPAL reports no size/date for cannot be judged from
				// metadata at all, so "unchanged" here is an assumption, not a
				// finding. Queue it as a verify job instead of skipping it
				// outright - see needsContentVerification.
				if !needsContentVerification(remoteFile, ok) {
					stats.Skipped++
					progress(Event{Type: EventFileSkipped, Course: remoteFile.Course, File: targetKey})
					continue
				}
				verify = true
			}
		}

		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			stats.Errors++
			fmt.Printf("  error: %s (%v)\n", targetKey, err)
			progress(Event{Type: EventError, Course: remoteFile.Course, File: targetKey, Err: err})
			continue
		}

		jobs = append(jobs, downloadJob{targetKey: targetKey, localPath: localPath, remoteFile: remoteFile, verify: verify})
	}

	concurrency := cfg.DownloadConcurrency
	if concurrency <= 0 {
		concurrency = config.DefaultDownloadConcurrency
	}
	if concurrency > len(jobs) {
		concurrency = len(jobs)
	}

	if concurrency > 0 {
		jobCh := make(chan downloadJob)
		resultCh := make(chan downloadResult)

		var workers sync.WaitGroup
		workers.Add(concurrency)
		for i := 0; i < concurrency; i++ {
			go func() {
				defer workers.Done()
				for job := range jobCh {
					fileTimer := timing.StartTimer()
					if job.verify {
						unchanged, verifyErr := runVerifyJob(job, downloadFn)
						resultCh <- downloadResult{job: job, elapsed: fileTimer.Elapsed(), err: verifyErr, unchanged: unchanged}
						continue
					}
					downloadErr := downloadFn(job.remoteFile.URL, job.localPath)
					resultCh <- downloadResult{job: job, elapsed: fileTimer.Elapsed(), err: downloadErr}
				}
			}()
		}

		go func() {
			defer close(jobCh)
			for _, job := range jobs {
				select {
				case <-ctx.Done():
					return
				default:
				}
				select {
				case jobCh <- job:
				case <-ctx.Done():
					return
				}
			}
		}()

		go func() {
			workers.Wait()
			close(resultCh)
		}()

		// All manifest/stats mutations and progress-event firing happen here
		// on the single goroutine draining resultCh, so concurrent workers
		// never race on shared state and progress observers never see
		// events out of order relative to each other.
		for result := range resultCh {
			targetKey := result.job.targetKey
			if result.err != nil {
				stats.Errors++
				fmt.Printf("  error: %s (%v)\n", targetKey, result.err)
				progress(Event{Type: EventError, Course: result.job.remoteFile.Course, File: targetKey, Err: result.err})
				continue
			}

			if result.unchanged {
				// Verified byte-for-byte identical: nothing was written, so
				// this is a skip. The manifest entry is deliberately left as
				// it is - there is still no remote signal to record, so it
				// stays a verify candidate next run.
				stats.Skipped++
				timing.PrintProfileLine("verified unchanged %s in %s", targetKey, result.elapsed)
				progress(Event{Type: EventFileSkipped, Course: result.job.remoteFile.Course, File: targetKey})
				continue
			}

			stats.Downloads.Record(result.elapsed, result.job.remoteFile.Size)
			timing.PrintProfileLine("downloaded %s in %s", targetKey, result.elapsed)

			manifest.Files[targetKey] = FileRecord{
				Size:     result.job.remoteFile.Size,
				Modified: result.job.remoteFile.Modified,
			}
			stats.Downloaded++
			fmt.Printf("  downloaded: %s\n", targetKey)
			progress(Event{Type: EventFileDownloaded, Course: result.job.remoteFile.Course, File: targetKey})
		}
	}

	return stats
}

func ListAvailableCourses(ctx context.Context, sc *scraper.OpalScraper) error {
	fmt.Println("Fetching courses from OPAL...")

	// Tracked so the summary can say what happened to every course OPAL
	// enrolled the user in, not just the ones that ended up with files - a
	// course whose crawl finds 0 files would otherwise vanish from the
	// output with no explanation (friction-campaign Walk 3 finding,
	// docs/friction-campaign.md).
	totalCourses := 0
	emptyCourses := 0
	sc.SetDiscoveryProgress(func(p scraper.DiscoveryProgress) {
		switch p.Phase {
		case scraper.PhaseCoursesFound:
			totalCourses = p.TotalCourses
		case scraper.PhaseCourseDone:
			if p.FileCount == 0 {
				emptyCourses++
			}
		}
	})

	files, err := sc.ScrapeWithSavedSession(ctx, []string{"*"})
	if err != nil {
		return err
	}

	courses := map[string]int{}
	for _, file := range files {
		courses[file.Course]++
	}

	courseNames := make([]string, 0, len(courses))
	for name := range courses {
		courseNames = append(courseNames, name)
	}
	sort.Strings(courseNames)

	fmt.Printf("\nFound %d courses:\n\n", len(courseNames))
	for _, course := range courseNames {
		fmt.Printf("  [%s] (%d files)\n", course, courses[course])
	}
	if emptyCourses > 0 {
		fmt.Printf("\n(%d of %d enrolled courses had no files and are not listed above)\n", emptyCourses, totalCourses)
	}
	return nil
}

// resolvedTarget describes where a remote file should land on disk.
//
// ManifestKey is always a relative path - it is used as the manifest's
// dedup/change-tracking key regardless of where the file physically lands,
// so files redirected to a subfolder_destinations override (or to an
// absolute default_course_folder/course_folders value) still get their own
// stable, portable manifest entry with no drive letter or absolute prefix.
//
// LocalPath is the actual filesystem path to download to. For the common
// case (a relative course folder, no override) it is
// filepath.Join(cfg.DownloadPath, ManifestKey). When a subfolder_destinations
// override applies, or the resolved course folder is itself absolute,
// LocalPath instead points directly at that absolute location (which may be
// outside cfg.DownloadPath entirely), bypassing the normal
// cfg.DownloadPath-relative join - joining an absolute path onto
// cfg.DownloadPath would otherwise silently produce a doubled, broken path
// (e.g. "<download_path>\<Course>\C:\Users\...").
type resolvedTarget struct {
	ManifestKey string
	LocalPath   string
}

func resolveRemoteTargetPath(cfg config.App, remoteFile scraper.RemoteFile) resolvedTarget {
	sectionName := strings.TrimSpace(remoteFile.SectionTitle)
	if cfg.UseSectionSubfolders && sectionName != "" {
		if destination, ok := config.ResolveSubfolderDestination(cfg, remoteFile.Course, sectionName); ok {
			// Override destinations bypass the normal course folder entirely and
			// may point outside download_path (e.g. an absolute path elsewhere on
			// disk), so LocalPath is built directly from destination rather than
			// being joined under cfg.DownloadPath. The manifest key still tracks
			// this file under its normal course/subfolder-relative location so
			// change detection stays stable even though the file physically lives
			// elsewhere.
			_, manifestBase := resolveCourseSubfolderBase(cfg, remoteFile.Course, sectionName)
			manifestKey := filepath.Join(manifestBase, remoteFile.Name)
			return resolvedTarget{
				ManifestKey: manifestKey,
				LocalPath:   filepath.Join(destination, remoteFile.Name),
			}
		}
	}

	localBase, manifestBase := resolveCourseSubfolderBase(cfg, remoteFile.Course, sectionName)
	manifestKey := filepath.Join(manifestBase, remoteFile.Name)

	localPath := filepath.Join(localBase, remoteFile.Name)
	if !filepath.IsAbs(localBase) {
		// Relative course folder (the documented, common case): LocalPath is
		// download_path + the same relative path used as the manifest key.
		localPath = filepath.Join(cfg.DownloadPath, manifestKey)
	}

	return resolvedTarget{
		ManifestKey: manifestKey,
		LocalPath:   localPath,
	}
}

// resolveCourseSubfolderBase computes the course-folder directory a file
// belongs in, applying section subfolders when enabled and a matching
// section name is available. It returns two variants:
//
//   - localBase is used to build LocalPath. It carries the resolved course
//     folder verbatim, so it is absolute whenever default_course_folder or a
//     matched course_folders entry is itself configured as an absolute path.
//   - manifestBase is always relative, for use as (part of) the manifest's
//     dedup/change-tracking key. When localBase is absolute, manifestBase
//     falls back to the sanitized course name (the same shape used when no
//     course folder mapping applies at all) instead of embedding the
//     absolute path, so manifest keys never carry a drive letter/absolute
//     prefix and existing relative-path users see no change in key shape.
func resolveCourseSubfolderBase(cfg config.App, courseName, sectionName string) (localBase string, manifestBase string) {
	folder, explicit := config.ResolveCourseFolder(cfg, courseName)

	var base string
	if explicit {
		base = folder
	} else if cfg.DefaultCourseFolder != "" {
		base = filepath.Join(folder, courseName)
	} else {
		base = folder
	}

	manifestBase = base
	if filepath.IsAbs(base) {
		manifestBase = config.SanitizePathComponent(courseName)
	}
	localBase = base

	if cfg.UseSectionSubfolders && sectionName != "" {
		subfolder := config.ResolveSectionFolderName(cfg, sectionName)
		if subfolder != "" {
			localBase = filepath.Join(localBase, subfolder)
			manifestBase = filepath.Join(manifestBase, subfolder)
		}
	}

	return localBase, manifestBase
}

// fileChanged decides whether a discovered remote file needs to be
// (re-)downloaded, by comparing OPAL's "Größe"/"Zuletzt geändert" column
// values against what the manifest recorded the last time this file was
// fetched.
//
// The one-sided cases below (remote has a signal, the manifest entry does
// not) are not a detail - they are the fix for a bug that silently pinned
// files at an outdated version forever. Both comparisons used to require
// the value to be non-nil on *both* sides:
//
//	if remote.Size != nil && previous.Size != nil && *remote.Size != *previous.Size
//
// which looks reasonable but is a trap, because a manifest entry can only
// ever gain a size/modified value by being downloaded, and with this
// guard it could only be downloaded if it already had one. Any entry
// written before size/modified parsing existed (or by a run where the
// parse missed) was therefore stuck: every future sync skipped it, so it
// never got re-recorded, so it stayed stuck. Nothing short of --force or
// deleting the manifest could break the cycle, and neither is something a
// user would know to do - the file just quietly stopped updating.
//
// Measured on the maintainer's real manifest (2026-07-22): 122 of 370
// entries had both values nil, including
// Analysis/Material/AnalysisSkriptChill.pdf - reported as "did not update
// even though the file was extended upstream". A live crawl of that same
// course showed OPAL now reports size=643686 modified="15.07.2026 um 12:52
// Uhr" for it while the local copy sat at 568742 bytes, i.e. the signal was
// available and the diff was real; only the nil manifest side stopped it
// being seen. Re-probing two courses live (Analysis 30/30, Algorithmen und
// Datenstrukturen 38/38) found a signal for every single file, confirming
// the nil entries are stale history rather than an ongoing parser gap.
//
// Treating "remote knows, manifest doesn't" as changed costs one
// re-download per affected entry, once: the download records the values, and
// every sync afterwards compares normally. That is the cheapest way to heal
// an entry, and it fails in the safe direction - a redundant download
// rather than a missed update.
func fileChanged(remote scraper.RemoteFile, hasPrevious bool, previous FileRecord) bool {
	if !hasPrevious {
		return true
	}
	if remote.Size != nil && previous.Size == nil {
		return true
	}
	if remote.Modified != nil && previous.Modified == nil {
		return true
	}
	if remote.Size != nil && previous.Size != nil && *remote.Size != *previous.Size {
		return true
	}
	if remote.Modified != nil && previous.Modified != nil && *remote.Modified != *previous.Modified {
		return true
	}
	return false
}
