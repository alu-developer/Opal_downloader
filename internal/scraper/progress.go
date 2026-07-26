package scraper

// DiscoveryPhase identifies which step of a crawl a DiscoveryProgress event
// describes.
type DiscoveryPhase string

const (
	// PhaseCoursesFound fires once, right after the dashboard has been read
	// and the set of matching courses is known. TotalCourses is set.
	PhaseCoursesFound DiscoveryPhase = "courses-found"
	// PhaseCourseStarted fires as each course's crawl begins. Course,
	// CourseIndex and TotalCourses are set.
	PhaseCourseStarted DiscoveryPhase = "course-started"
	// PhaseSection fires for each section page successfully visited within a
	// course. Course, Section and SectionsDone are set. This is the event
	// that actually keeps a long crawl visibly alive - a course with 100+
	// weekly folders spends nearly all of its time here.
	PhaseSection DiscoveryPhase = "section"
	// PhaseCourseDone fires when a course's crawl finishes. Course and
	// FileCount are set.
	PhaseCourseDone DiscoveryPhase = "course-done"
)

// DiscoveryProgress is one incremental notification from the crawl phase of
// a scrape. Fields not relevant to a given Phase are left at their zero
// value - see each phase constant for which are populated.
//
// CourseIndex is assigned in the order courses actually *start*, not the
// order they appear on the dashboard: with course_concurrency > 1 several
// crawls are in flight at once, so it is a "how many have begun" counter
// rather than a stable per-course identity. It is meant for a human-readable
// "course 3 of 6" style label, nothing more.
type DiscoveryProgress struct {
	Phase        DiscoveryPhase
	Course       string
	CourseIndex  int
	TotalCourses int
	Section      string

	// SectionURL is the page the Section field names. Carried so a stall
	// warning can point at something openable rather than a title that may
	// appear in several courses - see stallwatch.go.
	SectionURL string

	SectionsDone int
	FileCount    int
}

// SetDiscoveryProgress registers fn to receive crawl progress events. Pass
// nil to disable. It must be called before a scrape starts; fn may be
// invoked from several goroutines but never concurrently with itself (see
// progressMu on OpalScraper), so implementations do not need their own
// locking.
func (s *OpalScraper) SetDiscoveryProgress(fn func(DiscoveryProgress)) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	s.progressFn = fn
	s.progressStarted = 0
}

// publishProgress delivers one event to the registered callback, if any. It
// is a no-op when no callback is registered, so every call site can fire
// unconditionally.
func (s *OpalScraper) publishProgress(event DiscoveryProgress) {
	// Recorded before the nil check, and outside it: the stall watchdog has to
	// see progress whether or not anyone registered a callback, or a CLI run
	// (which registers none) would look permanently stalled.
	if s.stall != nil {
		s.stall.note(event.Course, event.Section, event.SectionURL)
	}

	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if s.progressFn == nil {
		return
	}
	s.progressFn(event)
}

// nextCourseIndex returns the 1-based position of a course among those whose
// crawl has begun. Separate from publishProgress so the counter is bumped
// exactly once per course even when no callback is registered, keeping the
// numbering identical whether or not anyone is listening.
func (s *OpalScraper) nextCourseIndex() int {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	s.progressStarted++
	return s.progressStarted
}
