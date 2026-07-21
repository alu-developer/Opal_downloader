package syncer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// writeLegacyManifest writes a manifest in the pre-versioning format (no
// schema_version field at all), which is what every manifest on disk today
// looks like.
func writeLegacyManifest(t *testing.T, path string, files map[string]FileRecord) {
	t.Helper()
	payload := map[string]any{
		"updated_at": "2026-07-12T10:00:00Z",
		"files":      files,
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sizePtr(n int64) *int64  { return &n }
func strPtr(s string) *string { return &s }
func manifestPathIn(dir string) string {
	return filepath.Join(dir, ManifestFileName)
}

func TestLoadManifestReportsLegacySchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := manifestPathIn(dir)
	writeLegacyManifest(t, path, map[string]FileRecord{
		"Analysis I/sheet.pdf": {Size: sizePtr(10)},
	})

	manifest, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != LegacyManifestSchemaVersion {
		t.Fatalf("expected legacy schema version %d, got %d", LegacyManifestSchemaVersion, manifest.SchemaVersion)
	}
}

func TestSaveWritesCurrentSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	manifest := &Manifest{Path: manifestPathIn(dir), Files: map[string]FileRecord{
		"Analysis I/sheet.pdf": {Size: sizePtr(10)},
	}}
	if err := manifest.Save(); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	var payload manifestJSON
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("expected schema_version %d on disk, got %d", ManifestSchemaVersion, payload.SchemaVersion)
	}

	reloaded, err := LoadManifest(manifest.Path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.SchemaVersion != ManifestSchemaVersion {
		t.Fatalf("expected reloaded schema version %d, got %d", ManifestSchemaVersion, reloaded.SchemaVersion)
	}
}

// TestSyncMigratesLegacyKeysInsteadOfRedownloading is the realistic
// reproduction the task asks for: a manifest written under the old flat key
// scheme, plus a config that has since turned use_section_subfolders on. The
// old behavior re-downloaded every file and orphaned the old copies; the new
// behavior must move the existing files into the new layout and skip them.
func TestSyncMigratesLegacyKeysInsteadOfRedownloading(t *testing.T) {
	dir := t.TempDir()

	remoteFiles := []scraper.RemoteFile{
		{Name: "Vorlesung_11.pdf", Course: "Algorithmen und Datenstrukturen", SectionTitle: "Vorlesung", URL: "https://example.invalid/1", Size: sizePtr(12), Modified: strPtr("2026-07-01")},
		{Name: "Uebung_03.pdf", Course: "Algorithmen und Datenstrukturen", SectionTitle: "Uebungen", URL: "https://example.invalid/2", Size: sizePtr(12), Modified: strPtr("2026-07-02")},
	}

	// Old (flat, pre-use_section_subfolders) layout, on disk and in the
	// manifest.
	writeFile(t, filepath.Join(dir, "Algorithmen und Datenstrukturen", "Vorlesung_11.pdf"), "fake-content")
	writeFile(t, filepath.Join(dir, "Algorithmen und Datenstrukturen", "Uebung_03.pdf"), "fake-content")
	writeLegacyManifest(t, manifestPathIn(dir), map[string]FileRecord{
		"Algorithmen und Datenstrukturen/Vorlesung_11.pdf": {Size: sizePtr(12), Modified: strPtr("2026-07-01")},
		"Algorithmen und Datenstrukturen/Uebung_03.pdf":    {Size: sizePtr(12), Modified: strPtr("2026-07-02")},
	})

	cfg := config.App{DownloadPath: dir, UseSectionSubfolders: true}
	downloader := &fakeDownloader{files: remoteFiles}

	stats, err := SyncCourses(context.Background(), downloader, cfg, false)
	if err != nil {
		t.Fatal(err)
	}

	if stats.Downloaded != 0 {
		t.Errorf("expected 0 downloads after migration, got %d (files were needlessly re-downloaded)", stats.Downloaded)
	}
	if stats.Skipped != 2 {
		t.Errorf("expected 2 skips after migration, got %d", stats.Skipped)
	}
	if len(downloader.calls) != 0 {
		t.Errorf("expected no DownloadFile calls, got %v", downloader.calls)
	}

	// Files must have physically moved into the new layout...
	for _, rel := range []string{
		filepath.Join("Algorithmen und Datenstrukturen", "Vorlesung", "Vorlesung_11.pdf"),
		filepath.Join("Algorithmen und Datenstrukturen", "Uebungen", "Uebung_03.pdf"),
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("expected relocated file at %s: %v", rel, err)
		}
	}
	// ...and must not be left orphaned at their old locations.
	for _, rel := range []string{
		filepath.Join("Algorithmen und Datenstrukturen", "Vorlesung_11.pdf"),
		filepath.Join("Algorithmen und Datenstrukturen", "Uebung_03.pdf"),
	} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err == nil {
			t.Errorf("expected old copy at %s to be gone (orphan), it is still there", rel)
		}
	}

	saved, err := LoadManifest(manifestPathIn(dir))
	if err != nil {
		t.Fatal(err)
	}
	if saved.SchemaVersion != ManifestSchemaVersion {
		t.Errorf("expected migrated manifest to be saved at schema version %d, got %d", ManifestSchemaVersion, saved.SchemaVersion)
	}
	if _, ok := saved.Files["Algorithmen und Datenstrukturen/Vorlesung/Vorlesung_11.pdf"]; !ok {
		t.Errorf("expected re-keyed manifest entry, got keys %v", keysOf(saved.Files))
	}
	if _, ok := saved.Files["Algorithmen und Datenstrukturen/Vorlesung_11.pdf"]; ok {
		t.Errorf("expected old key to be dropped, got keys %v", keysOf(saved.Files))
	}
}

