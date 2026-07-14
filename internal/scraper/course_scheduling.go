package scraper

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// This file implements the size/duration-aware course scheduler investigated
// by queue task investigate-size-aware-course-scheduling-for-concurrency, a
// follow-up left open by fix-concurrent-crawl-ajax-race-and-raise-
// concurrency (PR #73, see DefaultCourseConcurrency's doc comment in
// internal/config/config.go for the full history). That task root-caused
// and fixed several real AJAX-render races in concurrent course crawling,
// but could not get residual file loss (~1-2% in the worst case) to zero for
// the real account's full 8-course mix - specifically when its one very
// large course (198 files, ~5-7 minutes to crawl alone) ran concurrently
// with several smaller courses that have paginated sections. Per-section
// wait-tuning alone had diminishing returns against that specific
// contention pattern; this file instead avoids the contention in the first
// place by giving a course that history shows is dominant in size a
// dedicated, non-concurrent crawl pass, so it is never one of several tabs
// competing for the browser's rendering pipeline at once.
//
// courseSizeHintsFileName is a small per-machine cache of the file count
// each course produced on its most recent successful crawl, keyed by course
// RepoID (stable across a course being renamed, unlike title text). It is
// deliberately NOT internal/visitlog (which already tracks per-section file
// counts across runs): that package's doc comment explicitly scopes it to
// "observational only... nothing in this package changes crawl behavior",
// a constraint that exists because a prior task
// (research-structure-cache-and-priority-crawl.md) rejected skip-based-on-
// history caching as unsafe (OPAL can add new content to a previously-empty
// section at any time). Using history to change *scheduling order* (this
// file) carries a materially different risk profile - every course is still
// crawled in full every run, so a stale/wrong hint can only ever cost a
// scheduling optimization, never drop content - but keeping it as a
// separate, purpose-built file avoids blurring visitlog's stricter
// contract.
const courseSizeHintsFileName = ".opal-course-size-hints.json"

// largeCourseMinFiles / largeCourseDominanceRatio bound when
// selectDominantCourse below decides a course is worth a dedicated solo
// crawl pass. Both were chosen from the real TU Dresden account's live
// data: DefaultCourseConcurrency's doc comment in internal/config/config.go
// documents its one outsized course (198 files) against a next-largest of
// roughly 30-40 files in the same account - a floor of 60 and a 2x
// dominance ratio comfortably catch that shape while staying conservative
// enough not to fire for an account whose courses are all similarly sized.
// See selectDominantCourse's doc comment for why this bar is deliberately
// conservative.
const largeCourseMinFiles = 60
const largeCourseDominanceRatio = 2.0

// courseSizeHints is the on-disk shape of courseSizeHintsFileName.
type courseSizeHints struct {
	// Courses maps course RepoID to the file count that course produced on
	// its most recent successful crawl (see updateCourseSizeHints).
	Courses map[string]int `json:"courses"`
}

// courseSizeHintsPath returns the path a scraper instance reads/writes its
// size-hint cache at: next to the (already-expanded-to-absolute) session
// state file, so it lives in the same per-machine location without needing
// its own config.yaml setting. Falls back to the user's home directory if
// s.stateFile doesn't resolve to a usable directory (e.g. in tests that
// construct an OpalScraper directly with an empty/relative stateFile).
func (s *OpalScraper) courseSizeHintsPath() string {
	dir := filepath.Dir(s.stateFile)
	if dir == "" || dir == "." {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home
		}
	}
	return filepath.Join(dir, courseSizeHintsFileName)
}

// loadCourseSizeHints reads path's course-RepoID-to-file-count map. A
// missing or unreadable/corrupt file is not an error - it returns a nil map,
// matching syncer.LoadManifest's "first run starts from nothing" behavior,
// so a scrape can always fall back to today's flat concurrent scheduling
// when no history is available yet.
func loadCourseSizeHints(path string) map[string]int {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var payload courseSizeHints
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	return payload.Courses
}

