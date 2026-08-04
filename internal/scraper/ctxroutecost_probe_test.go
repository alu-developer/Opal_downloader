package scraper

// docs/sync-speed-model.md, Question 8 ("Next experiment", written
// 2026-08-04): Question 3 found ctx.Route installs two separate CDP
// mechanisms together - Network.setCacheDisabled(true) for the whole
// session, and a per-request Fetch pause/resume round trip - and could not
// separate which one costs the measured ~30% tax, because Playwright 1.61.1
// couples them rigidly through ctx.Route itself.
//
// This probe isolates them with raw CDP calls (CDPSession.Send/On) instead
// of going through ctx.Route, against a local httptest server rather than
// the real OPAL account - no login needed, no real-account load.
//
// Four conditions, same browser context reused across repeated navigations
// of a page with routeCostAssetCount small cacheable assets (simulating
// OPAL's Wicket JS/CSS bundle served identically on every section page):
//
//   - baseline: no CDP session at all.
//   - ctxRoute: ctx.Route on a pattern matching nothing - reproduces
//     Question 3's coupled tax locally, and confirms the harness is
//     comparable to the real finding.
//   - cacheOffOnly: raw Network.setCacheDisabled(true), no Fetch domain.
//   - fetchOnly: raw Fetch.enable (pattern "*") + immediate
//     Fetch.continueRequest on every paused request, cache left alone.
//
// The server counts requests per asset path itself - a direct check of which
// conditions actually defeat the cache, not inferred from timing alone.
//
// Prediction (docs/sync-speed-model.md, written before this file existed):
// cache-off is the dominant component of the ctx.Route tax (>=60% of the gap
// against baseline), and raw CDP calls DO decouple - fetchOnly leaves the
// per-asset request count at ~1 (cache intact) despite the Fetch domain
// being active, because Chrome does not require the two together; ctx.Route's
// coupling is Playwright's own driver-side choice.
//
// Counts as failed at: fetchOnly accounts for >=40% of the ctx.Route gap
// (cache-off not clearly dominant), or fetchOnly's own per-asset request
// count comes back at ~routeCostNavigations per asset (cache defeated even
// without an explicit setCacheDisabled call - Chrome itself couples the two
// at the protocol level, not just Playwright's binding).
//
// Usage:
//
//	OPAL_ROUTE_COST_PROBE=1 go test ./internal/scraper/ -run TestCtxRouteCostSplit -v -timeout 5m
import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const (
	routeCostAssetCount  = 25
	routeCostNavigations = 12
	routeCostRepeats     = 3
)

type routeCostCounter struct {
	mu     sync.Mutex
	counts map[string]int
}

func newRouteCostCounter() *routeCostCounter {
	return &routeCostCounter{counts: map[string]int{}}
}

func (c *routeCostCounter) record(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[path]++
}

func (c *routeCostCounter) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts = map[string]int{}
}

// maxCount is the metric that matters here, not the total: if caching works,
// every asset is fetched once regardless of how many navigations reuse it,
// so the max per-path count is the signal, not the sum across 25 assets.
func (c *routeCostCounter) maxCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	max := 0
	for _, n := range c.counts {
		if n > max {
			max = n
		}
	}
	return max
}

func newRouteCostServer(counter *routeCostCounter) *httptest.Server {
	var assetTags strings.Builder
	for i := 0; i < routeCostAssetCount; i++ {
		fmt.Fprintf(&assetTags, `<script src="/asset/%d.js"></script>`, i)
	}
	html := "<!doctype html><html><body>" + assetTags.String() + "</body></html>"

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(html))
	})
	mux.HandleFunc("/asset/", func(w http.ResponseWriter, r *http.Request) {
		counter.record(r.URL.Path)
		w.Header().Set("Content-Type", "application/javascript")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte("// asset\n"))
	})
	return httptest.NewServer(mux)
}

func navigateRepeatedly(page playwright.Page, url string, n int) (time.Duration, error) {
	start := time.Now()
	for i := 0; i < n; i++ {
		if _, err := page.Goto(url, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateLoad,
		}); err != nil {
			return 0, fmt.Errorf("navigation %d: %w", i, err)
		}
	}
	return time.Since(start), nil
}

type routeCostResult struct {
	elapsed  time.Duration
	maxCount int
}

func runRouteCostCondition(browser playwright.Browser, url string, counter *routeCostCounter, setup func(playwright.BrowserContext, playwright.Page) error) (routeCostResult, error) {
	bctx, err := browser.NewContext()
	if err != nil {
		return routeCostResult{}, fmt.Errorf("new context: %w", err)
	}
	defer bctx.Close()

	page, err := bctx.NewPage()
	if err != nil {
		return routeCostResult{}, fmt.Errorf("new page: %w", err)
	}

	if setup != nil {
		if err := setup(bctx, page); err != nil {
			return routeCostResult{}, fmt.Errorf("condition setup: %w", err)
		}
	}

	counter.reset()
	elapsed, err := navigateRepeatedly(page, url, routeCostNavigations)
	if err != nil {
		return routeCostResult{}, err
	}
	return routeCostResult{elapsed: elapsed, maxCount: counter.maxCount()}, nil
}

