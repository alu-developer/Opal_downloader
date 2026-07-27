package scraper

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// docs/sync-speed-campaign.md's change-detection cache is the only rejected
// approach that would actually reach the ~30s target, because it skips the
// browser crawl for unchanged sections rather than making the crawl faster. It
// was rejected on a real measurement - normalised section HTML matched across
// runs 0 times out of 276 - but that entry names its own unfinished business:
//
//	"What was NOT determined, and is the cheap next step if anyone retries
//	 this: the remaining volatile fragments were never isolated. The
//	 diagnostic that would do it [...] was attempted but botched [...] Do
//	 that diff properly before concluding the design is impossible rather
//	 than merely unproven."
//
// So this is not re-litigating a rejected approach. The rejection stands; what
// has never been established is *what* varies, and therefore whether it is
// normalisable. This probe answers only that.
//
// Plain HTTP on purpose: that is what the cache would use, and it removes the
// browser from the argument entirely - the botched attempt failed precisely by
// comparing two standalone fetches when it meant to compare two in-batch ones.
//
//	OPAL_HTML_STABILITY=1 go test ./internal/scraper/ -run TestSectionHTMLStabilityAcrossRuns -v -timeout 10m

// wicketVolatilePatterns are the fragments measured to vary between two
// fetches of the same unchanged section. Shared by both probes here.
var wicketVolatilePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)jsessionid=[^"'&;\s]*`),
	regexp.MustCompile(`(?i)\b\d{10,13}\b`),                      // epoch-ish stamps
	regexp.MustCompile(`(?i)(name|id|for)="[^"]*-[0-9a-f]{6,}"`), // generated ids
	regexp.MustCompile(`(?i)csrf[^"']*["'][^"']+["']`),
	// Found by this probe, 2026-07-27, and never isolated before: Wicket's
	// per-session page-version counter ("?2284-1.0-...") and its generated
	// component ids ("id3591a"). Both increment on every render and appear
	// only in the header navigation's Ajax glue - tabs, profile, logout,
	// search - never in the file table. They are bookkeeping, not content.
	regexp.MustCompile(`\?\d+-[\d.]+-`),
	regexp.MustCompile(`\?\d+"`),
	// Wicket's table-widget instance counter ("VFSItemTable_9072" ->
	// "VFSItemTable_9079"), found 2026-07-27 as the only residue left on a
	// file-bearing section. Narrow on purpose - matching "<word>Table_<digits>"
	// rather than any "_<digits>" keeps it to widget identity and away from
	// anything that could carry content.
	regexp.MustCompile(`\b\w+Table_\d+`),
	regexp.MustCompile(`\bid[0-9a-f]{4,}\b`),
}

func TestSectionHTMLStabilityAcrossRuns(t *testing.T) {
	if os.Getenv("OPAL_HTML_STABILITY") == "" {
		t.Skip("set OPAL_HTML_STABILITY=1 to run the live section-HTML stability probe")
	}

	// A node that actually HOLDS FILES. This matters more than it looks: an
	// enrolment node's HTML contains no file references at all, and probing one
	// nearly produced the conclusion that section HTML cannot reflect file
	// changes - when in truth that node simply has no files. A file-bearing
	// section does carry its filenames in the server HTML.
	sectionURL := os.Getenv("OPAL_HTML_STABILITY_URL")
	if sectionURL == "" {
		sectionURL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50999590912/CourseNode/1757212677096374003"
	}

	client, err := newSessionClient()
	if err != nil {
		t.Skipf("no usable saved session (%v) - run `login` first", err)
	}

	first, err := fetchBody(client, sectionURL)
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	// Long enough that anything time-derived has moved on, short enough that
	// the session cannot plausibly have rolled over.
	gap := 45 * time.Second
	t.Logf("fetched %d bytes; waiting %s before the second fetch", len(first), gap)
	time.Sleep(gap)

	second, err := fetchBody(client, sectionURL)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}

	if first == second {
		t.Logf("RAW HTML IDENTICAL across %s - nothing to normalise for this section", gap)
		return
	}

	// The four patterns the rejected implementation already normalised, per the
	// campaign entry. Applying them here is what makes "what is LEFT" the
	// question, rather than rediscovering the known ones.
	known := wicketVolatilePatterns
	normalise := func(s string) string {
		for _, re := range known {
			s = re.ReplaceAllString(s, "X")
		}
		return s
	}

	na, nb := normalise(first), normalise(second)
	if na == nb {
		t.Logf("NORMALISED FORMS MATCH - the patterns cover this section entirely")
		return
	}

	// What is actually left. Reported as differing lines rather than a byte
	// offset, because the question is which *fragments* vary - a byte offset
	// says a difference exists, which is already known.
	la, lb := strings.Split(na, "\n"), strings.Split(nb, "\n")
	diffs := 0
	var report strings.Builder
	for i := 0; i < len(la) && i < len(lb); i++ {
		if la[i] == lb[i] {
			continue
		}
		diffs++
		if diffs <= 15 {
			fmt.Fprintf(&report, "\nline %d:\n  A: %s\n  B: %s", i+1, trim(la[i]), trim(lb[i]))
		}
	}
	t.Logf("lines: %d vs %d; differing lines after known normalisation: %d%s", len(la), len(lb), diffs, report.String())

	dir := "tmp"
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "htmlstability-a.html"), []byte(na), 0o600)
	_ = os.WriteFile(filepath.Join(dir, "htmlstability-b.html"), []byte(nb), 0o600)
	t.Logf("normalised forms written to %s/htmlstability-{a,b}.html", dir)
}

