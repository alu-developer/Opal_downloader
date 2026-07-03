package scraper

import "testing"

func TestAppendDiscoveredCoursesV3AcceptsOnlyExactConfiguredCourseTitle(t *testing.T) {
	discovered := map[string]CourseRefV2{}
	candidates := []map[string]string{
		{
			"openHref":    "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881",
			"courseTitle": "Algorithmen und Datenstrukturen",
		},
		{
			"openHref":    "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53324447746",
			"courseTitle": "Mentorinnenprogramm Informatik",
		},
		{
			"openHref":    "",
			"courseTitle": "Analysis",
		},
	}

	appendDiscoveredCoursesV3(discovered, candidates, "https://bildungsportal.sachsen.de/opal/", []string{"Algorithmen und Datenstrukturen"})
	if len(discovered) != 1 {
		t.Fatalf("expected exactly one discovered course with strict exact match, got %d: %#v", len(discovered), discovered)
	}
	if _, ok := discovered["53290106881"]; !ok {
		t.Fatalf("expected repository entry 53290106881 to be discovered, got %#v", discovered)
	}
}

func TestAppendDiscoveredCoursesV3RequiresExactTitleNoPartialMatch(t *testing.T) {
	discovered := map[string]CourseRefV2{}
	candidates := []map[string]string{
		{
			"openHref":    "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50999590912",
			"courseTitle": "Analysis",
		},
	}

	appendDiscoveredCoursesV3(discovered, candidates, "https://bildungsportal.sachsen.de/opal/", []string{"Anal"})
	if len(discovered) != 0 {
		t.Fatalf("expected zero discovered courses for partial filter text, got %#v", discovered)
	}
}
