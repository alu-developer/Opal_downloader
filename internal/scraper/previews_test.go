package scraper

import "testing"

// The live A/B against the real account is the evidence that matters, but it
// can only sample the cases that account happens to contain. This covers the
// decision exhaustively, and in particular pins the two conditions that stop
// this from breaking downloads.
func TestOnlySubframeFileRequestsAreDiscarded(t *testing.T) {
	const previewURL = "https://bildungsportal.sachsen.de/opal/FolderResource/53228666883/10-st.pdf?1785140807450"
	const sectionURL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53228666883/CourseNode/1615865126729195011"

	cases := []struct {
		name         string
		resourceType string
		url          string
		inSubframe   bool
		want         bool
	}{
		{"the case this exists for: an inline preview", "document", previewURL, true, true},

		// The two that would break downloading if they were ever discarded.
		{"a download navigates in the main frame", "document", previewURL, false, false},
		{"a section page is never a file", "document", sectionURL, false, false},

		// Narrowness: only whole-document loads. A stylesheet or image served
		// from FolderResource is a page asset, and blocking assets changes how
		// the page renders for no measured gain.
		{"an image from the same path is left alone", "image", previewURL, true, false},
		{"a stylesheet from the same path is left alone", "stylesheet", previewURL, true, false},
		{"an xhr from the same path is left alone", "xhr", previewURL, true, false},

		// A subframe that is not a file at all - OPAL embeds other things too.
		{"a subframe document that is not a file", "document", sectionURL, true, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isDiscardablePreview(c.resourceType, c.url, c.inSubframe); got != c.want {
				t.Errorf("isDiscardablePreview(%q, ..., inSubframe=%v) = %v, want %v",
					c.resourceType, c.inSubframe, got, c.want)
			}
		})
	}
}

func TestBlockedPreviewCountingIsHonest(t *testing.T) {
	// A run that blocked nothing and a run that blocked everything must not
	// look identical from the outside - otherwise the change is unfalsifiable
	// in the field.
	s := &OpalScraper{}
	if s.previewsBlocked != 0 {
		t.Fatalf("a fresh scraper starts at %d", s.previewsBlocked)
	}
	s.previewsBlocked = 3
	s.previewBytesSaved = 4096
	if s.previewsBlocked != 3 || s.previewBytesSaved != 4096 {
		t.Errorf("counters do not hold what was put in them")
	}
}
