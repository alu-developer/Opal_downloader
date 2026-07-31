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

## In flight: closing out the Noticed section (2026-07-31, autopilot turn)

Everything up to and including commit `fe92347` is committed and pushed
(working tree clean as of this note). That commit closed the sync-speed
campaign's sign-off question with a real decision (not building the risky
wait-change autonomously - see `docs/sync-speed-campaign.md`'s closing entry
and `docs/work-quality.md`), per the retrospective another session wrote
this same day (`ca56a99`) that says questions like that are mine to decide,
not defer.

Current task: the autopilot Stop hook wants a Noticed item picked and
either done+deleted, or deleted as stale. Two items remain in
`docs/BACKLOG.md`'s Noticed section:

1. Unattended run can't wait for a background job / orphan detection not
   fixed - about to be decided (not built): building more detection
   machinery for this is exactly the "wanting to build a gate is the signal
   to do the work instead" anti-pattern `docs/work-quality.md` just named.
   Leaning toward closing this as "decided not to build, same reasoning as
   the sync-speed item" rather than adding another hook.
2. User-Agent fix mechanism still a theory - live-account experimentation
   needed to resolve further, low value, not touched this session.

Next action: write that decision into BACKLOG.md (short), verify with
scripts/dev.ps1 all, commit, push. If nothing else clearly unblocked remains
after that, say so plainly rather than manufacturing more work - the
BACKLOG's Now/Next items are all genuinely maintainer-reserved (visual/UX
judgment, an explicit new-dependency policy call), not the deferral pattern
being corrected.
