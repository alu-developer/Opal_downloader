package scraper

import (
	"regexp"
	"strings"
)

// This file implements the HTTP leaf-table extraction half of the serial
// hybrid discovery path (option A in docs/RESUME.md). The diagnosis behind it
// is recorded there and in docs/sync-speed-campaign.md; the short version:
//
//   - OPAL renders the course-content navigation TREE client-side (a browser
//     has to walk it), but renders leaf file tables SERVER-SIDE, so a plain
//     authenticated HTTP GET of a section URL returns the file anchors in its
//     markup. Measured 2026-07-31.
//
//     CORRECTED 2026-08-10 (docs/sync-speed-model.md Question 34): the first
//     half of that sentence is misleading and steered this file's design.
//     jstree does render client-side, but its DATA is server-delivered in the
//     very first response - every course page carries `var initial_data=[...]`
//     holding the COMPLETE course-node tree (152 nodes nested to depth 3 for
//     Softwaretechnologie, each with its absolute CourseNode href), not the
//     open-branch fragment MenuTreeRenderer.isRenderChildren() puts in the
//     rendered DOM. Checked against the crawl's own visit record: 147 of 147
//     bare CourseNode URLs recovered from one response. So no browser is
//     needed to ENUMERATE the tree; parseCourseTreeNodes below does it from
//     bytes. What the tree cannot supply is sub-paths INSIDE a node's own file
//     browser (16 of that course's 164 URLs), which still need that node's
//     response.

//   - Consequence, not yet acted on: scrapeCoursesHybrid still runs the whole
//     browser crawl before the HTTP phase, which is why mode=1 measured 254s
//     against the browser's own 207s. That ordering is a leftover of the
//     corrected belief above, not a constraint. See Question 36.
//   - The browser spends ~0.67s per section, most of it a settle wait that
//     proves the file table has finished rendering. An HTTP GET of the same
//     URL skips that wait entirely because there is no render to wait on.
//   - HTTP and the browser must NEVER run concurrently: measured to corrupt
//     the shared OPAL/Wicket session (the browser's show-all expansion fails
//     and drops most files). The hybrid is strictly serial: browser enumerates
//     section URLs first, HTTP fetches their file tables afterwards.
//
// The file is built so its correctness can be checked against captured real
// section HTML without touching the network: parseHTTPSectionCandidates and
// extractShowAllURLFromHTML are pure functions exercised by httpdiscovery_test
// against the dumps the probes already produced.

// httpAnchorRe extracts every <a ...>...</a> opening tag plus its inner text
// from a section's server-rendered HTML. It mirrors the browser-side
// extraction in files.go (extractSectionContentCandidates), which walks
// a[href], [onclick], [data-href], [data-url] elements and collects the same
// attributes - so the same appendSectionFiles predicate sees the same shape of
// candidate from either source. Regex rather than a DOM parser because the
// repo already parses OPAL HTML this way (download_refresh.go's anchorTagRe)
// and adding x/net/html would be a new dependency for no gain here.
//
// Case-insensitive and DOTALL so a multi-line anchor (OPAL wraps the file name
// in a <span> on its own line) matches as one tag. Non-greedy so two adjacent
// anchors don't collapse into one.
var httpAnchorRe = regexp.MustCompile(`(?is)<a\s([^>]*)>(.*?)</a>`)

// httpAttrRe extracts name="value" attribute pairs out of an anchor's opening
// tag, mirroring extractHTMLAttr (download_refresh.go) but capturing all of
// them at once so a candidate carries every attribute the browser extractor
// would have seen.
var httpAttrRe = regexp.MustCompile(`(?is)([a-z-]+)\s*=\s*"([^"]*)"`)

// httpTagStripRe removes inner HTML tags from an anchor's captured inner text,
// leaving the visible link text - the same job tagStripRe does in the probe.
var httpTagStripRe = regexp.MustCompile(`(?is)<[^>]*>`)

// httpCandidate represents one <a> element pulled from a section's HTTP
// response, in exactly the map[string]string shape appendSectionFiles reads
// (href/onclick/dataHref/dataUrl/text/title/rowText/className). Building the
// same shape lets the file-link decision reuse the battle-tested
// looksLikeFileLink/deriveFileName/isFileURLAllowedForCourse path rather than
// re-deriving it for HTTP.
func httpCandidate(tag, innerText string) map[string]string {
	return httpCandidateWithRowText(tag, innerText, "")
}

// httpCandidateWithRowText is httpCandidate with the enclosing table row's
// text supplied by the caller (see parseHTTPSectionCandidates). rowText falls
// back to the anchor's own text when no enclosing row was found, which is
// what this function used to do unconditionally.
//
// Why the row matters (live-measured 2026-10-15, the whole reason the
// download phase was slow): rowText is the only input
// parseRowSizeBytes/parseRowModified get, and a file row's size and "zuletzt
// geändert" date live in *sibling* <td> cells, never inside the anchor:
//
//	<td><a data-file-name="Analysis01.pdf">…</a></td>
//	<td><span>223,7KB</span></td>
//	<td><span>am 03.10.2025 um 11:07 Uhr</span></td>
//
// So approximating rowText with the anchor's inner text could only ever
// yield the bare file name, and every file discovered over HTTP came back
// with Size==nil and Modified==nil. The browser path never had this problem:
// its rowText is `el.closest('tr, li, …').textContent`, which spans those
// sibling cells. When HTTP-first discovery became the default (2026-08-11),
// that silently took change detection with it - syncer.needsContentVerification
// fires for exactly "no size and no date", so every file in every course
// turned into a verify job and every sync re-downloaded the entire library
// byte-for-byte to find out nothing had changed. Its doc comment still says
// the case covers "13 of 344 files (~4%)"; measured here it was 30 of 30.
func httpCandidateWithRowText(tag, innerText, rowText string) map[string]string {
	attrs := map[string]string{}
	for _, m := range httpAttrRe.FindAllStringSubmatch(tag, -1) {
		attrs[strings.ToLower(m[1])] = m[2]
	}
	text := strings.TrimSpace(httpTagStripRe.ReplaceAllString(innerText, " "))
	text = strings.Join(strings.Fields(text), " ")
	if rowText == "" {
		rowText = text
	}
	return map[string]string{
		"href":      attrs["href"],
		"onclick":   attrs["onclick"],
		"dataHref":  attrs["data-href"],
		"dataUrl":   attrs["data-url"],
		"title":     attrs["title"],
		"text":      text,
		"rowText":   rowText,
		"className": attrs["class"],
	}
}

