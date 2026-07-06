package scraper

import "testing"

func TestIsSectionURLAllowedForCourseV2RejectsForeignAndDashboardTargets(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "course root", url: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123", want: true},
		{name: "course folder without repo", url: "https://bildungsportal.sachsen.de/opal/goto.php?target=fold_1234", want: true},
		{name: "foreign repo", url: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/999", want: false},
		{name: "dashboard", url: "https://bildungsportal.sachsen.de/opal/auth/home#my-courses", want: false},
		{name: "catalog", url: "https://bildungsportal.sachsen.de/opal/auth/repository/catalog", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSectionURLAllowedForCourseV2(tt.url, "123"); got != tt.want {
				t.Fatalf("isSectionURLAllowedForCourseV2(%q, %q) = %v, want %v", tt.url, "123", got, tt.want)
			}
		})
	}
}

func TestLooksLikeSectionLinkV2RejectsGlobalAndUtilityLinks(t *testing.T) {
	tests := []struct {
		name  string
		href  string
		title string
		want  bool
	}{
		{name: "folder", href: "/opal/goto.php?target=fold_1234", title: "Materialien", want: true},
		{name: "current course view", href: "/opal/auth/RepositoryEntry/123?cmd=view", title: "Startseite", want: true},
		{name: "members", href: "/opal/auth/RepositoryEntry/123?baseClass=ilMembershipOverviewGUI", title: "Mitglieder", want: false},
		{name: "my courses", href: "/opal/auth/RepositoryEntry/mycourses", title: "Meine Kurse", want: false},
		{name: "forum title", href: "/opal/auth/RepositoryEntry/123", title: "Forum", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeSectionLinkV2(tt.href, tt.title); got != tt.want {
				t.Fatalf("looksLikeSectionLinkV2(%q, %q) = %v, want %v", tt.href, tt.title, got, tt.want)
			}
		})
	}
}

func TestSectionKeyV2NormalizesRepositoryRootVariants(t *testing.T) {
	repoID := "123"
	root := sectionKeyV2("https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123", repoID)
	view := sectionKeyV2("https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123?cmd=view&ref_id=123", repoID)
	if root != view {
		t.Fatalf("expected root and cmd=view URLs to share section key, got %q vs %q", root, view)
	}
}

func TestSectionKeyV2KeepsDistinctFolderTargets(t *testing.T) {
	repoID := "123"
	foldA := sectionKeyV2("https://bildungsportal.sachsen.de/opal/goto.php?target=fold_1234&client_id=bildung", repoID)
	foldB := sectionKeyV2("https://bildungsportal.sachsen.de/opal/goto.php?target=fold_5678&client_id=bildung", repoID)
	if foldA == foldB {
		t.Fatalf("expected different folder targets to produce different keys, got %q", foldA)
	}
}
