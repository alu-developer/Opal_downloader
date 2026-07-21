package scraper

import (
	"errors"
	"testing"
)

// TestCandidateStabilityPollWaitsOutSlowGrowingRender simulates a section
// whose AJAX-rendered content keeps growing across several reads before
// settling, the way OPAL's content renders more slowly under concurrent
// course-crawl load than it does serially. A single fixed-duration wait
// (the pre-fix behavior) can only ever see whichever candidate count
// happened to be true at that one moment; candidateStabilityPoll must keep
// polling until the count actually stops growing and return the final,
// complete set - not an early, incomplete one.
func TestCandidateStabilityPollWaitsOutSlowGrowingRender(t *testing.T) {
	// Simulates a render that "arrives" in stages: empty, then partial,
	// then complete, then stable (twice, to satisfy a requiredStableReads
	// of 2 used below) - the same shape as the real OPAL AJAX race
	// (files.go extraction reading the DOM before Wicket has finished
	// inserting rows).
	reads := [][]map[string]string{
		{},
		{{"href": "a"}},
		{{"href": "a"}, {"href": "b"}, {"href": "c"}},
		{{"href": "a"}, {"href": "b"}, {"href": "c"}}, // stable read 1
		{{"href": "a"}, {"href": "b"}, {"href": "c"}}, // stable read 2
	}
	call := 0
	extractFn := func() ([]map[string]string, error) {
		idx := call
		if idx >= len(reads) {
			idx = len(reads) - 1
		}
		call++
		return reads[idx], nil
	}
	waitCalls := 0
	waitFn := func() { waitCalls++ }

	got, err := candidateStabilityPoll(extractFn, waitFn, 10, 2, 2, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected the poll to wait out the slow render and return all 3 candidates, got %d: %#v", len(got), got)
	}
	// It should have stopped as soon as growth stabilized for 2 consecutive
	// reads (5 reads total: initial + 4 polls), not burned through the
	// entire maxPolls budget.
	if waitCalls != 4 {
		t.Fatalf("expected exactly 4 wait calls (poll stops once stable for requiredStableReads in a row), got %d", waitCalls)
	}
}

// TestCandidateStabilityPollSurvivesFalsePlateauBeforeLaterRenderStage is
// the actual bug this task (fix-concurrent-crawl-ajax-race-and-raise-
// concurrency) root-caused: OPAL/Wicket renders a section's content in
// stages - e.g. the row/file list appears first and can itself sit
// unchanged for one read (a coincidental plateau) before a later stage adds
// more content (in the real bug, the "show all" pagination control itself,
// which then never even got a click attempt because it hadn't rendered
// into the DOM yet at the moment extraction ran). A poll that trusts a
// single non-growing read as "stable" (requiredStableReads=1) stops during
// that plateau and silently returns an incomplete set; requiring multiple
// consecutive non-growing reads must wait through it and pick up the later
// growth. This was live-confirmed against the real TU Dresden account: a
// requiredStableReads-effectively-1 version of this fix still lost exactly
// the files past OPAL's ~20-item default page size in a paginated section,
// byte-for-byte identical to the unfixed code, until this was fixed.
func TestCandidateStabilityPollSurvivesFalsePlateauBeforeLaterRenderStage(t *testing.T) {
	twenty := make([]map[string]string, 20)
	for i := range twenty {
		twenty[i] = map[string]string{"href": "file"}
	}
	twentyEight := make([]map[string]string, 28)
	for i := range twentyEight {
		twentyEight[i] = map[string]string{"href": "file"}
	}
	reads := [][]map[string]string{
		twenty,      // initial read: first page rendered
		twenty,      // false plateau: looks stable for 1 read...
		twentyEight, // ...but a later render stage (e.g. "show all" control/expansion) adds more
		twentyEight, // confirming stable read 1
		twentyEight, // confirming stable read 2
	}
	call := 0
	extractFn := func() ([]map[string]string, error) {
		idx := call
		if idx >= len(reads) {
			idx = len(reads) - 1
		}
		call++
		return reads[idx], nil
	}

	// requiredStableReads=1 (the original, insufficient behavior) must stop
	// during the false plateau and lose the later-arriving content.
	call = 0
	gotWithStableReads1, err := candidateStabilityPoll(extractFn, func() {}, 10, 1, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotWithStableReads1) != 20 {
		t.Fatalf("expected requiredStableReads=1 to demonstrate the bug (stop at the false plateau, 20 candidates), got %d", len(gotWithStableReads1))
	}

	// requiredStableReads=3 (what waitForStableSectionContent actually uses
	// in crawl.go, sectionContentRequiredStableReads) must survive the
	// false plateau and pick up the full, later-arriving content.
	call = 0
	gotWithStableReads3, err := candidateStabilityPoll(extractFn, func() {}, 10, 3, 3, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(gotWithStableReads3) != 28 {
		t.Fatalf("expected requiredStableReads=3 to survive the false plateau and return all 28 candidates, got %d: %#v", len(gotWithStableReads3), gotWithStableReads3)
	}
}

// TestCandidateStabilityPollStopsQuicklyWhenAlreadyStable is the common/serial
// case: the very first read already has everything, so the poll should exit
// after just requiredStableReads confirming extra reads rather than
// spending its whole budget - this is what keeps the fix cheap for
// course_concurrency=1 (no contention, content is already rendered by the
// time the fixed contentFallbackWaitMs wait that precedes this poll has
// elapsed).
func TestCandidateStabilityPollStopsQuicklyWhenAlreadyStable(t *testing.T) {
	stable := []map[string]string{{"href": "a"}, {"href": "b"}}
	calls := 0
	extractFn := func() ([]map[string]string, error) {
		calls++
		return stable, nil
	}
	waitCalls := 0
	waitFn := func() { waitCalls++ }

	got, err := candidateStabilityPoll(extractFn, waitFn, 20, 1, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(got))
	}
	if waitCalls != 1 {
		t.Fatalf("expected exactly 1 extra poll before recognizing stability (requiredStableReads=1), got %d", waitCalls)
	}
	if calls != 2 {
		t.Fatalf("expected exactly 2 extract calls (initial + 1 confirming poll), got %d", calls)
	}
}

