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

**In flight: Question 19** (`docs/sync-speed-model.md`) - does the Vorlesung
folder's "show all" click get its Wicket `AJAX_CALL_DONE` signal under
`course_concurrency=2` contention, or not? Landed so far (both committed,
`git log` has the exact commits): (1) `expandShowAllInSection` (crawl.go) now
logs `expansionSignalled` itself via `auditLog("wicket-expand-signal", ...)`
- previously only observable indirectly through whether the settle wait got
skipped; (2) `internal/scraper/showallsignal_probe_test.go`
(`TestShowAllSignalUnderContention`, gated on `OPAL_SHOWALL_SIGNAL_TRACE=1`)
runs the known-failing contention condition (Algorithmen und
Datenstrukturen + Softwaretechnologie, `course_concurrency=2`, default
debounce) N times (default 3, `OPAL_SHOWALL_SIGNAL_RUNS` to change it),
greps the captured `--debug-clicks` output for the Vorlesung node
(`CourseNode/1775615795226691003`) and reports, per run, whether
`expansionSignalled` was true/false and whether `warnShowAllTruncated` fired
for it. Output also goes to `tmp/showall-signal-probe.txt`.

**Not yet done: actually running it.** The failure is intermittent (2 of 4 in
the archived Question 16/17 data), so a run that reproduces nothing is not a
result - rerun with `OPAL_SHOWALL_SIGNAL_RUNS` raised rather than concluding
it's fixed. Read the test file's header comment for the prediction and
failure criterion before running - they're written down there, not repeated
here. Once a real result lands (reproduced at least once, or genuinely
exhausted several rounds with nothing), write it into
`docs/sync-speed-model.md`'s Question 19 section and clear this note back to
the placeholder.

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
