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

func TestLooksLikeShowAllControlV2MatchesKnownOpalStylePatterns(t *testing.T) {
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
			if got := looksLikeShowAllControlV2(tt.linkTarget, tt.text, tt.title); got != tt.want {
				t.Fatalf("looksLikeShowAllControlV2(%q, %q, %q) = %v, want %v", tt.linkTarget, tt.text, tt.title, got, tt.want)
			}
		})
	}
}

func TestFindShowAllTargetV2FindsFirstMatchingCandidate(t *testing.T) {
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

	target, found := findShowAllTargetV2(candidates)
	if !found {
		t.Fatalf("expected to find a show-all target")
	}
	if target != "/opal/coursenode/123?7-1.-tbl-length=-1" {
		t.Fatalf("unexpected show-all target: %q", target)
	}
}

func TestFindShowAllTargetV2ReturnsNotFoundWhenAbsent(t *testing.T) {
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

	if _, found := findShowAllTargetV2(candidates); found {
		t.Fatalf("expected no show-all target to be found")
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
