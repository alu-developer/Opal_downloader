package scraper

import (
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/logging"
)

// This file reaches the files that live in a course node's *prose*, not in a
// file table.
//
// WHY IT EXISTS (diagnosed live 2026-11-04, the maintainer's AN30 course).
// OPAL's "Einzelne Seite" course-element type (node-sp) does not render its
// content into the section page at all. The section page carries only a
// wrapper:
//
//	<iframe width="100%" ... id="id7a4a"
//	        src="/opal/FolderResource/56300208134/infos-zum-ablauf.html?1793788446113"
//	        title="Infos zur Vorlesung" ...></iframe>
//
// and the HTML document behind that src is where the lecturer actually wrote
// the page - including, in this course, every single Übungsblatt:
//
//	<a href="MINT-2026-uebung00-aufgaben.pdf">MINT-2026-uebung00-aufgaben.pdf</a>
//	<a href="MINT-2026-uebung02-aufgaben.pdf">MINT-2026-uebung02-aufgaben.pdf</a>
//
// Discovery never fetched that document, so the section page it did fetch
// held zero file anchors and the whole course reported 0 files. AN30 has no
// file-list node anywhere - "Infos zur Vorlesung" and "Infos zur Klausur"
// are all there is - so this was not one missing file but the entire course,
// and the maintainer had been hand-downloading each sheet as it appeared.
//
// TWO THINGS ARE DIFFERENT ABOUT THESE LINKS, and both are why the existing
// parser could not simply be pointed at the inner document:
//
//   - The hrefs are RELATIVE to the embedded document's own directory
//     ("MINT-2026-uebung02-aufgaben.pdf"), not to opalURL. Resolving them the
//     normal way would produce /opal/MINT-2026-uebung02-aufgaben.pdf, which
//     404s. parseEmbeddedPageCandidates resolves against the page URL and
//     hands appendSectionFiles an already-absolute href (resolveURL leaves an
//     absolute reference alone, so the shared predicate is unaffected).
//
//   - The link TEXT is prose, not a file name. The same PDF is linked once as
//     "MINT-2026-uebung00-aufgaben.pdf" and three times as "Übungsblatt 0",
//     and deriveFileName prefers the text - so the file would have been saved
//     as "Übungsblatt 0", with no extension, depending purely on which anchor
//     the dedupe happened to see first. On a prose page the URL's own basename
//     is the only trustworthy name, so that is what this parser supplies.
//
// WHAT IT DELIBERATELY DOES NOT DO: the candidates it produces are fed to
// appendSectionFiles only, never to appendSectionFolderTargets. A prose page
// can link anywhere (this one links YouTube, datashare and cloudstore), and
// those links are not course structure to be crawled. Off-host links are
// dropped by isFileURLAllowedForCourse's existing host check, so the external
// ones cost nothing but a predicate call.

// embeddedPageIframeRe finds the <iframe> wrappers a section page uses to
// embed a content document. Attribute-order-agnostic (src is not first in
// OPAL's render), case-insensitive, and non-greedy so two iframes on one page
// stay separate. A regex rather than a DOM parser for the reason the rest of
// this package already gives: adding x/net/html would be a new dependency to
// do what httpAnchorRe/anchorTagRe already do here.
var embeddedPageIframeRe = regexp.MustCompile(`(?is)<iframe\s[^>]*>`)

// embeddedPageSrcRe pulls the src out of one matched iframe tag.
var embeddedPageSrcRe = regexp.MustCompile(`(?is)\bsrc\s*=\s*"([^"]*)"`)

// maxEmbeddedPagesPerSection bounds how many embedded documents one section
// may cost in extra fetches. Every real case measured has exactly one (a
// node-sp holds a single page), so this is purely a guard against a
// pathological page turning one section into dozens of requests - not a
// limit any genuine course is expected to reach.
const maxEmbeddedPagesPerSection = 4

// embeddedPage is one fetched content document: the URL it came from (which
// becomes the file's downloadCandidate SourceURL, and the base its relative
// links were resolved against) and the link candidates it contained.
type embeddedPage struct {
	URL        string
	Candidates []map[string]string
}

