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
// Fast-path root cause, corrected (queue task
// fix-fast-path-download-history-counter, 2026-07-12 live investigation):
// queue task click-wait-audit-and-speedup (2026-07-10) first identified that
// the fast path misses for nearly every file and traced it to OPAL's
// session-wide incrementing "history stack position" counter, theorizing
// that the file URL's embedded counter goes stale by download time. Live
// investigation for *this* task refined that finding: the plain,
// discovery-time file URL (".../CourseNode/<id>/<name>.pdf") actually embeds
// no counter at all, and requesting it directly - confirmed live both via a
// raw HTTP GET *and* via a real page.Goto() navigation - always
// 302-redirects to a counter-suffixed URL that serves a generic HTML page,
// never the file, regardless of how "fresh" the request is. What a real
// browser click does differently: OPAL wires each file row's link to a
// Wicket AJAX "click" behavior (present in the section page's own HTML as a
// `Wicket.Ajax.ajax({"u":"...","c":"<anchor id>",...,"e":"click",...})`
// script snippet - a row's anchor commonly has more than one such behavior,
// e.g. a hover/powertip preview alongside the real click-download one, so
// the "e":"click" one specifically must be selected, see
// findClickAjaxURL in download_refresh.go). Invoking that behavior's "u" URL
// with the right Wicket-Ajax headers (confirmed live: all three of
// Wicket-Ajax/Wicket-Ajax-BaseURL/X-Requested-With are required together,
// see wicketAjaxHeaders) returns an XML ajax-response whose body sets
// `window.location` to a one-time `downloadering?fibercode=<token>` URL -
// *that* URL is what actually serves the file (confirmed live: also
// reusable, a second GET of the same fibercode URL returned identical
// bytes). refreshCounterURL (download_refresh.go) implements exactly this:
// re-fetch the section page fresh, locate the file's click-behavior URL in
// that fresh HTML, invoke it, and extract the fibercode URL - all via the
// same stateless, concurrency-safe APIRequestContext used here, no browser
// tab involved. DownloadFile tries this refresh as a second attempt, below,
// before falling back to the browser-click path.
//
// When neither the original fast GET nor the counter-refresh retry returns a
// direct, non-HTML 200 response, DownloadFile falls back to
// downloadFileViaBrowser, which drives the single shared s.page and
// therefore must not run concurrently with itself or with any other
// navigation of s.page. That fallback is serialized behind
// s.browserDownloadMu so callers are free to invoke DownloadFile from a
// worker pool for the common (fast-path/refresh) case.
func (s *OpalScraper) DownloadFile(fileURL, localPath string) error {
	ctx := s.getContext()
	if ctx == nil {
		return errors.New("no authenticated browser context available")
	}

	reqCtx := ctx.Request()

	response, err := reqCtx.Get(fileURL)
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

	// Fast HTTP-GET path missed on the discovery-time URL (non-200 status,
	// request error, or a text/html response instead of the file) - before
	// falling back to the slow, serialized browser-click path, retry once
	// via the counter-refresh dance (see this function's doc comment and
	// download_refresh.go): re-fetch the file's section page, locate its
	// click-triggered Wicket AJAX behavior there, invoke it, and try the
	// one-time downloadering URL it returns. This stays entirely on the
	// concurrency-safe APIRequestContext (no browser tab), so it's free to
	// run from every download worker just like the first attempt above.
	if candidate, ok := s.downloadCandidates[fileURL]; ok {
		if refreshedURL, refreshErr := s.refreshCounterURL(reqCtx, candidate); refreshErr == nil {
			if handled, dlErr := attemptDirectDownload(reqCtx, refreshedURL, localPath); handled {
				if s.debugClicks {
					s.auditLog("fast-path-refresh-hit", nil, fileURL, fmt.Sprintf("counter-refresh retry succeeded via %s", refreshedURL))
				}
				return dlErr
			} else if s.debugClicks {
				s.auditLog("fast-path-refresh-miss", nil, fileURL, fmt.Sprintf("counter-refresh retry produced %s but it still wasn't a direct file response -> falling back to browser download for %s", refreshedURL, localPath))
			}
		} else if s.debugClicks {
			s.auditLog("fast-path-refresh-error", nil, fileURL, fmt.Sprintf("counter-refresh retry failed: %v", refreshErr))
		}
	}

	// Log why the fast path (including the refresh retry above) missed,
	// since this determines whether a file falls through to the serialized
	// browser-fallback path below, which queue task
	// click-wait-audit-and-speedup's audit is specifically meant to
	// diagnose (see performance-assessment-report.md's finding that the
	// fallback rate is the single biggest measured slowdown cause).
	if s.debugClicks {
		status := -1
		contentType := ""
		reqErr := ""
		if err != nil {
			reqErr = err.Error()
		} else if response != nil {
			status = response.Status()
			contentType = strings.ToLower(response.Headers()["content-type"])
		}
		s.auditLog("fast-path-miss", nil, fileURL, fmt.Sprintf("status=%d content-type=%q err=%q -> falling back to browser download for %s", status, contentType, reqErr, localPath))
	}

	s.browserDownloadMu.Lock()
	defer s.browserDownloadMu.Unlock()
	return s.downloadFileViaBrowser(fileURL, localPath)
}

