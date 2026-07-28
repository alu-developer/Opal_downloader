package scraper

// Wires internal/sectionhash and internal/sectioncache into the crawl: an
// HTTP-only probe of a section's page, taken before the browser ever visits
// it, that decides whether the visit can be skipped.
//
// OFF BY DEFAULT (sectionCacheEnv) per docs/sync-speed-campaign.md's own
// rule for this campaign - built and measured before being trusted with a
// default. The ceiling was measured 2026-07-27 at ~93s against a 210.3s
// baseline (2.3x), not the ~30s target; see docs/BACKLOG.md's sync-speed
// entry for the full ledger.
//
// Every failure here (fetch error, empty body, empty hash, no cache
// installed) degrades to "crawl it" - see checkSectionCache. A miss costs one
// section's crawl; a false hit means a changed section is reported unchanged
// and silently stops downloading, which is this project's worst failure
// mode, so nothing here ever turns an error into a hit.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alu-developer/opal-downloader/internal/sectionhash"
)

// SectionCacheEnv gates the whole feature. Set it to try the cache; leave it
// unset and a scrape behaves exactly as it did before this file existed, even
// if a caller passed SetSectionCache a real *sectioncache.Cache. Exported so
// internal/syncer can check the same flag before paying for
// sectioncache.Load/Save at all - see its doc comment there for why an
// unconditional Load/Save would be its own hazard.
const SectionCacheEnv = "OPAL_SECTION_CACHE"

// sectionCacheEnabled reports whether both halves of the opt-in are present:
// the env flag, and a caller that actually installed a cache via
// SetSectionCache. Either alone does nothing.
func (s *OpalScraper) sectionCacheEnabled() bool {
	return s != nil && s.sectionCache != nil && os.Getenv(SectionCacheEnv) != ""
}

// sectionCacheHTTPClient lazily builds (and reuses) an http.Client carrying
// the cookies saved at s.stateFile, so a probe fetch is authenticated without
// opening a browser tab. Mirrors htmlstability_probe_test.go's
// newSessionClient, generalized to read s.stateFile (whatever the caller
// configured, not a hardcoded path) and to scope cookies to s.opalURL's own
// host rather than a hardcoded "bildungsportal" substring, so a differently
// hosted Bildungsportal Sachsen deployment is not silently excluded.
func (s *OpalScraper) sectionCacheHTTPClient() (*http.Client, error) {
	s.sectionCacheHTTPMu.Lock()
	defer s.sectionCacheHTTPMu.Unlock()
	if s.sectionCacheHTTP != nil {
		return s.sectionCacheHTTP, nil
	}

	header, err := cookieHeaderForOpal(s.stateFile, s.opalURL)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &sectionCacheCookieTransport{header: header, base: http.DefaultTransport},
	}
	s.sectionCacheHTTP = client
	return client, nil
}

type sectionCacheCookieTransport struct {
	header string
	base   http.RoundTripper
}

func (t *sectionCacheCookieTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r.Header.Set("Cookie", t.header)
	r.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) opal-downloader")
	return t.base.RoundTrip(r)
}