// saveCourseSizeHints writes hints to path. A nil/empty hints map is a
// no-op (leaves any existing file untouched) rather than writing an empty
// file, so a run that crawled zero courses successfully (e.g. every course
// errored) doesn't wipe out previously-recorded history.
func saveCourseSizeHints(path string, hints map[string]int) error {
	if len(hints) == 0 {
		return nil
	}
	data, err := json.MarshalIndent(courseSizeHints{Courses: hints}, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// updateCourseSizeHints merges freshly observed per-course file counts
// (courseFileCounts, keyed by RepoID, populated only for courses that were
// actually part of this run and crawled without error - see
// scrapeCoursesBrowser) into existing (the hints loaded at the start of this
// run), returning a new map. Courses not touched by this run (e.g. a
// --courses-filtered run that only crawled a subset) keep their previous
// hint untouched, so history accumulates across differently-scoped runs
// instead of a narrow run erasing broader history.
func updateCourseSizeHints(existing, courseFileCounts map[string]int) map[string]int {
	merged := make(map[string]int, len(existing)+len(courseFileCounts))
	for repoID, count := range existing {
		merged[repoID] = count
	}
	for repoID, count := range courseFileCounts {
		merged[repoID] = count
	}
	return merged
}

// selectDominantCourse looks at hints (course RepoID -> previous file count)
// for the courses actually being crawled this run and decides whether one of
// them should be given a dedicated, non-concurrent crawl pass ahead of the
// rest. It returns that course's RepoID and true when found; otherwise
// ("", false), in which case the caller should fall back to the existing
// flat concurrent scheduling unchanged.
//
// The bar is intentionally conservative - both an absolute floor
// (largeCourseMinFiles) and a dominance ratio versus the next-largest hinted
// course (largeCourseDominanceRatio) must be met - so this only fires for
// the specific "one course dwarfs the others" shape that
// DefaultCourseConcurrency's doc comment (internal/config/config.go)
// documents as the actual trigger for the residual concurrent-crawl file
// loss, not for an account whose courses are all roughly similarly sized
// (where a dedicated pass would just serialize part of the crawl for no
// contention-avoidance benefit). Requires at least two hinted courses among
// those being crawled so there is something to compare against; a course
// with no hint at all (new course, or first-ever run) is never selected -
// it will get one after this run updates the hint file, so the scheduling
// benefit only ever lags by one run, never requires a dedicated upfront
// probe pass.
func selectDominantCourse(courses []CourseRef, hints map[string]int) (string, bool) {
	if len(hints) == 0 || len(courses) < 2 {
		return "", false
	}

	type entry struct {
		repoID string
		count  int
	}
	hinted := make([]entry, 0, len(courses))
	for _, c := range courses {
		if n, ok := hints[c.RepoID]; ok {
			hinted = append(hinted, entry{repoID: c.RepoID, count: n})
		}
	}
	if len(hinted) < 2 {
		return "", false
	}

	sort.Slice(hinted, func(i, j int) bool { return hinted[i].count > hinted[j].count })
	largest, second := hinted[0], hinted[1]

	if largest.count < largeCourseMinFiles {
		return "", false
	}
	if second.count <= 0 || float64(largest.count) < largeCourseDominanceRatio*float64(second.count) {
		return "", false
	}
	return largest.repoID, true
}

// splitOutCourse partitions courses into the single CourseRef whose RepoID
// matches dominantRepoID (as a one-element slice) and every other course, in
// their original relative order. If no course matches (should not happen
// given selectDominantCourse only ever returns a RepoID it found in
// courses, but guarded defensively), the first slice is empty and the
// second is the full input, so callers degrade to "no dedicated pass"
// rather than panicking or silently dropping a course.
func splitOutCourse(courses []CourseRef, dominantRepoID string) (dominant []CourseRef, rest []CourseRef) {
	rest = make([]CourseRef, 0, len(courses))
	for _, c := range courses {
		if c.RepoID == dominantRepoID && dominant == nil {
			dominant = []CourseRef{c}
			continue
		}
		rest = append(rest, c)
	}
	return dominant, rest
}

// courseFileCountsByTitle counts files in remoteFiles per course title (as
// carried on RemoteFile.Course - see courseFileCountsByRepoID's doc comment
// for what that string actually is).
func courseFileCountsByTitle(remoteFiles []RemoteFile) map[string]int {
	counts := make(map[string]int, len(remoteFiles))
	for _, f := range remoteFiles {
		counts[f.Course]++
	}
	return counts
}

// courseFileCountsByRepoID translates courseFileCountsByTitle's per-title
// counts into per-RepoID counts (RepoID being the stable key
// courseSizeHints is keyed by - see its doc comment), using courses (the
// CourseRef list that produced remoteFiles) to build the title->RepoID
// lookup.
//
// The lookup key MUST be sanitizeFilename(c.Title), not the raw title:
// RemoteFile.Course is populated from FileRef.CourseTitle, which
// appendSectionFiles (files.go) sets to sanitizeFilename(course.Title), not
// the raw title - so a course whose title contains characters
// sanitizeFilename rewrites (e.g. the account's real courses with a ":" in
// the title, like "TUDMATH ... Math-Ba-ST10: Stochastik") would otherwise
// never match here. Confirmed live: an earlier version of this function
// keyed by the raw, un-sanitized title, and both of this account's
// colon-containing course titles silently never got a hint recorded (their
// repoIDs were simply absent from the saved hints file after a run that did
// successfully crawl them) - the mismatch is silent because a missed title
// lookup just skips that course's hint update rather than erroring.
func courseFileCountsByRepoID(courses []CourseRef, remoteFiles []RemoteFile) map[string]int {
	titleToRepoID := make(map[string]string, len(courses))
	for _, c := range courses {
		titleToRepoID[sanitizeFilename(c.Title)] = c.RepoID
	}

	counts := make(map[string]int)
	for title, count := range courseFileCountsByTitle(remoteFiles) {
		if repoID, ok := titleToRepoID[title]; ok {
			counts[repoID] = count
		}
	}
	return counts
}
