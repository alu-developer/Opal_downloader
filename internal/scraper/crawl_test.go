package scraper

import "testing"

func TestAppendCourseSectionsKeepsRootAndAddsOnlyCourseNavigation(t *testing.T) {
	existing := []courseSection{{Label: "Kursstart", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/123"}}
	candidates := []map[string]string{
		{
			"href":  "/opal/goto.php?target=fold_1234&client_id=bildung",
			"text":  "Materialien",
			"title": "Materialien",
		},
		{
			"href":  "/opal/auth/RepositoryEntry/123?cmd=members",
			"text":  "Mitglieder",
			"title": "Mitglieder",
		},
		{
			"href":  "/opal/auth/RepositoryEntry/999",
			"text":  "Fremder Kurs",
			"title": "Fremder Kurs",
		},
		{
			"href":  "/opal/auth/repository/catalog",
			"text":  "Katalog",
			"title": "Katalog",
		},
		{
			"href":  "/opal/goto.php?target=fold_1234&client_id=bildung",
			"text":  "Materialien",
			"title": "Materialien",
		},
		{
			"href":  "/opal/auth/RepositoryEntry/123?ref_id=123&cmd=view",
			"text":  "Startseite",
			"title": "Startseite",
		},
	}

	sections := appendCourseSections(existing, candidates, "https://bildungsportal.sachsen.de/opal/", "123")
	if len(sections) != 3 {
		t.Fatalf("expected 3 sections, got %d: %#v", len(sections), sections)
	}
	if sections[0].Label != "Kursstart" {
		t.Fatalf("expected first section to remain course root, got %#v", sections[0])
	}
	if sections[1].Label != "Materialien" {
		t.Fatalf("expected Materialien section, got %#v", sections[1])
	}
	if sections[2].Label != "Startseite" {
		t.Fatalf("expected Startseite section, got %#v", sections[2])
	}
	if sections[1].URL != "https://bildungsportal.sachsen.de/opal/goto.php?target=fold_1234&client_id=bildung" {
		t.Fatalf("unexpected section URL: %s", sections[1].URL)
	}
}

func TestLooksLikeCourseBrowseLinkRejectsDashboardStyleLinks(t *testing.T) {
	tests := []struct {
		name string
		href string
		text string
		want bool
	}{
		{name: "course folder", href: "/opal/goto.php?target=fold_1234", text: "Materialien", want: true},
		{name: "dashboard", href: "/opal/auth/RepositoryEntry/mycourses", text: "Meine Kurse", want: false},
		{name: "membership", href: "/opal/auth/RepositoryEntry/123?baseClass=ilMembershipOverviewGUI", text: "Mitglieder", want: false},
		{name: "forum label", href: "/opal/auth/RepositoryEntry/123", text: "Forum", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeCourseBrowseLink(tt.href, tt.text); got != tt.want {
				t.Fatalf("looksLikeCourseBrowseLink(%q, %q) = %v, want %v", tt.href, tt.text, got, tt.want)
			}
		})
	}
}
