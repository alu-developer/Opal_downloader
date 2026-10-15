package scraper

import "testing"

// liveSectionRowHTML is a verbatim excerpt of a real OPAL section page,
// captured 2026-10-15 from the Analysis course's "Übungsblätter" section.
// Kept byte-for-byte (including the odd indentation and the empty lines
// OPAL's renderer emits) because the whole point of this test is that the
// size and date columns are reached in the shape the server actually sends,
// not in a tidied-up approximation of it.
const liveSectionRowHTML = `<table>
<tr>
	<td class="table-check">
		<input type="checkbox" id="idc9" name="contentContainer:tableContainer:multiSelect:multiSelectGroup" value="check5" aria-labelledby="idc8"/>
		<label class="visually-hidden" id="idc8" for="idc9">Diesen Eintrag auswählen</label>
	</td>
	<td>
	<span class="fonticon pdf" aria-hidden="true" title="PDF-Dokument"></span>
	<span class="visually-hidden">PDF-Dokument</span>
	</td><td>
		<a id="id115" href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50999590912/CourseNode/1757212677096580003/Analysis01.pdf" data-file-name="Analysis01.pdf">
			<span>Analysis01.pdf</span>
		</a>
	</td><td>
	<span>223,7KB</span>
	</td><td>
	<span>am 03.10.2025 um 11:07 Uhr</span>
	</td>
</tr>
<tr>
	<td><a id="id116" href="https://bildungsportal.sachsen.de/opal/auth/RepositoryEntry/50999590912/CourseNode/1757212677096580003/Analysis02.pdf" data-file-name="Analysis02.pdf"><span>Analysis02.pdf</span></a></td>
	<td><span>185,9KB</span></td>
	<td><span>am 04.10.2025 um 13:45 Uhr</span></td>
</tr>
</table>`

func candidateByFileName(t *testing.T, candidates []map[string]string, name string) map[string]string {
	t.Helper()
	for _, c := range candidates {
		if c["text"] == name {
			return c
		}
	}
	t.Fatalf("no candidate with text %q among %d candidates", name, len(candidates))
	return nil
}

// TestParseHTTPSectionCandidatesCarriesRowSizeAndDate is the regression test
// for the bug that made every sync re-download every file: HTTP-first
// discovery used the anchor's own text as rowText, so the size/date columns -
// which live in *sibling* <td> cells - were never seen, every file came back
// with Size==nil and Modified==nil, and syncer.needsContentVerification
// turned the lot into byte-compare re-downloads on every single run.
func TestParseHTTPSectionCandidatesCarriesRowSizeAndDate(t *testing.T) {
	candidates := parseHTTPSectionCandidates(liveSectionRowHTML)

	first := candidateByFileName(t, candidates, "Analysis01.pdf")
	if got := parseRowSizeBytes(first["rowText"]); got == nil {
		t.Fatalf("Analysis01.pdf: no size parsed from rowText %q", first["rowText"])
		// 223,7 KB * 1024, truncated the way parseRowSizeBytes truncates.
	} else if want := int64(229068); *got != want {
		t.Errorf("Analysis01.pdf size = %d, want %d", *got, want)
	}
	if got := parseRowModified(first["rowText"]); got == nil {
		t.Fatalf("Analysis01.pdf: no modified date parsed from rowText %q", first["rowText"])
	} else if *got != "03.10.2025 um 11:07 Uhr" {
		t.Errorf("Analysis01.pdf modified = %q, want %q", *got, "03.10.2025 um 11:07 Uhr")
	}

	// The second row must get its own row's numbers, not the first row's -
	// the failure mode of scoping rowText too widely.
	second := candidateByFileName(t, candidates, "Analysis02.pdf")
	if got := parseRowSizeBytes(second["rowText"]); got == nil {
		t.Fatalf("Analysis02.pdf: no size parsed from rowText %q", second["rowText"])
		// 185,9 KB * 1024, truncated.
	} else if want := int64(190361); *got != want {
		t.Errorf("Analysis02.pdf size = %d, want %d", *got, want)
	}
	if got := parseRowModified(second["rowText"]); got == nil || *got != "04.10.2025 um 13:45 Uhr" {
		t.Errorf("Analysis02.pdf modified = %v, want %q", got, "04.10.2025 um 13:45 Uhr")
	}
}

// TestParseHTTPSectionCandidatesSkipsAmbiguousRows holds the conservative
// half of the fix: a row carrying two file anchors cannot say which file the
// single size/date column belongs to, so both must fall back to the anchor's
// own text (and therefore to a nil size/date). A wrong signal is worse than
// none - it would make a genuinely changed file look unchanged forever,
// whereas nil only costs the re-download that already happens today.
func TestParseHTTPSectionCandidatesSkipsAmbiguousRows(t *testing.T) {
	html := `<table><tr>
		<td><a href="/opal/auth/x/A.pdf" data-file-name="A.pdf"><span>A.pdf</span></a></td>
		<td><a href="/opal/auth/x/B.pdf" data-file-name="B.pdf"><span>B.pdf</span></a></td>
		<td><span>500,0KB</span></td>
		<td><span>am 01.01.2026 um 10:00 Uhr</span></td>
	</tr></table>`

	for _, c := range parseHTTPSectionCandidates(html) {
		if parseRowSizeBytes(c["rowText"]) != nil {
			t.Errorf("%s: got a size from an ambiguous two-anchor row (rowText %q)", c["text"], c["rowText"])
		}
	}
}

// TestParseHTTPSectionCandidatesAnchorOutsideAnyRow covers the links that are
// not in a file table at all (breadcrumbs, the show-all pager, folder
// targets): they must keep behaving exactly as before the row scoping, with
// rowText equal to the anchor's own text.
func TestParseHTTPSectionCandidatesAnchorOutsideAnyRow(t *testing.T) {
	html := `<div><a href="/opal/auth/RepositoryEntry/1" class="pager-showall">Alle anzeigen</a></div>`

	candidates := parseHTTPSectionCandidates(html)
	if len(candidates) != 1 {
		t.Fatalf("got %d candidates, want 1", len(candidates))
	}
	if got := candidates[0]["rowText"]; got != "Alle anzeigen" {
		t.Errorf("rowText = %q, want the anchor's own text %q", got, "Alle anzeigen")
	}
}
