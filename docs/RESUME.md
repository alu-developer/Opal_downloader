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

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

**2026-07-30: auditing the other docs `CLAUDE.md` cites as "current state".**

`docs/setup-friction.md` turned out to list 8 shipped items as open suggestions
(fixed, `8ce2894`). `CLAUDE.md`'s philosophy section names two more files the
same way - `docs/installer-plan.md` and `docs/browser-profile-strategy.md` - so
the same rot is likely, and it has a real cost: I nearly re-implemented a
README fix that had been in place for weeks.

Method, same as last time: every status claim checked against the code before
the doc is touched, and each corrected row records how it was verified. No
claim goes in that I have not read the implementation for.

Nothing committed yet this iteration.
