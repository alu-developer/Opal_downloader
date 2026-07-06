package scraper

import "testing"

func TestAppendSectionFilesCollectsOnlyAllowedFiles(t *testing.T) {
	course := CourseRef{RepoID: "123", Title: "Programmierung 1", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123"}
	section := SectionRef{CourseRepoID: "123", Title: "Materialien", URL: "https://bildungsportal.sachsen.de/opal/goto.php?target=fold_1234"}
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

	files := appendSectionFiles(nil, map[string]struct{}{}, candidates, course, section, section.URL, "", "https://bildungsportal.sachsen.de/opal/", map[string]downloadCandidate{})
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

func TestAppendSectionFilesRecordsShowAllURLForExpansionOnlyFiles(t *testing.T) {
	course := CourseRef{RepoID: "123", Title: "Analysis", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123"}
	sectionURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123/CourseNode/456"
	section := SectionRef{CourseRepoID: "123", Title: "Downloads", URL: sectionURL}
	showAllURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123/CourseNode/456?7-1.-tbl-length=-1"

	// Simulates a file that was only present in the "show all"-expanded candidate
	// set - the caller passes the resolved show-all URL alongside the plain
	// section URL, as collectCourseFiles now does when expandShowAllInSection
	// reports it found and navigated to a distinct show-all URL.
	candidates := []map[string]string{
		{
			"href":  "/opal/goto.php?target=file_99&cmd=sendfile",
			"title": "Analysis21.pdf",
			"text":  "Analysis21.pdf",
		},
	}

	downloadCandidates := map[string]downloadCandidate{}
	files := appendSectionFiles(nil, map[string]struct{}{}, candidates, course, section, sectionURL, showAllURL, "https://bildungsportal.sachsen.de/opal/", downloadCandidates)
	if len(files) != 1 {
		t.Fatalf("expected one collected file, got %d: %#v", len(files), files)
	}

	fileURL := "https://bildungsportal.sachsen.de/opal/goto.php?target=file_99&cmd=sendfile"
	candidate, ok := downloadCandidates[fileURL]
	if !ok {
		t.Fatalf("expected a download candidate to be recorded for %q", fileURL)
	}
	if candidate.SourceURL != sectionURL {
		t.Fatalf("expected SourceURL to stay the plain section URL, got %q", candidate.SourceURL)
	}
	if candidate.ShowAllURL != showAllURL {
		t.Fatalf("expected ShowAllURL to be recorded as %q, got %q", showAllURL, candidate.ShowAllURL)
	}
	if candidate.ShowAllURL == candidate.SourceURL {
		t.Fatalf("expected ShowAllURL to differ from SourceURL for an expansion-only file")
	}
}

func TestAppendSectionFilesLeavesShowAllURLEmptyForNormalPageFiles(t *testing.T) {
	course := CourseRef{RepoID: "123", Title: "Analysis", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123"}
	sectionURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123/CourseNode/456"
	section := SectionRef{CourseRepoID: "123", Title: "Downloads", URL: sectionURL}

	candidates := []map[string]string{
		{
			"href":  "/opal/goto.php?target=file_1&cmd=sendfile",
			"title": "Analysis01.pdf",
			"text":  "Analysis01.pdf",
		},
	}

	downloadCandidates := map[string]downloadCandidate{}
	// No show-all expansion happened for this section - the caller (collectCourseFiles)
	// passes an empty showAllURL in that case.
	files := appendSectionFiles(nil, map[string]struct{}{}, candidates, course, section, sectionURL, "", "https://bildungsportal.sachsen.de/opal/", downloadCandidates)
	if len(files) != 1 {
		t.Fatalf("expected one collected file, got %d: %#v", len(files), files)
	}

	fileURL := "https://bildungsportal.sachsen.de/opal/goto.php?target=file_1&cmd=sendfile"
	candidate, ok := downloadCandidates[fileURL]
	if !ok {
		t.Fatalf("expected a download candidate to be recorded for %q", fileURL)
	}
	if candidate.SourceURL != sectionURL {
		t.Fatalf("expected SourceURL to be the section URL, got %q", candidate.SourceURL)
	}
	if candidate.ShowAllURL != "" {
		t.Fatalf("expected ShowAllURL to stay empty for a normal-page file, got %q", candidate.ShowAllURL)
	}
}

func TestLooksLikeShowAllControlMatchesKnownOpalStylePatterns(t *testing.T) {
	tests := []struct {
		name       string
		linkTarget string
		text       string
		title      string
		want       bool
	}{
		{name: "german show all text", linkTarget: "/opal/goto.php?target=fold_1234", text: "Alle anzeigen", title: "", want: true},
		{name: "german show all text uppercase", linkTarget: "", text: "ALLE ANZEIGEN", title: "", want: true},
		{name: "german show all entries text", linkTarget: "", text: "Alle Einträge anzeigen", title: "", want: true},
		{name: "english show all text via title", linkTarget: "", text: "", title: "Show all", want: true},
		{name: "length=-1 query param", linkTarget: "/opal/coursenode/123?7-1.-tbl-length=-1", text: "20", title: "", want: true},
		{name: "showAll url flag", linkTarget: "/opal/coursenode/123?showAll=true", text: "", title: "", want: true},
		{name: "unrelated file link", linkTarget: "/opal/goto.php?target=file_55&cmd=sendfile", text: "Folien Woche 1.pdf", title: "", want: false},
		{name: "unrelated folder link", linkTarget: "/opal/goto.php?target=fold_1234", text: "Materialien", title: "", want: false},
		{name: "empty candidate", linkTarget: "", text: "", title: "", want: false},
		{name: "pagination next link is not show all", linkTarget: "/opal/coursenode/123?page=2", text: "Nächste Seite", title: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeShowAllControl(tt.linkTarget, tt.text, tt.title); got != tt.want {
				t.Fatalf("looksLikeShowAllControl(%q, %q, %q) = %v, want %v", tt.linkTarget, tt.text, tt.title, got, tt.want)
			}
		})
	}
}

func TestFindShowAllTargetFindsFirstMatchingCandidate(t *testing.T) {
	candidates := []map[string]string{
		{
			"href":  "/opal/goto.php?target=file_55&cmd=sendfile",
			"title": "Folien Woche 1.pdf",
			"text":  "Folien Woche 1.pdf",
		},
		{
			"href":  "/opal/coursenode/123?7-1.-tbl-length=-1",
			"title": "",
			"text":  "Alle anzeigen",
		},
		{
			"href":  "/opal/goto.php?target=fold_1234",
			"title": "Materialien",
			"text":  "Materialien",
		},
	}

	target, found := findShowAllTarget(candidates)
	if !found {
		t.Fatalf("expected to find a show-all target")
	}
	if target != "/opal/coursenode/123?7-1.-tbl-length=-1" {
		t.Fatalf("unexpected show-all target: %q", target)
	}
}

func TestFindShowAllTargetReturnsNotFoundWhenAbsent(t *testing.T) {
	candidates := []map[string]string{
		{
			"href":  "/opal/goto.php?target=file_55&cmd=sendfile",
			"title": "Folien Woche 1.pdf",
			"text":  "Folien Woche 1.pdf",
		},
		{
			"href":  "/opal/goto.php?target=fold_1234",
			"title": "Materialien",
			"text":  "Materialien",
		},
	}

	if _, found := findShowAllTarget(candidates); found {
		t.Fatalf("expected no show-all target to be found")
	}
}

func TestIsFileURLAllowedForCourseRejectsForeignAndDashboardTargets(t *testing.T) {
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
			if got := isFileURLAllowedForCourse(tt.url, "123"); got != tt.want {
				t.Fatalf("isFileURLAllowedForCourse(%q, %q) = %v, want %v", tt.url, "123", got, tt.want)
			}
		})
	}
}
