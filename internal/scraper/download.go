package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// DownloadFile downloads fileURL to localPath. It first tries a plain HTTP
// GET through the shared Playwright APIRequestContext (s.context.Request()),
// which is safe to call concurrently from multiple goroutines - the
// underlying connection dispatches each call with its own atomic message ID
// and a sync.Map of pending callbacks (see playwright-go's connection.go),
// so it is not tied to a single page/tab the way page navigation is.
//
// If the fast path doesn't return a direct, non-HTML 200 response, it falls
// back to downloadFileViaBrowser, which drives the single shared s.page and
// therefore must not run concurrently with itself or with any other
// navigation of s.page. That fallback is serialized behind
// s.browserDownloadMu so callers are free to invoke DownloadFile from a
// worker pool for the common (fast-path) case.
func (s *OpalScraper) DownloadFile(fileURL, localPath string) error {
	ctx := s.getContext()
	if ctx == nil {
		return errors.New("no authenticated browser context available")
	}

	response, err := ctx.Request().Get(fileURL)
	if err == nil && response != nil && response.Status() == 200 {
		headers := response.Headers()
		contentType := strings.ToLower(headers["content-type"])
		body, bodyErr := response.Body()
		if bodyErr == nil && !strings.Contains(contentType, "text/html") {
			if strings.HasSuffix(strings.ToLower(localPath), ".pdf") && !strings.HasPrefix(string(body), "%PDF") {
				return fmt.Errorf("downloaded payload is not a valid PDF")
			}
			return os.WriteFile(localPath, body, 0o644)
		}
	}

	s.browserDownloadMu.Lock()
	defer s.browserDownloadMu.Unlock()
	return s.downloadFileViaBrowser(fileURL, localPath)
}

func (s *OpalScraper) downloadFileViaBrowser(fileURL, localPath string) error {
	page := s.getPage()
	if page == nil {
		return errors.New("no browser page available for download fallback")
	}

	download, err := page.ExpectDownload(func() error {
		_, navErr := page.Goto(fileURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
		return navErr
	}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
	if err == nil {
		return download.SaveAs(localPath)
	}

	candidate, ok := s.downloadCandidates[fileURL]
	if !ok {
		return errors.New("response is HTML, not a direct file download")
	}

	return tryCandidatePagesInOrder(candidate, func(pageURL string) error {
		return s.clickCandidateLinkOnPage(pageURL, candidate, localPath)
	})
}

// tryCandidatePagesInOrder implements the retry ordering for locating a download
// candidate's link: try the page where the candidate was originally recorded first. For
// files only revealed by a section's "show all"/"Alle anzeigen" expansion, that recorded
// SourceURL is the section's plain (unexpanded) page, which won't render the link - so if
// the click search comes up empty there, retry on candidate.ShowAllURL, the expanded page
// where the link actually renders, but only when it is non-empty and distinct from
// SourceURL (otherwise it would just repeat an identical failed attempt). tryPage is
// injected so this ordering can be unit tested without a real playwright.Page.
func tryCandidatePagesInOrder(candidate downloadCandidate, tryPage func(pageURL string) error) error {
	if err := tryPage(candidate.SourceURL); err == nil {
		return nil
	}

	if strings.TrimSpace(candidate.ShowAllURL) != "" && !strings.EqualFold(strings.TrimSpace(candidate.ShowAllURL), strings.TrimSpace(candidate.SourceURL)) {
		if err := tryPage(candidate.ShowAllURL); err == nil {
			return nil
		}
	}

	return errors.New("response is HTML, browser fallback click did not find downloadable link")
}

// clickCandidateLinkOnPage navigates to pageURL and attempts to locate and click the
// download candidate's link there, saving the resulting download to localPath. It
// returns an error (without wrapping context) whenever the link could not be found or
// clicked on that page, so callers can try an alternate page as a fallback.
func (s *OpalScraper) clickCandidateLinkOnPage(pageURL string, candidate downloadCandidate, localPath string) error {
	if strings.TrimSpace(pageURL) == "" {
		return errors.New("no page URL to search for downloadable link")
	}

	page := s.getPage()
	if page == nil {
		return errors.New("no browser page available for download fallback")
	}

	if _, err := page.Goto(pageURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
		return err
	}
	// Give the page's JS a chance to finish rendering the link-list before the
	// selector search below - this page may be revisited well after discovery
	// (concurrent downloads sharing the browser's network stack can slow down
	// in-page rendering), and unlike collectCourseFiles's crawl loop, nothing
	// here previously waited for interactive content before searching for the
	// candidate link. See PR #11 follow-up investigation (queue task
	// fix-download-fallback-html-errors-post-pr11) for the live-observed flake
	// this addresses.
	s.waitForInteractiveLinks(page, contentSelectorTimeoutMs, contentFallbackWaitMs)

	targetFragment := hrefSelectorFragment(candidate.LinkTarget)

	if targetFragment != "" {
		selector := hrefContainsSelector(targetFragment)
		download, clickErr := page.ExpectDownload(func() error {
			return page.Locator(selector).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			return download.SaveAs(localPath)
		}
	}

	if candidate.LinkText != "" {
		download, clickErr := page.ExpectDownload(func() error {
			return page.GetByText(candidate.LinkText, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			return download.SaveAs(localPath)
		}
	}

	return errors.New("downloadable link not found on page")
}

// hrefSelectorFragment reduces a download candidate's recorded LinkTarget (which may be
// an absolute URL or a relative href, as originally captured verbatim from the DOM's
// href attribute) to the path fragment used to build clickCandidateLinkOnPage's
// a[href*=...] CSS attribute selector.
//
// It deliberately uses url.URL.EscapedPath(), not the decoded .Path field: .Path
// percent-decodes escapes in the URL (e.g. "%C3%9C" becomes "Ü"), but the literal href
// attribute rendered in the DOM stays percent-encoded - a CSS attribute selector like
// a[href*='...'] matches against that raw, still-encoded attribute string, never the
// decoded form. Using .Path here silently produced a selector that could never match
// for any filename containing a percent-escaped character: confirmed live for
// "ÜB10.pdf" (href contains ".../%C3%9CB10.pdf") as part of investigating
// queue-task fix-download-fallback-html-errors-post-pr11 - the old selector searched
// for the literal "Ü" character and matched zero elements, producing exactly the
// "browser fallback click did not find downloadable link" error that task reported.
func hrefSelectorFragment(linkTarget string) string {
	fragment := strings.TrimSpace(linkTarget)
	if strings.HasPrefix(fragment, "http") {
		if parsed, err := url.Parse(fragment); err == nil {
			fragment = parsed.EscapedPath()
		}
	}
	return fragment
}

// hrefContainsSelector builds the a[href*='...'] CSS attribute selector used to find a
// candidate's download link on a page, escaping any single quote in fragment so it
// cannot break out of the selector's quoted string.
func hrefContainsSelector(fragment string) string {
	return fmt.Sprintf("a[href*='%s']", strings.ReplaceAll(fragment, "'", "\\'"))
}
