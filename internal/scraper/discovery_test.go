package scraper

import (
	"errors"
	"strings"
	"testing"
)

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

// The three cases below pin the distinction that was missing until
// 2026-07-27: "every course-listing page failed" and "this account has no
// courses" used to arrive at the caller as the identical (empty, nil) result.
// A live developer-mode run reported "Found 0 course links / Discovered 0
// remote files" - indistinguishable from a healthy sync - when in truth the
// browser window had gone and nothing had been read at all.
func TestAllCourseSourcesFailed(t *testing.T) {
	cases := []struct {
		name   string
		failed int
		total  int
		want   bool
	}{
		{"every source failed", 3, 3, true},
		{"a partial failure is only a warning", 2, 3, false},
		{"no failures", 0, 3, false},
		// Guards against the predicate reading "0 == 0" as total failure if
		// sourcePages were ever emptied, which would abort every discovery.
		{"no sources at all is not a failure", 0, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := allCourseSourcesFailed(tc.failed, tc.total); got != tc.want {
				t.Fatalf("allCourseSourcesFailed(%d, %d) = %v, want %v", tc.failed, tc.total, got, tc.want)
			}
		})
	}
}

func TestIsClosedBrowserError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		// Both strings are copied from the real 2026-07-27 log, which is the
		// point: these are matched on Playwright's wording, so a test that
		// invented its own phrasing would prove nothing.
		{
			"playwright's long form",
			errors.New(`Frame.Goto https://bildungsportal.sachsen.de/opal/auth/resource/courses: playwright: target closed: Target page, context or browser has been closed`),
			true,
		},
		{
			"playwright's short form",
			errors.New(`Frame.Goto https://bildungsportal.sachsen.de/opal/auth/home: target closed`),
			true,
		},
		{"an ordinary timeout is not a closed browser", errors.New("Timeout 20000ms exceeded"), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isClosedBrowserError(tc.err); got != tc.want {
				t.Fatalf("isClosedBrowserError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestCourseDiscoveryFailureExplainsAClosedWindow(t *testing.T) {
	closed := courseDiscoveryFailure(3, errors.New("target closed")).Error()
	if !strings.Contains(closed, "leave it open") {
		t.Fatalf("a closed-browser failure must tell the user to leave the window open, got: %s", closed)
	}

	// The hint must stay specific to the cause. Telling someone their browser
	// window closed when OPAL merely timed out sends them looking in the wrong
	// place, which is worse than saying nothing.
	timedOut := courseDiscoveryFailure(3, errors.New("Timeout 20000ms exceeded")).Error()
	if strings.Contains(timedOut, "leave it open") {
		t.Fatalf("a timeout must not be reported as a closed window, got: %s", timedOut)
	}
	if !strings.Contains(timedOut, "Timeout 20000ms exceeded") {
		t.Fatalf("the underlying error must survive into the message, got: %s", timedOut)
	}
}
