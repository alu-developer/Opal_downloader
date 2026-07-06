package scraper

import "testing"

func TestAppendDiscoveredCoursesAcceptsOnlyExactConfiguredCourseTitle(t *testing.T) {
	discovered := map[string]CourseRef{}
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

	appendDiscoveredCourses(discovered, candidates, "https://bildungsportal.sachsen.de/opal/", []string{"Algorithmen und Datenstrukturen"})
	if len(discovered) != 1 {
		t.Fatalf("expected exactly one discovered course with strict exact match, got %d: %#v", len(discovered), discovered)
	}
	if _, ok := discovered["53290106881"]; !ok {
		t.Fatalf("expected repository entry 53290106881 to be discovered, got %#v", discovered)
	}
}

func TestAppendDiscoveredCoursesRequiresExactTitleNoPartialMatch(t *testing.T) {
	discovered := map[string]CourseRef{}
	candidates := []map[string]string{
		{
			"openHref":    "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50999590912",
			"courseTitle": "Analysis",
		},
	}

	appendDiscoveredCourses(discovered, candidates, "https://bildungsportal.sachsen.de/opal/", []string{"Anal"})
	if len(discovered) != 0 {
		t.Fatalf("expected zero discovered courses for partial filter text, got %#v", discovered)
	}
}

func TestAppendDiscoveredCoursesRejectsBoilerplateCourseTitles(t *testing.T) {
	discovered := map[string]CourseRef{}
	candidates := []map[string]string{
		{
			"openHref":    "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53290106881",
			"courseTitle": "Weitere Kursinhalte ansehen",
		},
		{
			"openHref":    "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53324447746",
			"courseTitle": "Kurs öffnen",
		},
	}

	// No filter (nil) means "accept any title" as far as strictCourseFilterMatches
	// is concerned, so only the boilerplate denylist should be rejecting these.
	appendDiscoveredCourses(discovered, candidates, "https://bildungsportal.sachsen.de/opal/", nil)
	if len(discovered) != 0 {
		t.Fatalf("expected boilerplate OPAL call-to-action text to never be stored as a course title, got %#v", discovered)
	}
}

// TestAppendDiscoveredCoursesRejectsRichTextSubheadingAsTitle guards against a
// regression of the real production bug this file's selectors were fixed for:
// OPAL's "Meine Kurse" (auth/resource/courses) course tiles are rendered as
// `.content-preview` cards whose rich-text description can itself contain an
// `h3` heading such as "Was lernt man in diesem Kurs?". Before the fix,
// extractCourseCardsFromCurrentPage's title lookup fell back to a bare
// 'h1,h2,h3' selector and picked up this in-body heading instead of the real
// `.content-preview-title` course name, so it was surfaced as a fake course.
// This test exercises the append/accept path with that exact real-world string
// as the candidate title, asserting the boilerplate/denylist path still keeps
// it out even if it were ever produced by the in-page extraction again.
func TestAppendDiscoveredCoursesRejectsRichTextSubheadingAsTitle(t *testing.T) {
	discovered := map[string]CourseRef{}
	candidates := []map[string]string{
		{
			"openHref":    "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53722382336",
			"courseTitle": "Was lernt man in diesem Kurs?",
		},
	}

	appendDiscoveredCourses(discovered, candidates, "https://bildungsportal.sachsen.de/opal/", nil)
	if len(discovered) != 0 {
		t.Fatalf("expected the in-body FAQ heading text to never be accepted as a course title, got %#v", discovered)
	}
}

func TestIsBoilerplateCourseTitle(t *testing.T) {
	boilerplate := []string{
		"Weitere Kursinhalte ansehen",
		"weitere kursinhalte ansehen",
		"Kurs öffnen",
		"  Kurs öffnen  ",
		"Zum Kurs",
		"Details anzeigen",
		"Was lernt man in diesem Kurs?",
	}
	for _, title := range boilerplate {
		if !isBoilerplateCourseTitle(title) {
			t.Errorf("expected %q to be classified as boilerplate", title)
		}
	}

	realTitles := []string{
		"Algorithmen und Datenstrukturen",
		"Mentorinnenprogramm Informatik",
		"Analysis",
	}
	for _, title := range realTitles {
		if isBoilerplateCourseTitle(title) {
			t.Errorf("expected %q to NOT be classified as boilerplate", title)
		}
	}
}
