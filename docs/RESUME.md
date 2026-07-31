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

Unattended run 2026-07-31 evening: tree was clean, both Now items in
BACKLOG.md were blocked on the maintainer looking at the GUI. No display in
this session (`computer.screenshot` fails — pane can't composite frames), so
screenshots aren't a way to unblock it either. Turning both entries into
concrete options + a recommendation instead, per the scheduled task's "every
item blocked" fallback. Not touching `scheduler.Disable`'s missing-guard note
— investigated it, it's the intended single-task-per-machine design (CLI
comment: "there's exactly one underlying Windows Task Scheduler task"), not a
bug; changing it would fight the documented model, so leaving it as the
already-recorded FYI it is.
