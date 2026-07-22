package foldersuggest

import (
	"reflect"
	"testing"
)

// TestScoreMatchesRealAbbreviations is the calibration case for this whole
// package. The pairs below are the shapes OPAL course titles and hand-made
// coursework folders really take, and only one of them ("Analysis") matches
// by substring - which is why the naive version of this feature was never
// written.
func TestScoreMatchesRealAbbreviations(t *testing.T) {
	cases := []struct {
		course string
		folder string
		why    string
	}{
		{"Analysis", "Analysis", "identical names still have to work"},
		{"Algorithmen und Datenstrukturen", "AlgData", "word-by-word, second word abbreviated loosely"},
		{"TUDMATH SoSe2026 Modul Math-Ba-NM20: Numerische Mathematik - Iterationsverfahren", "NuMa", "word-by-word inside a title full of boilerplate"},
		{"So26 Programmieren - Weiterführende Konzepte (Math-Ba-PR20)", "Ma-Prog", "segments matching words in a different order"},
		{"Softwaretechnologie (SoSe 26)", "SoTech", "one German compound cut into two segments"},
		{"2026 LA20", "Lineare Algebra", "the course code is the initialism of the folder"},
	}

	for _, tc := range cases {
		got := Score(tc.course, tc.folder)
		if got < MinScore {
			t.Errorf("Score(%q, %q) = %.3f, want >= %.2f (%s)", tc.course, tc.folder, got, MinScore, tc.why)
		}
	}
}

// TestScoreRejectsUnrelated guards the half of the contract that matters
// more: a wrong folder is worse than an empty field, so anything that is not
// recognisably the same subject must stay below the threshold.
func TestScoreRejectsUnrelated(t *testing.T) {
	cases := []struct {
		course string
		folder string
	}{
		{"Analysis", "Physik"},
		{"Analysis", "_2. Semester"},
		{"Algorithmen und Datenstrukturen", "Downloads"},
		{"Softwaretechnologie (SoSe 26)", "Steuererklärung"},
		{"2026 LA20", "Sonstiges"},
		{"Numerische Mathematik", "Rechnungen"},
		{"Analysis", ""},
		{"", "Analysis"},
		// A shared first letter is not a match: initials only count when
		// every word lines up.
		{"Algorithmen und Datenstrukturen", "Alpen"},
	}

	for _, tc := range cases {
		if got := Score(tc.course, tc.folder); got >= MinScore {
			t.Errorf("Score(%q, %q) = %.3f, want < %.2f", tc.course, tc.folder, got, MinScore)
		}
	}
}

// TestScorePrefersTheBetterFolder checks the ordering the margin rule relies
// on: it is not enough that the right folder clears the threshold, it also
// has to beat the other plausible ones.
func TestScorePrefersTheBetterFolder(t *testing.T) {
	course := "TUDMATH SoSe2026 Modul Math-Ba-NM20: Numerische Mathematik - Iterationsverfahren"
	if Score(course, "NuMa") <= Score(course, "Numerik-Altklausuren") {
		t.Errorf("expected NuMa to beat Numerik-Altklausuren for %q", course)
	}
}

func TestSplitWords(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"AlgData", []string{"alg", "data"}},
		{"NuMa", []string{"nu", "ma"}},
		{"Ma-Prog", []string{"ma", "prog"}},
		{"Math-Ba-NM20", []string{"math", "ba", "nm", "20"}},
		{"TUDMath", []string{"tud", "math"}},
		{"_2. Semester", []string{"2", "semester"}},
		{"Übungsblätter", []string{"ubungsblatter"}},
		{"", nil},
	}

	for _, tc := range cases {
		if got := splitWords(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitWords(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestCourseTokensDropsBoilerplate matters for the acronym rule in
// particular: "Algorithmen und Datenstrukturen" has to reduce to the
// initials "ad", not "aud", and a semester marker must not be matchable at
// all.
func TestCourseTokensDropsBoilerplate(t *testing.T) {
	got := courseTokens("So26 Algorithmen und Datenstrukturen (SoSe 2026), Modul 4")
	want := []string{"algorithmen", "datenstrukturen"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("courseTokens = %v, want %v", got, want)
	}
}

func TestScoreMatchesInitialism(t *testing.T) {
	if got := Score("Algorithmen und Datenstrukturen", "AD"); got < MinScore {
		t.Errorf("Score for the initialism folder = %.3f, want >= %.2f", got, MinScore)
	}
}
