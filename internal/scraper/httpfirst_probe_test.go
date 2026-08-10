package scraper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// The instrument for docs/sync-speed-model.md Question 36 Step B1: can the
// section set the browser discovers by walking the tree be discovered instead
// by seeding from initial_data and expanding over plain HTTP?
//
// Step A2 established that one request per course yields every bare
// CourseNode URL (261 of 261, all 6 courses). That is 93% of the section set.
// The other 7% are sub-paths inside a node's own file browser, which no
// course-node tree can carry - and for Algorithmen und Datenstrukturen they
// are 3 of 4 sections, so they are not a rounding error. This probe asks
// whether the existing HTTP candidate extraction recovers them.
//
// WHY THIS AND NOT THE PRODUCTION RESTRUCTURE. Rebuilding
// scrapeCoursesHybrid's phase 1 is a large change to the code path that has
// silently lost files twice. This runs the same algorithm as a rider on an
// ordinary crawl, compares against that crawl's own section set, and costs one
// run. If the section sets match, the restructure is de-risked; if they do
// not, the restructure was never going to work and no production code was
// touched.
//
// It deliberately does NOT re-test file extraction. The hybrid's HTTP leaf
// parsing was verified byte-for-byte diff=0 against all 6 courses on
// 2026-07-31 and nothing here changes it; re-fetching every section a second
// time to re-prove it would double the request count for a known answer.
// Section discovery is the only unknown Step B introduces.
//
// Usage:
//
//	OPAL_HTTP_FIRST_PROBE=1 go test ./internal/scraper/ -run TestHTTPFirstSectionDiscovery -count=1 -v -timeout 30m
func TestHTTPFirstSectionDiscovery(t *testing.T) {
	if os.Getenv("OPAL_HTTP_FIRST_PROBE") == "" {
		t.Skip("set OPAL_HTTP_FIRST_PROBE=1 to probe HTTP-first section discovery against the real account")
	}

	beginLiveProbe(t)

	loaded, err := config.Load(filepath.Join("..", "..", "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	sc.SetCourseConcurrency(loaded.App.CourseConcurrency)
	sc.SetSectionConcurrency(loaded.App.SectionConcurrency)
	sc.SetSkipEnrollmentSections(loaded.App.SkipEnrollmentSections)
	defer sc.Close()

	browserStart := time.Now()
	if _, err := sc.ScrapeWithSavedSession(context.Background(), loaded.App.Courses); err != nil {
		t.Fatalf("scrape: %v", err)
	}
	browserElapsed := time.Since(browserStart)

	// Ground truth: every section the browser actually visited, keyed the same
	// way the crawl dedupes them, grouped by course root.
	type courseTruth struct {
		title  string
		root   string
		repoID string
		keys   map[string]string // sectionKey -> URL, for readable diffs
	}
	byRoot := map[string]*courseTruth{}
	var rootOrder []string
	for _, vr := range sc.VisitRecords() {
		if vr.SectionURL == "" {
			continue
		}
		root := repositoryEntryRoot(vr.SectionURL)
		if root == "" {
			continue
		}
		ct := byRoot[root]
		if ct == nil {
			ct = &courseTruth{title: vr.Course, root: root, repoID: repositoryEntryID(vr.SectionURL), keys: map[string]string{}}
			byRoot[root] = ct
			rootOrder = append(rootOrder, root)
		}
		ct.keys[sectionKey(vr.SectionURL, ct.repoID)] = vr.SectionURL
	}
	sort.Strings(rootOrder)

	fetch := sc.httpDiscoveryFetcher()
	if fetch == nil {
		t.Fatal("no authenticated request context after the crawl")
	}

	get := func(url string) (string, error) {
		resp, err := fetch.Get(url)
		if err != nil {
			return "", err
		}
		body, err := resp.Text()
		if err != nil {
			return "", err
		}
		if resp.Status() != 200 {
			return "", fmt.Errorf("status %d", resp.Status())
		}
		return body, nil
	}

	var report []string
	totalMissing, totalTruth, totalFetched, totalExtra := 0, 0, 0, 0
	httpStart := time.Now()

	for _, root := range rootOrder {
		ct := byRoot[root]

		// Seed exactly as Step B would: the course root, plus every course
		// node initial_data names. No browser, no navigation.
		rootBody, err := get(root)
		if err != nil {
			t.Errorf("%s: GET course root: %v", ct.title, err)
			continue
		}
		rootKey := sectionKey(root, ct.repoID)
		queue := []string{root}
		queued := map[string]struct{}{rootKey: {}}
		visited := map[string]struct{}{}
		sectionTitles := map[string]string{rootKey: ct.title}
		found := map[string]string{}

		for _, n := range ParseCourseTreeNodes(rootBody) {
			k := sectionKey(n.URL, ct.repoID)
			if _, dup := queued[k]; dup {
				continue
			}
			queued[k] = struct{}{}
			sectionTitles[k] = n.Title
			queue = append(queue, n.URL)
		}
		seeded := len(queue)

		// Expand over HTTP with the crawl's own predicates: the same candidate
		// shape, the same appendSectionFolderTargets that queues a folder's
		// children in the browser path.
		fetched := 0
		for len(queue) > 0 && len(visited) < 500 {
			current := queue[0]
			queue = queue[1:]
			k := sectionKey(current, ct.repoID)
			delete(queued, k)
			if _, seen := visited[k]; seen {
				continue
			}
			visited[k] = struct{}{}

			body := rootBody
			if current != root {
				body, err = get(current)
				if err != nil {
					t.Errorf("%s: GET %s: %v", ct.title, current, err)
					continue
				}
			}
			fetched++
			found[k] = current

			candidates := parseHTTPSectionCandidates(body)

			// Follow the pagination toggle before expanding. A paginated
			// section's first page carries only ~20 rows, and the rows beyond
			// it include SUB-SECTIONS, not just files - which is what the
			// first run of this probe got wrong. It missed exactly the four
			// .md sub-paths that live past Part-3's first page, and those four
			// appear in tmp/baseline/part3-showall.html while being absent
			// from tmp/baseline/part3-raw.html. fetchSectionFilesHTTP already
			// does this for files; folder-target expansion needs it too.
			if rel := extractShowAllURLFromHTML(body); rel != "" {
				showURL := resolveURL(sc.opalURL, rel)
				showBody, serr := get(showURL)
				if serr != nil {
					t.Errorf("%s: GET show-all %s: %v", ct.title, showURL, serr)
				} else {
					fetched++
					candidates = append(candidates, parseHTTPSectionCandidates(showBody)...)
				}
			}
			queue, _ = appendSectionFolderTargets(queue, queued, visited, candidates,
				sc.opalURL, ct.repoID, current, root, ct.title, sectionTitles,
				loaded.App.SkipEnrollmentSections)
		}

		var missing, extra []string
		for k, url := range ct.keys {
			if _, ok := found[k]; !ok {
				missing = append(missing, url)
			}
		}
		for k, url := range found {
			if _, ok := ct.keys[k]; !ok {
				extra = append(extra, url)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		totalMissing += len(missing)
		totalExtra += len(extra)
		totalTruth += len(ct.keys)
		totalFetched += fetched

		report = append(report, fmt.Sprintf(
			"%s\n  browser sections : %d\n  tree seed        : %d\n  http discovered  : %d (fetched %d)\n  MISSING          : %d\n  extra            : %d",
			ct.title, len(ct.keys), seeded-1, len(found), fetched, len(missing), len(extra)))
		for _, m := range missing {
			report = append(report, "    missing: "+m)
		}
		for _, e := range extra {
			report = append(report, "    extra  : "+e)
		}
	}
	httpElapsed := time.Since(httpStart)

	summary := fmt.Sprintf(
		"HTTP-FIRST SECTION DISCOVERY: %d courses | browser sections %d | http fetched %d | MISSING %d | extra %d\n"+
			"browser crawl %s (includes login) | http-first discovery %s",
		len(rootOrder), totalTruth, totalFetched, totalMissing, totalExtra,
		browserElapsed.Round(time.Millisecond), httpElapsed.Round(time.Millisecond))

	out := filepath.Join("..", "..", "tmp", "httpfirst-sections.txt")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(out, []byte(summary+"\n\n"+strings.Join(report, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	t.Log(summary)
	t.Logf("report written to %s", out)

	if totalMissing != 0 {
		t.Errorf("PREDICTION FAILED: HTTP-first discovery missed %d section(s) the browser found. See %s", totalMissing, out)
	}
}