// cookieHeaderForOpal reads Playwright's storage-state JSON at stateFile and
// returns a "name=value; ..." Cookie header carrying only the cookies scoped
// to opalURL's own host - a same-host or parent-domain cookie, the same rule
// a browser applies, rather than a hardcoded institution name.
func cookieHeaderForOpal(stateFile, opalURL string) (string, error) {
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		return "", err
	}
	var state struct {
		Cookies []struct {
			Name   string `json:"name"`
			Value  string `json:"value"`
			Domain string `json:"domain"`
		} `json:"cookies"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return "", err
	}

	host := hostOf(opalURL)
	pairs := make([]string, 0, len(state.Cookies))
	for _, c := range state.Cookies {
		domain := strings.TrimPrefix(c.Domain, ".")
		if domain == "" || host == "" {
			continue
		}
		if host != domain && !strings.HasSuffix(host, "."+domain) {
			continue
		}
		pairs = append(pairs, c.Name+"="+c.Value)
	}
	if len(pairs) == 0 {
		return "", fmt.Errorf("no cookies for %q in the saved session", host)
	}
	return strings.Join(pairs, "; "), nil
}

// sectionCacheProbe is the result of checking one section's cache state
// before deciding whether to crawl it.
type sectionCacheProbe struct {
	// hash is the section's freshly-fetched hash, or "" if it could not be
	// established (fetch failed, empty body, feature off). An empty hash can
	// never be recorded (sectioncache.Cache.Record no-ops on it), so a probe
	// that fails always degrades to a normal crawl with nothing cached
	// afterward for that section.
	hash string
	// hit is true only when hash matched what the cache recorded last time,
	// in which case visit is a ready-to-use replay of that recording.
	hit   bool
	visit sectionVisit
}

// checkSectionCache fetches sectionURL over plain HTTP (through the same
// politeness limiter gotoPolitely uses), hashes it, and compares that hash
// against the cache. Every branch that cannot positively confirm "unchanged"
// falls through to hit=false, which tells the caller to crawl the section
// with a browser exactly as it would without this feature.
func (s *OpalScraper) checkSectionCache(sectionURL string) sectionCacheProbe {
	if !s.sectionCacheEnabled() {
		return sectionCacheProbe{}
	}

	html, err := s.fetchSectionHTML(sectionURL)
	if err != nil {
		return sectionCacheProbe{}
	}
	hash := sectionhash.Of(html)
	if hash == "" {
		return sectionCacheProbe{}
	}
	if !s.sectionCache.Unchanged(sectionURL, hash) {
		return sectionCacheProbe{hash: hash}
	}

	var packed cachedSection
	if err := json.Unmarshal(s.sectionCache.Files(sectionURL), &packed); err != nil {
		// A hash match with unreadable rows is not trustworthy enough to
		// skip a crawl over - fall through to a normal visit, still carrying
		// the fresh hash so the crawl's result gets recorded correctly.
		return sectionCacheProbe{hash: hash}
	}
	candidates, showAllURL, expandedPageURL, expandedShowAll := unpackSection(packed)
	return sectionCacheProbe{
		hash: hash,
		hit:  true,
		visit: sectionVisit{
			url:             sectionURL,
			candidates:      candidates,
			showAllURL:      showAllURL,
			expandedPageURL: expandedPageURL,
			expandedShowAll: expandedShowAll,
		},
	}
}

// recordSectionCache stores what this visit produced, for the next sync.
// Safe to call unconditionally: sectioncache.Cache.Record no-ops when hash is
// empty (a failed probe) or the cache itself is nil, and this is a no-op
// entirely when the feature is off.
func (s *OpalScraper) recordSectionCache(sectionURL, hash string, candidates []map[string]string, showAllURL, expandedPageURL string, expandedShowAll bool) {
	if !s.sectionCacheEnabled() {
		return
	}
	s.sectionCache.Record(sectionURL, hash, packSection(candidates, showAllURL, expandedPageURL, expandedShowAll))
}

// fetchSectionHTML is fetchSectionHTMLPolitely in production, or
// s.sectionCacheFetch when a test has set that seam - see its doc comment.
func (s *OpalScraper) fetchSectionHTML(sectionURL string) (string, error) {
	if s.sectionCacheFetch != nil {
		return s.sectionCacheFetch(sectionURL)
	}
	return s.fetchSectionHTMLPolitely(sectionURL)
}

// fetchSectionHTMLPolitely fetches url's raw body, paced through the same
// polite.Limiter every browser navigation goes through (gotoPolitely,
// scraper.go) - docs/server-load.md's rate ceiling covers this tool's total
// request rate, not just its browser traffic, and a probe fetch is a real
// request to OPAL.
func (s *OpalScraper) fetchSectionHTMLPolitely(sectionURL string) (string, error) {
	client, err := s.sectionCacheHTTPClient()
	if err != nil {
		return "", err
	}

	if s.limiter != nil {
		if err := s.limiter.Wait(context.Background()); err != nil {
			return "", err
		}
	}

	resp, err := client.Get(sectionURL)
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	if s.limiter != nil {
		s.limiter.Observe(status)
	}
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d fetching %s", resp.StatusCode, sectionURL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// hostOf returns rawURL's hostname (no port, no scheme), or "" if it cannot
// be parsed - callers treat that as "match nothing" rather than panicking or
// falling back to a wildcard.
func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
