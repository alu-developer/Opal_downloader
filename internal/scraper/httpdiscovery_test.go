package scraper

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// Tests for the HTTP leaf-table extractor (httpdiscovery.go). These pin the
// two pure functions the serial-hybrid discovery path depends on, against
// snippets that mirror the real OPAL section HTML captured during the
// 2026-07-31 diagnosis. They run offline - no browser, no network.

func TestParseHTTPSectionCandidatesExtractsFileAnchor(t *testing.T) {
	// Shape taken verbatim from a real SW Part-3 row (tmp/baseline/part3-showall.html):
	// an <a> carrying href, data-file-name, a class, and inner <span> text.
	// Note the icon's title="PDF-Dokument" lives on the <span>, not the <a> -
	// exactly as in real OPAL HTML and exactly as the browser extractor sees
	// it (it reads getAttribute('title') off the <a>), so neither source gets
	// the icon title. data-file-name is the reliable name source, and
	// deriveFileName falls back to it.
	html := `<table><tr>
		<td><span class="icon" title="PDF-Dokument"></span></td>
		<td><a id="id19cf5" href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53228666883/CourseNode/1615865126729195011/12-st-crc-analysis_notes.pdf" data-file-name="12-st-crc-analysis_notes.pdf">
			<span>12-st-crc-analysis_notes.pdf</span>
		</a></td>
		<td><span>379,3KB</span></td>
		<td><span>am 16.02.2026 um 14:11 Uhr</span></td>
	</tr></table>`

	cands := parseHTTPSectionCandidates(html)
	if len(cands) != 1 {
		t.Fatalf("expected 1 anchor candidate, got %d", len(cands))
	}
	c := cands[0]
	wantHref := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53228666883/CourseNode/1615865126729195011/12-st-crc-analysis_notes.pdf"
	if c["href"] != wantHref {
		t.Errorf("href: got %q want %q", c["href"], wantHref)
	}
	if c["text"] != "12-st-crc-analysis_notes.pdf" {
		t.Errorf("text: got %q want the inner file name", c["text"])
	}
	// title is empty because it sits on the icon <span>, not the <a> - see above.
	if c["title"] != "" {
		t.Errorf("title: got %q, want empty (title is on the span, not the anchor)", c["title"])
	}
}

func TestParseHTTPSectionCandidatesDedupesRepeatedAnchors(t *testing.T) {
	// OPAL's nav/breadcrumb leak repeats the same announcement anchor on many
	// child pages (measured: 12 SW sections all returned the same 3 files).
	// The extractor must collapse exact-duplicate anchors so the downstream
	// fileSeen dedupe isn't the only thing keeping the count sane.
	dup := `<a href="/opal/auth/RepositoryEntry/1/CourseNode/2/x.pdf" data-file-name="x.pdf">x.pdf</a>`
	html := dup + dup + dup
	cands := parseHTTPSectionCandidates(html)
	if len(cands) != 1 {
		t.Fatalf("expected duplicates collapsed to 1, got %d", len(cands))
	}
}

func TestParseHTTPSectionCandidatesIgnoresEmptyAnchors(t *testing.T) {
	// An anchor with no href/onclick/data-* is not a link; skip it rather than
	// emit a candidate that nothing downstream can use.
	html := `<a name="top"></a><a href="/real.pdf">real.pdf</a>`
	cands := parseHTTPSectionCandidates(html)
	if len(cands) != 1 {
		t.Fatalf("expected the empty anchor skipped, got %d candidates", len(cands))
	}
	if cands[0]["href"] != "/real.pdf" {
		t.Errorf("kept the wrong anchor: %q", cands[0]["href"])
	}
}

