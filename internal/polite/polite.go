// Package polite bounds how hard this tool is allowed to hit OPAL.
//
// WHY THIS EXISTS
// ---------------
// opal-downloader crawls every section of every course. On one real account
// that is ~284 section page loads per sync. One student doing that once a day
// is nothing. The maintainer's concern (2026-07-26) is the other end: "wenn
// viele es nutzen, dass es nicht zu einer extremen Last für den Server wird" -
// and they asked for it to be *set up* for the long term rather than checked
// once and forgotten.
//
// The load is on somebody else's infrastructure. OPAL is run by Bildungsportal
// Sachsen for every student in the state; this tool has no relationship with
// them, no agreement, and no way to be told it is being a nuisance except by
// being blocked. That asymmetry is the argument for a ceiling that exists
// whether or not anyone is currently worried about it.
//
// WHAT THIS DOES, AND WHAT IT DELIBERATELY DOES NOT
// -------------------------------------------------
// The default ceiling is **not binding today**. A serial crawl already spends
// about a second per section waiting for Wicket to render, so it sits near 1
// request/second on its own, comfortably under the limit. That is the point:
// this is not here to slow the tool down now, it is here so that a future
// change cannot speed it up past a defensible rate without someone deciding
// to raise this number on purpose.
//
// That matters because there is active pressure in the opposite direction -
// see docs/sync-speed-campaign.md, which is a standing effort to make syncs
// dramatically faster, and docs/server-load.md, which is where the trade-off
// between the two is written down.
//
// Backoff is the other half. A 429 or a 503 is OPAL saying it is struggling,
// and the only decent response is to slow down rather than retry into it.
package polite

import (
	"context"
	"sync"
	"time"
)

// DefaultMinInterval is the shortest gap allowed between two requests to
// OPAL, which is to say a ceiling of about 4 requests per second.
//
// Chosen to sit a few times above what the tool actually does today (~1/s)
// rather than as tightly as possible: a limiter that binds during normal
// operation would make every future performance measurement a measurement of
// this constant instead, which is how a safety limit quietly becomes the thing
// everyone works around.
const DefaultMinInterval = 250 * time.Millisecond

// Backoff levels applied when the server reports it is overloaded. Each
// further signal steps up, and a clean response steps back down - so a single
// blip costs a moment and a genuinely struggling server gets progressively
// more room.
var backoffSteps = []time.Duration{
	0,
	2 * time.Second,
	10 * time.Second,
	30 * time.Second,
	60 * time.Second,
}

// MaxBackoffLevel is the most throttled state the limiter will reach.
// Exposed so a diagnostic can report "level 2 of 4" rather than a bare number
// that means nothing without reading this file.
var MaxBackoffLevel = len(backoffSteps) - 1

// Limiter paces requests to one host. The zero value is not usable; call New.
type Limiter struct {
	mu           sync.Mutex
	minInterval  time.Duration
	last         time.Time
	backoffLevel int

	// waits and delayed count how often Wait was called and how often it
	// actually had to hold something back. The whole design rests on the
	// ceiling not binding during normal operation (see docs/server-load.md),
	// and that is a claim about the real world, so it has to be measurable
	// rather than asserted.
	waits   int
	delayed int
	held    time.Duration

	// sleep and now are seams, so the tests can drive time rather than
	// spending it. A rate limiter tested by actually waiting is a rate
	// limiter nobody runs.
	sleep func(context.Context, time.Duration) error
	now   func() time.Time
}

func New(minInterval time.Duration) *Limiter {
	if minInterval <= 0 {
		minInterval = DefaultMinInterval
	}
	return &Limiter{minInterval: minInterval, sleep: sleepCtx, now: time.Now}
}

// Wait blocks until the next request may be made. It returns ctx.Err() if the
// context is cancelled while waiting, so a user pressing Cancel is not made to
// sit through a backoff.
func (l *Limiter) Wait(ctx context.Context) error {
	l.mu.Lock()
	delay := l.delayLocked()
	l.waits++
	if delay > 0 {
		l.delayed++
		l.held += delay
	}
	// The slot is claimed before releasing the lock, so two goroutines waiting
	// at once are spaced apart rather than both waking to the same moment.
	l.last = l.now().Add(delay)
	l.mu.Unlock()

	if delay <= 0 {
		return nil
	}
	return l.sleep(ctx, delay)
}

func (l *Limiter) delayLocked() time.Duration {
	gap := l.minInterval + l.currentBackoffLocked()
	if l.last.IsZero() {
		return 0
	}
	if wait := l.last.Add(gap).Sub(l.now()); wait > 0 {
		return wait
	}
	return 0
}

func (l *Limiter) currentBackoffLocked() time.Duration {
	if l.backoffLevel >= len(backoffSteps) {
		return backoffSteps[len(backoffSteps)-1]
	}
	return backoffSteps[l.backoffLevel]
}

// Observe records what the server said, so the pacing can respond to it.
// status is an HTTP status code; 0 means "no response" (a transport error),
// which is deliberately not treated as overload - a dropped connection says
// nothing about the server's capacity, and backing off on it would turn a
// flaky wifi connection into an ever-slower sync.
func (l *Limiter) Observe(status int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch {
	case status == 429 || status == 503:
		if l.backoffLevel < len(backoffSteps)-1 {
			l.backoffLevel++
		}
	case status >= 200 && status < 400:
		if l.backoffLevel > 0 {
			l.backoffLevel--
		}
	}
}

// BackoffLevel reports how far the limiter has stepped back. Exposed so a
// diagnostic can say "we are being throttled" rather than leaving a slow sync
// looking like a slow tool.
func (l *Limiter) BackoffLevel() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.backoffLevel
}

// Stats reports how much this limiter actually got in the way.
func (l *Limiter) Stats() (waits, delayed int, held time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.waits, l.delayed, l.held
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
