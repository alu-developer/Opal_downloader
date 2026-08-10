package scraper

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestExtractBalancedJSONArrayStopsAtTheRealEnd(t *testing.T) {
	// The array is followed by more JavaScript, and contains `]` inside a
	// string literal and an escaped quote - the two things a `.*?\];` regex
	// gets wrong.
	src := `var initial_data=[{"id":"a","text":"a] \" b","children":[{"id":"b"}]}];var other=1;`
	start := strings.Index(src, "[")
	got, ok := extractBalancedJSONArray(src, start)
	if !ok {
		t.Fatal("expected to find a balanced array")
	}
	want := `[{"id":"a","text":"a] \" b","children":[{"id":"b"}]}]`
	if got != want {
		t.Errorf("balanced array\n got: %s\nwant: %s", got, want)
	}
}

func TestExtractBalancedJSONArrayRejectsNonArrayAndUnterminated(t *testing.T) {
	if _, ok := extractBalancedJSONArray(`{"id":"a"}`, 0); ok {
		t.Error("expected failure when the offset is not a [")
	}
	if _, ok := extractBalancedJSONArray(`[{"id":"a"}`, 0); ok {
		t.Error("expected failure on an unterminated array")
	}
}

func TestParseCourseTreeNodesReadsHrefClassTitleAndDepth(t *testing.T) {
	html := `<script>var initial_data=[{"id":"idroot","text":"<span class=\"jstree-title\">Kurs<\/span>",` +
		`"icon":"fonticon icon-root","state":{"opened":true,"selected":true},"children":[` +
		`{"id":"id1","text":"<span class=\"jstree-title\">News<\/span>","li_attr":{"class":"node-info"},` +
		`"a_attr":{"href":"https://x/opal/auth/RepositoryEntry/1/CourseNode/11","title":"News","class":"node-info"}},` +
		`{"id":"id2","text":"<span class=\"jstree-title\">Ordner<\/span>","li_attr":{"class":"node-bc"},` +
		`"a_attr":{"href":"https://x/opal/auth/RepositoryEntry/1/CourseNode/22","title":"Ordner","class":"node-bc"},` +
		`"children":[{"id":"id3","text":"<span class=\"jstree-title\">Tief<\/span>","li_attr":{"class":"node-sp"},` +
		`"a_attr":{"href":"https://x/opal/auth/RepositoryEntry/1/CourseNode/33","title":"Tief","class":"node-sp"}}]}]}];</script>`

	got := ParseCourseTreeNodes(html)
	if len(got) != 3 {
		t.Fatalf("expected 3 course nodes (the root has no CourseNode href), got %d: %+v", len(got), got)
	}
	want := []CourseTreeNode{
		{ID: "11", URL: "https://x/opal/auth/RepositoryEntry/1/CourseNode/11", Class: "node-info", Title: "News", Depth: 1},
		{ID: "22", URL: "https://x/opal/auth/RepositoryEntry/1/CourseNode/22", Class: "node-bc", Title: "Ordner", Depth: 1},
		{ID: "33", URL: "https://x/opal/auth/RepositoryEntry/1/CourseNode/33", Class: "node-sp", Title: "Tief", Depth: 2},
	}
	for i, w := range want {
		if got[i] != w {
			t.Errorf("node %d\n got: %+v\nwant: %+v", i, got[i], w)
		}
	}
}

func TestParseCourseTreeNodesToleratesLazyChildrenAndMissingPayload(t *testing.T) {
	// jstree allows `"children":true` to mean "load lazily". That must not
	// abort the walk - the node itself is still a usable seed.
	html := `var initial_data=[{"id":"id1","children":true,` +
		`"a_attr":{"href":"https://x/opal/auth/RepositoryEntry/1/CourseNode/11","title":"A","class":"node-bc"}}];`
	got := ParseCourseTreeNodes(html)
	if len(got) != 1 || got[0].ID != "11" {
		t.Errorf("lazy-children node should still be returned, got %+v", got)
	}

	if got := ParseCourseTreeNodes(`<html><body>no payload here</body></html>`); got != nil {
		t.Errorf("a page without initial_data must return nil, got %+v", got)
	}
	if got := ParseCourseTreeNodes(`var initial_data=[{"id":`); got != nil {
		t.Errorf("an unparseable payload must return nil, got %+v", got)
	}
}

func TestParseCourseTreeNodesSkipsTheGroupsTree(t *testing.T) {
	// A real course page carries a second initial_data for the groups tree,
	// whose entries have no /CourseNode/ href.
	html := `var initial_data=[{"id":"idc","a_attr":{"href":"https://x/opal/auth/RepositoryEntry/1/CourseNode/11","class":"node-bc"}}];` +
		`var initial_data=[{"id":"idgroups","a_attr":{"href":"https://x/opal/auth/RepositoryEntry/1/Groups","class":"o_group"}}];`
	got := ParseCourseTreeNodes(html)
	if len(got) != 1 || got[0].ID != "11" {
		t.Errorf("expected only the course-node entry, got %+v", got)
	}
}

