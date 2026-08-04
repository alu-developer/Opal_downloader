package scraper

import (
	"fmt"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// OPAL's section pages are Wicket applications, and the "Alle anzeigen"
// ("show all") pagination control expands the file list via a Wicket AJAX
// call rather than a navigation. Wicket publishes an exact completion signal
// for that call: Wicket.Event.Topic.AJAX_CALL_DONE. This file arms a
// subscription to it before the click and awaits it after.
//
// WHAT THE SIGNAL DOES AND DOES NOT MEAN - read before changing this.
//
// It means the AJAX call finished. It does NOT mean the expanded DOM is
// complete. Queue task research-wicket-render-completion-signal (2026-07-20)
// concluded it was trailing-safe (0/8 parity mismatches, firing at
// 156-184ms), and a first version of this change acted on that by reading
// candidates once at DONE and skipping the stability poll entirely. A live
// full-account parity run refuted it: 290 files against a 342-file baseline,
// 52 lost, every one of them the tail of the largest paginated section in
// Softwaretechnologie - exactly the past-page-1 loss the poll exists to
// prevent. The research had only covered sections small enough to hide it.
//
// So the signal is used for what it can be trusted for and nothing more:
//
//   - It replaces the fixed contentFallbackWaitMs settle wait, which has
//     nothing left to wait for once the call is done.
//   - Its FAILURE variant is a real re-click trigger, in place of
//     inferring a dropped expansion from a Playwright error string.
//   - waitForStableExpandedCandidates STILL RUNS afterwards. Do not
//     "optimise" that away without new evidence from a 150+ file course.
//
// Measured live 2026-07-21 across the full account: armed=6 signalled=6
// failed=0 timedOut=0, i.e. the path engages on every show-all expansion and
// the signal always arrives. File parity byte-for-byte identical to baseline
// on two consecutive runs.
//
// Scope note: this applies ONLY to the post-click expansion. The initial
// per-section render needs no signal and has none - the same research found
// file rows are server-rendered in the initial document response, with Wicket
// firing zero AJAX on file-bearing sections. waitForContentSettled and
// waitForStableSectionContent are deliberately left alone.

// wicketWatchVar is the page-global the arming script writes its counters to.
// Namespaced to avoid colliding with anything OPAL itself defines.
const wicketWatchVar = "__opalWicketExpandWatch"

// armWicketExpansionWatch subscribes to Wicket's AJAX completion topics on
// page, so a later awaitWicketExpansionDone can tell whether the expansion
// call finished and whether it failed.
//
// MUST be called before the click that triggers the expansion: Wicket
// delivers these topics to whoever is subscribed at fire time, so a
// subscription installed afterwards can miss a fast call entirely.
//
// Returns armed=false (with no error) when the page is not a Wicket page, or
// exposes no Event/Topic API - both are legitimate states, not failures, and
// simply mean the caller must fall back to the count-stability poll.
func armWicketExpansionWatch(page playwright.Page) (armed bool, err error) {
	if page == nil {
		return false, nil
	}

	script := fmt.Sprintf(`() => {
		const state = { done: 0, failed: 0 };
		window[%q] = state;
		if (typeof Wicket === 'undefined' || !Wicket.Event || typeof Wicket.Event.subscribe !== 'function') {
			return false;
		}
		const topics = Wicket.Event.Topic;
		if (!topics || !topics.AJAX_CALL_DONE || !topics.AJAX_CALL_FAILURE) {
			return false;
		}
		Wicket.Event.subscribe(topics.AJAX_CALL_DONE, function () { state.done++; });
		Wicket.Event.subscribe(topics.AJAX_CALL_FAILURE, function () { state.failed++; });
		return true;
	}`, wicketWatchVar)

	result, err := page.Evaluate(script)
	if err != nil {
		return false, err
	}
	ok, _ := result.(bool)
	return ok, nil
}

// awaitWicketExpansionDone blocks until the armed watch sees a Wicket AJAX
// call complete, or until timeoutMs elapses.
//
// failed reports whether Wicket signalled AJAX_CALL_FAILURE for that call.
// Checking it is not optional: AJAX_CALL_DONE fires for both outcomes, so a
// waiter armed on DONE alone would treat a dropped expansion as a completed
// one - reintroducing exactly the silent file loss PR #94 fixed. Live
// evidence: an aborted XHR yields before -> FAILURE -> complete -> done, with
// the row count unchanged.
//
// signalled=false means the event never arrived within the budget (a page
// navigation wiped the watch, the page instance was evicted server-side, or
// the click never triggered an AJAX call at all). The caller must then fall
// back to the count-stability poll rather than assuming completion.
//
// waitErr is the underlying error from the wait itself, previously discarded
// entirely. Question 21 (docs/sync-speed-model.md, 2026-08-04) found live
// that signalled=false does NOT reliably mean "waited the full timeoutMs and
// gave up" - two contention runs both resolved in ~200ms, the same order as
// a successful signal, not anywhere near the budget. That is only possible
// if WaitForFunction itself returned early with an error (e.g. the page's
// execution context was destroyed by a navigation) rather than genuinely
// timing out - a materially different failure shape from "the call is just
// slow", and the caller now needs the real error text to tell the two apart
// instead of guessing from the elapsed time alone.
func awaitWicketExpansionDone(page playwright.Page, timeoutMs float64) (signalled bool, failed bool, waitErr error) {
	if page == nil {
		return false, false, nil
	}

	predicate := fmt.Sprintf(`() => {
		const state = window[%q];
		return !!state && state.done > 0;
	}`, wicketWatchVar)

	if _, err := page.WaitForFunction(predicate, nil, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(timeoutMs),
	}); err != nil {
		return false, false, err
	}

	result, err := page.Evaluate(fmt.Sprintf(`() => {
		const state = window[%q];
		return !!state && state.failed > 0;
	}`, wicketWatchVar))
	if err != nil {
		// The call completed but the failure flag could not be read. Treat
		// that as "cannot vouch for success": reporting signalled=false sends
		// the caller down the poll fallback, which is the safe direction.
		return false, false, err
	}
	didFail, _ := result.(bool)
	return true, didFail, nil
}

// classifyWicketWaitError buckets the error awaitWicketExpansionDone returns
// into a short, grep-stable category for the audit log, rather than logging
// raw (and highly variable) Playwright error text. "none" means no error
// (signalled=true, or watch not armed at all).
func classifyWicketWaitError(err error) string {
	if err == nil {
		return "none"
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"):
		return "timeout"
	case strings.Contains(msg, "execution context") || strings.Contains(msg, "destroyed"):
		return "context-destroyed"
	case strings.Contains(msg, "navigat"):
		return "navigation"
	case strings.Contains(msg, "closed"):
		return "closed"
	default:
		return "other"
	}
}

// isWicketWatchUnavailableError reports whether err from arming the watch
// means the page simply had no usable Wicket context (rather than something
// the caller should treat as a real error). Evaluate fails this way when the
// page is mid-navigation or the execution context was replaced.
func isWicketWatchUnavailableError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "execution context") ||
		strings.Contains(msg, "navigating") ||
		strings.Contains(msg, "target closed")
}