func trim(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// newSessionClient builds an http.Client carrying the cookies Playwright saved,
// so the probe sees what an authenticated request sees without opening a
// browser.
func newSessionClient() (*http.Client, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(home, ".opal_storage_state.json"))
	if err != nil {
		return nil, err
	}
	var state struct {
		Cookies []struct {
			Name   string `json:"name"`
			Value  string `json:"value"`
			Domain string `json:"domain"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nil, err
	}

	pairs := make([]string, 0, len(state.Cookies))
	for _, c := range state.Cookies {
		if !strings.Contains(c.Domain, "bildungsportal") {
			continue
		}
		pairs = append(pairs, c.Name+"="+c.Value)
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("no bildungsportal cookies in the saved session")
	}
	header := strings.Join(pairs, "; ")

	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: &cookieTransport{header: header, base: http.DefaultTransport},
	}, nil
}

type cookieTransport struct {
	header string
	base   http.RoundTripper
}

func (t *cookieTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Cookie", t.header)
	r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) opal-downloader-probe")
	return t.base.RoundTrip(r)
}

func fetchBody(c *http.Client, url string) (string, error) {
	resp, err := c.Get(url)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// TestSectionHTMLStabilityAcrossManySections generalises the single-section
// result, and answers the stronger question at the same time.
//
// The single-section probe showed the only cross-run variation is Wicket's
// per-session bookkeeping - a page-version counter and generated component ids
// - living entirely in the page chrome. If that is right, then the CONTENT
// region should be byte-identical across fetches with **no normalisation at
// all**, which would make the design independent of Wicket's patterns and of
// the session those counters belong to. That is the claim worth testing,
// because chasing normalisation patterns is exactly how the previous attempt
// convinced itself of something that was not true.
//
// Sections are discovered over plain HTTP from a course page rather than
// hardcoded, so this measures whatever the account actually has.
//
//	OPAL_HTML_STABILITY=1 go test ./internal/scraper/ -run TestSectionHTMLStabilityAcrossManySections -v -timeout 20m
func TestSectionHTMLStabilityAcrossManySections(t *testing.T) {
	if os.Getenv("OPAL_HTML_STABILITY") == "" {
		t.Skip("set OPAL_HTML_STABILITY=1 to run the live multi-section stability probe")
	}

	client, err := newSessionClient()
	if err != nil {
		t.Skipf("no usable saved session (%v) - run `login` first", err)
	}

	courseURL := os.Getenv("OPAL_HTML_STABILITY_COURSE")
	if courseURL == "" {
		courseURL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50999590912"
	}
	courseHTML, err := fetchBody(client, courseURL)
	if err != nil {
		t.Fatalf("fetch course page: %v", err)
	}

	// Bounded deliberately: docs/server-load.md is a standing constraint, and a
	// diagnostic has no business issuing a crawl's worth of requests.
	nodeRe := regexp.MustCompile(`/opal/auth/RepositoryEntry/\d+/CourseNode/\d+`)
	seen := map[string]bool{}
	var urls []string
	for _, m := range nodeRe.FindAllString(courseHTML, -1) {
		if seen[m] {
			continue
		}
		seen[m] = true
		urls = append(urls, "https://bildungsportal.sachsen.de"+m)
		if len(urls) >= 12 {
			break
		}
	}
	if len(urls) == 0 {
		t.Skip("no CourseNode links found on the course page - session may have expired")
	}
	t.Logf("discovered %d sections", len(urls))

	first := make(map[string]string, len(urls))
	for _, u := range urls {
		body, err := fetchBody(client, u)
		if err != nil {
			t.Fatalf("first pass %s: %v", u, err)
		}
		first[u] = body
	}

	t.Log("first pass done; waiting 60s")
	time.Sleep(60 * time.Second)

	rawMatches, contentMatches, contentFound, contentNormMatches, rawNormMatches := 0, 0, 0, 0, 0
	for _, u := range urls {
		second, err := fetchBody(client, u)
		if err != nil {
			t.Fatalf("second pass %s: %v", u, err)
		}
		if first[u] == second {
			rawMatches++
		}
		if normaliseWicket(first[u]) == normaliseWicket(second) {
			rawNormMatches++
		}
		a, okA := contentRegion(first[u])
		b, okB := contentRegion(second)
		if !okA || !okB {
			continue
		}
		contentFound++
		if a == b {
			contentMatches++
		}
		if normaliseWicket(a) == normaliseWicket(b) {
			contentNormMatches++
		}
	}

	t.Logf("RESULT over %d sections: whole page raw %d, whole page normalised %d, content region raw %d (located %d), content region normalised %d",
		len(urls), rawMatches, rawNormMatches, contentMatches, contentFound, contentNormMatches)
	if contentFound > 0 && contentNormMatches == contentFound {
		t.Logf("EVERY content region matched after normalisation - the change-detection cache is viable")
	}
}

// contentRegion slices out the main-content element the extractor already
// targets (see extractSectionContentCandidates' rootSelectors).
//
// Crude on purpose, and worth saying so: it is a substring between the opening
// tag and the footer, not a parsed subtree. That is fine for a stability
// comparison - both sides are sliced the same way, so a false slice cannot
// manufacture a match - but it is NOT good enough to hash for a real cache.
func contentRegion(html string) (string, bool) {
	start := strings.Index(html, `id="main-content"`)
	if start < 0 {
		start = strings.Index(html, `id="content"`)
	}
	if start < 0 {
		return "", false
	}
	end := strings.Index(html[start:], "<footer")
	if end < 0 {
		return html[start:], true
	}
	return html[start : start+end], true
}

func normaliseWicket(s string) string {
	for _, re := range wicketVolatilePatterns {
		s = re.ReplaceAllString(s, "X")
	}
	return s
}

// TestNormalisationDoesNotHideRealChanges is the safety half, and it matters
// far more than the hit rate.
//
// A cache MISS only costs a crawl of that section - the safe direction. A
// cache HIT that is wrong means a changed section is reported unchanged and
// silently stops downloading, which is this project's worst failure mode and
// the one it has actually suffered twice.
//
// A live test would need a file added to a real course, which is not something
// to do to the maintainer's account. So this works the other way round: take a
// real section's HTML and apply the kinds of change a real edit produces, then
// require the normalisation to still see each one. If a mutation survives
// normalisation undetected, the pattern set is too aggressive - which is
// exactly how a hash-based cache goes silently wrong.
//
//	OPAL_HTML_STABILITY=1 go test ./internal/scraper/ -run TestNormalisationDoesNotHideRealChanges -v
func TestNormalisationDoesNotHideRealChanges(t *testing.T) {
	if os.Getenv("OPAL_HTML_STABILITY") == "" {
		t.Skip("set OPAL_HTML_STABILITY=1 to run against a real section")
	}
	client, err := newSessionClient()
	if err != nil {
		t.Skipf("no usable saved session (%v)", err)
	}
	sectionURL := os.Getenv("OPAL_HTML_STABILITY_URL")
	if sectionURL == "" {
		sectionURL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50999590912/CourseNode/1757212677096374003"
	}
	html, err := fetchBody(client, sectionURL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	base := normaliseWicket(html)

	// Each mutation stands for a real edit a lecturer makes. Applied to the
	// live page rather than to a fixture, so the patterns are tested against
	// the markup they will actually meet.
	mutations := []struct {
		name  string
		apply func(string) string
	}{
		{"a file is renamed", func(s string) string {
			return strings.Replace(s, ".pdf", ".RENAMED.pdf", 1)
		}},
		{"a new file row appears", func(s string) string {
			return strings.Replace(s, "</table>", `<tr><td><a href="/opal/FolderResource/new.pdf">new.pdf</a></td></tr></table>`, 1)
		}},
		{"a file's href changes", func(s string) string {
			return strings.Replace(s, "/opal/FolderResource/", "/opal/FolderResource/v2/", 1)
		}},
		{"a single character changes anywhere in the body", func(s string) string {
			i := strings.Index(s, "<body")
			if i < 0 {
				return s + "x"
			}
			return s[:i+400] + "Z" + s[i+400:]
		}},
	}

	for _, m := range mutations {
		t.Run(m.name, func(t *testing.T) {
			mutated := m.apply(html)
			if mutated == html {
				t.Skipf("mutation did not apply to this page - nothing to conclude")
			}
			if normaliseWicket(mutated) == base {
				t.Fatalf("NORMALISATION HID A REAL CHANGE (%s) - a cache built on this would silently stop downloading", m.name)
			}
		})
	}
}
