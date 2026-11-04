package scraper

import "testing"

const testOpalURL = "https://bildungsportal.sachsen.de/opal/"

// liveSinglePageSectionHTML is the iframe wrapper OPAL rendered for the
// maintainer's AN30 course node "Infos zur Vorlesung", captured verbatim
// 2026-11-04 (attributes and order unchanged, the long sandbox/onload values
// shortened only where they contain nothing this parser reads). The point of
// keeping it in this shape is that src is neither the first nor the last
// attribute, so an attribute-order assumption would fail here.
const liveSinglePageSectionHTML = `<div class="iframe-container">
<iframe width="100%" style="height: 600px;" allow="fullscreen" webkitallowfullscreen="true" sandbox="allow-scripts allow-downloads allow-same-origin" id="id7a4a" src="/opal/FolderResource/56300208134/infos-zum-ablauf.html?1793788446113" title="Infos zur Vorlesung" onload="var iframe = $(&#039;#id7a4a&#039;);"></iframe>
</div>`

// liveEmbeddedPageHTML is an excerpt of the document behind that iframe,
// captured the same day. Every property this parser has to cope with is in
// here and was in the real page: the hrefs are relative to the document's own
// directory, the same PDF is linked once under its file name and again as
// prose ("Übungsblatt 0"), and off-host links sit in between.
const liveEmbeddedPageHTML = `<p>Übungsblätter:</p>
<a href="MINT-2026-uebung00-aufgaben.pdf">MINT-2026-uebung00-aufgaben.pdf</a>
<a href="MINT-2026-uebung00-loesungen.pdf">MINT-2026-uebung00-loesungen.pdf</a>
<a href="MINT-2026-uebung01-aufgaben.pdf" target="_blank" rel="noopener">MINT-2026-uebung01-aufgaben.pdf</a>
<a href="MINT-2026-uebung02-aufgaben.pdf">MINT-2026-uebung02-aufgaben.pdf</a>
<p>Das Buch finden Sie <a href="https://katalog.slub-dresden.de/id/0-1611803063" target="_blank" rel="noopener">hier</a>.</p>
<p><a href="https://www.youtube.com/watch?v=5UIF0PiPeHk" target="_blank" rel="noopener">Video</a></p>
<p>Zum <a href="MINT-2026-uebung00-aufgaben.pdf" target="_blank" rel="noopener">Übungsblatt 0</a></p>
<p><a href="https://cloudstore.zih.tu-dresden.de/index.php/s/ae553B8fkZ8FxY6">Cloudstore</a></p>`

func TestExtractEmbeddedPageURLsFindsSinglePageDocument(t *testing.T) {
	got := extractEmbeddedPageURLs(liveSinglePageSectionHTML, testOpalURL)
	want := "https://bildungsportal.sachsen.de/opal/FolderResource/56300208134/infos-zum-ablauf.html?1793788446113"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("extractEmbeddedPageURLs = %q, want exactly [%q]", got, want)
	}
}

// Only OPAL-hosted HTML documents may be fetched: anything else is either not
// content (a player/widget iframe) or would send this account's session to a
// third party.
func TestExtractEmbeddedPageURLsRejectsNonContentIframes(t *testing.T) {
	cases := map[string]string{
		"off-host page":   `<iframe src="https://www.youtube.com/embed/5UIF0PiPeHk.html"></iframe>`,
		"off-host player": `<iframe src="https://player.vimeo.com/video/123"></iframe>`,
		"script resource": `<iframe src="/opal/raw/20261027/js/app/script.min.js"></iframe>`,
		"no extension":    `<iframe src="/opal/auth/RepositoryEntry/56300208134"></iframe>`,
		"no src":          `<iframe name="hidden"></iframe>`,
	}
	for name, html := range cases {
		if got := extractEmbeddedPageURLs(html, testOpalURL); len(got) != 0 {
			t.Errorf("%s: extractEmbeddedPageURLs = %q, want none", name, got)
		}
	}
}

