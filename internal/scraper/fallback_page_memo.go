package scraper

import "strings"

// The browser-fallback download path is serialized per FILE, and every file
// re-navigates to its section and, for a click-expanded ("Alle anzeigen")
// section, re-clicks the expansion before searching for its link.
//
// Measured on a full account 2026-07-21: 349 clicks to recover 48 files spread
// over only ~5 distinct sections. The expansion work was being repeated an
// order of magnitude more often than necessary.
//
// The page does not move between those files: a download click fires a
// download, it does not navigate. So when the next file wants the same page
// that is already on screen, the navigation, the settle wait and the
// re-expansion can all be skipped.
//
// This memo records what the shared page currently shows. It is only ever
// touched while browserDownloadMu is held (see DownloadFile), so it needs no
// locking of its own.

// fallbackPageMemo remembers the state of the shared browser page between
// consecutive browser-fallback downloads.
type fallbackPageMemo struct {
	// RequestedURL is the pageURL a caller asked for.
	RequestedURL string

	// LandedURL is page.URL() once that navigation, and any show-all
	// expansion, had finished. It is compared against the page's *current*
	// URL to detect that something else moved the page in the meantime -
	// without that check, a stale memo would skip a navigation the page
	// actually needs.
	LandedURL string

	// Expanded records that a show-all expansion was already performed on
	// this page, so it does not have to be repeated for the next file from
	// the same section.
	Expanded bool
}

// canReuse reports whether the page is already showing what a request for
// requestedURL would produce, given where the page currently is.
//
// Deliberately strict: both the request and the page's current location must
// match what was recorded. Reusing a page that is not in the expected state
// would search for a file's link on the wrong listing and report it missing -
// turning a slow-but-correct path into a fast-but-wrong one, which is the
// trade this project rejects everywhere else.
func (m *fallbackPageMemo) canReuse(requestedURL, currentPageURL string) bool {
	if m == nil {
		return false
	}
	if strings.TrimSpace(requestedURL) == "" || strings.TrimSpace(m.RequestedURL) == "" {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(requestedURL), strings.TrimSpace(m.RequestedURL)) {
		return false
	}
	if strings.TrimSpace(m.LandedURL) == "" || strings.TrimSpace(currentPageURL) == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(m.LandedURL), strings.TrimSpace(currentPageURL))
}

// record stores the page state after a navigation (and optional expansion).
func (m *fallbackPageMemo) record(requestedURL, landedURL string, expanded bool) {
	if m == nil {
		return
	}
	m.RequestedURL = requestedURL
	m.LandedURL = landedURL
	m.Expanded = expanded
}

// markExpanded notes that the page currently on screen has now been expanded,
// keeping the recorded landed URL in step (a Wicket expansion click changes
// the page-instance counter in the URL).
func (m *fallbackPageMemo) markExpanded(landedURL string) {
	if m == nil {
		return
	}
	m.LandedURL = landedURL
	m.Expanded = true
}

// invalidate forgets the page state. Called whenever anything might have moved
// the page out from under the memo - the safe direction is always to navigate
// again.
func (m *fallbackPageMemo) invalidate() {
	if m == nil {
		return
	}
	m.RequestedURL = ""
	m.LandedURL = ""
	m.Expanded = false
}
