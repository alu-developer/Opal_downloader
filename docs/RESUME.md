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

**Autopilot run, 2026-08-06: about to run Question 22's next cycle.**
`docs/BACKLOG.md`'s Now item and `docs/sync-speed-model.md`'s "Next
experiment" both point here — same probe as before
(`showallsignallatency_probe_test.go`, `OPAL_SIGNAL_LATENCY_TRACE=1`),
default 2 runs, waiting for a cycle where the Vorlesung-node loss actually
reproduces so `signalWaitErr` can be read on a real failing sample. Prior
cycle (2026-08-04) got 2 clean runs, no failure, prediction untested. If this
process dies mid-run, the probe itself already appends to
`tmp/signal-latency-probe.log` incrementally — check there for any run this
turn produced before the kill.

---

**Two sync-speed cycles done today (2026-08-04). Question 22 (real-account,
deferred, still open) then Question 8 (local-only, closed, decisive).**

Question 22's first cycle: the failure did not reproduce, so `signalWaitErr`
was never read on a failing sample — but the 2 clean runs (167ms/177ms) sit
in the same tight band as Question 21's 2 failing runs (196ms/206ms), 4
samples inside a 40ms span with no outlier, weakly favouring
`context-destroyed` over "pure delay" without confirming it. Deferred to a
later cycle — real-account load from this sub-thread is now 10 two-course
contention crawls today (`docs/server-load.md`).

Question 8 (picked up specifically because it needs no real account): closed
with a clean local probe (`ctxroutecost_probe_test.go`,
`OPAL_ROUTE_COST_PROBE=1`, 3 repeats, no OPAL login) — cache-off is 60.7% of
the `ctx.Route` tax, the Fetch pause/resume round trip only 3.1%, and raw CDP
genuinely decouples them (a `Fetch.enable` session held the cache intact in
all 3 repeats, no `Network.setCacheDisabled` call needed). That refutes
"Playwright couples the two rigidly" — it's `ctx.Route`'s own driver-side
choice, not a CDP requirement. Opens **Question 23**: rewrite
`blockInlineFilePreviews` (`previews.go`) to drive `Fetch` through a raw
`CDPSession` instead of `ctx.Route`, to keep the previews saving while
mostly dropping the tax — an implementation task, not a probe, still needs
the byte-diff safety bar before shipping. Full write-up in
`docs/sync-speed-model.md`'s "Previous experiment (Question 8, closed)" and
"Next experiment" sections. (The first version of the probe hung on its 3rd
repeat from a reentrancy bug in its own event handler — fixed, documented in
the commit and the model file, not a finding about `ctx.Route` itself.)

Next up, either is available: **Question 22** on a later cycle (real-account,
deferred for load), or **Question 23** (local implementation + a real-account
byte-diff before shipping, bigger scope than a probe).

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