// extractEmbeddedPageURLs returns the absolute URLs of the content documents
// a section page embeds in an iframe, in document order, deduped.
//
// Two conditions, both required, keep this from turning into a general
// "fetch every iframe" crawler:
//
//   - Same host as opalURL. An embedded external page (node-tu) is not
//     OPAL-hosted content and cannot hold OPAL file links; fetching it would
//     send this account's session cookie to a third party.
//   - The path is an .html/.htm document. OPAL's own chrome embeds iframes
//     for players and widgets; a content page written by a lecturer is always
//     a stored HTML document. The query string (OPAL appends a cache-buster)
//     is ignored for this test but kept in the fetched URL.
func extractEmbeddedPageURLs(html, opalURL string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, tag := range embeddedPageIframeRe.FindAllString(html, -1) {
		m := embeddedPageSrcRe.FindStringSubmatch(tag)
		if m == nil {
			continue
		}
		src := strings.TrimSpace(unescapeHTMLText(m[1]))
		if src == "" {
			continue
		}
		absURL := resolveURL(opalURL, src)
		if !isSameHostAsOpal(absURL, opalURL) {
			continue
		}
		if !isHTMLDocumentURL(absURL) {
			continue
		}
		if _, dup := seen[absURL]; dup {
			continue
		}
		seen[absURL] = struct{}{}
		out = append(out, absURL)
		if len(out) == maxEmbeddedPagesPerSection {
			break
		}
	}
	return out
}

// isHTMLDocumentURL reports whether rawURL's path names an HTML document,
// ignoring any query string.
func isHTMLDocumentURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	ext := strings.ToLower(path.Ext(parsed.Path))
	return ext == ".html" || ext == ".htm"
}

// parseEmbeddedPageCandidates turns one embedded content document into link
// candidates in the map[string]string shape appendSectionFiles consumes.
//
// Differences from parseHTTPSectionCandidates, both covered by this file's
// top doc comment: hrefs are resolved against pageURL (so a relative prose
// link becomes the absolute URL it actually points at), and "title" carries
// the URL's own basename so deriveFileName names the file after the file
// rather than after whatever sentence it was linked from. rowText is left
// empty on purpose - a prose page has no size/date columns to parse, and
// inventing a rowText would only feed parseRowSizeBytes a number from the
// surrounding sentence.
func parseEmbeddedPageCandidates(html, pageURL string) []map[string]string {
	var out []map[string]string
	seen := map[string]struct{}{}
	for _, m := range httpAnchorRe.FindAllStringSubmatch(html, -1) {
		c := httpCandidate(m[1], m[2])
		target := extractLinkTarget(c["href"], c["onclick"], c["dataHref"], c["dataUrl"])
		if strings.TrimSpace(target) == "" {
			continue
		}
		absURL := resolveURL(pageURL, target)
		if _, dup := seen[absURL]; dup {
			continue
		}
		seen[absURL] = struct{}{}

		c["href"] = absURL
		// onclick/data-* already contributed via extractLinkTarget above and
		// would otherwise be resolved a second time, against the wrong base.
		c["onclick"] = ""
		c["dataHref"] = ""
		c["dataUrl"] = ""
		c["rowText"] = ""
		if base := basenameFromURL(absURL); base != "" {
			c["title"] = base
		}
		out = append(out, c)
	}
	return out
}

// basenameFromURL returns the last path segment of rawURL, percent-decoded,
// or "" when there is none. Percent-decoded because OPAL serves names with
// spaces and umlauts as %20/%C3%BC, and the decoded form is the name the file
// should be saved under.
func basenameFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	base := path.Base(parsed.Path)
	if base == "." || base == "/" || base == "" {
		return ""
	}
	if decoded, err := url.PathUnescape(base); err == nil {
		base = decoded
	}
	return strings.TrimSpace(base)
}

// fetchEmbeddedPages fetches every content document sectionHTML embeds and
// returns their candidates plus the number of HTTP requests issued.
//
// A fetch failure is reported through onError (may be nil) and that document
// simply contributes nothing - the section's own files, already extracted
// from the body in hand, are unaffected. Same bounded-failure shape as the
// show-all follow-up next to it in discoverSectionsHTTP.
func fetchEmbeddedPages(fetch httpFetcher, sectionHTML, opalURL string, onError func(url string, err error)) ([]embeddedPage, int) {
	urls := extractEmbeddedPageURLs(sectionHTML, opalURL)
	if len(urls) == 0 {
		return nil, 0
	}
	var pages []embeddedPage
	requests := 0
	for _, pageURL := range urls {
		body, err := httpGetText(fetch, pageURL)
		requests++
		if err != nil {
			if onError != nil {
				onError(pageURL, err)
			}
			continue
		}
		candidates := parseEmbeddedPageCandidates(body, pageURL)
		logging.Detail("Embedded page %s: %d link candidates", pageURL, len(candidates))
		pages = append(pages, embeddedPage{URL: pageURL, Candidates: candidates})
	}
	return pages, requests
}
