package scraper

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alu-developer/opal-downloader/internal/config"
)

// TestCourseNodeType is Question 45's "new open question, ranked above Part
// 2" (docs/sync-speed-model.md "Next experiment", cycle 2026-09-14): what
// course-node type are the So26 Programmieren `Woche 05`..`13` sections
// actually built from, given the 2026-09-11 cycle found their folder-browser
// toolbar (checkboxes, "Tabelle herunterladen", the bulk-download button)
// completely absent - not a missing button, a different page type.
//
// The previous attempt at this question guessed CSS selectors
// (.o_course_run, #o_main_container) against the rendered DOM and got page
// chrome back, no signal. This probe routes around that: coursetree.go's
// ParseCourseTreeNodes already reads the node's own `class="node-<type>"`
// marker straight out of the course root page's `initial_data` JSON payload
// - the same field isNonFileSectionType tests - with no browser rendering
// and no DOM guessing, just one HTTP GET of the course root through the
// already-authenticated Playwright request context.
//
// Usage:
//
//	OPAL_COURSENODE_TYPE=1 go test ./internal/scraper/ -run TestCourseNodeType -count=1 -v -timeout 10m
//
// Report written to tmp/coursenode-type-woche.txt.
func TestCourseNodeType(t *testing.T) {
	if os.Getenv("OPAL_COURSENODE_TYPE") == "" {
		t.Skip("set OPAL_COURSENODE_TYPE=1 to probe the Woche-section course-node type against the real account")
	}
	beginLiveProbe(t)

	const repo = `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	loaded, err := config.Load(filepath.Join(repo, "config.yaml"))
	if err != nil {
		t.Fatalf("load config: %v", err)
	}

	const courseTitle = "So26 Programmieren - Weiterführende Konzepte (Math-Ba-PR20)"

	sc := New(loaded.Credentials.URL, loaded.Credentials.StateFile)
	defer sc.Close()
	if serr := sc.ensureSession(false); serr != nil {
		t.Fatalf("ensure session: %v", serr)
	}

	refs, derr := sc.discoverCourseLinks([]string{courseTitle})
	if derr != nil {
		t.Fatalf("discover course links: %v", derr)
	}
	var course *CourseRef
	for i := range refs {
		if strings.EqualFold(refs[i].Title, courseTitle) {
			course = &refs[i]
			break
		}
	}
	if course == nil {
		t.Fatalf("course %q not found among %d discovered courses", courseTitle, len(refs))
	}
	t.Logf("course root: %s", course.URL)

	fetch := sc.httpDiscoveryFetcher()
	if fetch == nil {
		t.Fatal("no authenticated request context after ensureSession; cannot fetch course root")
	}
	resp, gerr := fetch.Get(course.URL)
	if gerr != nil {
		t.Fatalf("HTTP GET course root %s: %v", course.URL, gerr)
	}
	body, berr := responseBodyText(resp)
	if berr != nil || resp.Status() != 200 {
		t.Fatalf("HTTP read course root %s: status %d err %v", course.URL, resp.Status(), berr)
	}

	tree := ParseCourseTreeNodes(body)
	if len(tree) == 0 {
		t.Fatalf("PREDICTION HOLE: course root's initial_data payload parsed to 0 nodes - the tree does not cover this course the way it does Softwaretechnologie")
	}

	const controlA = "Klausurinformationen"
	const controlB = "Probeklausur"

	var report []string
	var wocheNodes []CourseTreeNode
	var controls []CourseTreeNode
	for _, n := range tree {
		if strings.Contains(n.Title, "Woche") {
			wocheNodes = append(wocheNodes, n)
		}
		if n.Title == controlA || n.Title == controlB {
			controls = append(controls, n)
		}
	}
	sort.Slice(wocheNodes, func(i, j int) bool { return wocheNodes[i].Title < wocheNodes[j].Title })

	report = append(report, fmt.Sprintf("course: %s", courseTitle))
	report = append(report, fmt.Sprintf("course root: %s", course.URL))
	report = append(report, fmt.Sprintf("tree nodes total: %d", len(tree)))
	report = append(report, "")
	report = append(report, fmt.Sprintf("Woche nodes found: %d", len(wocheNodes)))
	for _, n := range wocheNodes {
		report = append(report, fmt.Sprintf("  title=%q class=%q depth=%d url=%s", n.Title, n.Class, n.Depth, n.URL))
	}
	report = append(report, "")
	report = append(report, fmt.Sprintf("control nodes found: %d (expect 2: %s, %s)", len(controls), controlA, controlB))
	for _, n := range controls {
		report = append(report, fmt.Sprintf("  title=%q class=%q depth=%d url=%s", n.Title, n.Class, n.Depth, n.URL))
	}

	if len(wocheNodes) == 0 {
		report = append(report, "", "PREDICTION HOLE: no 'Woche' titles found anywhere in the tree - naming or coverage mismatch, not a node-type answer.")
	}

	out := filepath.Join(repo, "tmp", "coursenode-type-woche.txt")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(out, []byte(strings.Join(report, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	for _, line := range report {
		t.Log(line)
	}
	t.Logf("report written to %s", out)

	if len(wocheNodes) < 8 {
		t.Errorf("PREDICTION HOLE: only %d of an expected ~10 'Woche' sections found in the tree", len(wocheNodes))
	}
	if len(controls) < 2 {
		t.Errorf("PREDICTION HOLE: only %d of 2 expected control sections (%s, %s) found in the tree", len(controls), controlA, controlB)
	}
}
