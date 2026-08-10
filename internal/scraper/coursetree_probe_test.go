package scraper

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// The instrument for docs/sync-speed-model.md Question 36 Step A2: does the
// course-node tree in a course root's own response cover the browser crawl's
// whole section set for EVERY course, or only for the one course whose dumps
// happened to be on disk?
//
// Step A proved it offline for Softwaretechnologie: 147 of 147 bare CourseNode
// URLs recovered from one response (coursetree_test.go). That is one course,
// and it is the course this project's saved HTML is richest for - exactly the
// shape of evidence that has misled this campaign before. Step B (restructuring
// the hybrid to seed phase 1 from the tree) is a large change and must not be
// built on an n=1 result.
//
// This probe is deliberately the CHEAPEST thing that decides it: it rides along
// on one ordinary crawl. The crawl runs unchanged and its VisitRecords give the
// ground-truth section set per course; afterwards - strictly after, never
// concurrently, per fetchSectionFilesHTTP's invariant - it issues ONE extra
// HTTP GET per course root and compares. Six extra requests against a whole
// crawl.
//
// Usage:
//
//	OPAL_TREE_COVERAGE=1 go test ./internal/scraper/ -run TestCourseTreeCoverage -count=1 -v -timeout 30m
//
// The report is written to tmp/tree-coverage.txt so a killed turn does not
// lose it.
func TestCourseTreeCoverage(t *testing.T) {
	if os.Getenv("OPAL_TREE_COVERAGE") == "" {
		t.Skip("set OPAL_TREE_COVERAGE=1 to probe tree coverage against the real account")
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

	files, err := sc.ScrapeWithSavedSession(context.Background(), loaded.App.Courses)
	if err != nil {
		t.Fatalf("scrape: %v", err)
	}

	// Ground truth: what the browser actually visited, split the same way
	// coursetree_test.go splits it - bare course-node URLs (which the tree can
	// carry) against sub-paths inside a node's own file browser (which it
	// structurally cannot).
	type courseTruth struct {
		title    string
		root     string
		bare     map[string]bool
		subPaths int
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
			ct = &courseTruth{title: vr.Course, root: root, bare: map[string]bool{}}
			byRoot[root] = ct
			rootOrder = append(rootOrder, root)
		}
		m := baselineSectionURLRe.FindStringSubmatch(vr.SectionURL)
		switch {
		case m == nil || m[2] == "":
			// the course root itself
		case m[3] != "":
			ct.subPaths++
		default:
			ct.bare[vr.SectionURL] = true
		}
	}
	sort.Strings(rootOrder)

	fetch := sc.httpDiscoveryFetcher()
	if fetch == nil {
		t.Fatal("no authenticated request context after the crawl; cannot fetch course roots")
	}

	subPathRe := regexp.MustCompile(`/CourseNode/\d+/.+`)
	var report []string
	totalMissing, totalBare, totalTree := 0, 0, 0

	for _, root := range rootOrder {
		ct := byRoot[root]
		resp, err := fetch.Get(root)
		if err != nil {
			t.Errorf("HTTP GET course root %s: %v", root, err)
			continue
		}
		body, err := responseBodyText(resp)
		if err != nil || resp.Status() != 200 {
			t.Errorf("HTTP read course root %s: status %d err %v", root, resp.Status(), err)
			continue
		}

		tree := ParseCourseTreeNodes(body)
		inTree := map[string]bool{}
		treeSubPaths := 0
		for _, n := range tree {
			inTree[n.URL] = true
			if subPathRe.MatchString(n.URL) {
				treeSubPaths++
			}
		}

		var missing []string
		for url := range ct.bare {
			if !inTree[url] {
				missing = append(missing, url)
			}
		}
		sort.Strings(missing)

		totalMissing += len(missing)
		totalBare += len(ct.bare)
		totalTree += len(tree)

		report = append(report, fmt.Sprintf(
			"%s\n  root            : %s\n  tree nodes      : %d\n  visited bare    : %d\n  MISSING from tree: %d\n  visited subpaths : %d (tree cannot carry these)\n  tree subpaths    : %d (expected 0)",
			ct.title, root, len(tree), len(ct.bare), len(missing), ct.subPaths, treeSubPaths))
		for _, m := range missing {
			report = append(report, "    missing: "+m)
		}
		if treeSubPaths != 0 {
			t.Errorf("%s: tree carried %d sub-path URLs; a course-node tree was not supposed to be able to",
				ct.title, treeSubPaths)
		}
	}

	summary := fmt.Sprintf("TREE COVERAGE: %d courses, %d files crawled, %d tree nodes, %d visited bare URLs, %d MISSING",
		len(rootOrder), len(files), totalTree, totalBare, totalMissing)
	body := summary + "\n\n" + strings.Join(report, "\n") + "\n"

	out := filepath.Join("..", "..", "tmp", "tree-coverage.txt")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	t.Log(summary)
	t.Logf("report written to %s", out)

	// The registered prediction: zero missing, in every course.
	if totalMissing != 0 {
		t.Errorf("PREDICTION FAILED: %d visited course-node URLs across %d courses are absent from their course root's own response; "+
			"the tree is not a complete seed and Question 36 Step B cannot assume it is. See %s",
			totalMissing, len(rootOrder), out)
	}
}
