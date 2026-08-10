package scraper

import (
	"encoding/json"
	"regexp"
	"strings"
)

// This file reads OPAL's course-node tree straight out of a course page's
// bytes, with no browser and no navigation.
//
// WHY THIS EXISTS. Until 2026-08-10 this project believed the course tree
// could only be enumerated by a browser walking it one navigation per
// newly-opened branch - see docs/sync-speed-model.md Questions 2 and 9, which
// established that OpenOLAT's MenuTreeRenderer.isRenderChildren() puts only
// currently-open nodes plus the selection path into the RENDERED DOM. That is
// true of the rendered DOM and false of the response payload. Every course
// page also carries jstree's own data array:
//
//	var initial_data=[{"id":"id...","text":"<span ...>Titel</span>",
//	    "a_attr":{"href":"https://.../CourseNode/1679970544453334008", ...},
//	    "li_attr":{"class":"node-info"},"children":[ ... ]}, ...];
//
// and that array is the COMPLETE tree, not the open-branch fragment. Measured
// on the saved dumps (Question 34): 152 nodes nested to depth 3 for
// Softwaretechnologie, of which exactly one - the root - is marked
// "state":{"opened":true}. Cross-checked against the crawl's own recorded
// visit set: all 147 bare CourseNode URLs it visited are in there, none
// missing. The adjacent jstree config confirms the reading:
//
//	data: function(node,datacb){if(node.id==="#"){datacb(initial_data)}
//	                            else{load(datacb,node)}}
//
// i.e. the lazy per-branch load() Question 9 found is only ever reached for
// nodes that are NOT already in initial_data - and here that set is empty.
//
// WHAT IT CANNOT DO. initial_data is a course-NODE tree. Sub-paths inside a
// single node's own file browser (.../CourseNode/1615865126729195011/Part-3,
// and 11 .md documents under the same node) are folder entries, not course
// nodes, and are structurally absent. For Softwaretechnologie that is 16 of
// 164 section URLs. So this is a seed for discovery, not a replacement for it.

// initialDataAssignRe locates the `var initial_data=` assignments in a course
// page. A page carries more than one - the course tree and a separate groups
// tree (`{"id":"idgroups",...}`) - so every occurrence is scanned and the ones
// without course-node links simply contribute nothing.
//
// Regex only finds where the array STARTS; the end is found by
// extractBalancedJSONArray, because the array is followed by unrelated
// JavaScript on the same line and a non-greedy `.*?\];` would stop at the
// first `];` inside a string or nested structure.
var initialDataAssignRe = regexp.MustCompile(`var\s+initial_data\s*=\s*`)

// courseNodeHrefRe pulls the course-node id out of a tree node's href. The
// same shape sectionKey (crawl.go) normalizes, deliberately - a tree URL has
// to be comparable with a crawled one.
var courseNodeHrefRe = regexp.MustCompile(`/CourseNode/(\d+)`)

// courseTreeTagRe strips the markup jstree wraps a node's label in
// (`<span class="jstree-title">Titel</span>`).
var courseTreeTagRe = regexp.MustCompile(`(?is)<[^>]*>`)

// CourseTreeNode is one course-element node as the server delivered it.
// Class is the `node-<type>` marker (node-bc folder, node-sp single page,
// node-st structure, node-en enrollment, ...) - the same string
// isNonFileSectionType (section_type.go) tests, which today only ever sees it
// after a navigation has put it in the DOM.
type CourseTreeNode struct {
	ID    string
	URL   string
	Class string
	Title string
	Depth int
}

// jstreeNode mirrors the fields of one entry in initial_data that matter here.
// Children is a RawMessage because jstree accepts either a nested array or the
// literal `true` (meaning "load this branch lazily") in the same field, and a
// []jstreeNode would fail to unmarshal the latter.
type jstreeNode struct {
	ID       string          `json:"id"`
	Text     string          `json:"text"`
	Children json.RawMessage `json:"children"`
	AAttr    struct {
		Href  string `json:"href"`
		Title string `json:"title"`
		Class string `json:"class"`
	} `json:"a_attr"`
	LIAttr struct {
		Class string `json:"class"`
	} `json:"li_attr"`
}

// extractBalancedJSONArray returns the complete JSON array beginning at start,
// tracking bracket depth while skipping over string literals and their escape
// sequences. Returns ok=false if start is not a `[` or the array never closes.
func extractBalancedJSONArray(s string, start int) (string, bool) {
	if start >= len(s) || s[start] != '[' {
		return "", false
	}
	depth := 0
	inString := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return s[start : i+1], true
			}
		}
	}
	return "", false
}

// ParseCourseTreeNodes returns every course-node the page's initial_data
// payload describes, in document order, depth-first - which for a jstree array
// is the same order the sidebar displays. Nodes without a /CourseNode/<id>
// href (the root entry itself, group-tree entries, separators) are skipped, as
// is any node whose URL was already emitted.
//
// It never fails loudly: a page without the payload, or with one this parser
// cannot read, returns nil. Callers must treat an empty result as "no seed
// available, discover the normal way" rather than as "this course is empty" -
// silently trusting an empty tree is exactly how this project has lost files
// before.
func ParseCourseTreeNodes(html string) []CourseTreeNode {
	var out []CourseTreeNode
	seen := map[string]struct{}{}

	for _, loc := range initialDataAssignRe.FindAllStringIndex(html, -1) {
		raw, ok := extractBalancedJSONArray(html, loc[1])
		if !ok {
			continue
		}
		var roots []jstreeNode
		if err := json.Unmarshal([]byte(raw), &roots); err != nil {
			continue
		}
		collectCourseTreeNodes(roots, 0, seen, &out)
	}
	return out
}

// collectCourseTreeNodes walks one initial_data array depth-first, appending
// every node that carries a course-node href.
func collectCourseTreeNodes(nodes []jstreeNode, depth int, seen map[string]struct{}, out *[]CourseTreeNode) {
	for _, n := range nodes {
		href := strings.TrimSpace(n.AAttr.Href)
		if m := courseNodeHrefRe.FindStringSubmatch(href); m != nil {
			if _, dup := seen[href]; !dup {
				seen[href] = struct{}{}
				*out = append(*out, CourseTreeNode{
					ID:    m[1],
					URL:   href,
					Class: courseTreeNodeClass(n),
					Title: courseTreeNodeTitle(n),
					Depth: depth,
				})
			}
		}
		// Children is absent on a leaf and `true` on a lazily-loaded branch;
		// both unmarshal fine here and simply yield no child nodes.
		var kids []jstreeNode
		if len(n.Children) > 0 && json.Unmarshal(n.Children, &kids) == nil {
			collectCourseTreeNodes(kids, depth+1, seen, out)
		}
	}
}

// courseTreeNodeClass prefers the anchor's own class, because that is the
// string the DOM-side extractor puts in candidate["className"] and therefore
// the one isNonFileSectionType already knows how to read. li_attr carries the
// same value in every dump examined, and is the fallback.
func courseTreeNodeClass(n jstreeNode) string {
	if c := strings.TrimSpace(n.AAttr.Class); c != "" {
		return c
	}
	return strings.TrimSpace(n.LIAttr.Class)
}

// courseTreeNodeTitle prefers a_attr.title, which is already plain text, and
// falls back to the markup-wrapped label.
func courseTreeNodeTitle(n jstreeNode) string {
	if t := strings.TrimSpace(n.AAttr.Title); t != "" {
		return t
	}
	stripped := courseTreeTagRe.ReplaceAllString(n.Text, " ")
	return strings.Join(strings.Fields(stripped), " ")
}
