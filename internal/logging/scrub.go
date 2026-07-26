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
	var urls []string
	withPlaceholders := urlPattern.ReplaceAllStringFunc(msg, func(u string) string {
		urls = append(urls, stripQuery(u))
		return urlPlaceholder(len(urls) - 1)
	})

	scrubbed := statuslog.SanitizeMessage(withPlaceholders)

	for i, u := range urls {
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
