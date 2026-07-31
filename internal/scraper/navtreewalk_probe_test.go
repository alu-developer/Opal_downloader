package scraper

// Nav-tree-walk feasibility probe for the speed campaign's last lever.
//
// The tree-walk timing probe (treewalk_timing_probe_test.go) showed folder-nav
// links are stable by ~50ms on a pure-navigation node. This probe asks the
// harder, decision-making question: does a LIGHTWEIGHT tree-walk - navigate,
// wait a short budget, extract candidates, descend into folder links only,
// never wait for the file table to settle - discover the SAME set of section
// URLs the full browser crawl does? If it does, the navigation-only tree-walk
// is safe to build (HTTP then fetches every leaf), and option A gets its real
// speedup without touching visitSection's file-extraction wait.
//
// It runs a self-contained BFS using the crawl's own appendSectionFolderTargets
// to expand children, but with a short fixed wait instead of the settle+stability
// poll, and counts the distinct section URLs reached vs the known full-crawl
// count for the course.
//
// Usage:
//   OPAL_NAVTREE_COURSE="Softwaretechnologie (SoSe 26)" \
//   OPAL_NAVTREE_EXPECTED=163 \
//     go test ./internal/scraper/ -run TestNavTreeWalkProbe -v -timeout 20m

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
	"github.com/mxschmitt/playwright-go"
)

// navWaitMs is the per-section navigation-only budget. 50ms is where the
// timing probe saw folder links stabilize; a little headroom for a real
// BFS (vs the single-section probe) is prudent.
const navWaitMs = 100.0

func TestNavTreeWalkProbe(t *testing.T) {
	rootURL := os.Getenv("OPAL_NAVTREE_ROOT")
	if rootURL == "" {
		t.Skip("set OPAL_NAVTREE_ROOT=<course root RepositoryEntry URL> (and optionally OPAL_NAVTREE_EXPECTED=<n>)")
	}
	captureProbeLogs(t)

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	opalURL := loaded.Credentials.URL

	// Derive repoID from the root URL (.../RepositoryEntry/<id>).
	repoID := repositoryEntryID(rootURL)
	if repoID == "" {
		t.Fatalf("could not derive repoID from %s", rootURL)
	}
	t.Logf("nav tree-walk root: %s (repo %s, navWait=%.0fms)", rootURL, repoID, navWaitMs)

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
	page, err := bctx.NewPage()
	if err != nil {
		t.Fatalf("page: %v", err)
	}

	// Lightweight BFS: navigate, short wait, extract candidates, expand folder
	// targets only. Mirrors collectCourseFiles's BFS shape but with the file
	// extraction and settle wait stripped out.
	queue := []string{rootURL}
	visited := map[string]struct{}{sectionKey(rootURL, repoID): {}}
	queued := map[string]struct{}{sectionKey(rootURL, repoID): {}}
	const maxPages = 500
	start := time.Now()

	for len(queue) > 0 && len(visited) < maxPages {
		level := queue
		queue = nil
		for _, currentURL := range level {
			key := sectionKey(currentURL, repoID)
			if _, ok := visited[key]; ok {
				continue
			}
			visited[key] = struct{}{}

			if _, gerr := gotoPolitelyProbe(page, currentURL); gerr != nil {
				t.Logf("nav skip %s: %v", currentURL, gerr)
				continue
			}
			// Short navigation-only wait - the lever under test.
			page.WaitForTimeout(navWaitMs)
			candidates, cerr := extractCandidatesProbe(page)
			if cerr != nil {
				t.Logf("extract skip %s: %v", currentURL, cerr)
				continue
			}
			// Expand only folder targets (children).
			queue = appendSectionFolderTargetsProbe(queue, queued, visited, candidates, opalURL, repoID, currentURL, rootURL, rootURL)
		}
	}
	elapsed := time.Since(start)

	// Report: how many distinct sections did the lightweight walk reach?
	distinctSections := len(visited)
	expected := 0
	if v := os.Getenv("OPAL_NAVTREE_EXPECTED"); v != "" {
		fmt.Sscanf(v, "%d", &expected)
	}
	t.Logf("NAV TREE-WALK: %d distinct sections reached in %s (navWait=%.0fms)",
		distinctSections, elapsed.Round(time.Millisecond), navWaitMs)
	if expected > 0 {
		pct := 0.0
		if expected > 0 {
			pct = float64(distinctSections) / float64(expected) * 100
		}
		t.Logf("  vs full browser crawl expected=%d (%.0f%%)", expected, pct)
		if distinctSections < expected {
			t.Errorf("lightweight walk reached %d sections, expected %d - it is MISSING sections (would lose files)", distinctSections, expected)
		}
	}
}

// gotoPolitelyProbe is a thin Goto wrapper for the probe (no rate limiter).
func gotoPolitelyProbe(page playwright.Page, url string) (playwright.Response, error) {
	return page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	})
}

// extractCandidatesProbe pulls link candidates with the same JS the crawl uses.
func extractCandidatesProbe(page playwright.Page) ([]map[string]string, error) {
	val, err := page.Evaluate(`() => {
		const out = [];
		for (const el of document.querySelectorAll('a[href], [onclick], [data-href], [data-url]')) {
			out.push({
				href: el.getAttribute('href') || '',
				onclick: el.getAttribute('onclick') || '',
				dataHref: el.getAttribute('data-href') || '',
				dataUrl: el.getAttribute('data-url') || '',
				text: (el.textContent||'').trim(),
				title: el.getAttribute('title') || '',
				className: el.getAttribute('class') || ''
			});
		}
		return out;
	}`)
	if err != nil {
		return nil, err
	}
	return toStringMapSlice(val), nil
}

// appendSectionFolderTargetsProbe wraps appendSectionFolderTargets (the real
// crawl helper) for the probe's BFS, keeping the candidate shape compatible.
func appendSectionFolderTargetsProbe(queue []string, queued, visited map[string]struct{}, candidates []map[string]string, opalURL, repoID, currentURL, courseRootURL, courseTitle string) []string {
	// appendSectionFolderTargets also takes sectionTitles and returns skipped;
	// pass a throwaway titles map and ignore skipped for the probe.
	titles := map[string]string{}
	next, _ := appendSectionFolderTargets(queue, queued, visited, candidates, opalURL, repoID, currentURL, courseRootURL, courseTitle, titles, false)
	return next
}
