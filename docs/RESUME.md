# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end — the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

**Three mechanisms agreed with the maintainer (2026-07-27)** to make three
habits stick, after they pointed out that CLAUDE.md asks me to *want* things
and that does not survive a long session. The pattern that does work is a check
that runs and fails, not a preference.

1. **Noticing** - DONE: `.claude/hooks/noticed-gate.ps1`, a Stop hook that asks
   once per session for one thing noticed and not done, to be appended to a
   "## Noticed" section in docs/BACKLOG.md. Fails open, fires once, silenced by
   AUTOPILOT.OFF. 9 assertions in scripts/test-hooks.ps1 (116 total).
2. **Removing** - DONE: `codebudget_test.go`, a hard ratchet on non-test code
   lines (11181). Growth fails the build; raising the number is a visible
   one-line diff. The first version of this idea was "report the number and
   justify it" and the maintainer rejected it correctly - a number I defend is
   a paragraph, not a change.
3. **Investigation** - DONE: CLAUDE.md now requires quoting the measurement
   behind any "blocked"/"dead"/"already tried" claim, and separates "do not
   re-litigate" from "do not investigate".

Still outstanding from before: the sync-speed measurement (see the entry above
this one - instrument where each section's ~1s actually goes; the constants
suggest ~170s of the ~227s run may be our own poll loop).
