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

Questions 27, 28, 2 and 6 all landed 2026-08-09 (autopilot, all local/no live
run): Q27 confirmed the warm-session prediction (4.03% total wall-clock delta,
but only 1.14% of it inside the crawl); Q28 pinned the rest on `go test`'s own
build/cache-staleness check; Q2 (the highest-ranked open item per the
2026-08-07 report) closed by connecting Q1, Q9's `MenuTreeRenderer` finding
and `httpdiscovery.go`'s own design comment — the abandoned 2026-07-21
HTTP-first crawler failed on 2 of 6 courses because it never walked the tree
via the browser, not because of client-side rendering; Q6 closed as a stale
premise the campaign had already retracted on 2026-07-30, three days before
this question was carried into the model file. Opened Question 29. That
empties the ranked question list of anything answerable without a live run —
full write-up in `docs/sync-speed-model.md` and `docs/BACKLOG.md`'s "Done
recently". **Next up:** Questions 24 and 29 both need a real-account live run
(repeated-trial safety check for preview-blocking under load, and whether the
browser's own tree walk re-fetches nodes it's already seen) — real-account
load already spent today on Question 27's before/after batch, so both wait
for a fresh day per `docs/server-load.md` discipline. If a fresh day is
available, check `docs/sync-speed-model.md`'s "Next experiment" section
first for which one to run. If not, the "When ideas run out" moves in
`docs/sync-speed-model.md` are the next thing to try, since the ranked list
is now empty of local-only work.
