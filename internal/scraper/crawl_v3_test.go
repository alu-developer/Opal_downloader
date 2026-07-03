package scraper

import "testing"

func TestAppendSectionFolderTargetsV3SkipsRootAndCurrentSection(t *testing.T) {
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

	queue := appendSectionFolderTargetsV3(nil, map[string]struct{}{}, map[string]struct{}{}, candidates, "https://bildungsportal.sachsen.de/opal/", repoID, currentURL, courseRootURL)
	if len(queue) != 1 {
		t.Fatalf("expected exactly one queued section target, got %d: %#v", len(queue), queue)
	}
	if queue[0] != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003" {
		t.Fatalf("unexpected queued section target: %#v", queue)
	}
}

func TestAppendSectionFolderTargetsV3UsesStrictAllowlist(t *testing.T) {
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

	queue := appendSectionFolderTargetsV3(nil, map[string]struct{}{}, map[string]struct{}{}, candidates, "https://bildungsportal.sachsen.de/opal/", repoID, currentURL, courseRootURL)
	if len(queue) != 2 {
		t.Fatalf("expected only allowlisted folder links to be queued, got %d: %#v", len(queue), queue)
	}
	if queue[0] != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003/Probeklausur" {
		t.Fatalf("unexpected first queued folder target: %#v", queue)
	}
	if queue[1] != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881/CourseNode/1775615795226691003/%C3%9Cbungsbl%C3%A4tter" {
		t.Fatalf("unexpected second queued folder target: %#v", queue)
	}
}
