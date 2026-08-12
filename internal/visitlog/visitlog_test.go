package visitlog

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsEmptyNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".opal-visit-log.json")
	records, err := Load(path)
	if err != nil {
		t.Fatalf("Load on missing file returned error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no records, got %d", len(records))
	}
}

func TestAppendAccumulatesAcrossCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".opal-visit-log.json")

	first := []Record{
		{Course: "Analysis", SectionTitle: "Übungen", SectionURL: "https://opal/1", FilesFound: 3, Timestamp: time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)},
	}
	if err := Append(path, first); err != nil {
		t.Fatalf("first Append failed: %v", err)
	}

	second := []Record{
		{Course: "Analysis", SectionTitle: "Übungen", SectionURL: "https://opal/1", FilesFound: 0, Timestamp: time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)},
		{Course: "Analysis", SectionTitle: "Vorlesung", SectionURL: "https://opal/2", FilesFound: 5, Timestamp: time.Date(2026, 7, 2, 10, 1, 0, 0, time.UTC)},
	}
	if err := Append(path, second); err != nil {
		t.Fatalf("second Append failed: %v", err)
	}

	all, err := Load(path)
	if err != nil {
		t.Fatalf("Load after two Appends failed: %v", err)
	}
	// This is the crux of the "persistent across runs" requirement: a second
	// Append call must add to the file, not overwrite it - a run that only
	// wrote `second`'s two records here (losing `first`) would be exactly
	// the "single-run raw log, meant for one-off manual reading" behavior
	// this feature is explicitly supposed to be distinct from.
	if len(all) != 3 {
		t.Fatalf("expected 3 accumulated records across two Append calls, got %d: %#v", len(all), all)
	}
}

func TestAppendNoopOnEmptyInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".opal-visit-log.json")
	if err := Append(path, nil); err != nil {
		t.Fatalf("Append with no records failed: %v", err)
	}
	// Must not create the file at all - a no-op should leave no trace.
	if _, err := Load(path); err != nil {
		t.Fatalf("Load after no-op Append failed: %v", err)
	}
}

func TestAggregateCountsVisitsAndEmptyVisits(t *testing.T) {
	records := []Record{
		{Course: "Analysis", SectionTitle: "Forum", SectionURL: "https://opal/forum", FilesFound: 0, Timestamp: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{Course: "Analysis", SectionTitle: "Forum", SectionURL: "https://opal/forum", FilesFound: 0, Timestamp: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)},
		{Course: "Analysis", SectionTitle: "Übungen", SectionURL: "https://opal/uebungen", FilesFound: 4, Timestamp: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{Course: "Analysis", SectionTitle: "Übungen", SectionURL: "https://opal/uebungen", FilesFound: 0, Timestamp: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)},
	}

	stats := Aggregate(records)
	if len(stats) != 2 {
		t.Fatalf("expected 2 aggregated sections, got %d: %#v", len(stats), stats)
	}

	// The always-empty "Forum" section must sort first - that's the
	// specific signal the maintainer asked to see at a glance.
	forum := stats[0]
	if forum.SectionURL != "https://opal/forum" {
		t.Fatalf("expected the always-empty Forum section to sort first, got %#v", stats)
	}
	if forum.Visits != 2 || forum.EmptyVisits != 2 {
		t.Fatalf("expected Forum to have Visits=2 EmptyVisits=2, got Visits=%d EmptyVisits=%d", forum.Visits, forum.EmptyVisits)
	}
	if !forum.AlwaysEmpty() {
		t.Fatalf("expected Forum.AlwaysEmpty() to be true")
	}

	uebungen := stats[1]
	if uebungen.SectionURL != "https://opal/uebungen" {
		t.Fatalf("expected Übungen section second, got %#v", stats)
	}
	if uebungen.Visits != 2 || uebungen.EmptyVisits != 1 {
		t.Fatalf("expected Übungen to have Visits=2 EmptyVisits=1, got Visits=%d EmptyVisits=%d", uebungen.Visits, uebungen.EmptyVisits)
	}
	if uebungen.AlwaysEmpty() {
		t.Fatalf("expected Übungen.AlwaysEmpty() to be false (it found files on one visit)")
	}
}

func TestAggregateKeepsMostRecentSectionTitle(t *testing.T) {
	records := []Record{
		{Course: "Analysis", SectionTitle: "Old Name", SectionURL: "https://opal/1", FilesFound: 1, Timestamp: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)},
		{Course: "Analysis", SectionTitle: "New Name", SectionURL: "https://opal/1", FilesFound: 1, Timestamp: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC)},
	}
	stats := Aggregate(records)
	if len(stats) != 1 {
		t.Fatalf("expected 1 aggregated section, got %d", len(stats))
	}
	if stats[0].SectionTitle != "New Name" {
		t.Fatalf("expected the most recent visit's title %q, got %q", "New Name", stats[0].SectionTitle)
	}
}

func TestAggregateEmptyInput(t *testing.T) {
	stats := Aggregate(nil)
	if len(stats) != 0 {
		t.Fatalf("expected no stats for no records, got %d", len(stats))
	}
	report := FormatReport(stats)
	if report == "" {
		t.Fatalf("expected a non-empty placeholder report for no records")
	}
}

func TestFormatReportIncludesAlwaysEmptyNote(t *testing.T) {
	stats := []SectionStat{
		{Course: "Analysis", SectionTitle: "Forum", SectionURL: "https://opal/forum", Visits: 8, EmptyVisits: 8},
	}
	report := FormatReport(stats)
	if !contains(report, "empty on all 8 visit(s)") {
		t.Fatalf("expected report to flag the always-empty section, got:\n%s", report)
	}
}

func TestFormatReportFlagsIntermittentlyEmptySection(t *testing.T) {
	stats := []SectionStat{
		{Course: "Analysis", SectionTitle: "Übungsblätter", SectionURL: "https://opal/x", Visits: 92, EmptyVisits: 3},
	}
	report := FormatReport(stats)
	if !contains(report, "empty on 3 of 92 visit(s)") {
		t.Fatalf("expected report to flag the intermittently-empty section distinctly from an always-empty one, got:\n%s", report)
	}
}

func TestFormatReportLeavesReliableSectionNoteBlank(t *testing.T) {
	stats := []SectionStat{
		{Course: "Analysis", SectionTitle: "Part-4", SectionURL: "https://opal/y", Visits: 85, EmptyVisits: 0},
	}
	report := FormatReport(stats)
	if contains(report, "empty on") {
		t.Fatalf("expected no 'empty on' note for a section that was never empty, got:\n%s", report)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
