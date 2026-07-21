package syncer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// Mirrors the maintainer's real 2026-07-21 migration, where 24 of 26
// unmatched entries were a file name living in two different courses:
// U06.pdf..U12.pdf under both "Lineare Algebra" and "Ma-Prog". File-name
// matching alone cannot separate those, but the course can - the old key
// carries the configured folder and the current file carries the scraped
// course name, so both sides are facts, not inferences.
//
// Note the folder names deliberately do NOT resemble the course titles
// ("Ma-Prog" vs "So26 Programmieren ..."), which is exactly why the pairing
// must go through config's course_folders rather than any string similarity.
func TestMigrationPairsDuplicateFileNamesByCourse(t *testing.T) {
	dir := t.TempDir()

	laFolder := filepath.Join(dir, "_2. Semester", "Lineare Algebra", "Downloads")
	progFolder := filepath.Join(dir, "_2. Semester", "Ma-Prog", "Downloads")

	const laCourse = "2026 LA20"
	const progCourse = "So26 Programmieren - Weiterfuehrende Konzepte (Math-Ba-PR20)"

	remoteFiles := []scraper.RemoteFile{
		{Name: "U06.pdf", Course: laCourse, SectionTitle: "Material aus dem Wintersemester", URL: "https://example.invalid/la6", Size: sizePtr(11)},
		{Name: "U07.pdf", Course: laCourse, SectionTitle: "Material aus dem Wintersemester", URL: "https://example.invalid/la7", Size: sizePtr(12)},
		{Name: "U06.pdf", Course: progCourse, SectionTitle: "Woche 06", URL: "https://example.invalid/p6", Size: sizePtr(21)},
		{Name: "U07.pdf", Course: progCourse, SectionTitle: "Woche 07", URL: "https://example.invalid/p7", Size: sizePtr(22)},
	}

	// Files as they sit today: flat, under the old key scheme.
	writeFile(t, filepath.Join(laFolder, "U06.pdf"), "la-six")
	writeFile(t, filepath.Join(laFolder, "U07.pdf"), "la-seven")
	writeFile(t, filepath.Join(progFolder, "U06.pdf"), "prog-six")
	writeFile(t, filepath.Join(progFolder, "U07.pdf"), "prog-seven")

	manifest := &Manifest{
		Path:          manifestPathIn(dir),
		SchemaVersion: LegacyManifestSchemaVersion,
		Files: map[string]FileRecord{
			"_2. Semester/Lineare Algebra/Downloads/U06.pdf": {Size: sizePtr(11)},
			"_2. Semester/Lineare Algebra/Downloads/U07.pdf": {Size: sizePtr(12)},
			"_2. Semester/Ma-Prog/Downloads/U06.pdf":         {Size: sizePtr(21)},
			"_2. Semester/Ma-Prog/Downloads/U07.pdf":         {Size: sizePtr(22)},
		},
	}
	cfg := config.App{
		DownloadPath:         dir,
		UseSectionSubfolders: true,
		CourseFolders: map[string]string{
			laCourse:   laFolder,
			progCourse: progFolder,
		},
	}

	report := migrateManifestKeys(manifest, cfg, remoteFiles)
	if report == nil || !report.Attempted {
		t.Fatal("expected the migration pass to run")
	}
	if len(report.Ambiguous) != 0 {
		t.Errorf("expected the course to resolve every duplicate name, still ambiguous: %v", report.Ambiguous)
	}
	if len(report.Remapped) != 4 {
		t.Fatalf("expected all 4 entries remapped, got %d: %v", len(report.Remapped), report.Remapped)
	}
	if len(report.Orphans) != 0 {
		t.Errorf("expected no orphans left behind, got %v", report.Orphans)
	}

	// The decisive check: contents must follow their own course, never be
	// cross-assigned to the other one's section folder.
	wants := map[string]string{
		filepath.Join(laFolder, "Material aus dem Wintersemester", "U06.pdf"): "la-six",
		filepath.Join(laFolder, "Material aus dem Wintersemester", "U07.pdf"): "la-seven",
		filepath.Join(progFolder, "Woche 06", "U06.pdf"):                      "prog-six",
		filepath.Join(progFolder, "Woche 07", "U07.pdf"):                      "prog-seven",
	}
	for path, want := range wants {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("expected a relocated file at %s: %v", path, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s holds %q, want %q - a file was cross-assigned to the wrong course", path, got, want)
		}
	}

	// And the old flat copies must be gone (moved, not copied), so no
	// duplicates are left for the user to clean up.
	for _, stale := range []string{
		filepath.Join(laFolder, "U06.pdf"),
		filepath.Join(progFolder, "U06.pdf"),
	} {
		if _, err := os.Stat(stale); !os.IsNotExist(err) {
			t.Errorf("expected %s to have been moved, but it is still there", stale)
		}
	}
}

