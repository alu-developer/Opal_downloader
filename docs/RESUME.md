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

Next up, already decided and needing nobody: Frage 17's concurrency=1 control
run (`docs/sync-speed-model.md`) - the same course pair, same probe, twice at
`course_concurrency=1`, to rule out plain server-side variance before
blaming contention for the lost paginated section. `OPAL_DEBOUNCE_CONTENTION_COURSES`
already lets the probe take the pair; the concurrency it uses is hardcoded to
2 and needs a small env knob first.
