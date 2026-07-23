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

The SessionStart hook reads this file and hands it to the next session.

---

## In flight: unattended self-resume (2026-07-23)

**Maintainer decided this, don't re-ask:** they explicitly asked for automatic
self-restart ("du sollst dich selbst auch wieder zu einem passenden Zeitpunkt
neu starten. Ich will nicht nochmal extra eine neue session aufmachen
müssen."). The cost tradeoff was flagged beforehand and they asked for it
anyway. This closes the "Decide: should a killed run restart itself?" backlog
item.

**Correction to make:** `docs/agent-operating-model.md` §2b currently claims a
session-only cron "cannot rescue a session killed by the limit". That is wrong
and I wrote it — a usage limit kills the **turn**, not the session; the REPL
stays alive and idle, which is exactly when cron fires. The real reason the
cron approach failed on 2026-07-23 is that it was only ever *documented as an
instruction* to schedule one, and nobody ever did. Fix the reasoning, don't
just swap the mechanism.

**Design (two layers, because they fail differently):**

1. *In-session cron* — rescues a rate-limited session whose terminal is still
   open. Free to set up, immediate, but dies with the session and expires
   after 7 days.
2. *Windows Scheduled Task → `.claude/hooks/resume-runner.ps1`* — the durable
   one, survives a closed terminal. Its gate (budget rung, `AUTOPILOT.OFF`,
   cooldown, is-there-work) runs **in PowerShell, costing zero tokens**, and
   only launches a headless `claude` when the budget is actually healthy. That
   is the property the old cron design could not have: a no-op fire has to be
   free, or the resume mechanism becomes its own budget problem.

**Timing matters here and is not obvious:** 7d is at ~86%, above the Stop
gate's 80% threshold, and does not reset until **2026-07-25 07:00 local**. 5h
resets 19:00 today. So resuming this evening would burn the scarce 7d budget,
not the plentiful 5h one. The runner must gate on the *worst* window, which
`Get-BudgetRung` already does.

**Next action:** confirm `claude -p` flags (bounded turns?), then write
`resume-runner.ps1` + `scripts/register-resume-task.ps1`, tests, docs.

**Unattended runs must be bounded** — plan is `OPAL_UNATTENDED_RESUME=1`, which
`session-start-autopilot.ps1` reads to arm a small iteration cap instead of the
usual 20.
