package scraper

import "testing"

// candidateStabilityPoll used to be handed a single stable-read requirement
// chosen by a process-wide flag: course_concurrency>1 meant EVERY section paid
// the patient streak, contended or not. Measured on the real account that gate
// was the entire concurrency penalty (section poll 451ms serial -> 1.922s at
// concurrency 2, a 4.26x jump matching the 1->4 multiplier, accounting for
// essentially all of the run's +4m12s).
//
// These tests pin the replacement: open impatient, and escalate only on
// evidence from THIS page.

func candidatesOfLen(n int) []map[string]string {
	out := make([]map[string]string, n)
	for i := range out {
		out[i] = map[string]string{"href": "f"}
	}
	return out
}

// A page that is already final must not pay for patience it does not need -
// this is the whole point, and the reason the old global gate was expensive.
func TestStabilityPollStopsEarlyOnAnAlreadyStablePage(t *testing.T) {
	waitCalls := 0
	got, err := candidateStabilityPoll(
		func() ([]map[string]string, error) { return candidatesOfLen(12), nil },
		func() { waitCalls++ },
		10,
		1, // impatient opening
		4, // patient streak, only if earned
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 12 {
		t.Fatalf("got %d candidates, want 12", len(got))
	}
	if waitCalls != 1 {
		t.Errorf("waitCalls = %d, want 1 (a never-growing page must not pay the patient streak)", waitCalls)
	}
}

// Growth is proof this page renders in stages, which is exactly the condition
// a single plateau read cannot be trusted under. Once seen, the poll must
// demand the full patient streak for the rest of the run.
func TestStabilityPollEscalatesToPatientAfterObservedGrowth(t *testing.T) {
	// Grows once (proving staged rendering), then a false plateau, then grows
	// again. An impatient opening would stop at the plateau and return 24;
	// escalation must carry it through to 31.
	reads := []int{20, 24, 24, 24, 31, 31, 31, 31, 31, 31}
	i := -1
	extractFn := func() ([]map[string]string, error) {
		i++
		if i >= len(reads) {
			return candidatesOfLen(reads[len(reads)-1]), nil
		}
		return candidatesOfLen(reads[i]), nil
	}

	got, err := candidateStabilityPoll(extractFn, func() {}, 20, 1, 3, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 31 {
		t.Fatalf("got %d candidates, want 31 - the poll stopped at the false plateau instead of escalating", len(got))
	}
}

// KNOWN AND DELIBERATE LIMITATION, pinned so nobody mistakes it for a bug in
// the escalation logic: escalation is retrospective. A false plateau that
// happens BEFORE any growth is observed ends an impatient poll early, and no
// amount of later growth can bring it back.
//
// That case is not defended here - it is defended upstream, by only opening
// impatient when the MutationObserver reported the page had gone a full
// debounce window with zero mutations (see waitForStableSectionContent). If
// live parity ever regresses at concurrency>1, this is the first thing to
// suspect, and the fix is to stop trusting that calm signal - not to add
// retries here.
func TestStabilityPollCannotRecoverFromAPlateauBeforeAnyGrowth(t *testing.T) {
	reads := []int{20, 20, 20, 28, 28, 28, 28}
	i := -1
	extractFn := func() ([]map[string]string, error) {
		i++
		if i >= len(reads) {
			return candidatesOfLen(reads[len(reads)-1]), nil
		}
		return candidatesOfLen(reads[i]), nil
	}

	got, err := candidateStabilityPoll(extractFn, func() {}, 20, 1, 3, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 20 {
		t.Fatalf("got %d candidates, want 20 - this test documents the limitation; if it now returns more, the design changed and the comment above is stale", len(got))
	}
}

// The escalation must be sticky. A second plateau after growth is no more
// trustworthy than the first one was.
func TestStabilityPollKeepsPatienceOnceEarned(t *testing.T) {
	// grow once, then a 2-read plateau, then grow again.
	reads := []int{5, 9, 9, 9, 14, 14, 14, 14, 14, 14}
	i := -1
	extractFn := func() ([]map[string]string, error) {
		i++
		if i >= len(reads) {
			return candidatesOfLen(reads[len(reads)-1]), nil
		}
		return candidatesOfLen(reads[i]), nil
	}

	got, err := candidateStabilityPoll(extractFn, func() {}, 20, 1, 3, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 14 {
		t.Fatalf("got %d candidates, want 14 - patience was dropped after the first escalation", len(got))
	}
}

// Starting patient (the not-calm path: the MutationObserver hit its hard cap,
// so the page was still mutating) must still behave like the old gate did.
func TestStabilityPollHonoursAPatientOpening(t *testing.T) {
	reads := []int{20, 20, 20, 28, 28, 28, 28, 28}
	i := -1
	extractFn := func() ([]map[string]string, error) {
		i++
		if i >= len(reads) {
			return candidatesOfLen(reads[len(reads)-1]), nil
		}
		return candidatesOfLen(reads[i]), nil
	}

	got, err := candidateStabilityPoll(extractFn, func() {}, 20, 3, 3, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 28 {
		t.Fatalf("got %d candidates, want 28", len(got))
	}
}

// Guards the floor: a caller passing nonsense must not disable patience.
func TestStabilityPollFloorsBothStreakValues(t *testing.T) {
	waitCalls := 0
	got, err := candidateStabilityPoll(
		func() ([]map[string]string, error) { return candidatesOfLen(3), nil },
		func() { waitCalls++ },
		5,
		0, 0,
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d candidates, want 3", len(got))
	}
	if waitCalls != 1 {
		t.Errorf("waitCalls = %d, want 1 (0 must be floored to 1, not to 0)", waitCalls)
	}
}
