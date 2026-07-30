package scraper

// Feasibility probe for the sync-speed campaign (docs/sync-speed-campaign.md):
// can section discovery be done with plain authenticated HTTP instead of
// driving a browser page per section?
//
// Rationale: a no-op sync costs ~4m25s, essentially all of it ~1s of
// deliberate waiting on each of 284 section pages. Playwright's
// APIRequestContext is stateless, shares the session cookies, and opens no
// browser tab - download_refresh.go already fetches section HTML that way
// successfully. If the file anchors are present in that HTML, discovery
// becomes N cheap HTTP GETs at high concurrency.
//
// This probe does NOT change behaviour. It fetches the known section URLs and
// reports how many file anchors plain HTTP sees, so the idea can be judged on
// evidence before anything is built on it.
//
// Usage: OPAL_HTTPPROBE=<section-url-file> go test ./internal/scraper/ -run TestHTTPDiscoveryProbe -v -timeout 20m

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

var dataFileNameRe = regexp.MustCompile(`data-file-name="([^"]*)"`)

// anchorRe pulls every <a ...>...</a> out of the server-rendered HTML. The
// browser extractor (files.go) works on a[href], [onclick], [data-href],
// [data-url] and then decides with looksLikeFileLink; this probe reuses that
// same predicate so the comparison is against the real rule, not a guess.
var anchorRe = regexp.MustCompile(`(?is)<a\s([^>]*)>(.*?)</a>`)
var attrRe = regexp.MustCompile(`(?is)([a-z-]+)\s*=\s*"([^"]*)"`)
var tagStripRe = regexp.MustCompile(`(?is)<[^>]*>`)

// fileLinksInHTML returns the file names the crawl's own predicate would
// accept from this section HTML.
func fileLinksInHTML(html string) []string {
	var out []string
	for _, m := range anchorRe.FindAllStringSubmatch(html, -1) {
		attrs := map[string]string{}
		for _, a := range attrRe.FindAllStringSubmatch(m[1], -1) {
			attrs[strings.ToLower(a[1])] = a[2]
		}
		target := extractLinkTarget(attrs["href"], attrs["onclick"], attrs["data-href"], attrs["data-url"])
		name := strings.TrimSpace(tagStripRe.ReplaceAllString(m[2], " "))
		name = strings.Join(strings.Fields(name), " ")
		if df := attrs["data-file-name"]; df != "" {
			name = df
		}
		if name == "" {
			continue
		}
		if looksLikeFileLink(target, name) {
			out = append(out, name)
		}
	}
	return out
}

func TestHTTPDiscoveryProbe(t *testing.T) {
	urlFile := os.Getenv("OPAL_HTTPPROBE")
	if urlFile == "" {
		t.Skip("set OPAL_HTTPPROBE=<file with one section URL per line>")
	}
	captureProbeLogs(t) // see probelogging_test.go for the day a suppressed Warn cost

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	raw, err := os.ReadFile(urlFile)
	if err != nil {
		t.Fatalf("read url file: %v", err)
	}
	seen := map[string]bool{}
	var urls []string
	for _, line := range strings.Split(string(raw), "\n") {
		u := strings.TrimSpace(line)
		if u == "" {
			continue
		}
		// Drop the Wicket page-instance counter: we want the canonical
		// section URL, the one a fresh crawl would visit.
		if i := strings.LastIndex(u, "?"); i > 0 {
			u = u[:i]
		}
		if !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	sort.Strings(urls)
	t.Logf("probing %d distinct section URLs", len(urls))

	EnsurePlaywrightBrowsersPath()
	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("playwright: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	defer browser.Close()

	ctxOpts := playwright.BrowserNewContextOptions{}
	if _, statErr := os.Stat(loaded.Credentials.StateFile); statErr == nil {
		ctxOpts.StorageStatePath = playwright.String(loaded.Credentials.StateFile)
	}
	bctx, err := browser.NewContext(ctxOpts)
	if err != nil {
		t.Fatalf("context: %v", err)
	}
	reqCtx := bctx.Request()

	type result struct {
		url    string
		files  []string
		status int
		pager  bool
		err    error
	}

	parallel := 12
	if v := os.Getenv("OPAL_HTTPPROBE_PAR"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			parallel = n
		}
	}
	jobs := make(chan string)
	results := make(chan result, len(urls))
	var wg sync.WaitGroup

	start := time.Now()
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for u := range jobs {
				resp, err := reqCtx.Get(u)
				if err != nil {
					results <- result{url: u, err: err}
					continue
				}
				body, bodyErr := resp.Text()
				st := resp.Status()
				if bodyErr != nil {
					results <- result{url: u, status: st, err: bodyErr}
					continue
				}
				if dump := os.Getenv("OPAL_HTTPPROBE_DUMP"); dump != "" {
					_ = os.WriteFile(dump, []byte(body), 0o644)
				}
				names := fileLinksInHTML(body)
				hasPager := strings.Contains(body, "pager-showall")
				if os.Getenv("OPAL_HTTPPROBE_DATAATTR") == "1" {
					names = nil
					for _, m := range dataFileNameRe.FindAllStringSubmatch(body, -1) {
						names = append(names, m[1])
					}
				}
				results <- result{url: u, files: names, status: st, pager: hasPager}
			}
		}()
	}
	go func() {
		for _, u := range urls {
			jobs <- u
		}
		close(jobs)
	}()
	wg.Wait()
	close(results)
	elapsed := time.Since(start)

	distinct := map[string]bool{}
	pairs := 0
	failed := 0
	nonOK := 0
	withFiles := 0
	pagerSections := 0
	var pagerURLs []string
	for r := range results {
		if r.err != nil {
			failed++
			continue
		}
		if r.status != 200 {
			nonOK++
			continue
		}
		if len(r.files) > 0 {
			withFiles++
		}
		if r.pager {
			pagerSections++
			pagerURLs = append(pagerURLs, r.url)
		}
		for _, f := range r.files {
			distinct[f] = true
			pairs++
		}
	}

	t.Logf("HTTP PROBE: %d urls in %s at parallelism %d", len(urls), elapsed.Round(time.Millisecond), parallel)
	t.Logf("  sections returning file anchors : %d", withFiles)
	t.Logf("  (section,file) anchor pairs     : %d", pairs)
	t.Logf("  distinct file names             : %d", len(distinct))
	t.Logf("  request errors                  : %d", failed)
	t.Logf("  non-200 responses               : %d", nonOK)
	t.Logf("  sections advertising a pager    : %d  <- these need the browser", pagerSections)
	sort.Strings(pagerURLs)
	for i, u := range pagerURLs {
		if i >= 15 {
			t.Logf("    ... and %d more", len(pagerURLs)-15)
			break
		}
		t.Logf("    pager: %s", u)
	}

	if out := os.Getenv("OPAL_HTTPPROBE_OUT"); out != "" {
		names := make([]string, 0, len(distinct))
		for f := range distinct {
			names = append(names, f)
		}
		sort.Strings(names)
		if err := os.WriteFile(out, []byte(strings.Join(names, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write out: %v", err)
		}
	}
}
