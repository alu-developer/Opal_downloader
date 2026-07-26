package polite

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeClock drives the limiter without spending real time. A rate limiter
// whose tests actually wait is one nobody runs.
type fakeClock struct {
	now    time.Time
	slept  []time.Duration
	cancel error
}

func (c *fakeClock) sleep(_ context.Context, d time.Duration) error {
	if c.cancel != nil {
		return c.cancel
	}
	c.slept = append(c.slept, d)
	c.now = c.now.Add(d)
	return nil
}

func withClock(l *Limiter) *fakeClock {
	c := &fakeClock{now: time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)}
	l.sleep = c.sleep
	l.now = func() time.Time { return c.now }
	return c
}

func TestTheFirstRequestIsNotDelayed(t *testing.T) {
	l := New(250 * time.Millisecond)
	c := withClock(l)

	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if len(c.slept) != 0 {
		t.Fatalf("the first request was made to wait for nothing: %v", c.slept)
	}
}

func TestRequestsAreSpacedByTheMinimumInterval(t *testing.T) {
	l := New(250 * time.Millisecond)
	c := withClock(l)
	ctx := context.Background()

	for i := 0; i < 4; i++ {
		if err := l.Wait(ctx); err != nil {
			t.Fatalf("Wait %d: %v", i, err)
		}
	}

	if len(c.slept) != 3 {
		t.Fatalf("expected three gaps between four requests, got %d: %v", len(c.slept), c.slept)
	}
	for i, d := range c.slept {
		if d != 250*time.Millisecond {
			t.Errorf("gap %d was %s, want 250ms", i, d)
		}
	}
}

// A request that arrives after a natural pause should not be delayed at all -
// the limiter is a ceiling, not a metronome.
func TestATardyRequestIsNotDelayed(t *testing.T) {
	l := New(250 * time.Millisecond)
	c := withClock(l)
	ctx := context.Background()

	if err := l.Wait(ctx); err != nil {
		t.Fatalf("first Wait: %v", err)
	}
	c.now = c.now.Add(5 * time.Second) // a slow page render, as usual

	if err := l.Wait(ctx); err != nil {
		t.Fatalf("second Wait: %v", err)
	}
	if len(c.slept) != 0 {
		t.Fatalf("a request made long after the previous one was still delayed: %v", c.slept)
	}
}

// The ceiling has to be loose enough not to bind on what the tool does today,
// or every future performance measurement becomes a measurement of this
// constant instead.
func TestTheDefaultCeilingDoesNotBindOnTodaysCrawl(t *testing.T) {
	// A section costs roughly a second of Wicket rendering, measured
	// repeatedly during the sync-speed campaign.
	const observedPerSection = time.Second
	if DefaultMinInterval >= observedPerSection {
		t.Fatalf("the default ceiling (%s) is at or below the observed ~%s per section, so it would slow ordinary syncs",
			DefaultMinInterval, observedPerSection)
	}
}

func TestOverloadResponsesSlowThingsDown(t *testing.T) {
	l := New(250 * time.Millisecond)
	c := withClock(l)
	ctx := context.Background()

	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	l.Observe(503)
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait after 503: %v", err)
	}

	if len(c.slept) != 1 {
		t.Fatalf("expected one wait, got %v", c.slept)
	}
	if c.slept[0] <= 250*time.Millisecond {
		t.Fatalf("a 503 did not slow anything down: waited %s", c.slept[0])
	}
}

func TestBackoffStepsUpAndRecovers(t *testing.T) {
	l := New(250 * time.Millisecond)
	withClock(l)

	l.Observe(429)
	l.Observe(429)
	if got := l.BackoffLevel(); got != 2 {
		t.Fatalf("two overload responses should step back twice, got level %d", got)
	}

	l.Observe(200)
	if got := l.BackoffLevel(); got != 1 {
		t.Fatalf("a clean response should ease off one step, got level %d", got)
	}
	for i := 0; i < 5; i++ {
		l.Observe(200)
	}
	if got := l.BackoffLevel(); got != 0 {
		t.Fatalf("a recovered server should end up unthrottled, got level %d", got)
	}
}

func TestBackoffIsBounded(t *testing.T) {
	l := New(250 * time.Millisecond)
	withClock(l)

	for i := 0; i < 50; i++ {
		l.Observe(503)
	}
	if got, max := l.BackoffLevel(), len(backoffSteps)-1; got != max {
		t.Fatalf("backoff level ran past its cap: got %d, max %d", got, max)
	}
}

// A dropped connection says nothing about the server's capacity. Backing off
// on it would turn flaky wifi into an ever-slower sync.
func TestATransportFailureIsNotTreatedAsOverload(t *testing.T) {
	l := New(250 * time.Millisecond)
	withClock(l)

	l.Observe(0)
	if got := l.BackoffLevel(); got != 0 {
		t.Fatalf("a transport error was treated as server overload, level %d", got)
	}
}

// Someone pressing Cancel must not be made to sit through a backoff.
func TestWaitRespectsCancellation(t *testing.T) {
	l := New(250 * time.Millisecond)
	c := withClock(l)
	c.cancel = context.Canceled

	ctx := context.Background()
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("first Wait should not sleep at all: %v", err)
	}
	if err := l.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected the cancellation to surface, got %v", err)
	}
}

// Two callers waiting at once must be spaced apart rather than both waking to
// the same instant - otherwise concurrency defeats the ceiling entirely, which
// is the specific future this exists to bound.
func TestConcurrentWaitersAreSpacedApart(t *testing.T) {
	l := New(250 * time.Millisecond)
	c := withClock(l)
	ctx := context.Background()

	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	// Two more slots claimed without time advancing on its own.
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("Wait: %v", err)
	}

	var total time.Duration
	for _, d := range c.slept {
		total += d
	}
	if total < 500*time.Millisecond {
		t.Fatalf("three requests were spaced by only %s in total; the ceiling is not holding", total)
	}
}
