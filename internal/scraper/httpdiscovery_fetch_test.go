package scraper

import (
	"strings"
	"testing"

	"github.com/mxschmitt/playwright-go"
)

// fakeHTTPResponse implements playwright.APIResponse fully (Body, Dispose,
// Headers, HeadersArray, JSON, SecurityDetails, ServerAddr, Status,
// StatusText, Text, URL) so the fetch layer is testable without a browser.
type fakeHTTPResponse struct {
	body   string
	status int
}

func (f fakeHTTPResponse) Status() int                          { return f.status }
func (f fakeHTTPResponse) StatusText() string                   { return "" }
func (f fakeHTTPResponse) Text() (string, error)                { return f.body, nil }
func (f fakeHTTPResponse) Body() ([]byte, error)                { return []byte(f.body), nil }
func (f fakeHTTPResponse) Headers() map[string]string           { return nil }
func (f fakeHTTPResponse) HeadersArray() []playwright.NameValue { return nil }
func (f fakeHTTPResponse) Ok() bool                             { return f.status >= 200 && f.status < 300 }
func (f fakeHTTPResponse) URL() string                          { return "" }
func (f fakeHTTPResponse) Dispose() error                       { return nil }
func (f fakeHTTPResponse) JSON(v interface{}) error             { return nil }
func (f fakeHTTPResponse) SecurityDetails() (*playwright.ResponseSecurityDetailsResult, error) {
	return nil, nil
}
func (f fakeHTTPResponse) ServerAddr() (*playwright.ResponseServerAddrResult, error) {
	return nil, nil
}

// fakeHTTPFetcher records the URLs it was asked for and returns canned
// responses, so the test asserts both the request sequence (normal vs
// paginated) and the parsed result.
type fakeHTTPFetcher struct {
	responses map[string]fakeHTTPResponse
	requested []string
}

func (f *fakeHTTPFetcher) Get(url string, options ...playwright.APIRequestContextGetOptions) (playwright.APIResponse, error) {
	f.requested = append(f.requested, url)
	// Match by the LONGEST prefix in the canned map, so a specific show-all
	// key (...?1091-pager-showAllLink) wins over the section URL (.../Part-3)
	// when the show-all URL happens to start with the section's path.
	var bestPrefix string
	for prefix := range f.responses {
		if strings.HasPrefix(url, prefix) && len(prefix) > len(bestPrefix) {
			bestPrefix = prefix
		}
	}
	if bestPrefix != "" {
		return f.responses[bestPrefix], nil
	}
	return fakeHTTPResponse{status: 404}, nil
}

func TestFetchSectionFilesHTTPNormal(t *testing.T) {
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://opal/auth/RepositoryEntry/1"}
	section := SectionRef{CourseRepoID: "1", Title: "S", URL: "https://opal/auth/RepositoryEntry/1/CourseNode/2"}
	body := `<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/a.pdf" data-file-name="a.pdf"><span>a.pdf</span></a>` +
		`<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/b.pdf" data-file-name="b.pdf"><span>b.pdf</span></a>`
	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		section.URL: {body: body, status: 200},
	}}
	files, reqs, err := fetchSectionFilesHTTP(fetcher, course, section, "https://opal/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if reqs != 1 {
		t.Errorf("a non-paginated section should issue 1 request, got %d", reqs)
	}
}

func TestFetchSectionFilesHTTPPaginatedFollowsShowAll(t *testing.T) {
	// Use the same absolute opalURL base the code resolves against, so the
	// show-all URL the code computes matches the canned response's key.
	const opalURL = "https://bildungsportal.sachsen.de/opal/"
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1"}
	section := SectionRef{CourseRepoID: "1", Title: "Part-3", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3"}
	// First page: 2 files + a pager-showAllLink AJAX wiring. The 2 first-page
	// files deliberately overlap with the show-all set so the fileSeen dedupe
	// is exercised (must not double-count). The show-all "u" is relative and
	// gets resolved against opalURL by the code.
	firstPage := `<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3/p1.pdf" data-file-name="p1.pdf"><span>p1.pdf</span></a>` +
		`<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3/p2.pdf" data-file-name="p2.pdf"><span>p2.pdf</span></a>` +
		`Wicket.Ajax.ajax({"u":"/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3?1091-pager-showAllLink","e":"click"});`
	// Show-all response: the same 2 plus 3 more (rows past the first page).
	showAll := `<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3/p1.pdf" data-file-name="p1.pdf"><span>p1.pdf</span></a>` +
		`<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3/p2.pdf" data-file-name="p2.pdf"><span>p2.pdf</span></a>` +
		`<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3/p3.pdf" data-file-name="p3.pdf"><span>p3.pdf</span></a>` +
		`<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3/p4.pdf" data-file-name="p4.pdf"><span>p4.pdf</span></a>` +
		`<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3/p5.pdf" data-file-name="p5.pdf"><span>p5.pdf</span></a>`
	// The resolved show-all URL after resolveURL(opalURL, "/opal/auth/...?...").
	resolvedShowAll := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2/Part-3?1091-pager-showAllLink"
	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		section.URL:     {body: firstPage, status: 200},
		resolvedShowAll: {body: showAll, status: 200},
	}}

	files, reqs, err := fetchSectionFilesHTTP(fetcher, course, section, opalURL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reqs != 2 {
		t.Errorf("a paginated section should issue 2 requests, got %d", reqs)
	}
	names := fileNamesFromHTTPFiles(files)
	want := []string{"p1.pdf", "p2.pdf", "p3.pdf", "p4.pdf", "p5.pdf"}
	if len(names) != len(want) {
		t.Fatalf("files: got %v want %v", names, want)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("files[%d]: got %q want %q", i, names[i], n)
		}
	}
	// Both URLs must have been fetched: the section page, then the show-all endpoint.
	if len(fetcher.requested) != 2 {
		t.Errorf("requested %d URLs, want 2 (section + show-all): %v", len(fetcher.requested), fetcher.requested)
	}
}

func TestFetchSectionFilesHTTPNonOKStatus(t *testing.T) {
	course := CourseRef{RepoID: "1", Title: "C", URL: "https://opal/auth/RepositoryEntry/1"}
	section := SectionRef{CourseRepoID: "1", Title: "S", URL: "https://opal/auth/RepositoryEntry/1/CourseNode/2"}
	fetcher := &fakeHTTPFetcher{responses: map[string]fakeHTTPResponse{
		section.URL: {body: "forbidden", status: 403},
	}}
	_, _, err := fetchSectionFilesHTTP(fetcher, course, section, "https://opal/")
	if err == nil {
		t.Fatal("expected an error for non-200 status, got nil")
	}
}
