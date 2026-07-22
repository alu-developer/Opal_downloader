package foldersuggest

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"
)

// realisticTree is the folder layout this feature exists for: a download
// root holding semester folders, holding one folder per course, each with a
// "Downloads" leaf that is where files actually belong.
var realisticTree = []string{
	`_2. Semester/Lineare Algebra/Downloads`,
	`_2. Semester/AlgData/Downloads`,
	`_2. Semester/Analysis/Downloads`,
	`_2. Semester/Analysis/Übung`,
	`_2. Semester/Ma-Prog/Downloads`,
	`_4. Semester/SoTech/Downloads`,
	`_4. Semester/NuMa/Downloads`,
	`Sonstiges`,
	`Steuer/2025`,
}

func makeTree(t *testing.T, dirs []string) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("could not create %s: %v", dir, err)
		}
	}
	return root
}

func candidateFor(t *testing.T, candidates []Candidate, suffix string) string {
	t.Helper()
	want := filepath.FromSlash(suffix)
	for _, c := range candidates {
		if len(c.Path) >= len(want) && c.Path[len(c.Path)-len(want):] == want {
			return c.Path
		}
	}
	t.Fatalf("no candidate ending in %s; got %v", want, candidates)
	return ""
}

// TestScanPrefersTheDownloadsLeaf covers the "…/Downloads" convention: the
// course is identified by the folder named after it, but the destination
// offered is the downloads folder inside it, and the parent is not offered
// separately (two candidates meaning the same course would only ever
// deadlock the margin rule).
func TestScanPrefersTheDownloadsLeaf(t *testing.T) {
	root := makeTree(t, realisticTree)

	candidates, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	var algDataPaths []string
	for _, c := range candidates {
		if c.MatchName == "AlgData" {
			algDataPaths = append(algDataPaths, c.Path)
		}
	}
	if len(algDataPaths) != 1 {
		t.Fatalf("expected exactly one AlgData candidate, got %v", algDataPaths)
	}
	if filepath.Base(algDataPaths[0]) != "Downloads" {
		t.Errorf("AlgData candidate = %s, want the Downloads leaf", algDataPaths[0])
	}

	// A course folder without a Downloads leaf is still offered as itself.
	found := false
	for _, c := range candidates {
		if c.MatchName == "Sonstiges" && filepath.Base(c.Path) == "Sonstiges" {
			found = true
		}
	}
	if !found {
		t.Error("expected a plain folder with no Downloads leaf to be a candidate")
	}
}

func TestScanRespectsDepthAndBudget(t *testing.T) {
	root := makeTree(t, []string{`a/b/c/d/e`})

	candidates, err := ScanWith(root, Options{MaxDepth: 3})
	if err != nil {
		t.Fatalf("ScanWith: %v", err)
	}
	if len(candidates) != 3 {
		t.Errorf("depth 3 should yield 3 candidates, got %d (%v)", len(candidates), candidates)
	}

	limited, err := ScanWith(root, Options{MaxDepth: 3, MaxDirs: 2})
	if err != nil {
		t.Fatalf("ScanWith: %v", err)
	}
	if len(limited) != 2 {
		t.Errorf("a budget of 2 should yield 2 candidates, got %d", len(limited))
	}
}

func TestScanRejectsMissingRoot(t *testing.T) {
	if _, err := Scan(""); err == nil {
		t.Error("expected an error for an empty download path")
	}
	if _, err := Scan(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("expected an error for a download path that does not exist")
	}
}

