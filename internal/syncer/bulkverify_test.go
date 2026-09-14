package syncer

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// fakeBulkDownloader is a test double for bulkVerifyDownloader. results maps
// sectionURL to what DownloadFilesBulk should hand back for that call
// (writing bytesByName's per-filename content to the requested path first,
// mirroring what a real bulk fetch would have written), and errBySection
// lets a test simulate one section's fetch failing outright.
type fakeBulkDownloader struct {
	calls         []string // sectionURLs this was called with, in order
	bytesByName   map[string]string
	modifiedByURL map[string]map[string]time.Time
	errBySection  map[string]error
}

func (f *fakeBulkDownloader) DownloadFilesBulk(sectionURL string, wantLocalPaths map[string]string) (map[string]time.Time, error) {
	f.calls = append(f.calls, sectionURL)
	if err, ok := f.errBySection[sectionURL]; ok {
		return nil, err
	}
	result := map[string]time.Time{}
	for name, target := range wantLocalPaths {
		modified, ok := f.modifiedByURL[sectionURL][name]
		if !ok {
			// Simulates a file this section's zip did not contain.
			continue
		}
		content, ok := f.bytesByName[name]
		if !ok {
			content = "bulk-default-content"
		}
		if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
			return nil, err
		}
		result[name] = modified
	}
	return result, nil
}

