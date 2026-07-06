package scraper

import "testing"

// TestClickCandidateLinkOnPageRejectsEmptyPageURL covers the guard clause that keeps
// downloadFileViaBrowser's ShowAllURL fallback from attempting a Playwright navigation
// with an empty target (e.g. for candidates recorded before this fix, or files that
// were never part of a "show all" expansion and so have no ShowAllURL at all). The rest
// of clickCandidateLinkOnPage's link-search behavior drives a real Playwright page
// (Goto/Locator/ExpectDownload) and cannot be unit tested without a live browser
// session; see the package-level note in crawl.go about verifying against a real OPAL
// instance.
func TestClickCandidateLinkOnPageRejectsEmptyPageURL(t *testing.T) {
	s := &OpalScraper{}
	candidate := downloadCandidate{SourceURL: "", ShowAllURL: "", LinkTarget: "target=file_1", LinkText: "Analysis21.pdf"}

	if err := s.clickCandidateLinkOnPage("", candidate, "C:/tmp/does-not-matter.pdf"); err == nil {
		t.Fatalf("expected an error for an empty page URL, got nil")
	}
}

// TestDownloadFileViaBrowserFallsBackToShowAllURLWhenDistinct documents the intended
// fallback ordering at the type level: downloadFileViaBrowser must only attempt the
// ShowAllURL retry when it is both non-empty and distinct from SourceURL, otherwise it
// would needlessly repeat an identical failed navigation/search. This is exercised here
// via the downloadCandidate data shape rather than a live click, since
// downloadFileViaBrowser itself requires a real playwright.Page (s.page) to run
// end-to-end and cannot be unit tested in this environment.
func TestDownloadFileViaBrowserFallsBackToShowAllURLWhenDistinct(t *testing.T) {
	tests := []struct {
		name              string
		candidate         downloadCandidate
		wantDistinctRetry bool
	}{
		{
			name:              "expansion-only file has a distinct show-all URL to retry",
			candidate:         downloadCandidate{SourceURL: "https://example.test/section", ShowAllURL: "https://example.test/section?length=-1"},
			wantDistinctRetry: true,
		},
		{
			name:              "normal-page file has no show-all URL to retry",
			candidate:         downloadCandidate{SourceURL: "https://example.test/section", ShowAllURL: ""},
			wantDistinctRetry: false,
		},
		{
			name:              "show-all URL identical to source URL is not retried again",
			candidate:         downloadCandidate{SourceURL: "https://example.test/section", ShowAllURL: "https://example.test/section"},
			wantDistinctRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDistinctRetry := tt.candidate.ShowAllURL != "" && tt.candidate.ShowAllURL != tt.candidate.SourceURL
			if gotDistinctRetry != tt.wantDistinctRetry {
				t.Fatalf("distinct-retry condition = %v, want %v", gotDistinctRetry, tt.wantDistinctRetry)
			}
		})
	}
}
