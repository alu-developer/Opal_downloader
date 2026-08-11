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

Nothing in flight. Last landed: 2026-08-11 autopilot cycle closed Question
40's live verification (empty diff, 349/349, 41.6s vs 56.7s baseline,
inside the predicted 35-45s window) and opened Question 41 (does a second,
different-day run confirm it, and is default-promotion in scope for that
cycle) in `docs/sync-speed-model.md`. Not promoted to default - this
project's own bar is two clean runs, and both of Question 40's happened
minutes apart in one session. Next sync-speed cycle should pick up Question
41 (needs a different day, so not same-session repeatable) or Question 39
(process/product question, ranked above it, no live run needed).
