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

Questions 27 and 28 both landed 2026-08-09 (autopilot): Q27 confirmed the
warm-session prediction (4.03% total wall-clock delta, but only 1.14% of it
inside the crawl); Q28 (local only) pinned the rest on `go test`'s own
build/cache-staleness check, not raw compilation or process spin-up. Full
write-up in `docs/sync-speed-model.md` and `docs/BACKLOG.md`'s "Done
recently". **Next up:** Question 24 (repeated-trial safety check for
preview-blocking under load — real-account, correctness), `docs/sync-speed-model.md`,
"Next experiment". Real-account load already spent today on Question 27's
before/after batch, so Question 24 waits for a fresh day per
`docs/server-load.md` discipline.
