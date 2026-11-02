package syncer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// contentDownloadFn is a downloadFn that writes per-URL content, so a test
// can tell "the file OPAL serves" from "whatever happened to be on disk" -
// which fakeDownloader's single "fake-content" payload cannot.
func contentDownloadFn(t *testing.T, byURL map[string]string, fetched *[]string) func(string, string) error {
	t.Helper()
	return func(fileURL, localPath string) error {
		*fetched = append(*fetched, fileURL)
		body, ok := byURL[fileURL]
		if !ok {
			t.Fatalf("download of an unexpected URL: %s", fileURL)
		}
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return err
		}
		return os.WriteFile(localPath, []byte(body), 0o644)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// TestMigratedKeyIsNotTrustedAgainstAFileTheSyncNeverWrote is the live
// 2026-11-02 failure, reduced: re-pointing AL10 from a flat folder to a
// "Material" subfolder re-keyed the manifest, and the entry's Size/Modified -
// recorded from OPAL's current 7-page Kapitel1.pdf - travelled onto the new
// key. A hand-filed, older 5-page copy of that file already sat at the new
// path. fileChanged compared OPAL's values against the migrated record and
// said "unchanged"; os.Stat said "present"; the sync reported it skipped and
// left the stale copy in place while the manifest claimed it was current.
// Only `sync --force` repaired it.
//
// The second file is the other 19 from that run, which happened to be
// byte-identical at their new paths: those must stay skipped and - because
// download_path is inside OneDrive - must not be rewritten.
func TestMigratedKeyIsNotTrustedAgainstAFileTheSyncNeverWrote(t *testing.T) {
	dir := t.TempDir()

	const currentKapitel1 = "seven-page-current-version"
	const staleKapitel1 = "five-page-older-copy"
	const kapitel2 = "unchanged-either-way"

	remoteFiles := []scraper.RemoteFile{
		{
			Name: "Kapitel1.pdf", Course: "AL10", SectionTitle: "Material",
			URL:  "https://example.invalid/kapitel1",
			Size: sizePtr(int64(len(currentKapitel1))), Modified: strPtr("30.10.2026 um 09:12 Uhr"),
		},
		{
			Name: "Kapitel2.pdf", Course: "AL10", SectionTitle: "Material",
			URL:  "https://example.invalid/kapitel2",
			Size: sizePtr(int64(len(kapitel2))), Modified: strPtr("24.10.2026 um 11:03 Uhr"),
		},
	}

	// The destination already holds files this tool never put there: an
	// outdated Kapitel1.pdf and an identical Kapitel2.pdf.
	stalePath := filepath.Join(dir, "AL10", "Material", "Kapitel1.pdf")
	identicalPath := filepath.Join(dir, "AL10", "Material", "Kapitel2.pdf")
	writeFile(t, stalePath, staleKapitel1)
	writeFile(t, identicalPath, kapitel2)

	// The manifest still carries the pre-re-route (flat) keys, with the
	// Size/Modified OPAL reports for the files it serves *today*.
	manifest := &Manifest{
		Path:          manifestPathIn(dir),
		SchemaVersion: LegacyManifestSchemaVersion,
		Files: map[string]FileRecord{
			"AL10/Kapitel1.pdf": {Size: sizePtr(int64(len(currentKapitel1))), Modified: strPtr("30.10.2026 um 09:12 Uhr")},
			"AL10/Kapitel2.pdf": {Size: sizePtr(int64(len(kapitel2))), Modified: strPtr("24.10.2026 um 11:03 Uhr")},
		},
	}

	cfg := config.App{DownloadPath: dir, UseSectionSubfolders: true, DownloadConcurrency: 1}

	identicalBefore, err := os.Stat(identicalPath)
	if err != nil {
		t.Fatal(err)
	}

	var fetched []string
	downloadFn := contentDownloadFn(t, map[string]string{
		"https://example.invalid/kapitel1": currentKapitel1,
		"https://example.invalid/kapitel2": kapitel2,
	}, &fetched)

	stats, err := syncRemoteFiles(context.Background(), remoteFiles, manifest, cfg, false, downloadFn, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	// The whole point: the stale copy must not survive a sync that reported
	// success without --force.
	if got := readFile(t, stalePath); got != currentKapitel1 {
		t.Errorf("the stale local copy was left in place: %s holds %q, OPAL serves %q", stalePath, got, currentKapitel1)
	}
	if stats.Downloaded != 1 {
		t.Errorf("expected the stale file to be corrected (1 download), got %d", stats.Downloaded)
	}
	if stats.Skipped != 1 {
		t.Errorf("expected the byte-identical file to stay a skip, got %d skips", stats.Skipped)
	}

	// A byte-identical file must be verified, not rewritten - rewriting it
	// churns its mtime and triggers a pointless OneDrive re-upload.
	identicalAfter, err := os.Stat(identicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if !identicalAfter.ModTime().Equal(identicalBefore.ModTime()) {
		t.Error("a byte-identical file was rewritten instead of left alone")
	}

	// No scratch file may survive either verification.
	for _, p := range []string{stalePath, identicalPath} {
		if _, err := os.Stat(verificationTempPath(p)); !os.IsNotExist(err) {
			t.Errorf("verification temp file left behind next to %s: %v", p, err)
		}
	}

	// Both entries are now backed by bytes this run actually compared, so
	// neither may stay flagged - the next sync has to take the fast skip.
	saved, err := LoadManifest(manifestPathIn(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"AL10/Material/Kapitel1.pdf", "AL10/Material/Kapitel2.pdf"} {
		rec, ok := saved.Files[key]
		if !ok {
			t.Fatalf("expected re-keyed entry %s, got keys %v", key, keysOf(saved.Files))
		}
		if rec.Unverified {
			t.Errorf("%s is still flagged unverified after its content was checked", key)
		}
		if rec.Size == nil || rec.Modified == nil {
			t.Errorf("%s lost its change-detection signals: %+v", key, rec)
		}
	}
}

// The flag is set by the migration itself, and only for an entry whose new
// path holds a file this tool did not put there. An entry whose own
// downloaded file the migration physically relocated is the file the record
// describes, so it stays trusted and takes the ordinary fast skip.
func TestMigrationOnlyFlagsEntriesItCouldNotPlaceItself(t *testing.T) {
	dir := t.TempDir()

	remoteFiles := []scraper.RemoteFile{
		{Name: "moved.pdf", Course: "AL10", SectionTitle: "Material", URL: "https://example.invalid/1", Size: sizePtr(12), Modified: strPtr("01.10.2026")},
		{Name: "squatted.pdf", Course: "AL10", SectionTitle: "Material", URL: "https://example.invalid/2", Size: sizePtr(12), Modified: strPtr("02.10.2026")},
	}

	// moved.pdf is still where this tool downloaded it, under the old flat
	// layout; squatted.pdf is already sitting at its new path, put there by
	// somebody else.
	writeFile(t, filepath.Join(dir, "AL10", "moved.pdf"), "downloaded-by-us")
	writeFile(t, filepath.Join(dir, "AL10", "Material", "squatted.pdf"), "filed-by-hand")

	manifest := &Manifest{
		Path:          manifestPathIn(dir),
		SchemaVersion: LegacyManifestSchemaVersion,
		Files: map[string]FileRecord{
			"AL10/moved.pdf":    {Size: sizePtr(12), Modified: strPtr("01.10.2026")},
			"AL10/squatted.pdf": {Size: sizePtr(12), Modified: strPtr("02.10.2026")},
		},
	}
	cfg := config.App{DownloadPath: dir, UseSectionSubfolders: true}

	report := migrateManifestKeys(manifest, cfg, remoteFiles)
	if report == nil || !report.Attempted {
		t.Fatal("expected the migration pass to run")
	}

	moved := manifest.Files["AL10/Material/moved.pdf"]
	if moved.Unverified {
		t.Error("an entry whose own file the migration relocated must stay trusted")
	}
	squatted, ok := manifest.Files["AL10/Material/squatted.pdf"]
	if !ok {
		t.Fatalf("expected the entry to be re-keyed, got %v", keysOf(manifest.Files))
	}
	if !squatted.Unverified {
		t.Error("an entry re-pointed onto a file this tool never wrote must be flagged for verification")
	}
	if squatted.Size == nil || squatted.Modified == nil {
		t.Errorf("the flag must not throw the change-detection signals away: %+v", squatted)
	}

	unverified := 0
	for _, remap := range report.Remapped {
		if remap.Unverified {
			unverified++
		}
	}
	if unverified != 1 {
		t.Errorf("expected exactly 1 remap reported as unverified, got %d of %d", unverified, len(report.Remapped))
	}
}
