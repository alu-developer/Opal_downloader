package scheduler

import (
	"strings"
	"testing"
)

// Every install proposing the same 06:00 means every install asks OPAL for
// several hundred pages on the same tick. Nobody cares whether their sync runs
// at 06:00 or 06:37, so scattering it costs the user nothing and removes a
// load spike that exists purely because of a default. See docs/server-load.md.
func TestSuggestedTimeScattersTheMinuteButKeepsTheHour(t *testing.T) {
	got := SuggestedTime()

	if err := ValidateTime(got); err != nil {
		t.Fatalf("SuggestedTime produced something the scheduler will not accept: %q (%v)", got, err)
	}

	wantHour, _, _ := strings.Cut(DefaultTime, ":")
	gotHour, _, _ := strings.Cut(got, ":")
	if gotHour != wantHour {
		t.Errorf("the hour should be left alone (early morning is a deliberate choice), got %q want %q", gotHour, wantHour)
	}
}

// Stable, not random: a user who opens the schedule page twice must see the
// same time, and re-saving must not silently move a working schedule.
func TestSuggestedTimeIsStable(t *testing.T) {
	first := SuggestedTime()
	for i := 0; i < 5; i++ {
		if got := SuggestedTime(); got != first {
			t.Fatalf("SuggestedTime is not stable: %q then %q", first, got)
		}
	}
}
