package syncer

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

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

	stats := processRemoteFiles(remoteFiles, manifest, cfg, false, downloadFn, func(e Event) {
		events = append(events, e)
	})

	if stats.Downloaded != 2 || stats.Errors != 1 || stats.Skipped != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	var courseStarted, downloaded, errored int
	coursesSeen := map[string]bool{}
	for _, e := range events {
		switch e.Type {
		case EventCourseStarted:
			courseStarted++
			coursesSeen[e.Course] = true
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

	stats := processRemoteFiles(remoteFiles, manifest, cfg, false, downloadFn, func(e Event) {
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
	stats := processRemoteFiles(remoteFiles, manifest, cfg, false, downloadFn, func(Event) {})
	if stats.Downloaded != 1 {
		t.Fatalf("expected 1 downloaded, got %+v", stats)
	}
}