func TestCtxRouteCostSplit(t *testing.T) {
	if os.Getenv("OPAL_ROUTE_COST_PROBE") == "" {
		t.Skip("set OPAL_ROUTE_COST_PROBE=1 to run the local ctx.Route cost-split probe (docs/sync-speed-model.md Question 8)")
	}

	counter := newRouteCostCounter()
	ts := newRouteCostServer(counter)
	defer ts.Close()

	EnsurePlaywrightBrowsersPath()
	pw, err := playwright.Run()
	if err != nil {
		t.Fatalf("playwright: %v", err)
	}
	defer pw.Stop()

	browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{Headless: playwright.Bool(true)})
	if err != nil {
		t.Fatalf("launch chromium: %v", err)
	}
	defer browser.Close()

	var report []string
	say := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		report = append(report, line)
		t.Log(line)
	}

	conditions := []struct {
		name  string
		setup func(playwright.BrowserContext, playwright.Page) error
	}{
		{name: "baseline", setup: nil},
		{name: "ctxRoute (pattern matching nothing)", setup: func(bctx playwright.BrowserContext, _ playwright.Page) error {
			return bctx.Route("**/no-such-path-xyz/**", func(route playwright.Route) {
				_ = route.Continue()
			})
		}},
		{name: "cacheOffOnly (raw CDP, no Fetch)", setup: func(bctx playwright.BrowserContext, page playwright.Page) error {
			session, err := bctx.NewCDPSession(page)
			if err != nil {
				return err
			}
			if _, err := session.Send("Network.enable", map[string]any{}); err != nil {
				return err
			}
			_, err = session.Send("Network.setCacheDisabled", map[string]any{"cacheDisabled": true})
			return err
		}},
		{name: "fetchOnly (raw CDP, cache untouched)", setup: func(bctx playwright.BrowserContext, page playwright.Page) error {
			session, err := bctx.NewCDPSession(page)
			if err != nil {
				return err
			}
			session.On("Fetch.requestPaused", func(params map[string]any) {
				requestID, _ := params["requestId"].(string)
				if requestID == "" {
					return
				}
				// Must not call session.Send synchronously from inside this
				// handler: it runs on the connection's own dispatch loop, and
				// Send blocks waiting for a reply by re-entering that same
				// loop. With routeCostAssetCount requests pausing at once,
				// that recurses one dispatch call deep per pending request -
				// observed hanging outright on the 3rd of 3 repeats in the
				// first version of this probe (2 requests handled fine, then
				// the 3rd run's recursion never resolved). A goroutine keeps
				// the reply off the dispatch loop's own stack.
				go func() {
					_, _ = session.Send("Fetch.continueRequest", map[string]any{"requestId": requestID})
				}()
			})
			_, err = session.Send("Fetch.enable", map[string]any{
				"patterns": []map[string]any{{"urlPattern": "*", "requestStage": "Request"}},
			})
			return err
		}},
	}

	say("ctx.Route cost split probe (Question 8): %d navigations/condition, %d assets/page, %d repeat(s)",
		routeCostNavigations, routeCostAssetCount, routeCostRepeats)

	results := make(map[string][]routeCostResult)
	for repeat := 1; repeat <= routeCostRepeats; repeat++ {
		say("")
		say("--- repeat %d/%d ---", repeat, routeCostRepeats)
		for _, cond := range conditions {
			res, err := runRouteCostCondition(browser, ts.URL+"/", counter, cond.setup)
			if err != nil {
				t.Fatalf("condition %q, repeat %d: %v", cond.name, repeat, err)
			}
			results[cond.name] = append(results[cond.name], res)
			say("  %-38s elapsed=%v max-requests-for-one-asset=%d (cache %s)",
				cond.name, res.elapsed.Round(time.Millisecond), res.maxCount,
				cacheVerdict(res.maxCount))
		}
	}

	say("")
	say("--- means across %d repeat(s) ---", routeCostRepeats)
	means := map[string]time.Duration{}
	for _, cond := range conditions {
		var total time.Duration
		for _, r := range results[cond.name] {
			total += r.elapsed
		}
		mean := total / time.Duration(len(results[cond.name]))
		means[cond.name] = mean
		say("  %-38s mean elapsed=%v", cond.name, mean.Round(time.Millisecond))
	}

	baseline := means["baseline"]
	ctxRouteGap := means["ctxRoute (pattern matching nothing)"] - baseline
	cacheOffGap := means["cacheOffOnly (raw CDP, no Fetch)"] - baseline
	fetchOnlyGap := means["fetchOnly (raw CDP, cache untouched)"] - baseline

	say("")
	say("ctx.Route gap over baseline: %v", ctxRouteGap.Round(time.Millisecond))
	if ctxRouteGap > 0 {
		say("  cacheOffOnly gap as %% of ctx.Route gap: %.1f%%", float64(cacheOffGap)/float64(ctxRouteGap)*100)
		say("  fetchOnly gap as %% of ctx.Route gap: %.1f%%", float64(fetchOnlyGap)/float64(ctxRouteGap)*100)
	} else {
		say("  ctx.Route gap is not positive in this run - cannot compute percentages, see raw means above")
	}

	fetchOnlyMaxCounts := make([]int, 0, routeCostRepeats)
	for _, r := range results["fetchOnly (raw CDP, cache untouched)"] {
		fetchOnlyMaxCounts = append(fetchOnlyMaxCounts, r.maxCount)
	}
	say("")
	say("fetchOnly per-asset max request count across repeats: %v (cache intact means ~1, cache defeated means ~%d)",
		fetchOnlyMaxCounts, routeCostNavigations)

	outDir := filepath.Join("..", "..", "tmp")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Logf("could not create %s, result is in this log only: %v", outDir, err)
	} else {
		outPath := filepath.Join(outDir, "ctxroute-cost-split-probe.log")
		header := "ctx.Route cost split probe (Question 8) recorded " + time.Now().Format(time.RFC3339) + "\n\n"
		if err := os.WriteFile(outPath, []byte(header+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
			t.Logf("could not write %s, result is in this log only: %v", outPath, err)
		} else {
			t.Logf("findings written to %s", outPath)
		}
	}
}

func cacheVerdict(maxCount int) string {
	if maxCount <= 2 {
		return "intact"
	}
	if maxCount >= routeCostNavigations-1 {
		return "defeated"
	}
	return "partial"
}
