// Package sectionhash reduces an OPAL section's server HTML to a value that
// changes when the section's content changes and does not change otherwise.
//
// It exists because of a measurement, not a hope. docs/sync-speed-campaign.md
// rejected a change-detection cache in July on the finding that normalised
// section HTML matched across runs 0 times out of 276 - and named, but never
// carried out, the diagnostic that would explain why. Done on 2026-07-27, the
// answer was that every difference is Wicket's own bookkeeping: a per-session
// page-version counter, generated component ids, and a table-widget instance
// counter. With those normalised the match rate is 11 of 12 sections, and a
// file-bearing section is byte-identical across fetches.
//
// The safety property is the one that matters, and it is asymmetric. A hash
// that differs when nothing changed costs one section's crawl. A hash that
// matches when something DID change means a changed section is reported
// unchanged and silently stops downloading - this project's worst failure
// mode, and one it has suffered twice. So every pattern here is deliberately
// narrow: it targets widget identity, never anything that could carry a
// filename, a URL or a size.
package sectionhash

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
)

// volatile are the fragments measured to differ between two fetches of the
// same unchanged section. Each is anchored to Wicket's own naming so it cannot
// drift onto content:
//
//   - "?2284-1.0-..." and "?2288\"" - the page-version counter, which appears
//     in every Wicket.Ajax URL and in Wicket.Ajax.baseUrl.
//   - "id35a0c" - generated component ids, the same counter in hex. Five hex
//     digits minimum, so it cannot match a short human-written id.
//   - "VFSItemTable_9072" - the table-widget instance counter. Matched as
//     "<word>Table_<digits>" rather than any "_<digits>", which would be wide
//     enough to swallow content.
var volatile = []*regexp.Regexp{
	regexp.MustCompile(`\?\d+-[\d.]+-`),
	regexp.MustCompile(`\?\d+"`),
	regexp.MustCompile(`\bid[0-9a-f]{4,}\b`),
	regexp.MustCompile(`\b\w+Table_\d+`),
	// Carried over from the 2026-07-21 implementation, which established these
	// four handle within-request noise.
	regexp.MustCompile(`(?i)jsessionid=[^"'&;\s]*`),
	regexp.MustCompile(`(?i)\b\d{10,13}\b`),
	regexp.MustCompile(`(?i)(name|id|for)="[^"]*-[0-9a-f]{6,}"`),
	regexp.MustCompile(`(?i)csrf[^"']*["'][^"']+["']`),
}

// PatternsVersion identifies the pattern set above. Any change to `volatile` -
// adding, removing or widening a pattern - MUST bump this.
//
// It is not bookkeeping. A stored hash is only meaningful against the patterns
// that produced it: widen a pattern and an old hash can match HTML it should
// not, which is a false "unchanged" and a silently skipped download. A cache
// carrying this refuses entries that were not written by the same version, so
// the failure mode of editing the patterns is a full re-crawl rather than
// silent staleness.
const PatternsVersion = 1

// Normalize strips the volatile fragments, replacing each with a fixed marker.
// Exported mainly so a probe can diff normalised forms and show what is left;
// callers wanting a cache key should use Of.
func Normalize(html string) string {
	for _, re := range volatile {
		html = re.ReplaceAllString(html, "\x00")
	}
	return html
}

// Of returns the hex SHA-256 of the normalised HTML.
//
// Empty input returns the empty string rather than the hash of nothing, so a
// failed fetch can never be stored as a legitimate "this section looks like
// this" value - which would turn the next failed fetch into a false match.
func Of(html string) string {
	if html == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(Normalize(html)))
	return hex.EncodeToString(sum[:])
}
