package scraper

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// This file implements the fast-path counter-refresh retry described in
// download.go's DownloadFile doc comment (queue task
// fix-fast-path-download-history-counter, 2026-07-12 live investigation).
//
// Live investigation against a real OPAL course (see that task's PR
// description for the full transcript) found the actual mechanism to be more
// specific than PR #55's original "stale counter embedded in the file URL"
// theory: the file's plain, discovery-time href
// (".../CourseNode/<id>/<name>.pdf") carries **no** counter at all, and
// requesting it directly - via a raw HTTP GET *or* a real
// page.Goto() navigation, confirmed both ways live - always 302-redirects to
// a counter-suffixed URL that serves a generic HTML page, never the file.
// What a real browser click does differently: OPAL's file-list rows are
// wired up with a Wicket AJAX "click" behavior (visible in the section
// page's own HTML as a `Wicket.Ajax.ajax({"u":"...","c":"<anchor id>", ...,
// "e":"click", ...})` script snippet, keyed to the anchor's own `id`
// attribute) - not a plain link navigation at all. Invoking that AJAX
// behavior URL (with the standard Wicket-Ajax request headers) returns a
// small XML "ajax-response" whose body contains
// `window.location='.../downloadering?fibercode=<token>'` - a one-time-token
// URL that *does* serve the real file content when GETed. Confirmed live:
// this fibercode URL is not single-use (a second GET of the same fibercode
// URL returned the identical bytes), so no extra care is needed there.
//
// So "refresh the counter" concretely means: re-fetch the section page (via
// the same concurrency-safe APIRequestContext used elsewhere in this file,
// not a browser tab) to get its current HTML, locate the specific file's
// anchor by its discovery-time LinkText (matched against the DOM's
// data-file-name attribute), find that anchor's *click*-tagged Wicket AJAX
// behavior URL (a row can have more than one behavior on the same id - e.g.
// a hover/powertip preview - so the click-tagged one specifically must be
// selected), invoke it with the Wicket-Ajax headers, and extract the
// downloadering URL from the AJAX response. All of this stays on the
// stateless HTTP APIRequestContext - no browser tab/page is opened, so this
// retry is exactly as safe to run concurrently across download workers as
// the original fast path already was, and never touches s.page or
// s.browserDownloadMu.

// wicketFetcher performs a single HTTP GET (optionally with extra headers)
// and returns the response status and body as a string. It exists so
// refreshCounterURLPure (the actual parsing/orchestration logic) can be unit
// tested with a fake in-memory fetcher instead of a live Playwright
// APIRequestContext - mirroring how tryCandidatePagesInOrder is tested via
// an injected tryPage callback in download_test.go.
type wicketFetcher func(url string, headers map[string]string) (status int, body string, err error)

