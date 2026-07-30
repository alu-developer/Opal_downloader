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
- `.claude/hooks/hookbeat.ps1` (a22156b): every wired hook writes a liveness
  beat to `.claude/queue/.hookbeats/<hook>.json` on each fire. `Get-HookBeats`
  reads them back but **has no caller yet**.

**What's still missing, in order:**
1. A reader that turns hookbeats + git log into an actual verdict: which
   hooks haven't fired recently (dead-hook detection, the 2026-07-27
   failure), commits per session/turn, whether a turn ended with everything
   committed (`unattended-run.ps1` already has half of this - see BACKLOG's
   Noticed section on `run-left-uncommitted` - reuse rather than
   re-invent).
2. Where it runs: a hook (which one - Stop? SessionStart?) or a scheduled
   script alongside `resume-runner.ps1`. Decide by what it needs to see -
   if it's per-turn, a hook; if it's a rollup, scheduled.
3. Wire it, test it (mutation-test the same way every other hook here is:
   removing the check should fail an assertion), and write the verdict
   somewhere durable (a file under `.claude/queue/`, not just stdout).

**Do not build:** anything that grades whether the *code* is good - that is
the one thing `docs/work-quality.md` explicitly rules out (an agent auditing
its own output grades against the understanding that produced it). Process
only: liveness, volume, commit hygiene.

### Do not lose

- The maintainer will do their own GUI review "demnächst" (still true as of
  2026-07-30 evening). Do not treat the two "needs your eyes" backlog items
  as answered.
