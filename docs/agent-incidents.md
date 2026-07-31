# Agent incidents

The post-mortems behind `docs/agent-operating-model.md`. Split out on
2026-07-31: the operating model had grown to 529 lines, most of it the story of
how each rule was learned, and a session reading "how do I organise myself?"
had to wade through all of it first.

Nothing here is a rule. The rules are in the operating model; this is why they
say what they say. Read it when a rule looks arbitrary, or before proposing to
remove one.

---

## 2b. When the usage limit is hit mid-run

Hitting the limit stops the turn dead, instantly. **No `Stop` hook runs**, so
every guard described above — expiry, iteration cap, budget thresholds — is
bypassed entirely. This is not a rare edge case; it happened on 2026-07-23 and
is the reason the machinery below exists.

### What went wrong on 2026-07-23

Every rate-limit guard the repo had lived on the `Stop` hook, i.e. *between*
turns. A single long turn therefore ran past the budget with nothing watching
it, and was killed mid-run. The session record showed **1–2 autopilot
continuations against a cap of 20** — the gate was never given a chance to
fire, because the run never reached a stopping point.

Worse, nothing recorded the kill. Working out what had even happened, hours
later, meant comparing commit timestamps (last commit 11:46) against
rate-limit-window reset arithmetic. A failure mode the maintainer explicitly
cares about was invisible after the fact.

### The design rule: make the wall cheap, do not try to predict it

The budget data cannot support prediction. It is a **floor** that can be an
hour stale (see below), and the one attempt at a precise estimator was removed
the day it was written for reporting 83.5% against a real 46%. Anything built
on "we have N% left" is building on sand.

So the goal is not avoidance. It is that **being killed costs the current turn
and nothing more**. Three pieces, in the order they fire:

| Piece | Hook | Job |
|---|---|---|
| `budget-guard.ps1` | `PreToolUse`, no matcher | The only check that runs *during* a turn. As the floor climbs it tells the assistant, mid-turn, to commit and write down where it got to. |
| `turn-failure-checkpoint.ps1` | `StopFailure` | Fires *because* the turn died. Records what happened and captures uncommitted work. |
| `session-start-autopilot.ps1` | `SessionStart` | Hands the next session the failure record and `docs/RESUME.md`. |

**The rungs** (worst window wins; thresholds are on the floor, so the real
figure is always at least this):

| Rung | 5h | 7d | Effect |
|---|---|---|---|
| 1 notice | ≥50% | ≥65% | Work in savable increments; keep `docs/RESUME.md` current. |
| 2 checkpoint | ≥70% | ≥80% | Commit what is correct now, even mid-task. Update the resume note. |
| 3 critical | ≥80% | ≥85% | As above, plus new subagent launches are **denied**. |

Advice is throttled — once when the rung rises, then at most every 15 minutes.
An unthrottled reminder on every tool call would burn the budget it protects.

**Rung 3 is not a stop condition.** It says "stay savable", not "stop working".
Ending a run remains the guards' call or the maintainer's, exactly as in §1.

### What survives a kill

- **`docs/RESUME.md`** — tracked in git, deliberately messy, holds the thought
  that would otherwise exist only in a context window. Keep it current *while*
  working; the end of a task is precisely the part that does not always
  arrive.
- **Uncommitted work** — `turn-failure-checkpoint.ps1` runs `git stash create`
  and points `refs/wip-checkpoints/<unix>` at the result. That captures the
  tree in a recoverable commit **without touching the working tree, the index,
  the stash, or any branch**. Deliberately not a real commit: committing
  half-finished work unattended, at an arbitrary instant, is a side effect a
  hook has no business causing. The next session is told the SHA.
- **`.claude/queue/LAST_FAILURE.json`** and an append-only
  `turn-failures.log`, so a recurring pattern is visible instead of each
  failure erasing evidence of the last.

### What does *not* survive: a background process

A command started in the background belongs to the session that started it.
Its output goes to a per-session scratch file, and both die together — a run
still in flight when the turn ends leaves nothing behind at all.

This is not hypothetical. On 2026-07-27 an unattended run started a multi-
minute live network trace, hit its iteration cap, and ended with
`docs/RESUME.md` reading "second run in flight". The next session had no
result, no output file, and no way to tell a finished run from a lost one —
so it re-ran the whole thing.

Two rules follow:

- **A long verification run must write its result to a file** under `tmp/`
  (gitignored) or `docs/`, not only to stdout. `internal/scraper/
  network_trace_probe_test.go` is the worked example.