// TestSuggestOverRealisticTree is the end-to-end case: the six course titles
// this feature was designed against, against the folder tree they belong in.
func TestSuggestOverRealisticTree(t *testing.T) {
	root := makeTree(t, realisticTree)
	candidates, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	courses := []string{
		"2026 LA20",
		"Algorithmen und Datenstrukturen",
		"Analysis",
		"So26 Programmieren - Weiterführende Konzepte (Math-Ba-PR20)",
		"Softwaretechnologie (SoSe 26)",
		"TUDMATH SoSe2026 Modul Math-Ba-NM20: Numerische Mathematik - Iterationsverfahren",
	}
	want := map[string]string{
		"2026 LA20":                       candidateFor(t, candidates, `_2. Semester/Lineare Algebra/Downloads`),
		"Algorithmen und Datenstrukturen": candidateFor(t, candidates, `_2. Semester/AlgData/Downloads`),
		"Analysis":                        candidateFor(t, candidates, `_2. Semester/Analysis/Downloads`),
		"So26 Programmieren - Weiterführende Konzepte (Math-Ba-PR20)":                      candidateFor(t, candidates, `_2. Semester/Ma-Prog/Downloads`),
		"Softwaretechnologie (SoSe 26)":                                                    candidateFor(t, candidates, `_4. Semester/SoTech/Downloads`),
		"TUDMATH SoSe2026 Modul Math-Ba-NM20: Numerische Mathematik - Iterationsverfahren": candidateFor(t, candidates, `_4. Semester/NuMa/Downloads`),
	}

	got := Suggest(courses, candidates, nil)
	for course, wantPath := range want {
		if got[course] != wantPath {
			t.Errorf("Suggest[%q] = %q, want %q", course, got[course], wantPath)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d suggestions, want %d: %v", len(got), len(want), got)
	}
}

// TestSuggestWithholdsWhenAmbiguous is the rule that makes the feature safe
// to trust: two equally good folders both in current use produce no
// suggestion at all, rather than a coin flip the user has no reason to
// double-check. Both are touched now, so recency cannot break the tie.
func TestSuggestWithholdsWhenAmbiguous(t *testing.T) {
	root := makeTree(t, []string{
		`_2. Semester/Analysis/Downloads`,
		`_4. Semester/Analysis/Downloads`,
	})
	candidates, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Freshly created dirs share a modtime, so this is a genuine tie.

	if got := Suggest([]string{"Analysis"}, candidates, nil); len(got) != 0 {
		t.Errorf("expected no suggestion for an ambiguous match, got %v", got)
	}
}

// TestSuggestPrefersTheRecentlyUsedFolder is the tie-break that made the real
// tree work: an identically-named folder from an earlier semester, sitting
// untouched for months, must lose to the one being worked on now. Names alone
// score both identically.
func TestSuggestPrefersTheRecentlyUsedFolder(t *testing.T) {
	root := makeTree(t, []string{
		`_1. Semester/Analysis/Downloads`,
		`_2. Semester/Analysis/Downloads`,
	})
	old := filepath.Join(root, filepath.FromSlash(`_1. Semester/Analysis/Downloads`))
	stale := time.Now().Add(-90 * 24 * time.Hour)
	// The whole old course folder is what ages, mirroring a semester left behind.
	for _, p := range []string{old, filepath.Dir(old)} {
		if err := os.Chtimes(p, stale, stale); err != nil {
			t.Fatalf("chtimes %s: %v", p, err)
		}
	}

	candidates, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := Suggest([]string{"Analysis"}, candidates, nil)
	want := candidateFor(t, candidates, `_2. Semester/Analysis/Downloads`)
	if got["Analysis"] != want {
		t.Errorf("Suggest[Analysis] = %q, want the recently-used %q", got["Analysis"], want)
	}
}

// TestSuggestPrefersTheDownloadsLeafOverABareMatch covers the structural
// tie-break that ranks above recency: when one near-tie is a "…/Downloads"
// leaf and another is a bare folder of the same name, the convention wins
// even though names (and here, ages) do not separate them.
func TestSuggestPrefersTheDownloadsLeafOverABareMatch(t *testing.T) {
	root := makeTree(t, []string{
		`Alt/AlgData`,               // bare folder, no Downloads leaf
		`Aktuell/AlgData/Downloads`, // the convention
	})
	candidates, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := Suggest([]string{"Algorithmen und Datenstrukturen"}, candidates, nil)
	want := candidateFor(t, candidates, `Aktuell/AlgData/Downloads`)
	if got["Algorithmen und Datenstrukturen"] != want {
		t.Errorf("Suggest = %q, want the Downloads leaf %q", got["Algorithmen und Datenstrukturen"], want)
	}
}

// TestSuggestIgnoresDumpingGround covers the Scan Ignore option: the folder
// opal-downloader itself fills with "<default>/<exact course name>" scores a
// perfect match and would shadow the real, abbreviated folder. Excluding its
// subtree is what took the real tree from 0/6 to a clean result.
func TestSuggestIgnoresDumpingGround(t *testing.T) {
	root := makeTree(t, []string{
		`Default_downloads/Algorithmen und Datenstrukturen`,
		`_2. Semester/AlgData/Downloads`,
	})

	candidates, err := ScanWith(root, Options{Ignore: []string{"Default_downloads"}})
	if err != nil {
		t.Fatalf("ScanWith: %v", err)
	}
	for _, c := range candidates {
		if filepath.Base(filepath.Dir(c.Path)) == "Default_downloads" || filepath.Base(c.Path) == "Default_downloads" {
			t.Fatalf("ignored subtree still produced a candidate: %s", c.Path)
		}
	}

	got := Suggest([]string{"Algorithmen und Datenstrukturen"}, candidates, nil)
	want := candidateFor(t, candidates, `_2. Semester/AlgData/Downloads`)
	if got["Algorithmen und Datenstrukturen"] != want {
		t.Errorf("Suggest = %q, want the real folder %q", got["Algorithmen und Datenstrukturen"], want)
	}
}

func TestSuggestWithholdsWhenNothingMatches(t *testing.T) {
	root := makeTree(t, []string{`Steuer`, `Urlaubsfotos`, `Bewerbungen`})
	candidates, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	if got := Suggest([]string{"Analysis", "Softwaretechnologie (SoSe 26)"}, candidates, nil); len(got) != 0 {
		t.Errorf("expected no suggestions against an unrelated tree, got %v", got)
	}
}

// TestSuggestSkipsTakenFolders keeps a suggestion from pointing a second
// course at a folder the user has already assigned to a first one.
func TestSuggestSkipsTakenFolders(t *testing.T) {
	root := makeTree(t, realisticTree)
	candidates, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	taken := candidateFor(t, candidates, `_2. Semester/AlgData/Downloads`)

	got := Suggest([]string{"Algorithmen und Datenstrukturen"}, candidates, []string{taken})
	if _, ok := got["Algorithmen und Datenstrukturen"]; ok {
		t.Errorf("expected no suggestion once the only good folder is taken, got %v", got)
	}
}

// TestSuggestNeverSendsTwoCoursesToOneFolder covers the same collision
// arising inside a single call rather than from the existing config.
func TestSuggestNeverSendsTwoCoursesToOneFolder(t *testing.T) {
	root := makeTree(t, []string{`Analysis/Downloads`})
	candidates, err := Scan(root)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	got := Suggest([]string{"Analysis", "Analysis II"}, candidates, nil)
	if len(got) != 1 {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Errorf("expected one course to win the shared folder, got %v", keys)
	}
}