// bulkGroupFixture sets up two signal-less files sharing one section - the
// smallest case bulkVerifyMinGroupSize actually triggers on.
func bulkGroupFixture(t *testing.T) (cfg config.App, manifest *Manifest, remote []scraper.RemoteFile, dir string) {
	t.Helper()
	dir = t.TempDir()
	cfg = config.App{DownloadPath: dir, DownloadConcurrency: 1}

	for _, name := range []string{"U01.pdf", "U02.pdf"} {
		p := filepath.Join(dir, "Course A", name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("old-"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	manifest = &Manifest{
		Path: filepath.Join(dir, ManifestFileName),
		Files: map[string]FileRecord{
			"Course A/U01.pdf": {},
			"Course A/U02.pdf": {},
		},
	}
	remote = []scraper.RemoteFile{
		{Name: "U01.pdf", Course: "Course A", Path: "U01.pdf", URL: "https://example.test/U01.pdf", SectionURL: "https://example.test/section"},
		{Name: "U02.pdf", Course: "Course A", Path: "U02.pdf", URL: "https://example.test/U02.pdf", SectionURL: "https://example.test/section"},
	}
	return cfg, manifest, remote, dir
}

func TestProcessRemoteFilesBulkFetchesGroupedSignallessFilesWhenFlagged(t *testing.T) {
	t.Setenv(bulkVerifyDownloadEnvVar, "1")
	cfg, manifest, remote, dir := bulkGroupFixture(t)

	modTime := time.Date(2026, 4, 7, 16, 35, 0, 0, time.UTC)
	bulk := &fakeBulkDownloader{
		bytesByName: map[string]string{
			"U01.pdf": "old-U01.pdf", // identical to what's on disk - unchanged
			"U02.pdf": "NEW-U02-content",
		},
		modifiedByURL: map[string]map[string]time.Time{
			"https://example.test/section": {
				"U01.pdf": modTime,
				"U02.pdf": modTime,
			},
		},
	}

	perFileCalled := 0
	downloadFn := func(fileURL, target string) error {
		perFileCalled++
		return errors.New("per-file path must not be used when bulk handles the whole group")
	}

	stats := processRemoteFiles(context.Background(), remote, manifest, cfg, false, downloadFn, nil, bulk)

	if perFileCalled != 0 {
		t.Fatalf("expected 0 per-file downloadFn calls, got %d", perFileCalled)
	}
	if len(bulk.calls) != 1 || bulk.calls[0] != "https://example.test/section" {
		t.Fatalf("expected exactly one bulk call to the shared section, got %v", bulk.calls)
	}
	if stats.Skipped != 1 || stats.Downloaded != 1 || stats.Errors != 0 {
		t.Fatalf("expected 1 skipped (unchanged) + 1 downloaded (changed), got %+v", stats)
	}

	u01, err := os.ReadFile(filepath.Join(dir, "Course A", "U01.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(u01) != "old-U01.pdf" {
		t.Fatalf("unchanged file must be left as-is, got %q", u01)
	}
	u02, err := os.ReadFile(filepath.Join(dir, "Course A", "U02.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(u02) != "NEW-U02-content" {
		t.Fatalf("changed file must be replaced with the bulk-fetched content, got %q", u02)
	}

	want := modTime.Format(time.RFC3339)
	for _, key := range []string{"Course A/U01.pdf", "Course A/U02.pdf"} {
		rec, ok := manifest.Files[key]
		if !ok {
			t.Fatalf("expected a manifest entry for %s", key)
		}
		if rec.Modified == nil || *rec.Modified != want {
			t.Fatalf("expected manifest Modified=%q for %s, got %+v", want, key, rec)
		}
	}

	for _, name := range []string{"U01.pdf", "U02.pdf"} {
		tempPath := verificationTempPath(filepath.Join(dir, "Course A", name))
		if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
			t.Fatalf("bulk verify scratch file was left behind for %s: %v", name, err)
		}
	}
}

func TestProcessRemoteFilesBulkFallsBackToPerFileWhenSectionFetchFails(t *testing.T) {
	t.Setenv(bulkVerifyDownloadEnvVar, "1")
	cfg, manifest, remote, _ := bulkGroupFixture(t)

	bulk := &fakeBulkDownloader{
		errBySection: map[string]error{
			"https://example.test/section": errors.New("navigation failed"),
		},
	}

	perFileCalled := map[string]int{}
	downloadFn := func(fileURL, target string) error {
		perFileCalled[fileURL]++
		return os.WriteFile(target, []byte("per-file-content"), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remote, manifest, cfg, false, downloadFn, nil, bulk)

	if len(bulk.calls) != 1 {
		t.Fatalf("expected the bulk downloader to still be tried once, got %v", bulk.calls)
	}
	if perFileCalled["https://example.test/U01.pdf"] != 1 || perFileCalled["https://example.test/U02.pdf"] != 1 {
		t.Fatalf("expected both files to fall back to the per-file path after the bulk fetch failed, got %v", perFileCalled)
	}
	// Both files' local content ("old-U01.pdf"/"old-U02.pdf") differs from
	// "per-file-content", so both must count as downloaded (changed).
	if stats.Downloaded != 2 || stats.Errors != 0 {
		t.Fatalf("expected both files downloaded via fallback, got %+v", stats)
	}
}

func TestProcessRemoteFilesDoesNotBulkFetchASingleFileSection(t *testing.T) {
	t.Setenv(bulkVerifyDownloadEnvVar, "1")
	cfg, manifest, remote, _ := signallessFixture(t, "same-bytes")
	// signallessFixture's one file has no SectionURL set, matching a real
	// discovery result whose section only ever had this one signal-less
	// file - bulkVerifyMinGroupSize (2) must keep it on the per-file path.
	remote[0].SectionURL = "https://example.test/lonely-section"

	bulk := &fakeBulkDownloader{}
	fetched := 0
	downloadFn := func(fileURL, target string) error {
		fetched++
		return os.WriteFile(target, []byte("same-bytes"), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remote, manifest, cfg, false, downloadFn, nil, bulk)

	if len(bulk.calls) != 0 {
		t.Fatalf("expected no bulk call for a single-file section, got %v", bulk.calls)
	}
	if fetched != 1 {
		t.Fatalf("expected the single-file section to use the per-file path, got %d calls", fetched)
	}
	if stats.Skipped != 1 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestProcessRemoteFilesBulkFetchIsOffByDefault(t *testing.T) {
	// Deliberately no t.Setenv(bulkVerifyDownloadEnvVar, ...) - this is the
	// regression guard for the campaign's own non-negotiable rule that a new
	// speed experiment must be off by default.
	cfg, manifest, remote, _ := bulkGroupFixture(t)

	bulk := &fakeBulkDownloader{
		modifiedByURL: map[string]map[string]time.Time{
			"https://example.test/section": {
				"U01.pdf": time.Now(),
				"U02.pdf": time.Now(),
			},
		},
	}

	perFileCalled := 0
	downloadFn := func(fileURL, target string) error {
		perFileCalled++
		return os.WriteFile(target, []byte("old-"+filepath.Base(fileURL)), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remote, manifest, cfg, false, downloadFn, nil, bulk)

	if len(bulk.calls) != 0 {
		t.Fatalf("bulk downloader must not be called when %s is unset, got %v", bulkVerifyDownloadEnvVar, bulk.calls)
	}
	if perFileCalled != 2 {
		t.Fatalf("expected both files on the per-file path, got %d calls", perFileCalled)
	}
	if stats.Skipped != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}

func TestProcessRemoteFilesBulkFileMissingFromZipFallsBackPerFile(t *testing.T) {
	t.Setenv(bulkVerifyDownloadEnvVar, "1")
	cfg, manifest, remote, _ := bulkGroupFixture(t)

	// Only U01.pdf is "in" the section's zip; U02.pdf is a name mismatch or
	// simply absent from it.
	bulk := &fakeBulkDownloader{
		bytesByName: map[string]string{"U01.pdf": "old-U01.pdf"},
		modifiedByURL: map[string]map[string]time.Time{
			"https://example.test/section": {"U01.pdf": time.Now()},
		},
	}

	perFileCalled := map[string]int{}
	downloadFn := func(fileURL, target string) error {
		perFileCalled[fileURL]++
		return os.WriteFile(target, []byte("old-U02.pdf"), 0o644)
	}

	stats := processRemoteFiles(context.Background(), remote, manifest, cfg, false, downloadFn, nil, bulk)

	if perFileCalled["https://example.test/U02.pdf"] != 1 {
		t.Fatalf("expected U02.pdf to fall back to the per-file path, got %v", perFileCalled)
	}
	if perFileCalled["https://example.test/U01.pdf"] != 0 {
		t.Fatalf("U01.pdf was resolved by bulk and must not also go through the per-file path, got %v", perFileCalled)
	}
	// U01 unchanged via bulk (skip), U02 unchanged via fallback (skip too,
	// same content written back).
	if stats.Skipped != 2 || stats.Downloaded != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
}
