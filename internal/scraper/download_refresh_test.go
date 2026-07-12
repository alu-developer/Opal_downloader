package scraper

import (
	"errors"
	"strings"
	"testing"
)

// sampleSectionHTMLFixture mirrors the shape of a real OPAL section page's
// HTML confirmed live for queue task fix-fast-path-download-history-counter
// (see download_refresh.go's package doc comment): the file's anchor
// (id + data-file-name), a hover/powertip Wicket AJAX behavior registered on
// the same id (no "e":"click"), and the actual click-triggered behavior
// (same id, "e":"click") - trimmed down from the real captured HTML but
// preserving the exact JSON shapes findClickAjaxURL depends on.
const sampleSectionHTMLFixture = `<html><body>
<table><tr><td>
	<a id="id26674" href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2/ProbeKlausur.pdf" data-file-name="ProbeKlausur.pdf">
		<span>ProbeKlausur.pdf</span>
	</a>
</td></tr></table>
<script type="text/javascript">
(function() {var callback = function (callback) {
var attrs = {"u":"/opal/auth/RepositoryEntry/1/CourseNode/2/Probeklausur?1607-1.0-fluidContainer-tableRow-1-tableCellRepeating-2-tableLink","c":"id26674","i":"veil","sh":[function(attrs, jqXHR, data, textStatus){callback(data)}],"async":false,"wr":false,"dt":"html"};
var params = [];
attrs.ep = params.concat(attrs.ep || []);
Wicket.Ajax.ajax(attrs);
}
;var selector = jQuery("#id26674");selector.data("powertip", function() {	var content = "";	callback(function(data) {		content = data;	});	selector.data("powertip", content);	return content;});selector.powerTip({"placement":"w"});})();;
Wicket.Ajax.ajax({"u":"/opal/auth/RepositoryEntry/1/CourseNode/2/Probeklausur?1607-1.1-fluidContainer-tableRow-1-tableCellRepeating-2-tableLink","c":"id26674","i":"veil","bh":[function(attrs){jQuery(':focus').blur();}],"pre":[function(attrs){return true;}],"e":"click","pd":true});;
</script>
</body></html>`

const sampleAjaxResponseWithDownload = `<?xml version="1.0" encoding="UTF-8"?><ajax-response><evaluate><![CDATA[(function(){try{alterNodeJumps();}catch(e){}})();]]></evaluate><evaluate><![CDATA[(function(){window.location='https:\/\/bildungsportal.sachsen.de\/opal\/downloadering?fibercode=mrhzfvni';})();]]></evaluate></ajax-response>`

const sampleAjaxResponseWithoutDownload = `<ajax-response><redirect><![CDATA[/opal/auth/RepositoryEntry/1/CourseNode/2/Probeklausur?1608]]></redirect></ajax-response>`

func TestFindAnchorTagByDataFileName(t *testing.T) {
	tag := findAnchorTagByDataFileName(sampleSectionHTMLFixture, "ProbeKlausur.pdf")
	if tag == "" {
		t.Fatal("expected to find the anchor tag, got empty string")
	}
	if !strings.Contains(tag, `id="id26674"`) {
		t.Fatalf("anchor tag missing expected id attribute: %s", tag)
	}
	if !strings.Contains(tag, `data-file-name="ProbeKlausur.pdf"`) {
		t.Fatalf("anchor tag missing expected data-file-name attribute: %s", tag)
	}
}

func TestFindAnchorTagByDataFileNameNoMatch(t *testing.T) {
	if tag := findAnchorTagByDataFileName(sampleSectionHTMLFixture, "DoesNotExist.pdf"); tag != "" {
		t.Fatalf("expected no match, got %q", tag)
	}
}

func TestFindAnchorTagByDataFileNameEscapesSpecialChars(t *testing.T) {
	html := `<a id="idX" href="/x" data-file-name="A &amp; B.pdf">A &amp; B.pdf</a>`
	tag := findAnchorTagByDataFileName(html, "A & B.pdf")
	if tag == "" {
		t.Fatal("expected to find the anchor tag for a filename containing '&', got empty string")
	}
}

func TestExtractHTMLAttr(t *testing.T) {
	tag := `<a id="id26674" href="https://example.test/x" data-file-name="ProbeKlausur.pdf">`
	if got := extractHTMLAttr(tag, "id"); got != "id26674" {
		t.Fatalf("extractHTMLAttr(id) = %q, want %q", got, "id26674")
	}
	if got := extractHTMLAttr(tag, "data-file-name"); got != "ProbeKlausur.pdf" {
		t.Fatalf("extractHTMLAttr(data-file-name) = %q, want %q", got, "ProbeKlausur.pdf")
	}
	if got := extractHTMLAttr(tag, "missing"); got != "" {
		t.Fatalf("extractHTMLAttr(missing) = %q, want empty string", got)
	}
}

