package scraper

import (
	"errors"
	"testing"
)

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

// TestTryCandidatePagesInOrder exercises the real retry-order logic used by
// downloadFileViaBrowser's ShowAllURL fallback (extracted into
// tryCandidatePagesInOrder so it can be driven without a live playwright.Page). It
// asserts SourceURL is always tried first, and that ShowAllURL is only tried as a
// second attempt when it is non-empty and distinct from SourceURL - not merely by
// re-deriving that boolean condition in the test, but by observing which pages the
// injected tryPage callback was actually invoked with, and in what order.
func TestTryCandidatePagesInOrder(t *testing.T) {
	tests := []struct {
		name      string
		candidate downloadCandidate
		wantCalls []string
	}{
		{
			name:      "expansion-only file retries on distinct show-all URL after source fails",
			candidate: downloadCandidate{SourceURL: "https://example.test/section", ShowAllURL: "https://example.test/section?length=-1"},
			wantCalls: []string{"https://example.test/section", "https://example.test/section?length=-1"},
		},
		{
			name:      "normal-page file has no show-all URL so only source is tried",
			candidate: downloadCandidate{SourceURL: "https://example.test/section", ShowAllURL: ""},
			wantCalls: []string{"https://example.test/section"},
		},
		{
			name:      "show-all URL identical to source URL is not retried again",
			candidate: downloadCandidate{SourceURL: "https://example.test/section", ShowAllURL: "https://example.test/section"},
			wantCalls: []string{"https://example.test/section"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotCalls []string
			err := tryCandidatePagesInOrder(tt.candidate, func(pageURL string) error {
				gotCalls = append(gotCalls, pageURL)
				return errors.New("not found on this page")
			})

			if err == nil {
				t.Fatalf("expected an error when every page attempt fails, got nil")
			}
			if len(gotCalls) != len(tt.wantCalls) {
				t.Fatalf("tryPage called with %v, want %v", gotCalls, tt.wantCalls)
			}
			for i, want := range tt.wantCalls {
				if gotCalls[i] != want {
					t.Fatalf("tryPage call[%d] = %q, want %q", i, gotCalls[i], want)
				}
			}
		})
	}
}

// TestTryCandidatePagesInOrderStopsOnFirstSuccess confirms the ShowAllURL retry is
// never attempted once the SourceURL attempt already succeeds.
func TestTryCandidatePagesInOrderStopsOnFirstSuccess(t *testing.T) {
	candidate := downloadCandidate{SourceURL: "https://example.test/section", ShowAllURL: "https://example.test/section?length=-1"}

	var gotCalls []string
	err := tryCandidatePagesInOrder(candidate, func(pageURL string) error {
		gotCalls = append(gotCalls, pageURL)
		return nil
	})

	if err != nil {
		t.Fatalf("expected nil error on first-attempt success, got %v", err)
	}
	if len(gotCalls) != 1 || gotCalls[0] != candidate.SourceURL {
		t.Fatalf("tryPage called with %v, want single call to %q", gotCalls, candidate.SourceURL)
	}
}