// httpRowRe matches one file-table row of a section's server-rendered HTML.
// Only <tr> - not the browser extractor's wider 'tr, li, .o_file, …' set -
// because the size/date columns this exists to reach are a table's <td>
// cells, and a regex widened to generic containers would start pulling an
// unrelated neighbour's numbers into a file's row text. Non-greedy, and <tr>
// cannot nest, so each match is exactly one row.
var httpRowRe = regexp.MustCompile(`(?is)<tr[^>]*>.*?</tr>`)

// httpRowFileAnchorRe counts the file anchors in a row, so a row carrying
// more than one cannot hand the same size and date to both of them.
var httpRowFileAnchorRe = regexp.MustCompile(`(?i)data-file-name\s*=\s*"`)

// httpRowSpan is one <tr> of the section HTML: where it sits in the source,
// and its visible text with the tags stripped.
type httpRowSpan struct {
	start, end int
	text       string
}

// httpRowSpans finds the file-table rows of html that hold exactly one file
// anchor, which are the rows whose size/date columns can be attributed to a
// specific file without ambiguity. Rows with none are skipped (nothing to
// attribute), and so are rows with several: a row holding two file anchors
// would otherwise give both the same size and date, which is worse than the
// nil those files get today, because a wrong signal makes a genuinely
// changed file look unchanged forever. Falling back to nil only costs the
// re-download that already happens.
func httpRowSpans(html string) []httpRowSpan {
	var spans []httpRowSpan
	for _, loc := range httpRowRe.FindAllStringIndex(html, -1) {
		row := html[loc[0]:loc[1]]
		if len(httpRowFileAnchorRe.FindAllString(row, -1)) != 1 {
			continue
		}
		text := strings.TrimSpace(httpTagStripRe.ReplaceAllString(row, " "))
		spans = append(spans, httpRowSpan{start: loc[0], end: loc[1], text: strings.Join(strings.Fields(text), " ")})
	}
	return spans
}

// rowTextAt returns the text of the row containing offset, or "" when the
// offset falls outside every row. spans are in source order and do not
// overlap (<tr> cannot nest), so a linear scan that stops once a row starts
// past the offset is enough.
func rowTextAt(spans []httpRowSpan, offset int) string {
	for _, span := range spans {
		if span.start > offset {
			return ""
		}
		if offset < span.end {
			return span.text
		}
	}
	return ""
}

// parseHTTPSectionCandidates extracts the link candidates a section's
// server-rendered HTML exposes, in the map[string]string shape
// appendSectionFiles/appendSectionFolderTargets consume. It is the HTTP
// equivalent of files.go's extractSectionContentCandidates (browser JS),
// returning the same kind of data so the downstream predicates are shared.
func parseHTTPSectionCandidates(html string) []map[string]string {
	var out []map[string]string
	seen := map[string]bool{}
	rows := httpRowSpans(html)
	for _, m := range httpAnchorRe.FindAllStringSubmatchIndex(html, -1) {
		c := httpCandidateWithRowText(html[m[2]:m[3]], html[m[4]:m[5]], rowTextAt(rows, m[0]))
		// Dedupe by the same JSON-ish key shape the browser extractor uses:
		// the combination of attributes that identify a distinct link. A
		// section's HTML can repeat the same anchor (OPAL's breadcrumb/nav
		// leak), and feeding duplicates through appendSectionFiles is
		// harmless (fileSeen dedupes) but wasteful.
		key := c["href"] + "|" + c["onclick"] + "|" + c["dataHref"] + "|" + c["dataUrl"]
		if key == "|||" {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, c)
	}
	return out
}

// showAllLinkURLRe locates the Wicket AJAX "show all" behaviour's request URL
// in a section's HTML. OPAL wires the pagination "Alle anzeigen" toggle as
// Wicket.Ajax.ajax({"u":"<sectionURL>?<wicket-path>-pager-showAllLink", ...}).
// The "u" field is a plain GET endpoint - the browser's click just fires it.
// Fetching it over HTTP returns the full (un-paginated) file table, which is
// how the hybrid path recovers the files a section's first page caps at ~20.
// Confirmed live 2026-07-31: Part-3's showAllLink URL returned all 57
// data-file-name entries (the page advertised "57 Einträge") vs 20 on the
// first page.
//
// The URL is relative (starts with /opal/...); the caller resolves it against
// the OPAL base URL. Returns "" when the section is not paginated.
var showAllLinkURLRe = regexp.MustCompile(`"u":"([^"]*pager-showAllLink[^"]*)"`)

// extractShowAllURLFromHTML returns the relative pager-showAllLink AJAX URL
// embedded in a section's HTML, or "" if the section is not paginated. It is
// split out from the fetch path so it can be tested against captured HTML
// without a network round-trip.
func extractShowAllURLFromHTML(html string) string {
	m := showAllLinkURLRe.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}