// TestFindClickAjaxURLSelectsClickTaggedBehavior covers the core risk this
// function exists to guard against: an anchor id commonly has more than one
// Wicket AJAX behavior registered on it (confirmed live - a hover/powertip
// preview alongside the real click-download behavior), and picking the
// wrong one silently breaks the download (the hover behavior's response is
// tooltip HTML, not a download redirect - confirmed live in the
// investigation for this task). This must select the "-1.1-" click-tagged
// "u", not the "-1.0-" hover one, even though both share the same "c" id.
func TestFindClickAjaxURLSelectsClickTaggedBehavior(t *testing.T) {
	got := findClickAjaxURL(sampleSectionHTMLFixture, "id26674")
	want := "/opal/auth/RepositoryEntry/1/CourseNode/2/Probeklausur?1607-1.1-fluidContainer-tableRow-1-tableCellRepeating-2-tableLink"
	if got != want {
		t.Fatalf("findClickAjaxURL = %q, want %q", got, want)
	}
}

func TestFindClickAjaxURLNoMatchForUnknownID(t *testing.T) {
	if got := findClickAjaxURL(sampleSectionHTMLFixture, "id-does-not-exist"); got != "" {
		t.Fatalf("expected no match for an unknown id, got %q", got)
	}
}

// TestFindClickAjaxURLIgnoresNonClickBehavior confirms that an id with only
// a non-click (e.g. hover-only) behavior registered on it correctly reports
// no click URL, rather than incorrectly returning the hover behavior's "u".
func TestFindClickAjaxURLIgnoresNonClickBehavior(t *testing.T) {
	html := `Wicket.Ajax.ajax({"u":"/opal/hover-only?1-0-x","c":"idHoverOnly","i":"veil","sh":[function(){}],"async":false});`
	if got := findClickAjaxURL(html, "idHoverOnly"); got != "" {
		t.Fatalf("expected no click URL for a hover-only behavior, got %q", got)
	}
}

func TestExtractAjaxRedirectTarget(t *testing.T) {
	got := extractAjaxRedirectTarget(sampleAjaxResponseWithDownload)
	want := "https://bildungsportal.sachsen.de/opal/downloadering?fibercode=mrhzfvni"
	if got != want {
		t.Fatalf("extractAjaxRedirectTarget = %q, want %q", got, want)
	}
}

// TestExtractAjaxRedirectTargetNoDownload covers the live-confirmed miss
// case: requesting the same counter-suffixed AJAX URL a second time returns
// a plain <redirect> back to the section page instead of a download target -
// callers must treat this as "no target found", not error out trying to
// parse a URL that isn't there.
func TestExtractAjaxRedirectTargetNoDownload(t *testing.T) {
	if got := extractAjaxRedirectTarget(sampleAjaxResponseWithoutDownload); got != "" {
		t.Fatalf("expected no redirect target for a <redirect>-only response, got %q", got)
	}
}

func TestWicketAjaxHeaders(t *testing.T) {
	opalURL := "https://bildungsportal.sachsen.de/opal/"
	sectionURL := "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2/Probeklausur"
	headers := wicketAjaxHeaders(opalURL, sectionURL)

	if headers["Wicket-Ajax"] != "true" {
		t.Fatalf("Wicket-Ajax header = %q, want %q", headers["Wicket-Ajax"], "true")
	}
	if headers["X-Requested-With"] != "XMLHttpRequest" {
		t.Fatalf("X-Requested-With header = %q, want %q", headers["X-Requested-With"], "XMLHttpRequest")
	}
	wantBase := "auth/RepositoryEntry/1/CourseNode/2/Probeklausur"
	if headers["Wicket-Ajax-BaseURL"] != wantBase {
		t.Fatalf("Wicket-Ajax-BaseURL header = %q, want %q", headers["Wicket-Ajax-BaseURL"], wantBase)
	}
}

// fakeWicketFetcher builds a wicketFetcher backed by a map from URL to a
// canned (status, body, err) response, for driving refreshCounterURLPure in
// tests without a live Playwright APIRequestContext.
type fakeWicketResponse struct {
	status int
	body   string
	err    error
}

func fakeWicketFetcher(responses map[string]fakeWicketResponse) wicketFetcher {
	return func(url string, headers map[string]string) (int, string, error) {
		resp, ok := responses[url]
		if !ok {
			return 0, "", errors.New("no fake response configured for " + url)
		}
		return resp.status, resp.body, resp.err
	}
}