- **`docs/RESUME.md` must never describe a background process as in flight**
  without saying where its result will land. "Running, output at X" is a
  handoff; "running" alone is a dead end for whoever reads it next.

### Automatic resumption

Everything above makes a kill *cheap*. This makes work *restart*. The
maintainer asked for it explicitly on 2026-07-23 ("I don't want to have to open
a new session again"), after being told what it costs.

**First, a correction worth keeping.** An earlier version of this section said a
session-only cron "cannot rescue a session killed by the limit". That reasoning
is wrong: a usage limit kills the **turn**, not the session — the REPL stays
alive and idle, which is exactly when cron fires. The real reason the cron
approach never helped is duller: it was only ever written down here as an
instruction to schedule one, and nobody ever did. An instruction is not a
mechanism.

**One mechanism, not two.** The `OpalDownloader-ResumeRunner` scheduled task
survives closed terminals, reboots and logouts; it stops only when unregistered
or when `AUTOPILOT.OFF` exists.

An in-session `CronCreate` job was considered as a second layer and
**deliberately rejected**, despite being the obvious pairing:

- Its only real advantage is continuing *this* conversation with its context
  intact. But after a kill that is a liability, not a benefit — resuming a
  large context costs far more than a fresh session reading `docs/RESUME.md`,
  which is precisely what that file was built to make cheap.
- Every cron fire is a model turn, including one that concludes there is
  nothing to do. The scheduled task's gates are free.

So the cheap path *is* the fresh session. Adding cron would have meant paying
more to preserve something the resume note already replaces.

**The property that makes this affordable:** `.claude/hooks/resume-runner.ps1`
does all its gating in PowerShell — off switch, already-running, cooldown,
budget rung, is-there-actually-work — so **a quiet hour costs no model turn**.
The old cron design could not have this: every fire is a model turn, including
one that instantly concludes there is nothing to do. A resume mechanism that
costs tokens to say "nothing to do" is firing precisely when tokens are scarce.

### The deadlock this had to solve first

`rate-limit-status.json` is written by the status line, which only runs inside
a live `claude` session (see "the budget signal" above). This runner exists for
when there is **no** live session — so nothing refreshes that file while it
waits. It sits frozen at whatever the last dying session wrote.

Frozen is harmless while a window is still running: usage only climbs, so an
old figure remains a valid floor and "still too high" stays true. The trap is
what happens when both windows' `resets_at` finally pass. Every reading becomes
unusable — an expired window describes a window that no longer exists — and
that moment is *exactly* the moment the quota came back.

The first version treated "no usable reading" as a reason to give up. That
would have deadlocked the whole feature: it needs fresh numbers to decide it
may start a session, and only a session produces fresh numbers. The hourly task
would have logged `refusing to guess` forever, in silence. The maintainer
spotted it by asking the obvious question — where do the fresh numbers come
from? — before it ever ran in anger.

So an unusable reading is now the one condition worth spending a keep-warm
launch on: it forces a sync (`rate-limit-keepwarm.ps1 -Force`) and re-reads.
That is the sole case where a quiet fire costs anything, it happens only at a
window rollover, and the cost is one idle `claude` startup (~14s, measured).
`-Force` is required because the ordinary reuse path hands back the same stale
process — an idle `claude` makes no API calls, so it re-stamps its cached
figures without ever learning new ones.

A reading that *is* usable never triggers a refresh, since confirming a valid
floor would be pure waste.

Set it up with `scripts/register-resume-task.ps1` (`-Status` to inspect,
`-Remove` to unregister). It runs hourly while logged on.

### Two things it had to learn the hard way (2026-07-26)

**Its gates were right and it could not launch anything.** For two days every
fire either skipped correctly or logged `launch-failed  %1 is not a valid Win32
application`. `Start-Process -FilePath "claude"` does not walk PATHEXT the way
the shell prompt does — the Windows loader takes the first PATH match by name,
which is npm's extensionless POSIX shim. It now resolves explicitly to a
`.cmd`/`.exe` (`-WhichClaude` prints what it would run), and the prompt goes
over **stdin**, because a `.cmd` runs under cmd.exe and a multi-line prompt
passed as an argument ends at its first newline.

Nobody noticed because the runner's only output is a log line and nothing reads
it. `SessionStart` now surfaces unacknowledged `launch-failed` entries to the
next session, once each. **A watchdog whose failures are silent is worse than
none — it looks like a working safety net.**

**Stopping a run means stopping the tree.** The recorded pid is the `cmd.exe`
wrapper, not the agent. Killing it alone leaves `claude.exe` orphaned and still
editing the worktree; on 2026-07-26 an orphan ran on for five minutes and its
changes landed in an unrelated commit. Use
`.claude/hooks/resume-runner.ps1 -Stop`, which kills the recorded pid *and its
descendants*.

And because an unattended run must not join a session already working in the
tree, `budget-guard.ps1` stamps `.claude/queue/.session-heartbeat.json` on every
tool call; the runner skips while that stamp is under 20 minutes old. It is
deliberately not "is any `claude` process alive" — the keep-warm process is
permanently alive and idle, so that test would have vetoed every launch forever,
the same deadlock shape as the one above.

**Unattended runs are bounded by construction**, since nobody is watching:

- `OPAL_UNATTENDED_RESUME=1` makes `SessionStart` arm **5** iterations / 2h
  instead of the usual 20 / 4h. The `claude` CLI has no `--max-turns`, so that
  marker *is* the bound.
- `--model sonnet`, per §2. Escalating to Opus is a call the maintainer makes
  in person; an unattended run does not get to make it.
- A 2-hour cooldown, so a run that dies on startup cannot become a relaunch
  loop that drains the budget faster than working would have.
- It resumes only at rung 0–1, gating on the **worst** window. That matters:
  the situation this was built in had 5h freshly reset to ~0% while 7d sat at
  86%, and resuming on the healthy-looking number would have spent the scarce
  one.

The unattended prompt tells it to keep `docs/RESUME.md` current, commit as it
goes, and **not** to make decisions reserved for a human — those get written
into `docs/BACKLOG.md` as open questions instead.

### The machine has to be awake, and logged on (2026-07-29)

Every gate above was healthy and the runner still did nothing for **19 hours**.
Two independent faults, both silent, both invisible in the runner's own log
because neither one ever reached it.

**1. The hourly trigger was refused, not deferred.** From 2026-07-28 21:52 to
2026-07-29 16:52 every tick logged Task Scheduler event **332**, *"did not
launch because user was not logged on when the launching conditions were met"*.
This laptop idles in Modern Standby, which to an `InteractiveToken` principal
reads as nobody being logged on. `StartWhenAvailable` did not replay the missed
ticks — a refusal is a decision — and the machine leaving standby four times
that night changed nothing. `Get-ScheduledTaskInfo` said `State: Ready`
throughout; the only visible symptom was `NumberOfMissedRuns` climbing to 19,
which nothing looked at.

Fixed by triggering on moments an interactive token demonstrably exists:
**workstation unlock** and **logon**, alongside the hourly tick. That is also
when resuming is wanted — the maintainer has just come back to the machine.
`scripts/register-resume-task.ps1 -Status` now prints the missed-run counter and
points at event 332, because that number is what would have shown this in
seconds.

**2. The one launch that did happen was frozen 90 seconds in.** The 2026-07-28
21:45 run started a 30-minute background `go test`, said *"I'll continue once it
reports back"*, and its transcript ends mid-sentence at 21:46:43. The machine
had entered Modern Standby. Nothing was committed, stdout was 0 bytes, and the
runner's log still said `launched` and nothing else — a dead run and a working
one were indistinguishable.

So the agent is no longer started bare. `.claude/hooks/unattended-run.ps1`
wraps it and:

- holds `SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED)` for the
  life of the run, so idle standby cannot freeze it. Not
  `ES_DISPLAY_REQUIRED` — the screen should still go dark — and closing the lid
  still sleeps the machine, which is correct.
- waits, then writes **what the run achieved**: exit code, wall clock, bytes of
  output, commits gained. A run that exits clean in under two minutes having
  written nothing is labelled `run-died-early`, because that is the signature of
  this incident and not of a quick success.

And because holding a laptop awake on battery is worse than the problem it
solves, the runner now **refuses to launch unless on mains**. A missed hour on
battery costs nothing; the next tick after it is plugged in picks the work up.

**3. It also launched into a tree someone was working in.** The heartbeat gate
only sees sessions opened *in this repo* — the blind spot at the top of this
document. On 2026-07-29 a session opened in the home directory was editing these
very hooks when the tick fired; it saw no heartbeat and launched an agent that
committed twice alongside uncommitted human edits. The runner now also skips
when the **worktree was modified in the last 30 minutes**, which needs no hooks
in the other session at all. The age window is the design, not a detail: a bare
dirty check would wedge the runner shut forever the first time a run died
leaving half an edit behind.

