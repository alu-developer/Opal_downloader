package scraper

import (
	"fmt"
	"strings"

	"github.com/mxschmitt/playwright-go"
)

// OPAL's section pages are Wicket applications, and the "Alle anzeigen"
// ("show all") pagination control expands the file list via a Wicket AJAX
// call rather than a navigation. Wicket publishes an exact completion signal
// for that call - Wicket.Event.Topic.AJAX_CALL_DONE - which is trailing-safe:
// live measurement (queue task research-wicket-render-completion-signal,
// 2026-07-20) found the DOM already final when it fires, 0/8 parity
// mismatches against a read taken 6 further seconds later, at 156-184ms
// instead of the 800ms-1.6s+ the count-stability poll needs.
//
// This file implements arming that signal before the click and awaiting it
// after, so expandShowAllInSection can stop *inferring* completion from a
// candidate count that stopped growing. That inference is not merely slow:
// it is the failure mode queue task
// fix-candidate-stability-poll-concurrent-crawl-race traced concurrent-crawl
// file loss to, because a stable-but-incomplete read is indistinguishable
// from a finished one.
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
func awaitWicketExpansionDone(page playwright.Page, timeoutMs float64) (signalled bool, failed bool) {
	if page == nil {
		return false, false
	}

	predicate := fmt.Sprintf(`() => {
		const state = window[%q];
		return !!state && state.done > 0;
	}`, wicketWatchVar)

	if _, err := page.WaitForFunction(predicate, nil, playwright.PageWaitForFunctionOptions{
		Timeout: playwright.Float(timeoutMs),
	}); err != nil {
		return false, false
	}

	result, err := page.Evaluate(fmt.Sprintf(`() => {
		const state = window[%q];
		return !!state && state.failed > 0;
	}`, wicketWatchVar))
	if err != nil {
		// The call completed but the failure flag could not be read. Treat
		// that as "cannot vouch for success": reporting signalled=false sends
		// the caller down the poll fallback, which is the safe direction.
		return false, false
	}
	didFail, _ := result.(bool)
	return true, didFail
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