// refreshCounterURLPure re-fetches sectionURL, locates linkText's file
// anchor, invokes its click-tagged Wicket AJAX behavior, and returns the
// one-time downloadering URL extracted from that behavior's response. See
// the package-level doc comment above for the full mechanism this
// implements.
func refreshCounterURLPure(fetch wicketFetcher, opalURL, sectionURL, linkText string) (string, error) {
	if strings.TrimSpace(sectionURL) == "" {
		return "", errors.New("no section URL to refresh against")
	}
	if strings.TrimSpace(linkText) == "" {
		return "", errors.New("no link text recorded for this file, cannot locate it on a re-fetched page")
	}

	status, html, err := fetch(sectionURL, nil)
	if err != nil {
		return "", fmt.Errorf("re-fetching section page: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("re-fetching section page: unexpected status %d", status)
	}

	anchorTag := findAnchorTagByDataFileName(html, linkText)
	if anchorTag == "" {
		return "", fmt.Errorf("could not find an anchor for %q in the refreshed section HTML", linkText)
	}
	anchorID := extractHTMLAttr(anchorTag, "id")
	if anchorID == "" {
		return "", fmt.Errorf("anchor for %q has no id attribute to match against its AJAX behavior", linkText)
	}

	clickURL := findClickAjaxURL(html, anchorID)
	if clickURL == "" {
		return "", fmt.Errorf("no click-triggered Wicket AJAX behavior found for %q (id=%s)", linkText, anchorID)
	}
	absClickURL := resolveURL(opalURL, strings.ReplaceAll(clickURL, `\/`, "/"))

	ajaxStatus, ajaxBody, ajaxErr := fetch(absClickURL, wicketAjaxHeaders(opalURL, sectionURL))
	if ajaxErr != nil {
		return "", fmt.Errorf("invoking click AJAX behavior: %w", ajaxErr)
	}
	if ajaxStatus != 200 {
		return "", fmt.Errorf("click AJAX behavior returned unexpected status %d", ajaxStatus)
	}

	target := extractAjaxRedirectTarget(ajaxBody)
	if target == "" {
		return "", errors.New("click AJAX response did not contain a download redirect target")
	}
	return target, nil
}

// refreshCounterURL is refreshCounterURLPure's real Playwright wiring: it
// tries candidate.SourceURL first and, only if that doesn't work, falls back
// to candidate.ShowAllURL when recorded and distinct - reusing
// tryCandidatePagesInOrder's existing, already-tested retry ordering (see
// download.go) rather than duplicating that logic.
func (s *OpalScraper) refreshCounterURL(reqCtx playwright.APIRequestContext, candidate downloadCandidate) (string, error) {
	fetch := func(url string, headers map[string]string) (int, string, error) {
		opts := playwright.APIRequestContextGetOptions{}
		if len(headers) > 0 {
			opts.Headers = headers
		}
		resp, err := s.getPolitely(reqCtx, url, opts)
		if err != nil {
			return 0, "", err
		}
		body, bodyErr := resp.Body()
		if bodyErr != nil {
			return resp.Status(), "", bodyErr
		}
		return resp.Status(), string(body), nil
	}

	// Note tryCandidatePagesInOrder also tries candidate.ExpandedPageURL, the
	// Wicket page-instance URL left behind by a click-driven "show all"
	// expansion. That is the only one of the three that can contain a
	// beyond-the-first-page file's anchor: a plain HTTP fetch cannot run the
	// show-all click, so re-fetching SourceURL always yields the collapsed
	// first page. Live-confirmed 2026-07-20 that without it, every such file
	// (6 of 36 in one real course) permanently missed this refresh and fell
	// through to the slow serialized browser-click fallback.
	var result string
	err := tryCandidatePagesInOrder(candidate, func(sectionURL string) error {
		url, refreshErr := refreshCounterURLPure(fetch, s.opalURL, sectionURL, candidate.LinkText)
		if refreshErr != nil {
			return refreshErr
		}
		result = url
		return nil
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// dataFileNameAttrRe/anchorIDAttrRe are compiled lazily per findAnchorTagByDataFileName
// call (the needle varies per file), so only the outer <a ...> shape is a
// fixed pattern here.
var anchorTagRe = regexp.MustCompile(`<a\s[^>]*>`)

// findAnchorTagByDataFileName scans html for a <a ...> opening tag whose
// data-file-name attribute matches fileName exactly (after the same minimal
// HTML-attribute escaping OPAL itself would apply), regardless of what order
// its other attributes (id, href, ...) appear in. Returns "" when no match
// is found.
func findAnchorTagByDataFileName(html, fileName string) string {
	needle := `data-file-name="` + htmlAttrEscape(fileName) + `"`
	for _, tag := range anchorTagRe.FindAllString(html, -1) {
		if strings.Contains(tag, needle) {
			return tag
		}
	}
	return ""
}

// htmlAttrEscape applies the minimal HTML-entity escaping relevant to
// matching a double-quoted HTML attribute value: "&" must be escaped first
// (otherwise it would double-escape the entities produced by the other
// replacements), then '"' and the angle brackets for good measure.
func htmlAttrEscape(value string) string {
	replacer := strings.NewReplacer("&", "&amp;", `"`, "&quot;", "<", "&lt;", ">", "&gt;")
	return replacer.Replace(value)
}

// htmlAttrValueRe finds a single `attr="value"` pair; used by extractHTMLAttr
// with the attribute name substituted in.
func extractHTMLAttr(tag, attr string) string {
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(attr) + `="([^"]*)"`)
	m := re.FindStringSubmatch(tag)
	if len(m) > 1 {
		return m[1]
	}
	return ""
}

// wicketAjaxBehaviorWindow bounds how far past a "u"/"c" match
// findClickAjaxURL looks for that same behavior's "e":"click" marker before
// giving up and treating it as belonging to some other (non-click, e.g.
// hover/powertip) behavior registered on the same element id. Chosen
// generously relative to the longest observed attrs blob live (~650 chars
// for the click behavior's own "u"+"c"+"i"+"bh"+"pre"+"e"+"pd" object).
const wicketAjaxBehaviorWindow = 800

// findClickAjaxURL finds the "u" value of the Wicket AJAX behavior
// registered on anchorID whose attrs object also carries "e":"click" - a
// single anchor id commonly has more than one behavior registered on it
// (e.g. a hover/powertip preview alongside the actual click-download
// behavior, confirmed live), so matching on id alone is not enough; this
// additionally requires "e":"click" to appear before the next "u" occurrence
// (i.e. still within the same behavior's attrs object), not just somewhere
// later in the page.
func findClickAjaxURL(html, anchorID string) string {
	re := regexp.MustCompile(`"u":"([^"]+)","c":"` + regexp.QuoteMeta(anchorID) + `"`)
	matches := re.FindAllStringSubmatchIndex(html, -1)
	for _, m := range matches {
		uVal := html[m[2]:m[3]]
		windowStart := m[1]
		windowEnd := windowStart + wicketAjaxBehaviorWindow
		if windowEnd > len(html) {
			windowEnd = len(html)
		}
		window := html[windowStart:windowEnd]
		clickIdx := strings.Index(window, `"e":"click"`)
		if clickIdx == -1 {
			continue
		}
		if nextUIdx := strings.Index(window, `"u":"`); nextUIdx != -1 && nextUIdx < clickIdx {
			// "e":"click" belongs to a later behavior's attrs object, not
			// this one.
			continue
		}
		return uVal
	}
	return ""
}

// ajaxRedirectTargetRe matches the download-triggering JS a Wicket AJAX
// "click" response embeds in its <ajax-response>: an <evaluate> block whose
// script sets window.location to the one-time downloadering URL. Confirmed
// live: this is always a plain single-quoted JS string literal with forward
// slashes escaped as \/ (standard JS string escaping within the CDATA
// block), not URL-encoded.
var ajaxRedirectTargetRe = regexp.MustCompile(`window\.location='([^']+)'`)

// extractAjaxRedirectTarget extracts the one-time downloadering URL from a
// click AJAX behavior's XML response body. Returns "" when the response
// doesn't contain one - which live testing showed happens when the same
// counter-suffixed AJAX URL is requested a second time (it responds with a
// plain <redirect> back to the section page instead), so callers must treat
// that as a miss, not retry the identical URL again.
func extractAjaxRedirectTarget(xmlBody string) string {
	m := ajaxRedirectTargetRe.FindStringSubmatch(xmlBody)
	if len(m) < 2 {
		return ""
	}
	return strings.ReplaceAll(m[1], `\/`, "/")
}

// wicketAjaxHeaders builds the request headers a real browser's Wicket AJAX
// call sends. All three were confirmed live to be necessary together, not
// decorative: omitting Wicket-Ajax-BaseURL entirely (while still sending
// Wicket-Ajax/X-Requested-With) makes the request fail with HTTP 400;
// omitting all three returns the full HTML page instead of the expected XML
// ajax-response. Wicket-Ajax-BaseURL must be the section URL's path relative
// to opalURL (matching the `Wicket.Ajax.baseUrl="..."` value the section
// page itself embeds).
func wicketAjaxHeaders(opalURL, sectionURL string) map[string]string {
	return map[string]string{
		"Wicket-Ajax":         "true",
		"Wicket-Ajax-BaseURL": strings.TrimPrefix(sectionURL, opalURL),
		"X-Requested-With":    "XMLHttpRequest",
	}
}
