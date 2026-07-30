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

**What's still missing:**
1. Volume/commit-hygiene rollup ("too little worked on", "too many tokens
   for too little"). `unattended-run.ps1` already computes commits/mins/
   bytes/dirty/unpushed per unattended run and logs a verdict - that only
   covers autopilot runs, not the maintainer's own interactive sessions,
   which is where they actually reported these symptoms. Possible next step:
   at SessionStart, read the previous session's budget-floor + HEAD commit
   from a small state file, compare to now, and flag "budget floor rose a
   lot, few/no commits landed" - operationalizes "too many tokens" without
   needing per-session token counts (which aren't exposed to hooks at all).
2. Write the verdict somewhere durable beyond the SessionStart
   `additionalContext` (a file under `.claude/queue/`?) so it survives past
   one session's transcript.

**Do not build:** anything that grades whether the *code* is good - that is
the one thing `docs/work-quality.md` explicitly rules out (an agent auditing
its own output grades against the understanding that produced it). Process
only: liveness, volume, commit hygiene.

### Do not lose

- The maintainer will do their own GUI review "demnächst" (still true as of
  2026-07-30 evening). Do not treat the two "needs your eyes" backlog items
  as answered.
