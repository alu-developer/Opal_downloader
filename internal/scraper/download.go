package scraper

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/logging"
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

	response, err := s.getPolitely(reqCtx, fileURL)
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
			if handled, dlErr := s.attemptDirectDownload(reqCtx, refreshedURL, localPath); handled {
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

	// plainURLServesHTML records what the fast-path GET above actually
	// observed for this exact URL: a 200 whose body is an HTML page, not the
	// file. downloadFileViaBrowser uses it to skip its own
	// navigate-and-hope-for-a-download attempt, which in that case is a
	// guaranteed ~15s timeout per file (see its doc comment).
	plainURLServesHTML := err == nil && response != nil && response.Status() == 200 &&
		strings.Contains(strings.ToLower(response.Headers()["content-type"]), "text/html")

	// Sync-speed Question 44 follow-on (2026-08-20): --debug-clicks-gated
	// instrumentation to measure how often a worker actually waits behind
	// another file's browser-fallback resolution, rather than guessing from
	// first principles. auditLog already no-ops when s.debugClicks is
	// false, so this costs one time.Now() call on the normal path.
	waitStart := time.Now()
	s.browserDownloadMu.Lock()
	if wait := time.Since(waitStart); s.debugClicks {
		s.auditLog("browser-fallback-lock-wait", nil, fileURL, fmt.Sprintf("waited %s for browserDownloadMu", wait))
	}
	holdStart := time.Now()
	defer func() {
		if s.debugClicks {
			s.auditLog("browser-fallback-lock-hold", nil, fileURL, fmt.Sprintf("held browserDownloadMu for %s", time.Since(holdStart)))
		}
		s.browserDownloadMu.Unlock()
	}()
	return s.downloadFileViaBrowser(fileURL, localPath, plainURLServesHTML)
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
func (s *OpalScraper) attemptDirectDownload(reqCtx playwright.APIRequestContext, requestURL, localPath string) (handled bool, err error) {
	response, reqErr := s.getPolitely(reqCtx, requestURL)
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

// browserFallbackMaxAttempts bounds how many times downloadFileViaBrowser
// replays its whole per-page click search before giving up on a file.
//
// Why more than one (queue task investigate-per-file-html-fallback-failures,
// 2026-07-20): a specific, structurally-identifiable set of files - those past
// the first pagination page of a click-expanded ("Alle anzeigen") section -
// can never be served by the fast HTTP path or the counter-refresh, so for
// them this browser-click fallback is not a safety net, it is the *only* path.
// It was nonetheless a strictly one-shot chain of individually flaky steps
// (navigate, wait for render, click by href, click by text, re-click show-all,
// search again), each with its own timeout, and any single flake meant a
// permanent failure for that file with a generic error message. Live evidence:
// in one clean 36-file run of a real course, 6 files reached this path and all
// 6 succeeded, but `Vorlesung_9_10.pdf` only succeeded on the very last step of
// the chain - the same file that failed outright in the run that triggered this
// task. Two attempts is deliberately modest: this path is serialized behind
// s.browserDownloadMu, so every extra attempt costs the whole sync wall-clock
// time, and the evidence points at a marginal flake rather than something that
// needs many retries to shake loose.
const browserFallbackMaxAttempts = 2

// browserFallbackRetryWaitMs is the pause between those attempts, giving a
// slow OPAL/Wicket render (the most likely cause of a marginal miss) time to
// settle before the identical sequence is replayed.
const browserFallbackRetryWaitMs = 1500

// downloadFileViaBrowser is DownloadFile's last resort: drive the shared
// browser page to click the file's real link. It must not run concurrently
// with itself - DownloadFile holds s.browserDownloadMu across the call.
//
// plainURLServesHTML tells it what DownloadFile's own fast-path GET already
// observed: that requesting fileURL directly returns a 200 HTML page rather
// than the file. When that is known *and* a recorded download candidate exists
// to click instead, the initial "navigate to fileURL and hope a download
// starts" attempt below is skipped - it cannot succeed (a plain request for a
// file URL always 302s to a generic HTML page, established live and documented
// at length in DownloadFile's doc comment), and letting it run costs a full
// 15s ExpectDownload timeout per affected file, serialized. It is still
// attempted whenever there is no candidate to fall back to, since then it is
// the only thing left to try.
func (s *OpalScraper) downloadFileViaBrowser(fileURL, localPath string, plainURLServesHTML bool) error {
	page := s.getPage()
	if page == nil {
		return errors.New("no browser page available for download fallback")
	}

	candidate, hasCandidate := s.downloadCandidates[fileURL]

	if !hasCandidate || !plainURLServesHTML {
		// This navigates the shared page away, so whatever the memo recorded
		// about it is no longer true.
		s.fallbackPage.invalidate()
		download, err := page.ExpectDownload(func() error {
			_, navErr := s.gotoPolitely(page, fileURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)})
			return navErr
		}, playwright.PageExpectDownloadOptions{Timeout: playwright.Float(15000)})
		if err == nil {
			return download.SaveAs(localPath)
		}
	}

	if !hasCandidate {
		return errors.New("response is HTML, not a direct file download, and no download candidate was recorded for it during discovery")
	}

	// This is the point of no return to the slow path: every cheaper option
	// (a direct GET, the counter-refresh retry, and - unless plainURLServesHTML
	// already ruled it out - the plain navigate-and-hope attempt above) has
	// already missed, and everything left is a serialized, multi-page,
	// multi-click search that can legitimately run for minutes (see
	// browserFallbackMaxAttempts's own arithmetic in docs/sync-speed-model.md's
	// "Next experiment" - 2 outer attempts x up to 3 candidate pages x 2 click
	// strategies, each with its own multi-second timeout). Said once here
	// rather than per attempt/page/click so a real user sees one line
	// explaining the wait instead of either silence (looks hung) or a stream
	// of internals (2026-08-19 maintainer decision, item 2 - this must be
	// visible, not just eventually explained after the fact the way a
	// download error already is).
	logging.User("%s is not available as a direct link - resolving it the slow way (this can take a minute or two)", localPath)

	var lastErr error
	for attempt := 1; attempt <= browserFallbackMaxAttempts; attempt++ {
		if attempt > 1 {
			page.WaitForTimeout(browserFallbackRetryWaitMs)
		}
		lastErr = tryCandidatePagesInOrder(candidate, func(pageURL string) error {
			return s.clickCandidateLinkOnPage(pageURL, candidate, localPath)
		})
		if lastErr == nil {
			return nil
		}
		s.auditLog("browser-fallback-attempt-failed", page, fileURL, fmt.Sprintf("attempt %d/%d for %s failed: %v", attempt, browserFallbackMaxAttempts, localPath, lastErr))
	}

	// The "(technical detail: " marker matches internal/netcheck's own
	// convention (see its doc comment) so any consumer of this error can
	// split the one sentence a user needs - a file could not be downloaded -
	// from the Playwright locator/timeout internals three past
	// investigations (PRs #35/#89/#95) needed but a normal user does not.
	return fmt.Errorf("response is HTML, browser fallback click did not find a downloadable link after %d attempts (technical detail: %w)", browserFallbackMaxAttempts, lastErr)
}