// baselineSectionURLRe splits a recorded section URL into its RepositoryEntry
// root, its CourseNode id, and any sub-path inside that node.
var baselineSectionURLRe = regexp.MustCompile(`^(https://\S*?/RepositoryEntry/\d+)(?:/CourseNode/(\d+)(/.*)?)?$`)

// TestParseCourseTreeNodesAgainstCapturedCourseRoot is the experiment
// docs/sync-speed-model.md Question 36 Step A registered its prediction for,
// run against data already on disk rather than the live account.
//
// The claim under test: ONE course-root response contains every bare
// CourseNode URL the browser crawl reached by walking the tree level by level.
// tmp/baseline/sw-root.html is a plain authenticated HTTP GET of
// .../RepositoryEntry/53228666883; tmp/baseline/swt-all-sections.txt is the
// section set that crawl actually visited.
//
// Predicted before the parser existed: 147 of 147 bare CourseNode URLs
// recovered, 0 missing, 0 of the 16 sub-path URLs recovered, and no recovered
// URL the crawl never visited except the 4 node-fo forums the existing filters
// drop on purpose (the root carries no CourseNode href at all, so it does not
// appear here the way the prediction's "known 5" assumed).
//
// Skipped when the dumps are absent (fresh clone, CI) so the suite stays
// hermetic, following TestParseHTTPSectionCandidatesAgainstCapturedDump.
func TestParseCourseTreeNodesAgainstCapturedCourseRoot(t *testing.T) {
	root := filepath.Join("..", "..", "tmp", "baseline", "sw-root.html")
	sections := filepath.Join("..", "..", "tmp", "baseline", "swt-all-sections.txt")

	htmlBytes, err := os.ReadFile(root)
	if err != nil {
		t.Skipf("skip: %v (dump not present)", err)
	}
	f, err := os.Open(sections)
	if err != nil {
		t.Skipf("skip: %v (visit record not present)", err)
	}
	defer f.Close()

	visitedBare := map[string]bool{}
	subPaths := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		m := baselineSectionURLRe.FindStringSubmatch(line)
		switch {
		case m == nil || m[2] == "":
			// the course root itself
		case m[3] != "":
			subPaths++
		default:
			visitedBare[line] = true
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading %s: %v", sections, err)
	}
	if len(visitedBare) != 147 || subPaths != 16 {
		t.Fatalf("the recorded visit set changed under this test: %d bare + %d sub-paths (expected 147 + 16); "+
			"re-derive Question 36's prediction before trusting the assertions below",
			len(visitedBare), subPaths)
	}

	tree := ParseCourseTreeNodes(string(htmlBytes))
	inTree := map[string]bool{}
	for _, n := range tree {
		inTree[n.URL] = true
	}

	var missing []string
	for url := range visitedBare {
		if !inTree[url] {
			missing = append(missing, url)
		}
	}
	if len(missing) != 0 {
		t.Errorf("PREDICTION FAILED: %d of %d visited course-node URLs are absent from the single root response; "+
			"the tree is not a complete seed. First few: %v",
			len(missing), len(visitedBare), missing[:min(len(missing), 5)])
	}

	var extra []string
	for _, n := range tree {
		if !visitedBare[n.URL] {
			extra = append(extra, n.Class+" "+n.Title)
		}
	}
	// Predicted: exactly the 4 node-fo forums.
	if len(extra) != 4 {
		t.Errorf("expected exactly 4 never-visited tree nodes (the node-fo forums), got %d: %v", len(extra), extra)
	}
	for _, e := range extra {
		if !strings.HasPrefix(e, "node-fo ") {
			t.Errorf("a never-visited tree node is not one of the expected forums: %q", e)
		}
	}

	// The other half of the prediction: a course-node tree structurally cannot
	// carry sub-paths inside a node's own file browser, so none may appear.
	for _, n := range tree {
		if strings.Count(strings.TrimPrefix(n.URL, "https://"), "/CourseNode/") > 0 &&
			regexp.MustCompile(`/CourseNode/\d+/.+`).MatchString(n.URL) {
			t.Errorf("tree unexpectedly carries a sub-path URL: %s", n.URL)
		}
	}

	t.Logf("tree: %d course nodes from ONE response; covers %d/%d visited bare URLs, "+
		"%d sub-path URLs still need their own node's response",
		len(tree), len(visitedBare)-len(missing), len(visitedBare), subPaths)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
