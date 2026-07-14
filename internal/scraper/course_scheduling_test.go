package scraper

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectDominantCourseFindsDominantCourse(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "big", Title: "Softwaretechnologie"},
		{RepoID: "small1", Title: "Analysis"},
		{RepoID: "small2", Title: "Algorithmen und Datenstrukturen"},
	}
	hints := map[string]int{
		"big":    198,
		"small1": 29,
		"small2": 34,
	}

	repoID, ok := selectDominantCourse(courses, hints)
	if !ok {
		t.Fatalf("expected a dominant course to be found")
	}
	if repoID != "big" {
		t.Fatalf("expected 'big' to be selected, got %q", repoID)
	}
}

func TestSelectDominantCourseNoHistoryFallsBack(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
		{RepoID: "2", Title: "Course B"},
	}
	if _, ok := selectDominantCourse(courses, nil); ok {
		t.Fatalf("expected no dominant course with no hints at all")
	}
	if _, ok := selectDominantCourse(courses, map[string]int{}); ok {
		t.Fatalf("expected no dominant course with empty hints")
	}
}

func TestSelectDominantCourseRequiresTwoHintedCourses(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
		{RepoID: "2", Title: "Course B"},
	}
	hints := map[string]int{"1": 200}
	if _, ok := selectDominantCourse(courses, hints); ok {
		t.Fatalf("expected no dominant course when only one course has history to compare against")
	}
}

func TestSelectDominantCourseRequiresAbsoluteFloor(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
		{RepoID: "2", Title: "Course B"},
	}
	// 10 vs 2 is a 5x dominance ratio, comfortably over
	// largeCourseDominanceRatio, but neither course is anywhere near
	// largeCourseMinFiles - a small account shouldn't get a dedicated pass
	// just because one course happens to have proportionally more files.
	hints := map[string]int{"1": 10, "2": 2}
	if _, ok := selectDominantCourse(courses, hints); ok {
		t.Fatalf("expected no dominant course below the absolute file-count floor")
	}
}

func TestSelectDominantCourseRequiresDominanceRatio(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
		{RepoID: "2", Title: "Course B"},
	}
	// Both comfortably over the floor, but only ~1.5x apart - not dominant
	// enough to justify serializing part of the crawl.
	hints := map[string]int{"1": 90, "2": 65}
	if _, ok := selectDominantCourse(courses, hints); ok {
		t.Fatalf("expected no dominant course when the ratio bar isn't met")
	}
}

func TestSelectDominantCourseIgnoresCoursesNotInThisRun(t *testing.T) {
	// Hints carry history for a course that isn't part of this run's
	// courses slice (e.g. filtered out via --courses) - it must not be
	// selected or count toward the two-hinted-courses requirement.
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
	}
	hints := map[string]int{
		"1":            100,
		"not-in-run-2": 300,
	}
	if _, ok := selectDominantCourse(courses, hints); ok {
		t.Fatalf("expected no dominant course when only one *in-run* course has history")
	}
}

func TestSplitOutCourseSeparatesDominantFromRest(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
		{RepoID: "2", Title: "Course B"},
		{RepoID: "3", Title: "Course C"},
	}
	dominant, rest := splitOutCourse(courses, "2")
	if len(dominant) != 1 || dominant[0].RepoID != "2" {
		t.Fatalf("expected dominant slice to contain only course 2, got %#v", dominant)
	}
	if len(rest) != 2 || rest[0].RepoID != "1" || rest[1].RepoID != "3" {
		t.Fatalf("expected rest to contain courses 1 and 3 in original order, got %#v", rest)
	}
}

func TestSplitOutCourseNoMatchReturnsEmptyDominant(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Course A"},
		{RepoID: "2", Title: "Course B"},
	}
	dominant, rest := splitOutCourse(courses, "does-not-exist")
	if len(dominant) != 0 {
		t.Fatalf("expected no dominant course on no match, got %#v", dominant)
	}
	if len(rest) != 2 {
		t.Fatalf("expected all courses in rest on no match, got %#v", rest)
	}
}

func TestCourseSizeHintsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".opal-course-size-hints.json")

	if hints := loadCourseSizeHints(path); hints != nil {
		t.Fatalf("expected nil hints for a missing file, got %#v", hints)
	}

	want := map[string]int{"repo1": 198, "repo2": 34}
	if err := saveCourseSizeHints(path, want); err != nil {
		t.Fatalf("saveCourseSizeHints failed: %v", err)
	}

	got := loadCourseSizeHints(path)
	if len(got) != len(want) {
		t.Fatalf("expected %d hints after round-trip, got %d: %#v", len(want), len(got), got)
	}
	for repoID, count := range want {
		if got[repoID] != count {
			t.Fatalf("expected hint %q=%d, got %d", repoID, count, got[repoID])
		}
	}
}

func TestSaveCourseSizeHintsEmptyIsNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".opal-course-size-hints.json")

	if err := saveCourseSizeHints(path, map[string]int{"a": 1}); err != nil {
		t.Fatalf("initial save failed: %v", err)
	}
	if err := saveCourseSizeHints(path, nil); err != nil {
		t.Fatalf("no-op save failed: %v", err)
	}

	got := loadCourseSizeHints(path)
	if len(got) != 1 || got["a"] != 1 {
		t.Fatalf("expected existing hints to survive a no-op save, got %#v", got)
	}
}

func TestLoadCourseSizeHintsCorruptFileReturnsNil(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".opal-course-size-hints.json")
	if err := os.WriteFile(path, []byte("not valid json"), 0o644); err != nil {
		t.Fatalf("failed to write corrupt hint file: %v", err)
	}
	if hints := loadCourseSizeHints(path); hints != nil {
		t.Fatalf("expected nil hints for a corrupt file (safe fallback), got %#v", hints)
	}
}

