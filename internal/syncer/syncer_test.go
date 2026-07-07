package syncer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

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