// tryCandidatePagesInOrder implements the retry ordering for locating a download
// candidate's link: try the page where the candidate was originally recorded first. For
// files only revealed by a section's "show all"/"Alle anzeigen" expansion, that recorded
// SourceURL is the section's plain (unexpanded) page, which won't render the link - so if
// the click search comes up empty there, retry on candidate.ShowAllURL (the expanded page
// where the link actually renders, for sections expanded by navigating to a distinct URL)
// and then on candidate.ExpandedPageURL (the Wicket page-instance URL the browser was
// left on after a *click*-driven expansion, see its doc comment). Each alternate page is
// only tried when non-empty and distinct from everything already tried, so a candidate
// that has no real alternative never repeats an identical failed attempt. tryPage is
// injected so this ordering can be unit tested without a real playwright.Page.
//
// On total failure it returns the joined underlying errors rather than a fixed message.
// That matters more than it looks: the single generic string this used to return ("...
// browser fallback click did not find downloadable link") was the *only* thing three
// separate investigations (PRs #35, #89, #95) had to work from, which is why each had to
// re-derive the actual cause live from scratch. It also leaked into an unrelated caller -
// download_refresh.go's refreshCounterURL shares this ordering helper, so a counter-
// refresh miss used to be reported as a "browser fallback click" failure, hiding the real
// reason (typically: the file's anchor is genuinely absent from the re-fetched HTML
// because it lives past the first pagination page).
func tryCandidatePagesInOrder(candidate downloadCandidate, tryPage func(pageURL string) error) error {
	var errs []error
	tried := []string{}

	attempt := func(pageURL string) bool {
		trimmed := strings.TrimSpace(pageURL)
		for _, already := range tried {
			if strings.EqualFold(already, trimmed) {
				return false
			}
		}
		tried = append(tried, trimmed)
		if err := tryPage(pageURL); err != nil {
			errs = append(errs, fmt.Errorf("on %s: %w", pageURL, err))
			return false
		}
		return true
	}

	// SourceURL is always tried, even when empty - clickCandidateLinkOnPage
	// has its own guard for that and its error is worth surfacing.
	if attempt(candidate.SourceURL) {
		return nil
	}
	for _, alternate := range []string{candidate.ShowAllURL, candidate.ExpandedPageURL} {
		if strings.TrimSpace(alternate) == "" {
			continue
		}
		if attempt(alternate) {
			return nil
		}
	}

	return errors.Join(errs...)
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

	// Skip the navigation entirely when the page is already showing exactly
	// this listing from the previous file's download - a download click does
	// not navigate, so consecutive files from one section can share the page.
	// See fallback_page_memo.go for the measurement that motivated this.
	reusing := s.fallbackPage.canReuse(pageURL, page.URL())
	if !reusing {
		s.fallbackPage.invalidate()
		if _, err := s.gotoPolitely(page, pageURL, playwright.PageGotoOptions{WaitUntil: playwright.WaitUntilStateDomcontentloaded, Timeout: playwright.Float(20000)}); err != nil {
			return err
		}
	}
	// Give the page's JS a chance to finish rendering the link-list before the
	// selector search below - this page may be revisited well after discovery
	// (concurrent downloads sharing the browser's network stack can slow down
	// in-page rendering), and unlike collectCourseFiles's crawl loop, nothing
	// here previously waited for interactive content before searching for the
	// candidate link. See PR #11 follow-up investigation (queue task
	// fix-download-fallback-html-errors-post-pr11) for the live-observed flake
	// this addresses.
	if !reusing {
		s.waitForInteractiveLinks(page, contentFallbackWaitMs)
		s.fallbackPage.record(pageURL, page.URL(), false)
	}

	firstErr := s.tryClickDownloadSelectors(page, candidate, localPath)
	if firstErr == nil {
		return nil
	}

	if !candidate.ShowAllViaClick {
		return fmt.Errorf("%w (page has no click-expandable pagination to retry)", firstErr)
	}

	// This candidate was only revealed by a click-triggered (non-navigable) "show
	// all" expansion during discovery (see downloadCandidate.ShowAllViaClick's doc
	// comment) - there is no distinct ShowAllURL to fall back to, so replicate the
	// same click here on the page we just landed on before giving up. Found live
	// (queue task fix-html-response-download-fallback-failures, 2026-07-17): this
	// is what was causing every beyond-the-first-page file in a click-only-expanded
	// section to permanently fail both the fast-path counter-refresh and this
	// browser-fallback click.
	// Already expanded by a previous file from this same section: re-clicking
	// would toggle the pagination back, not help.
	if s.fallbackPage.Expanded && reusing {
		return fmt.Errorf("%w (page was already expanded for an earlier file from this section)", firstErr)
	}
	clicked, _ := s.attemptShowAllExpandClick(page, pageURL)
	if !clicked {
		return fmt.Errorf("%w (the show-all re-expansion click did not succeed either)", firstErr)
	}
	s.waitForInteractiveLinks(page, contentFallbackWaitMs)
	s.fallbackPage.markExpanded(page.URL())
	if err := s.tryClickDownloadSelectors(page, candidate, localPath); err != nil {
		return fmt.Errorf("%w (still not found after re-expanding the paginated listing)", err)
	}
	return nil
}

// tryClickDownloadSelectors searches page for candidate's download link, first by its
// recorded href fragment then by its recorded visible text, clicking and saving whichever
// matches first. Extracted from clickCandidateLinkOnPage so it can be retried a second
// time, after a show-all re-expansion click, without duplicating the two selector
// strategies - see ShowAllViaClick's doc comment for why that retry is needed.
func (s *OpalScraper) tryClickDownloadSelectors(page playwright.Page, candidate downloadCandidate, localPath string) error {
	targetFragment := hrefSelectorFragment(candidate.LinkTarget)
	var attemptErrs []error

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
		attemptErrs = append(attemptErrs, fmt.Errorf("href-match %s: %w", logging.ProtectPath(selector), clickErr))
	} else {
		attemptErrs = append(attemptErrs, errors.New("href-match skipped: candidate has no usable link target"))
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
		attemptErrs = append(attemptErrs, fmt.Errorf("text-match %q: %w", logging.ProtectPath(candidate.LinkText), clickErr))
	} else {
		attemptErrs = append(attemptErrs, errors.New("text-match skipped: candidate has no recorded link text"))
	}

	return fmt.Errorf("downloadable link not found on page: %w", errors.Join(attemptErrs...))
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
