package scraper

import "testing"

func TestLooksLikeNavigableShowAllURL(t *testing.T) {
	tests := []struct {
		name       string
		linkTarget string
		want       bool
	}{
		{name: "plain relative link", linkTarget: "/opal/coursenode/123?length=-1", want: true},
		{name: "absolute link", linkTarget: "https://bildungsportal.sachsen.de/opal/coursenode/123?length=-1", want: true},
		{name: "empty target", linkTarget: "", want: false},
		{name: "bare hash", linkTarget: "#", want: false},
		{name: "javascript pseudo url", linkTarget: "javascript:void(0)", want: false},
		{name: "javascript pseudo url mixed case", linkTarget: "JavaScript:doShowAll()", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeNavigableShowAllURL(tt.linkTarget); got != tt.want {
				t.Fatalf("looksLikeNavigableShowAllURL(%q) = %v, want %v", tt.linkTarget, got, tt.want)
			}
		})
	}
}

func TestAppendSectionFolderTargetsSkipsRootAndCurrentSection(t *testing.T) {
	repoID := "53290106881"
	currentURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1771558760922192006"
	courseRootURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881"
	candidates := []map[string]string{
		{
			"href":     currentURL,
			"title":    "Algorithmen und Datenstrukturen",
			"text":     "Algorithmen und Datenstrukturen",
			"rootText": "Algorithmen und Datenstrukturen",
		},
		{
			"href":     courseRootURL,
			"title":    "Kursstart",
			"text":     "Kursstart",
			"rootText": "Kursstart",
		},
		{
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003",
			"title":    "Materialien",
			"text":     "Materialien",
			"rootText": "Materialien",
		},
	}

	queue, _ := appendSectionFolderTargets(nil, map[string]struct{}{}, map[string]struct{}{}, candidates, "https://bildungsportal.sachsen.de/opal/", repoID, currentURL, courseRootURL, "Algorithmen und Datenstrukturen", map[string]string{}, false)
	if len(queue) != 1 {
		t.Fatalf("expected exactly one queued section target, got %d: %#v", len(queue), queue)
	}
	if queue[0] != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003" {
		t.Fatalf("unexpected queued section target: %#v", queue)
	}
}

func TestAppendSectionFolderTargetsAllowsAnyNestedCourseNodePath(t *testing.T) {
	repoID := "53290106881"
	currentURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003"
	courseRootURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881"
	candidates := []map[string]string{
		{
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003/Probeklausur",
			"title":    "Probeklausur",
			"text":     "Probeklausur",
			"rootText": "Probeklausur",
		},
		{
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003/%C3%9Cbungsbl%C3%A4tter",
			"title":    "",
			"text":     "Übungsblätter",
			"rootText": "Übungsblätter",
		},
		{
			// A folder name that was never in the old hardcoded allowlist -
			// must still be descended into, since it's structurally nested
			// under the current CourseNode's path.
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003/Foliensatz",
			"title":    "Foliensatz",
			"text":     "Foliensatz",
			"rootText": "Foliensatz",
		},
		{
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003?7-1.-fluidContainer-downloadTableContainer-btn&antiCache=123",
			"title":    "Tabelle herunterladen",
			"text":     "Tabelle herunterladen",
			"rootText": "Tabelle herunterladen",
		},
		{
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1771558760922192006",
			"title":    "Algorithmen und Datenstrukturen",
			"text":     "Algorithmen und Datenstrukturen",
			"rootText": "Algorithmen und Datenstrukturen",
		},
	}

	queue, _ := appendSectionFolderTargets(nil, map[string]struct{}{}, map[string]struct{}{}, candidates, "https://bildungsportal.sachsen.de/opal/", repoID, currentURL, courseRootURL, "Algorithmen und Datenstrukturen", map[string]string{}, false)
	want := []string{
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003/Probeklausur",
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003/%C3%9Cbungsbl%C3%A4tter",
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003/Foliensatz",
	}
	if len(queue) != len(want) {
		t.Fatalf("expected %d queued nested folder targets, got %d: %#v", len(want), len(queue), queue)
	}
	for i, target := range want {
		if queue[i] != target {
			t.Fatalf("unexpected queued folder target at %d: got %q, want %q", i, queue[i], target)
		}
	}
}

func TestAppendSectionFolderTargetsRecordsRealSectionTitles(t *testing.T) {
	repoID := "53290106881"
	currentURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1771558760922192006"
	courseRootURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881"
	candidates := []map[string]string{
		{
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003",
			"title":    "Übungen",
			"text":     "Übungen",
			"rootText": "Übungen",
		},
	}

	sectionTitles := map[string]string{}
	queue, _ := appendSectionFolderTargets(nil, map[string]struct{}{}, map[string]struct{}{}, candidates, "https://bildungsportal.sachsen.de/opal/", repoID, currentURL, courseRootURL, "Algorithmen und Datenstrukturen", sectionTitles, false)
	if len(queue) != 1 {
		t.Fatalf("expected one queued section target, got %d: %#v", len(queue), queue)
	}

	key := sectionKey(queue[0], repoID)
	got, ok := sectionTitles[key]
	if !ok {
		t.Fatalf("expected sectionTitles to record a title for queued target %q", queue[0])
	}
	// This is the crux of the SectionTitle reliability fix: without recording the
	// real folder-link title here, every file nested under this section would
	// have been labeled with the generic, uninformative "CourseNode" fallback
	// from deriveSectionTitleFromURL instead of the actual OPAL section name.
	if got != "Übungen" {
		t.Fatalf("expected real section title %q to be recorded, got %q", "Übungen", got)
	}
}

func TestAppendSectionFolderTargetsAllowsUnlistedSiblingSectionByDefault(t *testing.T) {
	repoID := "53290106881"
	currentURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003"
	courseRootURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881"
	candidates := []map[string]string{
		{
			// Not in the old hardcoded 5-word allowlist and not an administrative
			// label either - should now be allowed since it isn't self-referential.
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/9988776655",
			"title":    "Klausurergebnisse",
			"text":     "Klausurergebnisse",
			"rootText": "Klausurergebnisse",
		},
		{
			// A breadcrumb-style self-reference back to the course - must stay excluded.
			"href":     "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1122334455",
			"title":    "Algorithmen und Datenstrukturen",
			"text":     "Algorithmen und Datenstrukturen",
			"rootText": "Algorithmen und Datenstrukturen",
		},
	}

	queue, _ := appendSectionFolderTargets(nil, map[string]struct{}{}, map[string]struct{}{}, candidates, "https://bildungsportal.sachsen.de/opal/", repoID, currentURL, courseRootURL, "Algorithmen und Datenstrukturen", map[string]string{}, false)
	if len(queue) != 1 {
		t.Fatalf("expected exactly one queued sibling section, got %d: %#v", len(queue), queue)
	}
	if queue[0] != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/9988776655" {
		t.Fatalf("unexpected queued sibling section: %#v", queue)
	}
}
