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

func SyncCourses(sc *scraper.OpalScraper, cfg config.App, force bool) (Stats, error) {
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

	return syncRemoteFiles(remoteFiles, manifest, cfg, force, sc.DownloadFile)
}

// syncRemoteFiles runs the manifest-diff-and-download phase of a sync given
// an already-discovered list of remote files. It is split out from
// SyncCourses so the download-timing behavior (stats.DownloadDuration must
// exclude discovery/crawl time, which happens before this function is ever
// called) can be exercised in tests with a fake downloadFn, without needing
// a real *scraper.OpalScraper/browser.
func syncRemoteFiles(remoteFiles []scraper.RemoteFile, manifest *Manifest, cfg config.App, force bool, downloadFn func(url, localPath string) error) (Stats, error) {
	stats := Stats{Downloads: &timing.DownloadTracker{}}

	// The download timer starts only after discovery has returned (the
	// caller only invokes syncRemoteFiles post-discovery), so
	// stats.DownloadDuration (and the "Download:" line printed from it)
	// measures actual download work, not discovery/crawl time.
	syncTimer := timing.StartTimer()
	fmt.Printf("Discovered %d remote files. Comparing against local manifest...\n", len(remoteFiles))

	sort.Slice(remoteFiles, func(i, j int) bool { return remoteFiles[i].Path < remoteFiles[j].Path })
	for _, remoteFile := range remoteFiles {
		targetPath := resolveRemoteTargetPath(cfg, remoteFile)
		targetKey := filepath.ToSlash(targetPath)
		localPath := filepath.Join(cfg.DownloadPath, targetPath)

		previous, ok := manifest.Files[targetKey]
		changed := force || fileChanged(remoteFile, ok, previous)

		if !changed {
			if _, err := os.Stat(localPath); err == nil {
				stats.Skipped++
				continue
			}
		}

		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			stats.Errors++
			fmt.Printf("  error: %s (%v)\n", targetKey, err)
			continue
		}

		fileTimer := timing.StartTimer()
		downloadErr := downloadFn(remoteFile.URL, localPath)
		fileElapsed := fileTimer.Elapsed()
		if downloadErr != nil {
			stats.Errors++
			fmt.Printf("  error: %s (%v)\n", targetKey, downloadErr)
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
	}

	if err := manifest.Save(); err != nil {
		return stats, err
	}

	stats.DownloadDuration = syncTimer.Elapsed()
	return stats, nil
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
