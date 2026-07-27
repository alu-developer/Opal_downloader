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
func TestSectionHTMLStabilityAcrossRuns(t *testing.T) {
	if os.Getenv("OPAL_HTML_STABILITY") == "" {
		t.Skip("set OPAL_HTML_STABILITY=1 to run the live section-HTML stability probe")
	}

	sectionURL := os.Getenv("OPAL_HTML_STABILITY_URL")
	if sectionURL == "" {
		sectionURL = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50999590912/CourseNode/1757385431705760008"
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
	known := []*regexp.Regexp{
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
		regexp.MustCompile(`\bid[0-9a-f]{4,}\b`),
	}
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