func TestParseHTTPSectionCandidatesFeedsAppendSectionFiles(t *testing.T) {
	// The whole point of matching the browser extractor's candidate shape: the
	// existing, battle-tested appendSectionFiles must accept HTTP candidates
	// and produce the same FileRef the browser path would. This is the contract
	// that makes the serial hybrid safe - HTTP and browser results merge by the
	// same Path|URL key because they are built by the same code.
	course := CourseRef{RepoID: "53228666883", Title: "Softwaretechnologie (SoSe 26)", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53228666883"}
	section := SectionRef{CourseRepoID: course.RepoID, Title: "Part-3", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/53228666883/CourseNode/1615865126729195011/Part-3"}
	html := `<a href="/opal/auth/RepositoryEntry/53228666883/CourseNode/1/notes.pdf" data-file-name="notes.pdf"><span>notes.pdf</span></a>`
	cands := parseHTTPSectionCandidates(html)

	files := appendSectionFiles(nil, map[string]struct{}{}, cands, course, section, section.URL, "", "", false, "https://bildungsportal.sachsen.de/opal/", nil)
	if len(files) != 1 {
		t.Fatalf("appendSectionFiles produced %d files, want 1", len(files))
	}
	f := files[0]
	if f.Name != "notes.pdf" {
		t.Errorf("Name: got %q want notes.pdf", f.Name)
	}
	// Path is safeCourse/safeName - the merge key half. If HTTP doesn't build
	// the same Path the browser does, dedupe silently fails (duplicates).
	wantPath := "Softwaretechnologie (SoSe 26)/notes.pdf"
	if f.Path != wantPath {
		t.Errorf("Path: got %q want %q", f.Path, wantPath)
	}
	if f.URL == "" {
		t.Errorf("URL should be resolved to an absolute download URL")
	}
}

func TestExtractShowAllURLFromHTML(t *testing.T) {
	// Verbatim shape from Part-3's raw HTML: the Wicket.Ajax.ajax({"u":"..."})
	// block carrying the pager-showAllLink endpoint.
	html := `Wicket.Ajax.ajax({"u":"/opal/auth/RepositoryEntry/53228666883/CourseNode/1615865126729195011/Part-3?1091-1.0-fluidContainer-rowContainer-outerContentContainer-contentContainer-contentPanel-nodePanel-folderPanel-contentPanel-wrapper-tableForm-contentContainer-tableContainer-pager-showAllLink","c":"id19c2e","e":"click"});`
	got := extractShowAllURLFromHTML(html)
	want := "/opal/auth/RepositoryEntry/53228666883/CourseNode/1615865126729195011/Part-3?1091-1.0-fluidContainer-rowContainer-outerContentContainer-contentContainer-contentPanel-nodePanel-folderPanel-contentPanel-wrapper-tableForm-contentContainer-tableContainer-pager-showAllLink"
	if got != want {
		t.Errorf("show-all URL: got %q want %q", got, want)
	}
}

func TestExtractShowAllURLFromHTMLAbsent(t *testing.T) {
	// A non-paginated section has no show-all control; the extractor must say
	// so with "" so the caller doesn't try to fetch a non-existent endpoint.
	if got := extractShowAllURLFromHTML(`<a href="/x.pdf">x.pdf</a>`); got != "" {
		t.Errorf("expected empty show-all URL for non-paginated section, got %q", got)
	}
}

// TestParseHTTPSectionCandidatesAgainstCapturedDump is an opt-in cross-check
// against a real captured section HTML dump, when present. It exists because
// the inline snippets above are necessarily simplified; this reads the actual
// 226KB Part-3 dump the probe produced (tmp/baseline/part3-showall.html) and
// asserts the extractor finds a non-trivial number of file candidates on it -
// the same shape that recovered 33/34 missing files live. Skipped when the
// dump is absent (e.g. CI, fresh clone) so the suite stays hermetic.
func TestParseHTTPSectionCandidatesAgainstCapturedDump(t *testing.T) {
	repo := `C:\07_Arbeitszeug\Open_github\Opal_downloader`
	for _, name := range []string{"part3-showall.html", "part3-raw.html"} {
		path := filepath.Join(repo, "tmp", "baseline", name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Logf("skip %s: %v (not present)", name, err)
			continue
		}
		cands := parseHTTPSectionCandidates(string(data))
		// How many of those candidates look like real file links, via the same
		// predicate appendSectionFiles uses.
		fileCount := 0
		var names []string
		for _, c := range cands {
			target := extractLinkTarget(c["href"], c["onclick"], c["dataHref"], c["dataUrl"])
			name := deriveFileName(c["title"], c["text"], target)
			if looksLikeFileLink(target, name) {
				fileCount++
				names = append(names, name)
			}
		}
		sort.Strings(names)
		t.Logf("%s: %d candidates, %d look like files", name, len(cands), fileCount)
		if fileCount < 10 {
			t.Errorf("%s: expected >=10 file-like candidates from real dump, got %d", name, fileCount)
		}
	}
}