// TestCandidateStabilityPollGivesUpAfterMaxPolls covers a render that never
// stabilizes within the budget (keeps growing every single read) - the poll
// must stop at maxPolls and return the largest set actually seen, not hang
// or error.
func TestCandidateStabilityPollGivesUpAfterMaxPolls(t *testing.T) {
	call := 0
	extractFn := func() ([]map[string]string, error) {
		call++
		out := make([]map[string]string, call)
		for i := range out {
			out[i] = map[string]string{"href": "x"}
		}
		return out, nil
	}
	got, err := candidateStabilityPoll(extractFn, func() {}, 5, 1, 1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// initial call (call=1) + 5 polls (call=2..6) - largest observed is 6.
	if len(got) != 6 {
		t.Fatalf("expected the poll to return the largest set seen before giving up (6), got %d", len(got))
	}
}

// TestCandidateStabilityPollTreatsTransientErrorAsNoProgress matches
// waitForStableExpandedCandidates's original error-handling contract: a
// non-fatal extraction error mid-poll (e.g. "execution context was
// destroyed", confirmed live to be transient) must not discard an
// already-successful read - it should be treated as "no growth this round"
// and polling should continue, without counting toward or resetting the
// stable-read streak either (since it isn't a confirming read).
func TestCandidateStabilityPollTreatsTransientErrorAsNoProgress(t *testing.T) {
	call := 0
	extractFn := func() ([]map[string]string, error) {
		call++
		switch call {
		case 1:
			return []map[string]string{{"href": "a"}}, nil
		case 2:
			return nil, errors.New("execution context was destroyed")
		case 3:
			return []map[string]string{{"href": "a"}, {"href": "b"}}, nil
		default:
			return []map[string]string{{"href": "a"}, {"href": "b"}}, nil
		}
	}
	got, err := candidateStabilityPoll(extractFn, func() {}, 10, 1, 1, func(error) bool { return false })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected the transient error to be shrugged off and polling to continue to the eventual 2-candidate read, got %d: %#v", len(got), got)
	}
}

// TestCandidateStabilityPollReturnsFatalErrorImmediately matches the
// page-crash contract relied on by both waitForStableExpandedCandidates
// (crawl.go, pre-existing) and waitForStableSectionContent (crawl.go, new
// in this task): a fatal error (isPageCrashError-classified) must abort
// polling immediately, not be retried in place, so the caller's
// recoverFromPageCrash path runs against a definitely-dead page as soon as
// possible rather than after burning through the poll budget.
func TestCandidateStabilityPollReturnsFatalErrorImmediately(t *testing.T) {
	fatalErr := errors.New("target crashed")
	call := 0
	extractFn := func() ([]map[string]string, error) {
		call++
		if call == 2 {
			return nil, fatalErr
		}
		return []map[string]string{{"href": "a"}}, nil
	}
	waitCalls := 0
	got, err := candidateStabilityPoll(extractFn, func() { waitCalls++ }, 10, 1, 1, func(e error) bool { return e == fatalErr })
	if err != fatalErr {
		t.Fatalf("expected the fatal error to be returned as-is, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil candidates on fatal error, got %#v", got)
	}
	if waitCalls != 1 {
		t.Fatalf("expected polling to stop immediately on the fatal error (1 wait call before it), got %d", waitCalls)
	}
}

// TestCandidateStabilityPollRequiredStableReadsFloorsAtOne covers the
// defensive floor on requiredStableReads (values < 1 are treated as 1) so a
// caller passing 0 doesn't cause an infinite loop or a nonsensical
// immediate-stop-on-first-read.
func TestCandidateStabilityPollRequiredStableReadsFloorsAtOne(t *testing.T) {
	stable := []map[string]string{{"href": "a"}}
	waitCalls := 0
	got, err := candidateStabilityPoll(func() ([]map[string]string, error) { return stable, nil }, func() { waitCalls++ }, 5, 0, 0, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(got))
	}
	if waitCalls != 1 {
		t.Fatalf("expected requiredStableReads=0 to behave like 1 (stop after 1 confirming poll), got %d wait calls", waitCalls)
	}
}
