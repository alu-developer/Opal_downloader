package logging

import (
	"strings"
	"testing"
)

// The failure this exists to prevent was found by reading the first real log
// the package produced: every diagnostic line had had its URL replaced with
// "[redacted]", because "/" is in the base64 alphabet that the shared
// credential scrub treats as token-shaped. A section URL is the one field
// that makes a crawl diagnostic actionable.
func TestSectionURLsSurviveTheScrub(t *testing.T) {
	const msg = `skipping section "Woche 07" (https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/43310120963/CourseNode/1678901234567890): navigation failed after retry`

	got := scrubForFile(msg)

	if strings.Contains(got, "[redacted]") {
		t.Fatalf("the URL was redacted, which is what makes this log useless for diagnosis:\n%s", got)
	}
	for _, want := range []string{"RepositoryEntry/43310120963", "CourseNode/1678901234567890", "Woche 07"} {
		if !strings.Contains(got, want) {
			t.Errorf("scrubbed message lost %q:\n%s", want, got)
		}
	}
}

// A path identifies a course node; a query string is where a jsessionid or a
// one-time token would appear. Keeping the first and dropping the second is
// the whole trade.
func TestQueryStringsAreDroppedFromURLs(t *testing.T) {
	const msg = `visited https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/12345?jsessionid=A1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6 now`

	got := scrubForFile(msg)

	if strings.Contains(got, "A1B2C3D4E5F6G7H8I9J0K1L2M3N4O5P6") {
		t.Fatalf("a token in a query string survived the scrub:\n%s", got)
	}
	if !strings.Contains(got, "RepositoryEntry/12345") {
		t.Fatalf("dropping the query string took the useful path with it:\n%s", got)
	}
}

// Holding URLs out of the scrub must not create a hole for everything else.
func TestNonURLCredentialsAreStillRedacted(t *testing.T) {
	const token = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEF"
	msg := "request to https://bildungsportal.sachsen.de/opal/auth failed, Cookie: shibsession=" + token

	got := scrubForFile(msg)

	if strings.Contains(got, token) {
		t.Fatalf("a credential outside a URL survived the scrub:\n%s", got)
	}
}

// Several URLs in one message must each come back in their own place, not
// swapped or duplicated.
func TestMultipleURLsAreRestoredIndependently(t *testing.T) {
	msg := "moved from https://example.invalid/opal/first/aaaa to https://example.invalid/opal/second/bbbb"

	got := scrubForFile(msg)

	first := strings.Index(got, "/opal/first/aaaa")
	second := strings.Index(got, "/opal/second/bbbb")
	if first < 0 || second < 0 {
		t.Fatalf("a URL went missing:\n%s", got)
	}
	if first > second {
		t.Fatalf("URLs came back in the wrong order:\n%s", got)
	}
}

// The placeholder is a control character, so a truncated message must never
// leak one into the log.
func TestNoPlaceholderMarkersReachTheLog(t *testing.T) {
	msg := strings.Repeat("padding text ", 60) + "https://example.invalid/opal/trailing"

	got := scrubForFile(msg)

	if strings.ContainsRune(got, '\x00') {
		t.Fatalf("a placeholder marker survived into the output: %q", got)
	}
}

// Friction campaign walk 13: a manifest key with a no-spaces tail (course
// name is fine, the path segment after it is 32+ chars of
// [A-Za-z0-9+/_-]) reads as token-shaped to the general scrub and lost the
// filename that identifies which download failed. ProtectPath is the fix a
// call site uses to hold a known-safe path out of the scrub, the same way
// scrubForFile already holds URLs out.
func TestProtectedPathSurvivesTheScrub(t *testing.T) {
	const path = "Softwaretechnologie (SoSe 26)/Part-3/37-st-analysis-eu-rent-example_slides.pdf"
	msg := "download error detail for " + ProtectPath(path) + ": response is HTML (technical detail: browser fallback click did not find a downloadable link)"

	got := scrubForFile(msg)

	if strings.Contains(got, "[redacted]") {
		t.Fatalf("the manifest path was redacted, which is what makes this log line useless for diagnosis:\n%s", got)
	}
	if !strings.Contains(got, path) {
		t.Fatalf("scrubbed message lost the manifest path %q:\n%s", path, got)
	}
	if strings.ContainsAny(got, "\x01\x02") {
		t.Fatalf("a path marker survived into the output: %q", got)
	}
}

// Holding a protected path out of the scrub must not create a hole for a
// real credential sitting right next to it in the same message.
func TestNonPathCredentialsAreStillRedactedAlongsideAProtectedPath(t *testing.T) {
	const token = "abcdefghijklmnopqrstuvwxyz0123456789ABCDEF"
	msg := "download error detail for " + ProtectPath("Course/file.pdf") + ": failed, Cookie: shibsession=" + token

	got := scrubForFile(msg)

	if strings.Contains(got, token) {
		t.Fatalf("a credential next to a protected path survived the scrub:\n%s", got)
	}
	if !strings.Contains(got, "Course/file.pdf") {
		t.Fatalf("the protected path was lost:\n%s", got)
	}
}
