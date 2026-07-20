package scraper

import (
	"errors"
	"strings"
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

// TestTryCandidatePagesInOrderTriesExpandedPageURL covers the retry target added by
// queue task investigate-per-file-html-fallback-failures (2026-07-20): a file past the
// first pagination page of a *click*-expanded section has no ShowAllURL at all, and
// re-fetching SourceURL always yields the collapsed first page that does not contain its
// anchor. ExpandedPageURL - the Wicket page-instance URL the browser was left on right
// after the expansion rendered - is the only recorded page that can contain it, so it
// must be tried, last, and only when it is distinct from what was already tried.
func TestTryCandidatePagesInOrderTriesExpandedPageURL(t *testing.T) {
	tests := []struct {
		name      string
		candidate downloadCandidate
		wantCalls []string
	}{
		{
			name: "click-expanded candidate falls back to the expanded page instance URL",
			candidate: downloadCandidate{
				SourceURL:       "https://example.test/section",
				ShowAllViaClick: true,
				ExpandedPageURL: "https://example.test/section?1032",
			},
			wantCalls: []string{"https://example.test/section", "https://example.test/section?1032"},
		},
		{
			name: "all three are tried in order when all are distinct",
			candidate: downloadCandidate{
				SourceURL:       "https://example.test/section",
				ShowAllURL:      "https://example.test/section?length=-1",
				ExpandedPageURL: "https://example.test/section?1032",
			},
			wantCalls: []string{
				"https://example.test/section",
				"https://example.test/section?length=-1",
				"https://example.test/section?1032",
			},
		},
		{
			name: "expanded page URL identical to source URL is not retried again",
			candidate: downloadCandidate{
				SourceURL:       "https://example.test/section",
				ExpandedPageURL: "https://example.test/section",
			},
			wantCalls: []string{"https://example.test/section"},
		},
		{
			name: "expanded page URL identical to show-all URL is not retried again",
			candidate: downloadCandidate{
				SourceURL:       "https://example.test/section",
				ShowAllURL:      "https://example.test/section?length=-1",
				ExpandedPageURL: "https://example.test/section?length=-1",
			},
			wantCalls: []string{"https://example.test/section", "https://example.test/section?length=-1"},
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

// TestTryCandidatePagesInOrderReportsUnderlyingCauses locks in the diagnostic fix from
// the same task: the helper used to swallow every underlying error and return one fixed
// string ("response is HTML, browser fallback click did not find downloadable link"),
// which is the entire reason three separate investigations into that message had to
// re-derive the real cause live from scratch. Each failed page's own error - and which
// page it came from - must survive into the returned error.
func TestTryCandidatePagesInOrderReportsUnderlyingCauses(t *testing.T) {
	candidate := downloadCandidate{
		SourceURL:       "https://example.test/section",
		ExpandedPageURL: "https://example.test/section?1032",
	}

	err := tryCandidatePagesInOrder(candidate, func(pageURL string) error {
		return errors.New("anchor missing for " + pageURL)
	})
	if err == nil {
		t.Fatalf("expected an error when every page attempt fails, got nil")
	}
	for _, want := range []string{
		"https://example.test/section",
		"https://example.test/section?1032",
		"anchor missing for",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err.Error(), want)
		}
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

// TestHrefSelectorFragmentKeepsPercentEncodingForNonASCIINames covers the root cause
// found while investigating queue-task fix-download-fallback-html-errors-post-pr11:
// a live sync consistently failed to download "ÜB10.pdf" via the browser-fallback
// click path even though the file's link was correctly discovered and recorded as a
// download candidate. clickCandidateLinkOnPage used to reduce an absolute LinkTarget
// to url.URL.Path, which percent-decodes escapes (e.g. "%C3%9C" -> "Ü"); the actual
// href attribute rendered in the DOM stays percent-encoded, so the resulting
// a[href*='...'] CSS selector searched for a literal "Ü" that could never match,
// confirmed live via a direct Playwright Locator.Count() against the real page
// (count=0 with the old .Path-derived fragment). hrefSelectorFragment must instead
// preserve the encoding via EscapedPath so the selector matches what is actually in
// the DOM.
func TestHrefSelectorFragmentKeepsPercentEncodingForNonASCIINames(t *testing.T) {
	tests := []struct {
		name       string
		linkTarget string
		want       string
	}{
		{
			name:       "absolute URL with percent-encoded umlaut stays encoded",
			linkTarget: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2/%C3%9CB10.pdf",
			want:       "/opal/auth/RepositoryEntry/1/CourseNode/2/%C3%9CB10.pdf",
		},
		{
			name:       "absolute URL with plain ASCII name is unaffected",
			linkTarget: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2/Vorlesung_11.pdf",
			want:       "/opal/auth/RepositoryEntry/1/CourseNode/2/Vorlesung_11.pdf",
		},
		{
			name:       "relative target is used verbatim (not parsed as a URL)",
			linkTarget: "/opal/goto.php?target=file_55&cmd=sendfile",
			want:       "/opal/goto.php?target=file_55&cmd=sendfile",
		},
		{
			name:       "empty target stays empty",
			linkTarget: "   ",
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hrefSelectorFragment(tt.linkTarget); got != tt.want {
				t.Fatalf("hrefSelectorFragment(%q) = %q, want %q", tt.linkTarget, got, tt.want)
			}
		})
	}
}

// TestHrefContainsSelectorEscapesSingleQuotes confirms the CSS attribute selector
// built for the click fallback can't be broken out of by a single quote in the
// fragment (e.g. present in an unusual filename).
func TestHrefContainsSelectorEscapesSingleQuotes(t *testing.T) {
	got := hrefContainsSelector("/opal/goto.php?target=file_1'2")
	want := "a[href*='/opal/goto.php?target=file_1\\'2']"
	if got != want {
		t.Fatalf("hrefContainsSelector(...) = %q, want %q", got, want)
	}
}