// The regression this whole file exists for: the four sheets of AN30's
// "Infos zur Vorlesung", which a course-tree crawl reports as zero files.
func TestEmbeddedPageYieldsProseLinkedFiles(t *testing.T) {
	pageURL := "https://bildungsportal.sachsen.de/opal/FolderResource/56300208134/infos-zum-ablauf.html?1793788446113"
	course := CourseRef{RepoID: "56300208134", Title: "TUDMATH WiSe 2026/27 Ba AN30: Analysis - Maß und Integral"}
	section := SectionRef{CourseRepoID: "56300208134", Title: "Infos zur Vorlesung", URL: "https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/56300208134/CourseNode/1790131225021725003"}

	candidates := parseEmbeddedPageCandidates(liveEmbeddedPageHTML, pageURL)
	files := appendSectionFiles(nil, map[string]struct{}{}, candidates, course, section, pageURL, "", "", false, testOpalURL, nil)

	want := map[string]string{
		"MINT-2026-uebung00-aufgaben.pdf":  "https://bildungsportal.sachsen.de/opal/FolderResource/56300208134/MINT-2026-uebung00-aufgaben.pdf",
		"MINT-2026-uebung00-loesungen.pdf": "https://bildungsportal.sachsen.de/opal/FolderResource/56300208134/MINT-2026-uebung00-loesungen.pdf",
		"MINT-2026-uebung01-aufgaben.pdf":  "https://bildungsportal.sachsen.de/opal/FolderResource/56300208134/MINT-2026-uebung01-aufgaben.pdf",
		"MINT-2026-uebung02-aufgaben.pdf":  "https://bildungsportal.sachsen.de/opal/FolderResource/56300208134/MINT-2026-uebung02-aufgaben.pdf",
	}
	if len(files) != len(want) {
		t.Fatalf("got %d files, want %d: %+v", len(files), len(want), fileNamesFromHTTPFiles(files))
	}
	for _, f := range files {
		wantURL, ok := want[f.Name]
		if !ok {
			t.Errorf("unexpected file %q (%s)", f.Name, f.URL)
			continue
		}
		if f.URL != wantURL {
			t.Errorf("%s: URL = %q, want %q", f.Name, f.URL, wantURL)
		}
		if f.SectionTitle != "Infos zur Vorlesung" {
			t.Errorf("%s: SectionTitle = %q, want the section's own title", f.Name, f.SectionTitle)
		}
	}
}

// A prose link's text is not the file name. The same PDF is linked as
// "Übungsblatt 0" further down the page, and whichever anchor won the dedupe
// decided the saved name - so the name has to come from the URL, not the
// sentence.
func TestEmbeddedPageNamesFilesFromURLNotLinkText(t *testing.T) {
	pageURL := "https://bildungsportal.sachsen.de/opal/FolderResource/56300208134/infos-zum-ablauf.html"
	html := `<p>Zum <a href="MINT-2026-uebung00-aufgaben.pdf" target="_blank">Übungsblatt 0</a> geht es hier.</p>`

	candidates := parseEmbeddedPageCandidates(html, pageURL)
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	course := CourseRef{RepoID: "56300208134", Title: "AN30"}
	files := appendSectionFiles(nil, map[string]struct{}{}, candidates, course, SectionRef{CourseRepoID: "56300208134"}, pageURL, "", "", false, testOpalURL, nil)
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1", len(files))
	}
	if files[0].Name != "MINT-2026-uebung00-aufgaben.pdf" {
		t.Fatalf("Name = %q, want the URL's basename (the link text was %q)", files[0].Name, "Übungsblatt 0")
	}
}

// Percent-encoded names must come out decoded, since that is the name the
// file is saved under.
func TestEmbeddedPageDecodesPercentEncodedNames(t *testing.T) {
	pageURL := "https://bildungsportal.sachsen.de/opal/FolderResource/1/page.html"
	html := `<a href="%C3%9Cbung%2001.pdf">hier</a>`

	candidates := parseEmbeddedPageCandidates(html, pageURL)
	course := CourseRef{RepoID: "1", Title: "Kurs"}
	files := appendSectionFiles(nil, map[string]struct{}{}, candidates, course, SectionRef{CourseRepoID: "1"}, pageURL, "", "", false, testOpalURL, nil)
	if len(files) != 1 || files[0].Name != "Übung 01.pdf" {
		t.Fatalf("got %+v, want one file named %q", fileNamesFromHTTPFiles(files), "Übung 01.pdf")
	}
}

// The external links a prose page is full of must not become files. They are
// dropped by isFileURLAllowedForCourse's host check, and this pins that the
// embedded-page path really does route through it.
func TestEmbeddedPageDropsOffHostLinks(t *testing.T) {
	pageURL := "https://bildungsportal.sachsen.de/opal/FolderResource/56300208134/infos-zum-ablauf.html"
	candidates := parseEmbeddedPageCandidates(liveEmbeddedPageHTML, pageURL)
	course := CourseRef{RepoID: "56300208134", Title: "AN30"}
	files := appendSectionFiles(nil, map[string]struct{}{}, candidates, course, SectionRef{CourseRepoID: "56300208134"}, pageURL, "", "", false, testOpalURL, nil)
	for _, f := range files {
		if !isSameHostAsOpal(f.URL, testOpalURL) {
			t.Errorf("off-host link became a file: %q (%s)", f.Name, f.URL)
		}
	}
}

// A section page with no iframe must cost no extra fetch at all - the common
// case, and the thing that keeps this from adding a request per section.
func TestFetchEmbeddedPagesIssuesNoRequestWithoutIframe(t *testing.T) {
	pages, requests := fetchEmbeddedPages(nil, liveSectionRowHTML, testOpalURL, nil)
	if requests != 0 || pages != nil {
		t.Fatalf("got %d pages / %d requests, want 0/0 for a page with no embedded document", len(pages), requests)
	}
}
