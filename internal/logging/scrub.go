package logging

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/alu-developer/opal-downloader/internal/statuslog"
)

// urlPattern finds http(s) URLs so they can be held out of the general
// credential scrub and put back afterwards.
var urlPattern = regexp.MustCompile(`https?://[^\s"'<>)\]]+`)

// pathMarkerStart and pathMarkerEnd bracket a value wrapped by ProtectPath.
// Control characters, so they cannot occur in a real manifest path or file
// name and cannot be confused with the message's own text.
const (
	pathMarkerStart = "\x01"
	pathMarkerEnd   = "\x02"
)

// protectedPattern finds values a call site has already wrapped with
// ProtectPath.
var protectedPattern = regexp.MustCompile(pathMarkerStart + `(.*?)` + pathMarkerEnd)

// ProtectPath marks s so scrubForFile's token-shaped redaction skips over
// it, the same way it already skips URLs.
//
// tokenLikePattern (see statuslog.SanitizeMessage) redacts any run of 32+
// characters from the base64/hex-plus-path alphabet - right for a session
// cookie, wrong for a manifest key like
// "Softwaretechnologie (SoSe 26)/Part-3/37-st-analysis-eu-rent-example_slides.pdf",
// whose no-spaces tail is exactly that shape and was silently eaten from the
// one log line meant to say which download failed (friction campaign walk
// 13). Call this only with a value the call site fully controls the origin
// of - a manifest key, a course or file name built from data this project
// already scraped - never with anything that could carry raw session state,
// since wrapping it here means the scrub never looks at it again.
func ProtectPath(s string) string {
	return pathMarkerStart + s + pathMarkerEnd
}

// StripProtectionMarkers removes ProtectPath's markers, leaving the wrapped
// value visible as plain text. For any output path that never runs
// scrubForFile's credential scrub in the first place (the console handler,
// see consoleHandler.Handle) - there is nothing for the markers to protect
// the value from there, so left alone they print as raw, unpaired control
// bytes instead of disappearing invisibly the way they do on the scrubbed
// file path.
func StripProtectionMarkers(s string) string {
	return protectedPattern.ReplaceAllString(s, "$1")
}

// scrubForFile sanitizes a message for the diagnostic log while keeping the
// one field that makes the log worth having.
//
// statuslog.SanitizeMessage redacts any run of 32+ characters from the
// base64/hex alphabet, which is the right rule for a status message and the
// wrong one for a URL: "/" is in that alphabet, so an OPAL section link
// collapses to "https://bildungsportal.sachsen.[redacted]". That was
// discovered by reading the first real log this package produced - every
// diagnostic line had had its URL destroyed, and the URL is exactly what
// "which section lost the files" needs (see scripts/compare-visit-runs.ps1,
// which diagnoses precisely that by comparing section URLs between runs).
//
// So URLs are lifted out, the rest of the message is scrubbed as before, and
// the URLs go back with their query and fragment removed. That last part is
// not cosmetic: a path identifies a course node, while a query string is
// where a jsessionid or a one-time token would appear. Dropping it keeps the
// diagnostic value and gives up nothing worth keeping.
//
// The whole-message checks in SanitizeMessage still apply to what is left, so
// a message that looks like raw session state is still discarded outright.
func scrubForFile(msg string) string {
	var lifted []string
	withPlaceholders := urlPattern.ReplaceAllStringFunc(msg, func(u string) string {
		lifted = append(lifted, stripQuery(u))
		return urlPlaceholder(len(lifted) - 1)
	})
	withPlaceholders = protectedPattern.ReplaceAllStringFunc(withPlaceholders, func(m string) string {
		inner := m[len(pathMarkerStart) : len(m)-len(pathMarkerEnd)]
		lifted = append(lifted, inner)
		return urlPlaceholder(len(lifted) - 1)
	})

	scrubbed := statuslog.SanitizeMessage(withPlaceholders)

	for i, u := range lifted {
		scrubbed = strings.Replace(scrubbed, urlPlaceholder(i), u, 1)
	}
	// SanitizeMessage truncates at 500 characters, which can cut a
	// placeholder in half and leave it with nothing to restore. Strip the
	// markers rather than let a control character reach the log.
	return strings.ReplaceAll(scrubbed, "\x00", "")
}

// urlPlaceholder stands in for a URL during the scrub. Wrapped in NULs
// because they cannot occur in a real message, and kept short so the
// placeholder is never itself long enough to look like a token and get
// redacted while it is holding the URL's place.
func urlPlaceholder(i int) string {
	return "\x00url" + strconv.Itoa(i) + "\x00"
}

func stripQuery(u string) string {
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		return u[:i] + "?..."
	}
	return u
}