// attemptDirectDownload performs a stateless HTTP GET against requestURL
// and, if the response is a genuine direct file (200, non-text/html), writes
// it to localPath. handled=false (err=nil) means "not a direct file
// response - try the next fallback step"; handled=true means a definitive
// outcome was reached (a successful write, or a definitive failure like a
// corrupt PDF payload) and the caller should stop trying further steps and
// return err as-is. Mirrors the original fast-path GET's own success/PDF
// -validity logic in DownloadFile above, factored out so the counter-refresh
// retry (which GETs a different, refreshed URL) can share it exactly rather
// than duplicating slightly-different logic.
func attemptDirectDownload(reqCtx playwright.APIRequestContext, requestURL, localPath string) (handled bool, err error) {
	response, reqErr := reqCtx.Get(requestURL)
	if reqErr != nil || response == nil || response.Status() != 200 {
		return false, nil
	}
	contentType := strings.ToLower(response.Headers()["content-type"])
	if strings.Contains(contentType, "text/html") {
		return false, nil
	}
	body, bodyErr := response.Body()
	if bodyErr != nil {
		return false, nil
	}
	if strings.HasSuffix(strings.ToLower(localPath), ".pdf") && !strings.HasPrefix(string(body), "%PDF") {
		return true, fmt.Errorf("downloaded payload is not a valid PDF")
	}
	return true, os.WriteFile(localPath, body, 0o644)
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
	s.waitForInteractiveLinks(page, contentFallbackWaitMs)

	targetFragment := hrefSelectorFragment(candidate.LinkTarget)

	if targetFragment != "" {
		selector := hrefContainsSelector(targetFragment)
		s.auditLog("click", page, selector, "download fallback href-match attempt for "+localPath)
		download, clickErr := page.ExpectDownload(func() error {
			return page.Locator(selector).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			s.auditLog("click-success", page, selector, "download fallback href-match succeeded for "+localPath)
			return download.SaveAs(localPath)
		}
	}

	if candidate.LinkText != "" {
		s.auditLog("click", page, candidate.LinkText, "download fallback text-match attempt for "+localPath)
		download, clickErr := page.ExpectDownload(func() error {
			return page.GetByText(candidate.LinkText, playwright.PageGetByTextOptions{Exact: playwright.Bool(false)}).First().Click(playwright.LocatorClickOptions{Timeout: playwright.Float(5000)})
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if clickErr == nil {
			s.auditLog("click-success", page, candidate.LinkText, "download fallback text-match succeeded for "+localPath)
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
