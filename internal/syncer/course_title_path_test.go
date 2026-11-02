package syncer

import (
	"path/filepath"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/alu-developer/opal-downloader/internal/scraper"
)

// courseTitleWithSeparators is the shape that broke: a real TU Dresden course
// title carrying both a "/" and a ":" - neither of which survives as a path
// component, and both of which a human writes into course_folders verbatim
// because that is how OPAL displays the course.
const courseTitleWithSeparators = "TUDMATH WS2026/27 Modul Math-Ba-AL10: Algebra - Grundlegende Konzepte"

// A course_folders entry written as the title OPAL shows must win over
// default_course_folder, separators and all. Before the fix the scraper
// handed the syncer the sanitized title, which matched no pattern, and every
// such course silently piled up in default_course_folder instead.
func TestResolveRemoteTargetPathMatchesCourseFolderWithSeparatorsInTitle(t *testing.T) {
	downloadRoot := t.TempDir()
	mappedFolder := filepath.Join(t.TempDir(), "AL10", "Downloads")
	defaultFolder := filepath.Join(t.TempDir(), "Default_downloads")

	cfg := config.App{
		DownloadPath:        downloadRoot,
		DefaultCourseFolder: defaultFolder,
		CourseFolders: map[string]string{
			courseTitleWithSeparators: mappedFolder,
		},
	}

	resolved := resolveRemoteTargetPath(cfg, scraper.RemoteFile{
		Name:   "Blatt1.pdf",
		Course: courseTitleWithSeparators,
	})

	wantLocal := filepath.Join(mappedFolder, "Blatt1.pdf")
	if resolved.LocalPath != wantLocal {
		t.Fatalf("expected the configured course folder to win, got %s, want %s", resolved.LocalPath, wantLocal)
	}
	wantKey := "TUDMATH WS2026_27 Modul Math-Ba-AL10_ Algebra - Grundlegende Konzepte/Blatt1.pdf"
	if filepath.ToSlash(resolved.ManifestKey) != wantKey {
		t.Fatalf("expected sanitized course-name manifest key, got %s, want %s", filepath.ToSlash(resolved.ManifestKey), wantKey)
	}
}

// A course with no mapping still falls back to default_course_folder, and the
// title has to be sanitized on the way into the path: a raw "/" would split
// into two nested folders and a raw ":" is rejected outright on Windows.
func TestResolveRemoteTargetPathSanitizesTitleUnderDefaultCourseFolder(t *testing.T) {
	downloadRoot := t.TempDir()
	defaultFolder := filepath.Join(t.TempDir(), "Default_downloads")

	cfg := config.App{
		DownloadPath:        downloadRoot,
		DefaultCourseFolder: defaultFolder,
		CourseFolders:       map[string]string{},
	}

	resolved := resolveRemoteTargetPath(cfg, scraper.RemoteFile{
		Name:   "Blatt1.pdf",
		Course: courseTitleWithSeparators,
	})

	wantLocal := filepath.Join(defaultFolder, "TUDMATH WS2026_27 Modul Math-Ba-AL10_ Algebra - Grundlegende Konzepte", "Blatt1.pdf")
	if resolved.LocalPath != wantLocal {
		t.Fatalf("expected a single sanitized course folder, got %s, want %s", resolved.LocalPath, wantLocal)
	}
}
