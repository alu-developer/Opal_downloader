package scraper

import (
	"fmt"
	"testing"
)

// courseRootHTML builds a minimal course-root page: an initial_data tree
// payload (what ParseCourseTreeNodes reads) plus, optionally, an anchor in
// the body so the root itself can also carry a folder-link candidate for
// appendSectionFolderTargets to find - real course roots do (a "Kursstart"
// link back to themselves, filtered by the existing root/current-section
// skip in appendSectionFolderTargets).
func courseRootHTML(treeNodes string) string {
	return fmt.Sprintf(`<html><body>var initial_data=[%s];</body></html>`, treeNodes)
}

func TestDiscoverSectionsHTTPSeedsFromTreeAndExpandsSubPaths(t *testing.T) {
	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1"}

	rootHTML := courseRootHTML(`{"id":"id22","text":"Ordner",` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22","title":"Ordner","class":"node-bc"}}`)

	// The tree seed only ever reaches the bare CourseNode/22 URL. The
	// sub-path CourseNode/22/Sub is a folder row INSIDE that node's own page,
	// which only fetching CourseNode/22 and expanding can discover - the
	// thing Step B1 proved works (docs/sync-speed-model.md Question 36).
	node22HTML := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub" title="Sub">Sub</a>`
	subHTML := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub/file.pdf" data-file-name="file.pdf"><span>file.pdf</span></a>`

	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: rootHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub": {body: subHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22":     {body: node22HTML, status: 200},
	}}

	sections, reqs, err := discoverSectionsHTTP(fetcher, course, opalURL, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs != 3 {
		t.Errorf("expected 3 requests (root, node22, sub-path), got %d", reqs)
	}
	var urls []string
	for _, s := range sections {
		urls = append(urls, s.URL)
	}
	want := []string{
		course.URL,
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22",
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub",
	}
	if len(urls) != len(want) {
		t.Fatalf("sections: got %v want %v", urls, want)
	}
	for i, w := range want {
		if urls[i] != w {
			t.Errorf("section %d: got %q want %q", i, urls[i], w)
		}
	}
}

func TestDiscoverSectionsHTTPSkipsNonFileSectionAtSeed(t *testing.T) {
	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1"}

	rootHTML := courseRootHTML(`{"id":"iden","text":"Einschreibung",` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/99","title":"Einschreibung","class":"node-en"}}`)

	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: rootHTML, status: 200},
		// Deliberately no canned response for CourseNode/99: if the seed ever
		// fetches it, the fake returns 404 and discoverSectionsHTTP reports a
		// section error - proof the filter kept it out of the queue.
	}}

	var errored []string
	sections, reqs, err := discoverSectionsHTTP(fetcher, course, opalURL, true, func(url string, _ error) {
		errored = append(errored, url)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs != 1 {
		t.Errorf("expected 1 request (root only, node-en never fetched), got %d", reqs)
	}
	if len(errored) != 0 {
		t.Errorf("node-en should never have been fetched, but got a section error for: %v", errored)
	}
	if len(sections) != 1 || sections[0].URL != course.URL {
		t.Errorf("expected only the course root as a section, got %+v", sections)
	}
}

func TestDiscoverSectionsHTTPFollowsShowAllDuringExpansion(t *testing.T) {
	// Reproduces the exact mechanism Step B1 run 1 diagnosed as its miss: a
	// section's first (paginated) page does not carry a sub-path link that
	// only shows up past the pagination cap, in the show-all response.
	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1"}

	rootHTML := courseRootHTML(`{"id":"id22","text":"Ordner",` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22","title":"Ordner","class":"node-bc"}}`)

	firstPage := `Wicket.Ajax.ajax({"u":"/opal/auth/RepositoryEntry/1/CourseNode/22?1091-pager-showAllLink","e":"click"});`
	showAll := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub" title="Sub">Sub</a>`
	resolvedShowAll := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22?1091-pager-showAllLink"
	subHTML := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub/file.pdf" data-file-name="file.pdf"><span>file.pdf</span></a>`

	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL:      {body: rootHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22": {body: firstPage, status: 200},
		resolvedShowAll: {body: showAll, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub": {body: subHTML, status: 200},
	}}

	sections, _, err := discoverSectionsHTTP(fetcher, course, opalURL, false, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var found bool
	for _, s := range sections {
		if s.URL == "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub" {
			found = true
		}
	}
	if !found {
		t.Errorf("PREDICTION FAILED (Step B1 run 1's exact miss): sub-path past the pagination cap was not discovered. sections: %+v", sections)
	}
}

func TestDiscoverSectionsHTTPRootFetchFailureIsFatal(t *testing.T) {
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://opal/auth/RepositoryEntry/1"}
	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: "not found", status: 404},
	}}
	sections, _, err := discoverSectionsHTTP(fetcher, course, "https://opal/", false, nil)
	if err == nil {
		t.Fatal("expected an error when the course root itself cannot be fetched")
	}
	if sections != nil {
		t.Errorf("expected no sections on a fatal root failure, got %+v", sections)
	}
}

func TestDiscoverSectionsHTTPSectionFetchFailureIsLoggedAndSkipped(t *testing.T) {
	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1"}

	rootHTML := courseRootHTML(`{"id":"id22","text":"Ordner",` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22","title":"Ordner","class":"node-bc"}},` +
		`{"id":"id23","text":"Andere",` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/23","title":"Andere","class":"node-bc"}}`)

	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: rootHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22": {body: "forbidden", status: 403},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/23": {body: "<html></html>", status: 200},
	}}

	var errored []string
	sections, _, err := discoverSectionsHTTP(fetcher, course, opalURL, false, func(url string, _ error) {
		errored = append(errored, url)
	})
	if err != nil {
		t.Fatalf("a single section's fetch failure must not be fatal for the whole course: %v", err)
	}
	if len(errored) != 1 || errored[0] != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22" {
		t.Errorf("expected onSectionError called once for CourseNode/22, got %v", errored)
	}
	var urls []string
	for _, s := range sections {
		urls = append(urls, s.URL)
	}
	want := []string{course.URL, "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/23"}
	if len(urls) != len(want) {
		t.Fatalf("sections: got %v want %v (the failing node22 must be absent, node23 must still be found)", urls, want)
	}
	for i, w := range want {
		if urls[i] != w {
			t.Errorf("section %d: got %q want %q", i, urls[i], w)
		}
	}
}
