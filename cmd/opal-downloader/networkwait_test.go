package opaldownloader

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alu-developer/opal-downloader/internal/netcheck"
)

// withNetworkFakes replaces the retry loop's three moving parts (the check,
// the description, and the sleep) so a test can drive a whole 15-minute wait
// in microseconds. Returns a pointer to the slept-duration total.
func withNetworkFakes(t *testing.T, online func() bool) *time.Duration {
	t.Helper()
	origOnline, origDescribe, origSleep := networkOnline, networkDescribe, networkSleep
	t.Cleanup(func() {
		networkOnline, networkDescribe, networkSleep = origOnline, origDescribe, origSleep
	})

	var slept time.Duration
	networkOnline = func(context.Context, string) bool { return online() }
	networkDescribe = func(_ context.Context, _ string, cause error) error {
		if online() {
			return nil
		}
		return errors.Join(errors.New("No internet connection."), netcheck.ErrOffline)
	}
	networkSleep = func(d time.Duration) { slept += d }
	return &slept
}

func TestScheduledRunProceedsImmediatelyWhenOnline(t *testing.T) {
	slept := withNetworkFakes(t, func() bool { return true })

	if err := waitForNetworkBeforeScheduledRun(context.Background(), "https://example.org/opal/"); err != nil {
		t.Fatalf("waitForNetworkBeforeScheduledRun() = %v, want nil", err)
	}
	if *slept != 0 {
		t.Fatalf("a healthy connection must not wait at all, slept %s", *slept)
	}
}

// The case this exists for: the connection is not up at the scheduled
// minute, but comes back shortly after. The run must wait it out rather than
// throwing away the day's sync.
func TestScheduledRunWaitsForALateConnection(t *testing.T) {
	calls := 0
	slept := withNetworkFakes(t, func() bool {
		calls++
		return calls > 2 // offline for the first two checks
	})

	if err := waitForNetworkBeforeScheduledRun(context.Background(), "https://example.org/opal/"); err != nil {
		t.Fatalf("waitForNetworkBeforeScheduledRun() = %v, want nil once the connection returns", err)
	}
	if *slept == 0 {
		t.Fatal("expected the run to have waited before retrying")
	}
	if *slept >= totalRetryWait() {
		t.Fatalf("should have stopped waiting as soon as the connection returned, slept %s", *slept)
	}
}

func TestScheduledRunGivesUpWithAPlainMessage(t *testing.T) {
	slept := withNetworkFakes(t, func() bool { return false })

	err := waitForNetworkBeforeScheduledRun(context.Background(), "https://example.org/opal/")
	if err == nil {
		t.Fatal("waitForNetworkBeforeScheduledRun() = nil, want an offline error")
	}
	if *slept != totalRetryWait() {
		t.Fatalf("slept %s, want the full %s of retries", *slept, totalRetryWait())
	}
	if !errors.Is(err, netcheck.ErrOffline) {
		t.Fatalf("error must keep the ErrOffline sentinel so the exit code is right: %v", err)
	}
	if !strings.Contains(err.Error(), "No internet connection") {
		t.Fatalf("message must stay plain-language: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "next scheduled sync") {
		t.Fatalf("message should say the next run will try again: %s", err.Error())
	}
}

// An offline run is not the same kind of event as a broken setup, and an
// unattended caller can tell them apart without parsing the message.
func TestOfflineHasItsOwnExitCode(t *testing.T) {
	err := errors.Join(errors.New("No internet connection."), netcheck.ErrOffline)
	if got := exitCodeForError(err); got != exitCodeOffline {
		t.Fatalf("exitCodeForError() = %d, want %d", got, exitCodeOffline)
	}
	if got := exitCodeForError(errors.New("something else")); got != exitCodeGenericError {
		t.Fatalf("exitCodeForError() = %d, want %d for an unrelated error", got, exitCodeGenericError)
	}
}

func totalRetryWait() time.Duration {
	var total time.Duration
	for _, d := range networkRetryDelays {
		total += d
	}
	return total
}