// TestMigrationWarnsInsteadOfGuessingWhenAmbiguous covers the fallback half
// of the policy: when the same file name appears more than once, the mapping
// is not recoverable, so nothing is moved or re-keyed and the user is warned
// loudly with the exact paths of the files left behind.
func TestMigrationWarnsInsteadOfGuessingWhenAmbiguous(t *testing.T) {
	dir := t.TempDir()

	remoteFiles := []scraper.RemoteFile{
		{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Vorlesung", URL: "https://example.invalid/1", Size: sizePtr(12)},
		{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Uebungen", URL: "https://example.invalid/2", Size: sizePtr(12)},
	}
	writeFile(t, filepath.Join(dir, "Analysis I", "sheet.pdf"), "fake-content")

	manifest := &Manifest{
		Path:          manifestPathIn(dir),
		SchemaVersion: LegacyManifestSchemaVersion,
		Files: map[string]FileRecord{
			"Analysis I/sheet.pdf": {Size: sizePtr(12)},
		},
	}
	cfg := config.App{DownloadPath: dir, UseSectionSubfolders: true}

	report := migrateManifestKeys(manifest, cfg, remoteFiles)
	if report == nil || !report.Attempted {
		t.Fatal("expected the migration pass to run")
	}
	if len(report.Remapped) != 0 {
		t.Errorf("expected no remap for an ambiguous file name, got %v", report.Remapped)
	}
	if len(report.Ambiguous) != 1 || report.Ambiguous[0] != "Analysis I/sheet.pdf" {
		t.Errorf("expected the stale key to be reported as ambiguous, got %v", report.Ambiguous)
	}
	if len(report.Orphans) != 1 {
		t.Fatalf("expected the left-behind file to be reported as an orphan, got %v", report.Orphans)
	}
	if !strings.HasSuffix(report.Orphans[0], filepath.Join("Analysis I", "sheet.pdf")) {
		t.Errorf("expected the orphan path to point at the old file, got %s", report.Orphans[0])
	}
	if !filepath.IsAbs(report.Orphans[0]) {
		t.Errorf("expected an absolute orphan path for a user-facing message, got %s", report.Orphans[0])
	}

	// The old file must still be there - nothing is ever deleted.
	if _, err := os.Stat(filepath.Join(dir, "Analysis I", "sheet.pdf")); err != nil {
		t.Errorf("expected the ambiguous old file to be left untouched: %v", err)
	}
	// The manifest must be left exactly as it was.
	if _, ok := manifest.Files["Analysis I/sheet.pdf"]; !ok || len(manifest.Files) != 1 {
		t.Errorf("expected the manifest to be untouched, got %v", keysOf(manifest.Files))
	}

	lines := strings.Join(report.Lines(), "\n")
	if !strings.Contains(lines, "WARNING") {
		t.Errorf("expected a loud warning in the report lines, got:\n%s", lines)
	}
}

// TestMigrationDoesNotRunOnAnOrdinarySync is the safety guard: a normal sync
// where most stored keys still match must not enter the name-matching remap
// pass at all, so a deleted remote file and an unrelated new file that happen
// to share a name can never be mis-associated.
func TestMigrationDoesNotRunOnAnOrdinarySync(t *testing.T) {
	dir := t.TempDir()

	remoteFiles := []scraper.RemoteFile{
		{Name: "a.pdf", Course: "Analysis I", Size: sizePtr(1)},
		{Name: "b.pdf", Course: "Analysis I", Size: sizePtr(1)},
		{Name: "c.pdf", Course: "Analysis I", Size: sizePtr(1)},
		// New this run, same file name as a since-removed file in another
		// course - the exact mis-association the gate protects against.
		{Name: "gone.pdf", Course: "Lineare Algebra", Size: sizePtr(1)},
	}

	manifest := &Manifest{
		Path:          manifestPathIn(dir),
		SchemaVersion: LegacyManifestSchemaVersion,
		Files: map[string]FileRecord{
			"Analysis I/a.pdf":    {Size: sizePtr(1)},
			"Analysis I/b.pdf":    {Size: sizePtr(1)},
			"Analysis I/c.pdf":    {Size: sizePtr(1)},
			"Analysis I/gone.pdf": {Size: sizePtr(1)},
		},
	}
	cfg := config.App{DownloadPath: dir}

	if report := migrateManifestKeys(manifest, cfg, remoteFiles); report != nil {
		t.Fatalf("expected no migration on an ordinary sync, got %+v", report)
	}
	if _, ok := manifest.Files["Analysis I/gone.pdf"]; !ok {
		t.Errorf("expected the unrelated stale key to be left alone, got %v", keysOf(manifest.Files))
	}
}

// TestMigrationRemapsWithoutMovingWhenOldFileIsMissing covers the case where
// the manifest key can be salvaged but the file itself is not where the old
// key implies (e.g. it was written through a subfolder_destinations
// override). Nothing is moved, nothing is claimed to be an orphan, and the
// file is simply re-downloaded into the new layout.
func TestMigrationRemapsWithoutMovingWhenOldFileIsMissing(t *testing.T) {
	dir := t.TempDir()

	remoteFiles := []scraper.RemoteFile{
		{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Vorlesung", Size: sizePtr(12)},
	}
	manifest := &Manifest{
		Path:          manifestPathIn(dir),
		SchemaVersion: LegacyManifestSchemaVersion,
		Files:         map[string]FileRecord{"Analysis I/sheet.pdf": {Size: sizePtr(12)}},
	}
	cfg := config.App{DownloadPath: dir, UseSectionSubfolders: true}

	report := migrateManifestKeys(manifest, cfg, remoteFiles)
	if report == nil || len(report.Remapped) != 1 {
		t.Fatalf("expected exactly one remap, got %+v", report)
	}
	if report.Remapped[0].Relocated {
		t.Error("expected no relocation when the old file is not on disk")
	}
	if len(report.Orphans) != 0 {
		t.Errorf("expected no orphans when the old file does not exist, got %v", report.Orphans)
	}
	if _, ok := manifest.Files["Analysis I/Vorlesung/sheet.pdf"]; !ok {
		t.Errorf("expected the key to be remapped, got %v", keysOf(manifest.Files))
	}
}

// TestMigrationReportsOrphanWhenNewLocationIsOccupied verifies the syncer
// never overwrites a file that is already sitting at the new location - it
// re-keys the history entry and reports the old copy instead.
func TestMigrationReportsOrphanWhenNewLocationIsOccupied(t *testing.T) {
	dir := t.TempDir()

	remoteFiles := []scraper.RemoteFile{
		{Name: "sheet.pdf", Course: "Analysis I", SectionTitle: "Vorlesung", Size: sizePtr(12)},
	}
	writeFile(t, filepath.Join(dir, "Analysis I", "sheet.pdf"), "old-copy")
	writeFile(t, filepath.Join(dir, "Analysis I", "Vorlesung", "sheet.pdf"), "new-copy")

	manifest := &Manifest{
		Path:          manifestPathIn(dir),
		SchemaVersion: LegacyManifestSchemaVersion,
		Files:         map[string]FileRecord{"Analysis I/sheet.pdf": {Size: sizePtr(12)}},
	}
	cfg := config.App{DownloadPath: dir, UseSectionSubfolders: true}

	report := migrateManifestKeys(manifest, cfg, remoteFiles)
	if report == nil || len(report.Orphans) != 1 {
		t.Fatalf("expected the old copy to be reported as an orphan, got %+v", report)
	}
	if report.Remapped[0].Relocated {
		t.Error("expected no relocation onto an occupied destination")
	}

	content, err := os.ReadFile(filepath.Join(dir, "Analysis I", "Vorlesung", "sheet.pdf"))
	if err != nil || string(content) != "new-copy" {
		t.Errorf("expected the file at the new location to be untouched, got %q (%v)", content, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Analysis I", "sheet.pdf")); err != nil {
		t.Errorf("expected the old copy to still exist (nothing is auto-deleted): %v", err)
	}
}

func keysOf(files map[string]FileRecord) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	return keys
}
