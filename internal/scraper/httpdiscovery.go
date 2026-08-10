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
	attrs := map[string]string{}
	for _, m := range httpAttrRe.FindAllStringSubmatch(tag, -1) {
		attrs[strings.ToLower(m[1])] = m[2]
	}
	// rowText is the closest table-row/list-item ancestor's text. Over HTTP we
	// do not have a DOM tree to walk up, so approximate it with the anchor's
	// own inner text - enough for parseRowSizeBytes/parseRowModified to find a
	// size/date in the same cell, which is all those parsers need. The browser
	// path scopes rowText tighter (the <tr>), but a file row's size/date live
	// in sibling <td> cells outside the anchor anyway, so neither source gets
	// them from the anchor itself; both are best-effort.
	text := strings.TrimSpace(httpTagStripRe.ReplaceAllString(innerText, " "))
	text = strings.Join(strings.Fields(text), " ")
	return map[string]string{
		"href":      attrs["href"],
		"onclick":   attrs["onclick"],
		"dataHref":  attrs["data-href"],
		"dataUrl":   attrs["data-url"],
		"title":     attrs["title"],
		"text":      text,
		"rowText":   text,
		"className": attrs["class"],
	}
}

// parseHTTPSectionCandidates extracts the link candidates a section's
// server-rendered HTML exposes, in the map[string]string shape
// appendSectionFiles/appendSectionFolderTargets consume. It is the HTTP
// equivalent of files.go's extractSectionContentCandidates (browser JS),
// returning the same kind of data so the downstream predicates are shared.
func parseHTTPSectionCandidates(html string) []map[string]string {
	var out []map[string]string
	seen := map[string]bool{}
	for _, m := range httpAnchorRe.FindAllStringSubmatch(html, -1) {
		c := httpCandidate(m[1], m[2])
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
