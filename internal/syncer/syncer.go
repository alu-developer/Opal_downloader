package syncer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

type Stats struct {
	Downloaded int
	Skipped    int
	Errors     int
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

	stats := Stats{}
	if err := os.MkdirAll(cfg.DownloadPath, 0o755); err != nil {
		return stats, err
	}

	manifestPath := filepath.Join(cfg.DownloadPath, ".opal-sync.manifest.json")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return stats, err
	}

	remoteFiles, err := sc.ScrapeWithSavedSession(cfg.Courses)
	if err != nil {
		return stats, err
	}
	fmt.Printf("Discovered %d remote files. Comparing against local manifest...\n", len(remoteFiles))

	stats = processRemoteFiles(remoteFiles, manifest, cfg, force, sc.DownloadFile, progress)

	if err := manifest.Save(); err != nil {
		return stats, err
	}

	progress(Event{Type: EventComplete, Stats: stats})

	return stats, nil
}

// processRemoteFiles applies the changed/skip/download decision and
// progress-event firing for a discovered file list. Extracted from
// SyncCoursesWithProgress so it can be unit tested without a real
// *scraper.OpalScraper (downloadFn stands in for sc.DownloadFile).
func processRemoteFiles(remoteFiles []scraper.RemoteFile, manifest *Manifest, cfg config.App, force bool, downloadFn func(fileURL, localPath string) error, progress ProgressFunc) Stats {
	stats := Stats{}

	sort.Slice(remoteFiles, func(i, j int) bool { return remoteFiles[i].Path < remoteFiles[j].Path })

	seenCourses := map[string]bool{}
	for _, remoteFile := range remoteFiles {
		if !seenCourses[remoteFile.Course] {
			seenCourses[remoteFile.Course] = true
			progress(Event{Type: EventCourseStarted, Course: remoteFile.Course})
		}

		targetPath := resolveRemoteTargetPath(cfg, remoteFile)
		targetKey := filepath.ToSlash(targetPath)
		localPath := filepath.Join(cfg.DownloadPath, targetPath)

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

		if err := downloadFn(remoteFile.URL, localPath); err != nil {
			stats.Errors++
			fmt.Printf("  error: %s (%v)\n", targetKey, err)
			progress(Event{Type: EventError, Course: remoteFile.Course, File: targetKey, Err: err})
			continue
		}

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

func resolveRemoteTargetPath(cfg config.App, remoteFile scraper.RemoteFile) string {
	folder, explicit := config.ResolveCourseFolder(cfg, remoteFile.Course)
	if explicit {
		return filepath.Join(folder, remoteFile.Name)
	}
	if cfg.DefaultCourseFolder != "" {
		return filepath.Join(folder, remoteFile.Course, remoteFile.Name)
	}
	return filepath.Join(folder, remoteFile.Name)
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