func TestUpdateCourseSizeHintsMergesAndPreservesUntouched(t *testing.T) {
	existing := map[string]int{"1": 100, "2": 50}
	fresh := map[string]int{"1": 120}

	merged := updateCourseSizeHints(existing, fresh)

	if merged["1"] != 120 {
		t.Fatalf("expected course 1's hint to be updated to 120, got %d", merged["1"])
	}
	if merged["2"] != 50 {
		t.Fatalf("expected course 2's untouched hint to survive at 50, got %d", merged["2"])
	}
	// existing must not be mutated by the merge.
	if existing["1"] != 100 {
		t.Fatalf("expected existing map to be untouched, got %d", existing["1"])
	}
}

func TestCourseFileCountsByTitle(t *testing.T) {
	remoteFiles := []RemoteFile{
		{Course: "Course A", Name: "a.pdf"},
		{Course: "Course A", Name: "b.pdf"},
		{Course: "Course B", Name: "c.pdf"},
	}
	counts := courseFileCountsByTitle(remoteFiles)
	if counts["Course A"] != 2 {
		t.Fatalf("expected Course A count 2, got %d", counts["Course A"])
	}
	if counts["Course B"] != 1 {
		t.Fatalf("expected Course B count 1, got %d", counts["Course B"])
	}
}

// TestCourseFileCountsByRepoIDMatchesSanitizedTitles is a regression test
// for a real bug found live: RemoteFile.Course is
// sanitizeFilename(course.Title) (see appendSectionFiles, files.go), not
// the raw title, so a title->RepoID lookup keyed by the raw title silently
// never matches a course whose title contains characters sanitizeFilename
// rewrites (e.g. ":"). Confirmed live against the real TU Dresden account:
// two real courses with a ":" in their title never got a hint recorded
// under the buggy version of this code, even though they crawled
// successfully with real file counts.
func TestCourseFileCountsByRepoIDMatchesSanitizedTitles(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "1", Title: "Analysis"},
		{RepoID: "2", Title: "TUDMATH SoSe2026 Modul Math-Ba-ST10: Stochastik"},
	}
	remoteFiles := []RemoteFile{
		{Course: "Analysis", Name: "a.pdf"},
		{Course: "Analysis", Name: "b.pdf"},
		// sanitizeFilename replaces ":" with "_" - this is what
		// RemoteFile.Course actually contains for the colon-titled course.
		{Course: sanitizeFilename("TUDMATH SoSe2026 Modul Math-Ba-ST10: Stochastik"), Name: "c.pdf"},
	}

	counts := courseFileCountsByRepoID(courses, remoteFiles)

	if counts["1"] != 2 {
		t.Fatalf("expected RepoID 1 (Analysis) count 2, got %d: %#v", counts["1"], counts)
	}
	if counts["2"] != 1 {
		t.Fatalf("expected RepoID 2 (colon-titled course) count 1 - the bug this regression-tests would silently omit this key entirely: %#v", counts)
	}
}

// TestScrapeCoursesBrowserDedicatedPassDoesNotDropOrDuplicateFiles exercises
// the actual dedicated-pass code path (via collectCourseFilesConcurrently,
// the same primitive scrapeCoursesBrowser uses for both phases) end-to-end
// with a fake collectFn, confirming the two-phase split-then-concatenate
// scheduling can't lose or duplicate a course's files relative to running
// everything in one flat concurrent pass.
func TestScrapeCoursesBrowserDedicatedPassDoesNotDropOrDuplicateFiles(t *testing.T) {
	courses := []CourseRef{
		{RepoID: "big", Title: "Softwaretechnologie", URL: "https://opal.example/big"},
		{RepoID: "small1", Title: "Analysis", URL: "https://opal.example/small1"},
		{RepoID: "small2", Title: "Algorithmen", URL: "https://opal.example/small2"},
	}
	hints := map[string]int{"big": 198, "small1": 29, "small2": 34}

	fileCountByRepo := map[string]int{"big": 198, "small1": 29, "small2": 34}
	collectFn := func(course CourseRef) (courseCrawlResult, error) {
		n := fileCountByRepo[course.RepoID]
		files := make([]FileRef, 0, n)
		for i := 0; i < n; i++ {
			files = append(files, FileRef{
				CourseRepoID: course.RepoID,
				CourseTitle:  course.Title,
				Name:         course.RepoID + "-file.pdf",
				URL:          course.URL + "/file" + string(rune('a'+i%26)),
				Path:         course.Title + "/file" + string(rune('a'+i%26)) + string(rune('A'+i/26)),
			})
		}
		return courseCrawlResult{files: files}, nil
	}

	dominantRepoID, ok := selectDominantCourse(courses, hints)
	if !ok || dominantRepoID != "big" {
		t.Fatalf("expected 'big' to be selected as dominant, got %q ok=%v", dominantRepoID, ok)
	}
	dominant, rest := splitOutCourse(courses, dominantRepoID)

	dominantFiles := collectCourseFilesConcurrently(dominant, 1, collectFn, nil)
	restFiles := collectCourseFilesConcurrently(rest, 3, collectFn, nil)
	twoPhaseFiles := append(dominantFiles, restFiles...)

	flatFiles := collectCourseFilesConcurrently(courses, 3, collectFn, nil)

	if len(twoPhaseFiles) != len(flatFiles) {
		t.Fatalf("expected two-phase scheduling to produce the same file count as flat scheduling: two-phase=%d flat=%d", len(twoPhaseFiles), len(flatFiles))
	}
	wantTotal := 198 + 29 + 34
	if len(twoPhaseFiles) != wantTotal {
		t.Fatalf("expected %d total files, got %d", wantTotal, len(twoPhaseFiles))
	}
}
