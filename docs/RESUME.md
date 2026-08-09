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

Six questions landed 2026-08-09 (autopilot): Q27 (warm-session delta
confirmed, mostly `go test` noise), Q28 (pinned that noise to `go test`'s
own cache-staleness check), Q2 and Q6 (both closed as documentation debt,
no live run), Q30 (OpenOLAT's folder browser does offer a participant
bulk-ZIP download, but bounded to the 86s first-sync floor, not the 207s
crawl floor — no live run needed to find that out), and Q24 (closed live:
6 trials, 0 truncated, but the original prediction used the wrong reference
rate — see below). Fifth-cycle report appended per the reporting cadence.

**Maintainer decision, same day: the "one live-run batch per day"
self-caution this campaign had been applying is retired** — server load was
never actually bound by that (`docs/server-load.md`'s real mechanisms are
unchanged), it was just this campaign rationing its own cycles. Proceeded
straight into Question 24's live run the same session rather than waiting.

**Question 24's run also surfaced a real methodology hazard: `go test`
silently cached and replayed one trial instead of re-executing it** (identical
env vars, no `-count=1` — confirmed by a byte-identical log with the network
call never actually happening). Every run after that used `-count=1`. This
cannot be retroactively ruled out for Questions 20/21's older "N clean runs
in a row" batches (their raw logs are gone) — recorded as an open caveat on
those two closed questions, not a reopening. **`-count=1` is now required
for any repeated-trial live-run design in this campaign** going forward.

Opened Question 31 (does the Question 25 fix also survive
`course_concurrency>1` contention — potentially reopening a previously
rejected concurrency lever): needs its own prediction written and committed
before it runs, per Rule 1 — not done yet, next up. Full write-up in
`docs/sync-speed-model.md`; short version also in `docs/BACKLOG.md`'s "Done
recently".