// The course tie-break must not become a licence to guess. Two stale entries
// from the SAME course sharing a file name stay unresolved, exactly as before.
func TestMigrationStillRefusesToGuessWithinOneCourse(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "AlgData", "Downloads")
	const course = "Algorithmen und Datenstrukturen"

	remoteFiles := []scraper.RemoteFile{
		{Name: "sheet.pdf", Course: course, SectionTitle: "Vorlesung", URL: "https://example.invalid/1", Size: sizePtr(12)},
		{Name: "sheet.pdf", Course: course, SectionTitle: "Uebung", URL: "https://example.invalid/2", Size: sizePtr(12)},
	}
	writeFile(t, filepath.Join(folder, "sheet.pdf"), "content")

	manifest := &Manifest{
		Path:          manifestPathIn(dir),
		SchemaVersion: LegacyManifestSchemaVersion,
		Files: map[string]FileRecord{
			"AlgData/Downloads/sheet.pdf": {Size: sizePtr(12)},
		},
	}
	cfg := config.App{
		DownloadPath:         dir,
		UseSectionSubfolders: true,
		CourseFolders:        map[string]string{course: folder},
	}

	report := migrateManifestKeys(manifest, cfg, remoteFiles)
	if report == nil || !report.Attempted {
		t.Fatal("expected the migration pass to run")
	}
	if len(report.Remapped) != 0 {
		t.Errorf("expected no remap when one course has the name twice, got %v", report.Remapped)
	}
	if len(report.Ambiguous) != 1 {
		t.Errorf("expected the entry to stay ambiguous, got %v", report.Ambiguous)
	}
	if _, err := os.Stat(filepath.Join(folder, "sheet.pdf")); err != nil {
		t.Errorf("expected the ambiguous file to be left untouched: %v", err)
	}
}

// A stale key no configured course folder claims (course removed from config,
// or the file came from default_course_folder) must fall back to the old
// behaviour rather than being attached to some arbitrary course.
func TestOldKeyCourseReportsUnknownForUnclaimedKeys(t *testing.T) {
	cfg := config.App{
		DownloadPath: filepath.Join("C:", "root"),
		CourseFolders: map[string]string{
			"Course A": filepath.Join("C:", "root", "sem", "A", "Downloads"),
			"Course B": filepath.Join("C:", "root", "sem", "A", "Downloads", "Nested"),
		},
	}

	if course, ok := oldKeyCourse(cfg, "sem/A/Downloads/file.pdf"); !ok || course != "Course A" {
		t.Errorf("got (%q, %v), want (\"Course A\", true)", course, ok)
	}
	// Longest prefix wins, so the nested folder resolves to its own course.
	if course, ok := oldKeyCourse(cfg, "sem/A/Downloads/Nested/file.pdf"); !ok || course != "Course B" {
		t.Errorf("got (%q, %v), want (\"Course B\", true)", course, ok)
	}
	if course, ok := oldKeyCourse(cfg, "elsewhere/file.pdf"); ok {
		t.Errorf("got (%q, true), want unknown", course)
	}
}
