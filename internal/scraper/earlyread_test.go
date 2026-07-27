package scraper

import "testing"

// canonicalCandidates is the whole comparison, so it is the thing worth
// getting wrong quietly. A count would have called all four of these cases
// equal.
func TestCanonicalCandidatesComparesRowsNotCounts(t *testing.T) {
	base := []map[string]string{
		{"name": "Folien.pdf", "href": "/a"},
		{"name": "Uebung.pdf", "href": "/b"},
	}

	t.Run("reordering is not a difference", func(t *testing.T) {
		shuffled := []map[string]string{base[1], base[0]}
		if canonicalCandidates(base) != canonicalCandidates(shuffled) {
			t.Fatal("row order changed the verdict; a reordering is not a loss")
		}
	})

	t.Run("a changed field is a difference", func(t *testing.T) {
		altered := []map[string]string{
			{"name": "Folien.pdf", "href": "/a"},
			{"name": "Uebung.pdf", "href": "/DIFFERENT"},
		}
		if canonicalCandidates(base) == canonicalCandidates(altered) {
			t.Fatal("a changed href read as identical - this is exactly the silent loss the probe exists to catch")
		}
	})

	t.Run("a missing row is a difference", func(t *testing.T) {
		if canonicalCandidates(base) == canonicalCandidates(base[:1]) {
			t.Fatal("a dropped row read as identical")
		}
	})

	// The case a count cannot see at all: same number of rows, different rows.
	t.Run("a swapped row with the same count is a difference", func(t *testing.T) {
		swapped := []map[string]string{
			{"name": "Folien.pdf", "href": "/a"},
			{"name": "SomethingElse.pdf", "href": "/c"},
		}
		if len(swapped) != len(base) {
			t.Fatal("test setup: counts must match for this case to mean anything")
		}
		if canonicalCandidates(base) == canonicalCandidates(swapped) {
			t.Fatal("a swapped row with an unchanged count read as identical")
		}
	})
}

// The probe must be completely inert when off - it runs inside the crawl's
// hot path, and a probe that costs something is a probe that changes what it
// measures.
func TestEarlyReadProbeIsInertWhenDisabled(t *testing.T) {
	t.Setenv(earlyReadProbeEnv, "")
	p := newEarlyReadProbe()
	if p.enabled {
		t.Fatal("probe must default to off")
	}
	p.compare("s", "u", nil, []map[string]string{{"name": "x"}})
	if len(p.results) != 0 {
		t.Fatalf("a disabled probe recorded %d results", len(p.results))
	}
	// Must survive being nil too, since that is how a zero-value scraper in
	// other tests reaches it.
	var nilProbe *earlyReadProbe
	nilProbe.compare("s", "u", nil, nil)
	nilProbe.report()
}
