# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end - the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The scheduled Desktop task's prompt reads this file first, so stale content
here sends an unattended run after work that is already done. Clear it.

---

_Nothing in flight._

**Question 21's first cycle is done (2026-08-04): too few samples to call
bimodal-vs-smooth, but it caught a wrong assumption the same day it was
written.** 2 live contention runs both showed `expansionSignalled=false`
resolving in ~200ms - the same order as a successful signal, not anywhere
near the 4000ms timeout the instrumentation's own comment (written a few
hours earlier) assumed a failure would consume. That is only possible if the
wait errored out fast rather than genuinely timing out - and the error text
was being discarded. Fixed: `awaitWicketExpansionDone` (`wicket.go`) now
returns the real error, classified via `classifyWicketWaitError` into
`signalWaitErr` (`none`/`timeout`/`context-destroyed`/`navigation`/`closed`/
`other`) on the `wicket-expand-signal` audit line. Full write-up in
`docs/sync-speed-model.md`'s Question 21 section.

Next up, already decided: **Question 22** (`docs/sync-speed-model.md`, "Next
experiment") - read `signalWaitErr` on a failing run. Prediction:
`context-destroyed`, tying this to the same mechanism
`waitForInteractiveLinks`'s `contextWasDestroyed` fallback already handles a
few lines below in `crawl.go`. Not yet run live. Reuses Question 21's probe
(`showallsignallatency_probe_test.go`, `OPAL_SIGNAL_LATENCY_TRACE=1`) as-is -
the new field is already wired into its output and into
`tmp/signal-latency-probe.log`. **Real-account load caution stands**: this
sub-thread has spent 8 two-course contention crawls today
(`docs/server-load.md`) - a couple more on a later cycle is enough, no need
for a large batch.

---

**Do not run Question 17's concurrency=1 control run.** It was the "next up" here
until 2026-08-03 and is now unnecessary: Question 17 was answered from the
archived run log instead (`tmp/frage16-run.log`, 4/4 correlation with
`warnShowAllTruncated`). Server-side variance is refuted, so there is nothing for
that run to rule out. No env knob needed, no probe change needed.

**Question 18 is closed and its alarm was false** - no files were ever missing
there, the detector was counting table rows instead of file rows and flagging an
enrolment table. Fixed and re-verified live the same day. If you find an older
note claiming the 345-file ground truth is short, it is wrong; that was my
prediction, and the run refuted it.