const opalURLFixture = "https://bildungsportal.sachsen.de/opal/"
const sectionURLFixture = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2/Probeklausur"
const clickAjaxURLFixture = "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/1/CourseNode/2/Probeklausur?1607-1.1-fluidContainer-tableRow-1-tableCellRepeating-2-tableLink"

// TestRefreshCounterURLPureSuccess exercises the full happy path: re-fetch
// section -> find anchor -> find click behavior -> invoke it -> extract the
// downloadering URL.
func TestRefreshCounterURLPureSuccess(t *testing.T) {
	fetch := fakeWicketFetcher(map[string]fakeWicketResponse{
		sectionURLFixture:   {status: 200, body: sampleSectionHTMLFixture},
		clickAjaxURLFixture: {status: 200, body: sampleAjaxResponseWithDownload},
	})

	got, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, "ProbeKlausur.pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "https://bildungsportal.sachsen.de/opal/downloadering?fibercode=mrhzfvni"
	if got != want {
		t.Fatalf("refreshCounterURLPure = %q, want %q", got, want)
	}
}

func TestRefreshCounterURLPureGuardsEmptyInputs(t *testing.T) {
	fetch := fakeWicketFetcher(nil)

	if _, err := refreshCounterURLPure(fetch, opalURLFixture, "", "ProbeKlausur.pdf"); err == nil {
		t.Fatal("expected an error for an empty section URL")
	}
	if _, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, ""); err == nil {
		t.Fatal("expected an error for empty link text")
	}
}

func TestRefreshCounterURLPureSectionFetchError(t *testing.T) {
	fetch := fakeWicketFetcher(map[string]fakeWicketResponse{
		sectionURLFixture: {err: errors.New("network error")},
	})
	if _, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, "ProbeKlausur.pdf"); err == nil {
		t.Fatal("expected an error when the section page fetch fails")
	}
}

func TestRefreshCounterURLPureSectionNon200(t *testing.T) {
	fetch := fakeWicketFetcher(map[string]fakeWicketResponse{
		sectionURLFixture: {status: 404, body: "not found"},
	})
	if _, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, "ProbeKlausur.pdf"); err == nil {
		t.Fatal("expected an error for a non-200 section page fetch")
	}
}

func TestRefreshCounterURLPureAnchorNotFound(t *testing.T) {
	fetch := fakeWicketFetcher(map[string]fakeWicketResponse{
		sectionURLFixture: {status: 200, body: sampleSectionHTMLFixture},
	})
	if _, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, "SomeOtherFile.pdf"); err == nil {
		t.Fatal("expected an error when the file's anchor isn't present on the refreshed page")
	}
}

func TestRefreshCounterURLPureNoClickBehavior(t *testing.T) {
	html := `<a id="idX" href="/x" data-file-name="NoClick.pdf">NoClick.pdf</a>`
	fetch := fakeWicketFetcher(map[string]fakeWicketResponse{
		sectionURLFixture: {status: 200, body: html},
	})
	if _, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, "NoClick.pdf"); err == nil {
		t.Fatal("expected an error when the anchor has no click-triggered AJAX behavior")
	}
}

func TestRefreshCounterURLPureAjaxInvokeError(t *testing.T) {
	fetch := fakeWicketFetcher(map[string]fakeWicketResponse{
		sectionURLFixture:   {status: 200, body: sampleSectionHTMLFixture},
		clickAjaxURLFixture: {err: errors.New("network error")},
	})
	if _, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, "ProbeKlausur.pdf"); err == nil {
		t.Fatal("expected an error when invoking the click AJAX behavior fails")
	}
}

func TestRefreshCounterURLPureAjaxNon200(t *testing.T) {
	fetch := fakeWicketFetcher(map[string]fakeWicketResponse{
		sectionURLFixture:   {status: 200, body: sampleSectionHTMLFixture},
		clickAjaxURLFixture: {status: 400, body: "bad request"},
	})
	if _, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, "ProbeKlausur.pdf"); err == nil {
		t.Fatal("expected an error for a non-200 AJAX behavior response")
	}
}

// TestRefreshCounterURLPureNoRedirectTarget covers the live-confirmed case
// where the AJAX behavior responds with a plain <redirect> (e.g. because the
// exact counter-suffixed URL was already consumed once) instead of a
// download target.
func TestRefreshCounterURLPureNoRedirectTarget(t *testing.T) {
	fetch := fakeWicketFetcher(map[string]fakeWicketResponse{
		sectionURLFixture:   {status: 200, body: sampleSectionHTMLFixture},
		clickAjaxURLFixture: {status: 200, body: sampleAjaxResponseWithoutDownload},
	})
	if _, err := refreshCounterURLPure(fetch, opalURLFixture, sectionURLFixture, "ProbeKlausur.pdf"); err == nil {
		t.Fatal("expected an error when the AJAX response has no download redirect target")
	}
}
