package syncer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
	"github.com/alu-developer/opal-downloader/internal/timing"
)

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

// SyncCourses runs a sync with no progress callback; CLI output is
// unchanged from before progress reporting was added.
func SyncCourses(sc *scraper.OpalScraper, cfg config.App, force bool) (Stats, error) {
	return SyncCoursesWithProgress(sc, cfg, force, nil)
}

// SyncCoursesWithProgress runs a sync, invoking progress (if non-nil) with
// incremental events as courses/files are processed, in addition to the
// existing stdout output. progress may be nil, in which case behavior is
// identical to SyncCourses.
func SyncCoursesWithProgress(sc *scraper.OpalScraper, cfg config.App, force bool, progress ProgressFunc) (Stats, error) {
	if progress == nil {
		progress = func(Event) {}
	}

	if err := os.MkdirAll(cfg.DownloadPath, 0o755); err != nil {
		return Stats{}, err
	}

	manifestPath := filepath.Join(cfg.DownloadPath, ".opal-sync.manifest.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return Stats{}, err
	}

	remoteFiles, err := sc.ScrapeWithSavedSession(cfg.Courses)
	if err != nil {
		return Stats{}, err
	}

	return syncRemoteFiles(remoteFiles, manifest, cfg, force, sc.DownloadFile, progress)
}

// syncRemoteFiles runs the manifest-diff-and-download phase of a sync given
// an already-discovered list of remote files. It is split out from
// SyncCoursesWithProgress so the download-timing behavior (stats.DownloadDuration
// must exclude discovery/crawl time, which happens before this function is
// ever called) can be exercised in tests with a fake downloadFn, without
// needing a real *scraper.OpalScraper/browser.
func syncRemoteFiles(remoteFiles []scraper.RemoteFile, manifest *Manifest, cfg config.App, force bool, downloadFn func(url, localPath string) error, progress ProgressFunc) (Stats, error) {
	if progress == nil {
		progress = func(Event) {}
	}

	// The download timer starts only after discovery has returned (the
	// caller only invokes syncRemoteFiles post-discovery), so
	// stats.DownloadDuration (and the "Download:" line printed from it)
	// measures actual download work, not discovery/crawl time.
	syncTimer := timing.StartTimer()
	fmt.Printf("Discovered %d remote files. Comparing against local manifest...\n", len(remoteFiles))

	stats := processRemoteFiles(remoteFiles, manifest, cfg, force, downloadFn, progress)

	if err := manifest.Save(); err != nil {
		return stats, err
	}

	stats.DownloadDuration = syncTimer.Elapsed()
	progress(Event{Type: EventComplete, Stats: stats})

	return stats, nil
}

// processRemoteFiles applies the changed/skip/download decision and
// progress-event firing for a discovered file list. Extracted from
// syncRemoteFiles so it can be unit tested without a real
// *scraper.OpalScraper (downloadFn stands in for sc.DownloadFile).
func processRemoteFiles(remoteFiles []scraper.RemoteFile, manifest *Manifest, cfg config.App, force bool, downloadFn func(fileURL, localPath string) error, progress ProgressFunc) Stats {
	stats := Stats{Downloads: &timing.DownloadTracker{}}

	sort.Slice(remoteFiles, func(i, j int) bool { return remoteFiles[i].Path < remoteFiles[j].Path })

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

		fileTimer := timing.StartTimer()
		downloadErr := downloadFn(remoteFile.URL, localPath)
		fileElapsed := fileTimer.Elapsed()
		if downloadErr != nil {
			stats.Errors++
			fmt.Printf("  error: %s (%v)\n", targetKey, downloadErr)
			progress(Event{Type: EventError, Course: remoteFile.Course, File: targetKey, Err: downloadErr})
			continue
		}

		stats.Downloads.Record(fileElapsed, remoteFile.Size)
		timing.PrintProfileLine("downloaded %s in %s", targetKey, fileElapsed)

		manifest.Files[targetKey] = FileRecord{
			Size:     remoteFile.Size,
			Modified: remoteFile.Modified,
		}
		stats.Downloaded++
		fmt.Printf("  downloaded: %s\n", targetKey)
		progress(Event{Type: EventFileDownloaded, Course: remoteFile.Course, File: targetKey})
	}

	return stats
}

func ListAvailableCourses(sc *scraper.OpalScraper) error {
	fmt.Println("Fetching courses from OPAL...")
	files, err := sc.ScrapeWithSavedSession([]string{"*"})
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
// ManifestKey is always relative to cfg.DownloadPath - it is used as the
// manifest's dedup/change-tracking key regardless of where the file
// physically lands, so files redirected to a subfolder_destinations override
// still get their own stable manifest entry.
//
// LocalPath is the actual filesystem path to download to. For the common
// case (no override) it is filepath.Join(cfg.DownloadPath, ManifestKey). When
// a subfolder_destinations override applies, LocalPath instead points
// directly at the configured destination (which may be outside
// cfg.DownloadPath entirely), bypassing the normal course folder.
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
			manifestKey := filepath.Join(resolveCourseSubfolderBase(cfg, remoteFile.Course, sectionName), remoteFile.Name)
			return resolvedTarget{
				ManifestKey: manifestKey,
				LocalPath:   filepath.Join(destination, remoteFile.Name),
			}
		}
	}

	base := resolveCourseSubfolderBase(cfg, remoteFile.Course, sectionName)
	manifestKey := filepath.Join(base, remoteFile.Name)
	return resolvedTarget{
		ManifestKey: manifestKey,
		LocalPath:   filepath.Join(cfg.DownloadPath, manifestKey),
	}
}

// resolveCourseSubfolderBase computes the course-folder-relative directory a
// file belongs in, applying section subfolders when enabled and a matching
// section name is available. This is shared between the manifest key
// computation and the default (non-override) local path computation so both
// stay in sync.
func resolveCourseSubfolderBase(cfg config.App, courseName, sectionName string) string {
	folder, explicit := config.ResolveCourseFolder(cfg, courseName)

	var base string
	if explicit {
		base = folder
	} else if cfg.DefaultCourseFolder != "" {
		base = filepath.Join(folder, courseName)
	} else {
		base = folder
	}

	if cfg.UseSectionSubfolders && sectionName != "" {
		subfolder := config.ResolveSectionFolderName(cfg, sectionName)
		if subfolder != "" {
			base = filepath.Join(base, subfolder)
		}
	}

	return base
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
