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

## In flight: the self-audit / acceptance instance (item 3+5 of the
## 2026-07-30 evening batch)

Items 1, 2, and 4 of that batch are **done** - see BACKLOG's "Done recently"
and `docs/sync-speed-campaign.md`/`docs/work-quality.md` for the record.
This is the one still open, and it's the big one: `docs/work-quality.md`
(commit 775a86b) already names the cause and drafts a definition of done,
in response to the maintainer's

> "es fehlt iwie eine prüfinstanz oder so die sagt: ja, jetzt passts... und es
> werden halt meist minimalinvasive oder so halb-änderungen gemacht"

and the standing request to self-monitor **too many tokens**, **a hook
silently dead**, **too little actually worked on** - all three previously
only ever caught by the maintainer, never by the machinery.

**What exists so far:**
- `docs/work-quality.md`: names the two causes (no acceptance authority; the
  budget hook's own "prefer the small fix under pressure" instruction),
  states plainly what machinery can/cannot fix (process, never product), and
  drafts a definition of done (5 rules, rule 5 - budget was never the
  deciding factor, or the work is paused and RESUME says so - is the one
  with teeth).
- `.claude/hooks/hookbeat.ps1`: every wired hook writes a liveness beat.
  **Dead-hook detection now has a caller.** `Test-HookLiveness` (new this
  session) flags `autopilot-gate`/`noticed-gate`/`budget-guard` - the three
  hooks that fire on (almost) every turn - as dead if their last beat
  predates the newest commit (a commit can only land inside a turn that hit
  Stop and made a tool call, so that's a safe, false-positive-free
  comparison). `session-start-autopilot.ps1` calls it and surfaces
  `SELF-AUDIT: possible dead hook(s) - ...` as `additionalContext` at the
  start of every new session. 8 new tests in `scripts/test-hooks.ps1` (pure
  function + the SessionStart integration); `dev.ps1 all` green (202 hook
  assertions).
- **Real bug found and fixed while wiring this up:** `hookbeat.ps1` ignored
  `$env:OPAL_AUTOPILOT_QUEUE_DIR` entirely and always wrote to the real
  repo's `.claude/queue/.hookbeats`, unlike every sibling piece of hook state
  (the AUTOPILOT marker, `.session-heartbeat.json`, `resume-runner.log`).
  Every `scripts/test-hooks.ps1` run (including the ones earlier this
  session) was overwriting the real beats with test-invocation timestamps -
  which would have made the dead-hook check above permanently blind, since
  running the test suite kept "healing" the very thing it's supposed to
  catch. Fixed (`Get-HookBeatsDir` now checks the env var first); the
  polluted real beat files were deleted so they reflect reality again
  (self-heals on the next real hook firing, i.e. immediately).

**All three named symptoms now have a detector.** The second one landed this
session too: `session-start-autopilot.ps1` persists the budget floor + HEAD
commit to `.claude/queue/.session-budget-audit.json` on every session start,
and on the next one compares - if a window's floor rose >=15 points since
then with 0 commits in between (same-window only, window resets excluded,
previous commit must be a real ancestor of HEAD or it falls back to "no
baseline"), it surfaces `SELF-AUDIT: ... 'too many tokens for too little'`.
7 new tests (fresh baseline, real rise+flag, below-threshold, window-reset,
non-ancestor-commit fallback). `dev.ps1 all` green (209 hook assertions).

**Not done, and worth being honest about:** none of this has been observed
firing against real production sessions yet - both self-audits (dead-hook,
budget-vs-commits) are unit/integration-tested against synthetic state, not
watched catching an actual live incident. That's the same gap every hook in
this family started with (`docs/agent-operating-model.md` names it), so it
is not a special weakness of this feature, but don't oversell it as proven
in the field.

**Still open, lower priority than the above:**
1. "Too little actually worked on" is currently only detected for unattended
   runs (`unattended-run.ps1`'s existing verdict), not interactive sessions.
   Extending it there would need a definition of "too little" for a session
   the maintainer is actively driving, which is a fuzzier bar than an
   autopilot run's commits-vs-minutes - possibly not worth building; the
   maintainer noticing is arguably fine for this one specifically, since it's
   the most product-judgment-shaped of the three.
2. Write the verdicts somewhere more durable than SessionStart
   `additionalContext` (which only lives in that one transcript) - e.g.
   append one line per finding to a small log under `.claude/queue/`, so a
   pattern across many sessions ("this keeps happening") becomes visible
   without anyone having to remember individual transcripts.

**Do not build:** anything that grades whether the *code* is good - that is
the one thing `docs/work-quality.md` explicitly rules out (an agent auditing
its own output grades against the understanding that produced it). Process
only: liveness, volume, commit hygiene.

### Do not lose

- The maintainer will do their own GUI review "demnächst" (still true as of
  2026-07-30 evening). Do not treat the two "needs your eyes" backlog items
  as answered.
