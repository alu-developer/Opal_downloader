package scraper

import "testing"

func TestAppendSectionFilesV2CollectsOnlyAllowedFiles(t *testing.T) {
	course := CourseRefV2{RepoID: "123", Title: "Programmierung 1", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123"}
	section := SectionRefV2{CourseRepoID: "123", Title: "Materialien", URL: "https://bildungsportal.sachsen.de/opal/goto.php?target=fold_1234"}
	candidates := []map[string]string{
		{
			"href":  "/opal/goto.php?target=file_55&cmd=sendfile",
			"title": "Folien Woche 1.pdf",
			"text":  "Folien Woche 1.pdf",
		},
		{
			"href":  "/opal/auth/RepositoryEntry/999?cmd=download",
			"title": "Fremde Datei.pdf",
			"text":  "Fremde Datei.pdf",
		},
		{
			"href":  "/opal/goto.php?target=fold_1234",
			"title": "Materialien",
			"text":  "Materialien",
		},
		{
			"href":  "/opal/auth/home#my-courses",
			"title": "Meine Kurse",
			"text":  "Meine Kurse",
		},
		{
			"href":  "/opal/goto.php?target=file_55&cmd=sendfile",
			"title": "Folien Woche 1.pdf",
			"text":  "Folien Woche 1.pdf",
		},
	}

	files := appendSectionFilesV2(nil, map[string]struct{}{}, candidates, course, section, section.URL, "https://bildungsportal.sachsen.de/opal/", map[string]downloadCandidate{})
	if len(files) != 1 {
		t.Fatalf("expected one collected file, got %d: %#v", len(files), files)
	}
	if files[0].Name != "Folien Woche 1.pdf" {
		t.Fatalf("unexpected file entry: %#v", files[0])
	}
	if files[0].CourseRepoID != "123" || files[0].SectionTitle != "Materialien" {
		t.Fatalf("expected file to stay bound to course section: %#v", files[0])
	}
	if files[0].URL != "https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile" {
		t.Fatalf("unexpected file URL: %#v", files[0])
	}
}

func TestIsFileURLAllowedForCourseV2RejectsForeignAndDashboardTargets(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "sendfile same course", url: "https://bildungsportal.sachsen.de/opal/goto.php?target=file_55&cmd=sendfile", want: true},
		{name: "repo-local download", url: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123?cmd=download", want: true},
		{name: "foreign repo", url: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/999?cmd=download", want: false},
		{name: "dashboard", url: "https://bildungsportal.sachsen.de/opal/auth/home#my-courses", want: false},
		{name: "catalog", url: "https://bildungsportal.sachsen.de/opal/auth/repository/catalog", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFileURLAllowedForCourseV2(tt.url, "123"); got != tt.want {
				t.Fatalf("isFileURLAllowedForCourseV2(%q, %q) = %v, want %v", tt.url, "123", got, tt.want)
			}
		})
	}
}
