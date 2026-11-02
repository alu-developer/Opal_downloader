package scraper

import "testing"

// Regression test for the bug that sent the maintainer's AL10 and NM10
// courses to default_course_folder for weeks (found 2026-11-02): OPAL course
// titles routinely contain "/" and ":", FileRef.CourseTitle used to carry the
// sanitized form of the title, and config.ResolveCourseFolder matches that
// value against course_folders patterns a human copied from the real title -
// so such a course could never match its own mapping.
func TestAppendSectionFilesKeepsTitlesVerbatimButSanitizesPath(t *testing.T) {
	const courseTitle = "TUDMATH WS2026/27 Modul Math-Ba-AL10: Algebra - Grundlegende Konzepte"
	const sectionTitle = "Übungen: Blätter/Lösungen"

	course := CourseRef{RepoID: "123", Title: courseTitle, URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123"}
	section := SectionRef{CourseRepoID: "123", Title: sectionTitle, URL: "https://bildungsportal.sachsen.de/opal/goto.php?target=fold_1234"}
	candidates := []map[string]string{
		{
			"href":  "/opal/goto.php?target=file_55&cmd=sendfile",
			"title": "Blatt1.pdf",
			"text":  "Blatt1.pdf",
		},
	}

	files := appendSectionFiles(nil, map[string]struct{}{}, candidates, course, section, section.URL, "", "", false, "https://bildungsportal.sachsen.de/opal/", map[string]downloadCandidate{})
	if len(files) != 1 {
		t.Fatalf("expected one collected file, got %d: %#v", len(files), files)
	}
	if files[0].CourseTitle != courseTitle {
		t.Fatalf("expected CourseTitle verbatim, got %q, want %q", files[0].CourseTitle, courseTitle)
	}
	if files[0].SectionTitle != sectionTitle {
		t.Fatalf("expected SectionTitle verbatim, got %q, want %q", files[0].SectionTitle, sectionTitle)
	}
	wantPath := "TUDMATH WS2026_27 Modul Math-Ba-AL10_ Algebra - Grundlegende Konzepte/Blatt1.pdf"
	if files[0].Path != wantPath {
		t.Fatalf("expected Path to stay sanitized, got %q, want %q", files[0].Path, wantPath)
	}
}
