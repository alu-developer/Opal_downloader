package syncer

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/timing"
)

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
)

// Event is a single incremental progress notification fired during
// SyncCoursesWithProgress. Course/File are populated for the events they're
// relevant to; Err is set for EventError; Stats is set for EventComplete.
type Event struct {
	Type   EventType
	Course string
	File   string
	Err    error
	Stats  Stats
}

// ProgressFunc receives incremental progress events during a sync run.
type ProgressFunc func(Event)

type FileRecord struct {
	Size     *int64  `json:"size"`
	Modified *string `json:"modified"`
}

type Manifest struct {
	Path  string
	Files map[string]FileRecord
}

type manifestJSON struct {
	UpdatedAt string                `json:"updated_at"`
	Files     map[string]FileRecord `json:"files"`
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
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Files:     sortedFiles,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.Path, data, 0o644)
}

// downloadJob is one file queued for download after the manifest comparison
// pass has decided it needs to be fetched.
type downloadJob struct {
	targetKey  string
	localPath  string
	remoteFile scraper.RemoteFile
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
}

// SyncCourses runs a sync with no progress callback; CLI output is
// unchanged from before progress reporting was added.
func SyncCourses(ctx context.Context, sc Downloader, cfg config.App, force bool) (Stats, error) {
	return SyncCoursesWithProgress(ctx, sc, cfg, force, nil)
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

	if err := os.MkdirAll(cfg.DownloadPath, 0o755); err != nil {
		return Stats{}, err
	}

	manifestPath := filepath.Join(cfg.DownloadPath, ".opal-sync.manifest.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return Stats{}, err
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
func processRemoteFiles(ctx context.Context, remoteFiles []scraper.RemoteFile, manifest *Manifest, cfg config.App, force bool, downloadFn func(fileURL, localPath string) error, progress ProgressFunc) Stats {
	stats := Stats{Downloads: &timing.DownloadTracker{}}

	sort.Slice(remoteFiles, func(i, j int) bool { return remoteFiles[i].Path < remoteFiles[j].Path })

	jobs := make([]downloadJob, 0, len(remoteFiles))
	seenCourses := map[string]bool{}
	for _, remoteFile := range remoteFiles {
		if !seenCourses[remoteFile.Course] {
			seenCourses[remoteFile.Course] = true
			progress(Event{Type: EventCourseStarted, Course: remoteFile.Course})
		}

		resolved := resolveRemoteTargetPath(cfg, remoteFile)
		targetKey := filepath.ToSlash(resolved.ManifestKey)
		localPath := resolved.LocalPath

		previous, ok := manifest.Files[targetKey]
		changed := force || fileChanged(remoteFile, ok, previous)

		if !changed {
			if _, err := os.Stat(localPath); err == nil {
				stats.Skipped++
				progress(Event{Type: EventFileSkipped, Course: remoteFile.Course, File: targetKey})
				continue
			}
		}

		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			stats.Errors++
			fmt.Printf("  error: %s (%v)\n", targetKey, err)
			progress(Event{Type: EventError, Course: remoteFile.Course, File: targetKey, Err: err})
			continue
		}

		jobs = append(jobs, downloadJob{targetKey: targetKey, localPath: localPath, remoteFile: remoteFile})
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

func fileChanged(remote scraper.RemoteFile, hasPrevious bool, previous FileRecord) bool {
	if !hasPrevious {
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
