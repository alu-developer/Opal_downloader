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

func fileNames(files []FileRef) []string {
	var out []string
	for _, f := range files {
		out = append(out, f.Name)
	}
	return out
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
	// file.pdf lives one level deeper still, inside the sub-path's own body -
	// proof that a section's files are extracted from the same fetch that
	// discovers it, not a second one.
	node22HTML := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub" title="Sub">Sub</a>`
	subHTML := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub/file.pdf" data-file-name="file.pdf"><span>file.pdf</span></a>`

	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: rootHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub": {body: subHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22":     {body: node22HTML, status: 200},
	}}

	files, reqs, _, err := discoverSectionsHTTP(fetcher, course, opalURL, false, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs != 3 {
		t.Errorf("expected 3 requests (root, node22, sub-path) - not a second fetch per section for files, got %d", reqs)
	}
	names := fileNames(files)
	if len(names) != 1 || names[0] != "file.pdf" {
		t.Fatalf("expected exactly [file.pdf], got %v", names)
	}
	if files[0].SectionURL != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub" {
		t.Errorf("file attributed to wrong section: %+v", files[0])
	}
}

// TestDiscoverSectionsHTTPReportsVisitsForVisitLog guards the 2026-08-11
// finding: shipping HTTP-first as the default silently stopped
// internal/visitlog's persistent cross-run log from accumulating anything,
// because nothing on this path ever told the caller which sections it had
// actually reached. onSectionVisited is that missing hook - one call per
// section reached (root included, since the root's body is read and
// extracted from just like any other section here), each with the number of
// NEW files that section contributed, mirroring recordSectionVisit's
// semantics on the browser path (crawl.go).
func TestDiscoverSectionsHTTPReportsVisitsForVisitLog(t *testing.T) {
	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1"}

	rootHTML := courseRootHTML(`{"id":"id22","text":"Ordner",` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22","title":"Ordner","class":"node-bc"}}`)
	node22HTML := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub" title="Sub">Sub</a>`
	subHTML := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub/file.pdf" data-file-name="file.pdf"><span>file.pdf</span></a>`

	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: rootHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub": {body: subHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22":     {body: node22HTML, status: 200},
	}}

	type visit struct {
		title, url  string
		filesFound  int
		hadChildren bool
	}
	var visits []visit
	_, _, _, err := discoverSectionsHTTP(fetcher, course, opalURL, false, nil, func(sectionTitle, sectionURL string, filesFound int, hadChildren bool) {
		visits = append(visits, visit{sectionTitle, sectionURL, filesFound, hadChildren})
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(visits) != 3 {
		t.Fatalf("expected one onSectionVisited call per reached section (root, node22, sub-path), got %d: %+v", len(visits), visits)
	}
	// hadChildren distinguishes a container (root: seeds node22; node22: its
	// own body links to Sub) from a terminal leaf (Sub: its body links only
	// to file.pdf, a file link, not a folder link) - see
	// visitlog.Record.HadChildren's doc comment for what this feeds.
	want := map[string]struct {
		files       int
		hadChildren bool
	}{
		course.URL: {0, true},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22":     {0, true},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub": {1, false},
	}
	for _, v := range visits {
		wantVisit, ok := want[v.url]
		if !ok {
			t.Errorf("unexpected visit for %s", v.url)
			continue
		}
		if v.filesFound != wantVisit.files {
			t.Errorf("%s: expected filesFound=%d, got %d", v.url, wantVisit.files, v.filesFound)
		}
		if v.hadChildren != wantVisit.hadChildren {
			t.Errorf("%s: expected hadChildren=%v, got %v", v.url, wantVisit.hadChildren, v.hadChildren)
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
	files, reqs, _, err := discoverSectionsHTTP(fetcher, course, opalURL, true, func(url string, _ error) {
		errored = append(errored, url)
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs != 1 {
		t.Errorf("expected 1 request (root only, node-en never fetched), got %d", reqs)
	}
	if len(errored) != 0 {
		t.Errorf("node-en should never have been fetched, but got a section error for: %v", errored)
	}
	if len(files) != 0 {
		t.Errorf("expected no files (the root page carries none in this fixture), got %+v", files)
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
		course.URL: {body: rootHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22": {body: firstPage, status: 200},
		resolvedShowAll: {body: showAll, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22/Sub": {body: subHTML, status: 200},
	}}

	files, _, _, err := discoverSectionsHTTP(fetcher, course, opalURL, false, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	names := fileNames(files)
	if len(names) != 1 || names[0] != "file.pdf" {
		t.Errorf("PREDICTION FAILED (Step B1 run 1's exact miss): sub-path past the pagination cap was not discovered. files: %v", names)
	}
}

func TestDiscoverSectionsHTTPRootFetchFailureIsFatal(t *testing.T) {
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://opal/auth/RepositoryEntry/1"}
	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: "not found", status: 404},
	}}
	files, _, _, err := discoverSectionsHTTP(fetcher, course, "https://opal/", false, nil, nil)
	if err == nil {
		t.Fatal("expected an error when the course root itself cannot be fetched")
	}
	if files != nil {
		t.Errorf("expected no files on a fatal root failure, got %+v", files)
	}
}

func TestDiscoverSectionsHTTPSectionFetchFailureIsLoggedAndSkipped(t *testing.T) {
	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1"}

	rootHTML := courseRootHTML(`{"id":"id22","text":"Ordner",` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22","title":"Ordner","class":"node-bc"}},` +
		`{"id":"id23","text":"Andere",` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/23","title":"Andere","class":"node-bc"}}`)

	node23HTML := `<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/23/file.pdf" data-file-name="file.pdf"><span>file.pdf</span></a>`

	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: rootHTML, status: 200},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22": {body: "forbidden", status: 403},
		"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/23": {body: node23HTML, status: 200},
	}}

	var errored []string
	files, _, _, err := discoverSectionsHTTP(fetcher, course, opalURL, false, func(url string, _ error) {
		errored = append(errored, url)
	}, nil)
	if err != nil {
		t.Fatalf("a single section's fetch failure must not be fatal for the whole course: %v", err)
	}
	if len(errored) != 1 || errored[0] != "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22" {
		t.Errorf("expected onSectionError called once for CourseNode/22, got %v", errored)
	}
	names := fileNames(files)
	if len(names) != 1 || names[0] != "file.pdf" {
		t.Errorf("expected node23's file.pdf despite node22's failure, got %v", names)
	}
}
