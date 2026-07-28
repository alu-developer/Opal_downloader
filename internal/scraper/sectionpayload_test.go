package scraper

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The round trip has to be exact. Anything the cache hands back that differs
// from what a crawl would have produced makes the cached path behave
// differently from the crawled one, which is a divergence nobody would see
// until files went missing.
func TestSectionPayloadRoundTripsExactly(t *testing.T) {
	const rootText = "Material Skript.pdf 1,2 MB Uebung.pdf 900 KB"
	original := []map[string]string{
		{"href": "/opal/a/Skript.pdf", "text": "Skript.pdf", "rowText": "Skript.pdf 1,2 MB", "rootText": rootText, "className": "row", "title": "Skript"},
		{"href": "/opal/a/Uebung.pdf", "text": "Uebung.pdf", "rowText": "Uebung.pdf 900 KB", "rootText": rootText, "onclick": "go()", "dataUrl": "/x"},
	}

	// Through JSON, because that is how it really travels.
	body, err := json.Marshal(packSection(original, "https://example/show-all", "https://example/expanded?7", true))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var packed cachedSection
	if err := json.Unmarshal(body, &packed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	gotCandidates, gotShowAllURL, gotExpandedPageURL, gotExpandedShowAll := unpackSection(packed)
	if !reflect.DeepEqual(gotCandidates, original) {
		t.Fatalf("round trip changed the candidates:\n got: %#v\nwant: %#v", gotCandidates, original)
	}
	if gotShowAllURL != "https://example/show-all" || gotExpandedPageURL != "https://example/expanded?7" || !gotExpandedShowAll {
		t.Fatalf("round trip changed the show-all fields: url=%q expanded=%q flag=%v", gotShowAllURL, gotExpandedPageURL, gotExpandedShowAll)
	}
}

// The point of the exercise: rootText is stored once, not once per candidate.
// Storing it per candidate was 31 MB of a 52 MB file.
func TestRootTextIsStoredOnce(t *testing.T) {
	const rootText = "a very long section body repeated across every candidate"
	candidates := make([]map[string]string, 50)
	for i := range candidates {
		candidates[i] = map[string]string{"href": "/x", "rootText": rootText}
	}

	packed := packSection(candidates, "", "", false)
	if packed.RootText != rootText {
		t.Fatalf("rootText was not lifted out, got %q", packed.RootText)
	}
	for i, c := range packed.Candidates {
		if _, present := c[rootTextKey]; present {
			t.Fatalf("candidate %d still carries rootText", i)
		}
	}

	body, _ := json.Marshal(packed)
	naive, _ := json.Marshal(candidates)
	if len(body) >= len(naive)/2 {
		t.Fatalf("interning saved almost nothing: %d bytes vs %d naive", len(body), len(naive))
	}
}

// A section with no candidates must survive as an empty list, not as nil -
// sectioncache treats "nothing recorded" and "recorded zero" differently on
// purpose, and collapsing them would re-crawl every empty section forever.
func TestEmptySectionPacksToAnEmptyList(t *testing.T) {
	packed := packSection(nil, "", "", false)
	if packed.Candidates == nil {
		t.Fatal("an empty section packed to nil, which is indistinguishable from nothing recorded")
	}
	body, err := json.Marshal(packed)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) == "null" {
		t.Fatalf("packed to JSON null: %s", body)
	}
	if got, _, _, _ := unpackSection(packed); len(got) != 0 {
		t.Fatalf("expected no candidates back, got %d", len(got))
	}
}
