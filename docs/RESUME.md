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

**Question 22's first cycle is done (2026-08-04): the failure did not
reproduce, so `signalWaitErr` was never read on a failing sample — but the 2
clean runs (167ms/177ms) sit in the same tight band as Question 21's 2
failing runs (196ms/206ms), 4 samples inside a 40ms span with no outlier.**
That weakly favours the `context-destroyed` prediction over "pure delay"
(a queueing explanation would predict failing runs run *longer*, not
identical) but is not the confirmation the prediction needs — that still
requires an actual `expansionSignalled=false` sample with `signalWaitErr`
read on it. Full write-up in `docs/sync-speed-model.md`'s "Previous
experiment (Question 22, first cycle)" section.

Next up: **Question 22 stays open**, same probe
(`showallsignallatency_probe_test.go`, `OPAL_SIGNAL_LATENCY_TRACE=1`),
deferred to a later cycle rather than forced — landing on a failing sample is
partly luck at the condition's ~33-50% historical rate. **Real-account load
caution stands, raised**: this sub-thread has now spent 10 two-course
contention crawls today (`docs/server-load.md`). An alternative that adds no
real-account load if picked up next: **Question 8** (`ctx.Route` cost split,
cache-off vs. pause/resume) is reproducible with a synthetic local page, no
OPAL account needed, and has been open since Question 3 without anyone
starting it.

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
