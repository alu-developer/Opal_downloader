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

Phase 3 sync-speed cycle in flight, 2026-09-14 (autopilot, second cycle this
run). Prediction registered in `docs/sync-speed-model.md`'s "Next
experiment" (top entry): does a `node-st` page's raw HTML (Woche 05) carry
an inline per-file date next to `U05.pdf`'s link, a possible cheaper
"option E" for Question 45's Woche cluster. Plan: one
`httpDiscoveryFetcher().Get()` on the Woche 05 URL already resolved last
cycle, save to `tmp/woche05-raw.html`, grep by hand for `U05.pdf` and a
date-shaped string nearby - no parser yet. Next step: run it, record the
result in `docs/sync-speed-model.md`, clear this file.
