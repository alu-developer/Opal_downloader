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

**Question 20 is closed (2026-08-04): inconclusive, honestly reported as
such.** Raising the signal-wait ceiling to 15000ms produced 3 clean runs in a
row (248/248/248 files) - but the test's own pre-written verdict logic says
that is not proof of "pure delay" at this condition's ~33-50% historical
failure rate; 3 clean runs happen by chance often enough not to count. Full
data in `docs/sync-speed-model.md`'s Question 20 write-up.

Next up, already decided: **Question 21** - timestamp the click-to-signal
latency itself (not just a threshold boolean) across many contention runs,
to see whether it's bimodal (usually ~150ms, occasionally stuck for seconds -
points at something dropping/blocking outright) or a smooth spread (points at
ordinary queueing delay under load). Cheap instrumentation, but needs more
than 3 runs to see a real distribution - **note it explicitly asks to be
spread across more than one cycle**, since Questions 19+20 already spent 6
two-course contention crawls against the real account today
(`docs/server-load.md`). Prediction and failure criterion are in the model
file's "Next experiment" - read them before running.

---

**Question 19 is closed (2026-08-04), and its own prediction was wrong.**
`expansionSignalled` (now logged, `crawl.go`/`wicket-expand-signal`) came back
**false** in both runs that lost the Vorlesung tail - Wicket's `AJAX_CALL_DONE`
never arrived within the 4000ms budget at all, not late. That refutes
Candidate B (the signal arrives, the read is just early) and re-opens
Candidate A in a sharper form: pure delay under contention vs. a call that is
never actually issued/received. Full data and the split are in
`docs/sync-speed-model.md`'s Question 19 write-up.

Next up, already decided: **Question 20** (`docs/sync-speed-model.md`,
"Next experiment") - raise `wicketExpansionSignalTimeoutMs` well past 4000ms
for one diagnostic run and see whether a failing run's signal shows up before
the raised ceiling (pure delay, fix is a bigger budget) or never shows up at
all (fix is at the click/arm sequence, not the wait). Prediction and failure
criterion are already written down there - read them before running, not
after. Same contention condition as Question 19
(`internal/scraper/showallsignal_probe_test.go` is the probe to extend or
copy), same "expect repeats" caveat (2 of 3 this cycle, consistent with prior
cycles).

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
