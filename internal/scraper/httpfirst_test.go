package scraper

import (
	"sort"
	"testing"
)

// TestHTTPFirstCourseCrawlSeedsFromTreeAndFollowsShowAll exercises the whole
// httpFirstCourseCrawl path against fakes, no browser and no network: a course
// root whose initial_data seeds one folder section, that section's first page
// carries one file plus a pager-showAllLink, and the show-all response carries
// a second file AND a link to a further sub-section - the exact shape Step
// B1's first run got wrong (rows past the pagination cap can be sub-sections,
// not just files; see appendSectionFolderTargets's call site in this file).
func TestHTTPFirstCourseCrawlSeedsFromTreeAndFollowsShowAll(t *testing.T) {
	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	course := CourseRef{
		RepoID: "1",
		Title:  "Analysis",
		URL:    "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1",
	}

	rootBody := `<script>var initial_data=[{"id":"idroot","text":"<span class=\"jstree-title\">Kurs<\/span>",` +
		`"a_attr":{},"children":[` +
		`{"id":"id1","text":"<span class=\"jstree-title\">Ordner<\/span>","li_attr":{"class":"node-bc"},` +
		`"a_attr":{"href":"https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/11","title":"Ordner","class":"node-bc"}}]}];</script>`

	folderURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/11"
	folderBody := `<a href="/opal/auth/RepositoryEntry/1/CourseNode/11/a.pdf" data-file-name="a.pdf"><span>a.pdf</span></a>` +
		`Wicket.Ajax.ajax({"u":"/opal/auth/RepositoryEntry/1/CourseNode/11?1091-pager-showAllLink","e":"click"});`

	showAllURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/11?1091-pager-showAllLink"
	subSectionURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22"
	showAllBody := `<a href="/opal/auth/RepositoryEntry/1/CourseNode/11/a.pdf" data-file-name="a.pdf"><span>a.pdf</span></a>` +
		`<a href="/opal/auth/RepositoryEntry/1/CourseNode/11/b.pdf" data-file-name="b.pdf"><span>b.pdf</span></a>` +
		`<a href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/22" title="Unterordner">Unterordner</a>`

	subSectionBody := `<a href="/opal/auth/RepositoryEntry/1/CourseNode/22/c.pdf" data-file-name="c.pdf"><span>c.pdf</span></a>`

	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL:    {body: rootBody, status: 200},
		folderURL:     {body: folderBody, status: 200},
		showAllURL:    {body: showAllBody, status: 200},
		subSectionURL: {body: subSectionBody, status: 200},
	}}

	sc := New(opalURL, "")
	files, downloadCandidates, err := sc.httpFirstCourseCrawl(fetcher, course)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name)
	}
	sort.Strings(names)
	want := []string{"a.pdf", "b.pdf", "c.pdf"}
	if len(names) != len(want) {
		t.Fatalf("files: got %v want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("files[%d]: got %q want %q", i, names[i], n)
		}
	}

	if len(downloadCandidates) != 3 {
		t.Errorf("downloadCandidates: got %d entries, want 3: %+v", len(downloadCandidates), downloadCandidates)
	}

	// The sub-section discovered only via the show-all response must actually
	// have been visited, not just found as a candidate.
	if !containsURL(fetcher.requested, subSectionURL) {
		t.Errorf("expected the sub-section revealed by show-all to be fetched; requested %v", fetcher.requested)
	}

	visits := sc.VisitRecords()
	if len(visits) != 3 {
		t.Fatalf("expected 3 recorded section visits (root's folder seed, the folder, and the sub-section), got %d: %+v", len(visits), visits)
	}
}

func containsURL(urls []string, target string) bool {
	for _, u := range urls {
		if u == target {
			return true
		}
	}
	return false
}

// TestHTTPFirstCourseCrawlReportsAllSectionsFailed mirrors
// collectCourseFiles' identical guard in crawl.go: if every attempted section
// fails to load, that is a crawl error, not a confirmed-empty course.
func TestHTTPFirstCourseCrawlReportsAllSectionsFailed(t *testing.T) {
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://opal/auth/RepositoryEntry/1"}
	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		course.URL: {body: "forbidden", status: 403},
	}}
	sc := New("https://opal/", "")
	_, _, err := sc.httpFirstCourseCrawl(fetcher, course)
	if err == nil {
		t.Fatal("expected an error when every attempted section fails to load")
	}
}
