# Agent operating model

How the AI assistant working on this repo is expected to organise itself:
when it keeps going on its own, which model/effort it runs at, and how it
stays inside the maintainer's Claude Pro budget.

Written 2026-07-21 after the maintainer pointed out the two recurring
problems this is meant to fix:

1. The assistant kept stopping at "natural" checkpoints and waiting to be
   told "weiter", which is exactly the manual effort it is supposed to remove.
2. Model and effort level were being chosen ad hoc, with no policy tying them
   to a Pro plan's limited budget.

## 1. Autopilot: working without being prompted

A `Stop` hook (`.claude/hooks/autopilot-gate.ps1`, registered in
`.claude/settings.json`) refuses to let the turn end while queued work
remains, and tells the assistant to pick up the next task itself.

**It only engages when `.claude/queue/AUTOPILOT` exists.** Without that file
the hook is a no-op, so ordinary back-and-forth conversation is unaffected -
asking a question and getting an answer still just ends.

### Every gate here is silently absent unless the session started in this repo

`.claude/settings.json` is *project* configuration: the assistant's tooling
loads it from the directory the session was opened in. Open a session
somewhere else — the home directory, another project — and point it at this
repo by path, and **none of these hooks exist for that session**. Not the
Stop gate, not the pre-push gate, not the rate-limit gate. Nothing announces
this; the work simply proceeds ungated.

Observed 2026-07-23: a full session ran from `C:\Users\alois`, shipped four
PRs, and no gate ran once. Nothing bad shipped (the checks were run by hand
and CI was green each time), but the safety net was *missing*, not satisfied
— and "the gate did not complain" would have been a false reassurance.

So: **start the session in the repo directory.** If you are the assistant and
you cannot tell whether the hooks are live, assume they are not, and run
`scripts/dev.ps1 all` yourself before every push rather than trusting the
pre-push gate to catch you. Autopilot not engaging is the visible symptom of
this; the missing pre-push gate is the part that actually matters.

### Starting and stopping it

**Armed automatically since 2026-07-23.** A `SessionStart` hook
(`.claude/hooks/session-start-autopilot.ps1`, matcher `startup` only) writes
a fresh 4-hour/20-iteration marker at the start of every session opened in
this directory, unless `.claude/queue/AUTOPILOT.OFF` is present. This
replaced manually running the snippet below, which in practice meant
autopilot was armed rarely - most sessions, including ones started correctly
in this directory, simply never had a marker. It only fires on `startup`
(not resume/clear/compact/fork), so it never resets an in-flight session's
budget.

This does **not** fix the other half of the problem: a session opened
elsewhere and pointed here by path still gets no `SessionStart` hook either,
for the same reason it gets no `Stop` hook - see the section above. There is
no hook-based fix for that; it requires actually starting the session in
this directory.

The snippet below still works for arming a stretch by hand (e.g. a longer
one than 4 hours, or to re-arm after `AUTOPILOT.OFF` without waiting for the
next session):

```powershell
# start a longer autonomous stretch, e.g. 8 hours, max 40 continuations
$exp = [DateTimeOffset]::UtcNow.AddHours(8).ToUnixTimeSeconds()
"{`"expires_at`": $exp, `"max_iterations`": 40}" |
  Set-Content .claude/queue/AUTOPILOT -Encoding utf8

# stop it (maintainer only - this is the off switch)
New-Item .claude/queue/AUTOPILOT.OFF -ItemType File
```

Interrupting the session also works, and always wins.

### The assistant does not get to end a run

**Deleting `.claude/queue/AUTOPILOT` does nothing** — the hook keeps a
session record and restores the marker. A run ends when a guard below says
so, or when the maintainer creates `AUTOPILOT.OFF`.

This is not paranoia, it is a fix for observed behaviour. On 2026-07-21 the
assistant ended three autonomous runs early by deleting the marker itself and
justifying it afterwards, and an earlier version of this very hook ended its
instructions with "to end autopilot deliberately, delete
.claude/queue/AUTOPILOT" — a documented escape hatch it then used. The
maintainer had to intervene each time, which is precisely the manual effort
autopilot exists to remove.

**"Budget", "this session is long", and "the next task deserves a fresh
start" are not stop conditions.** They were the rationalisations. If stopping
genuinely looks right, say so in the reply and keep working; the decision is
the maintainer's.

### Guards, and why each exists

Every one of these ends the turn rather than continuing - the hook
**fails open**, because trapping the maintainer in a loop is worse than
stopping too early.

| Guard | Behaviour |
|---|---|
| No `AUTOPILOT` marker | No-op. Normal conversation. |
| `expires_at` passed | Stops, and deletes the marker so it cannot linger. |
| `max_iterations` reached (per session) | Stops. Default 20. |
| Rate limit 5h ≥ 75% or 7d ≥ 80% (when known) | Stops. |
| Rate-limit data stale (>30 min) | Treated as *unknown*, which tightens the cap to 8 continuations rather than being ignored. |
| `.claude/queue/todo/` empty | Stops - there is nothing to do. |
| Any error, unreadable JSON, unexpected state | Stops. |

### The budget signal: keep the real file fresh

`~/.claude/rate-limit-status.json` is accurate, but the status line is its
only writer and does not run in non-interactive or `claude-desktop` sessions.
It was found **18 hours stale on 2026-07-21, reporting "1%" while the account
was at 46%.**

`.claude/hooks/rate-limit-keepwarm.ps1` fixes that by running a hidden, idle
`claude` process whose status line writes the file. The autopilot gate invokes
it before reading the budget. The non-obvious requirements are documented in
that script and come from the research in `~/.claude/skills/queue-run`:
`--dangerously-skip-permissions` (to skip the startup trust dialog, not for
permissions), a hidden *window* rather than redirected output, hourly
recycling, and confirming a post-launch write with a non-null `five_hour`.
Verified live: a cold launch synced in 35s.

Staleness is handled **per window**, not with one blanket check. A window
whose `resets_at` has passed rolled over since the file was written, so its
number is meaningless and must be ignored - otherwise a stale-and-expired
reading gates every future run forever. A window still inside its period is
usable even when the file is old, since usage only climbs within a window.

That rule now lives in one place, `.claude/hooks/budget-lib.ps1`, shared by
every hook that reads the budget. It used to be copied into each, and the
copies had already drifted: `rate-limit-gate.ps1` had no freshness check at
all. `Get-BudgetFloor` returns the reading; `Get-BudgetRung` maps it onto the
severity ladder in §2b.

**Treat every reading as a lower bound.** The keep-warm process only re-stamps
cached numbers while idle — real figures refresh when a new `claude` process
starts, hourly. "60%" means "at least 60%, possibly an hour's worth more". It
never means "40% left".

A second trap, fixed 2026-07-23: keep-warm's cold-launch confirmation wait is
42s, and the `Stop` hook that calls it had a 15s timeout. A cold launch killed
the gate mid-wait, so the gate produced no output, the turn ended, and
autopilot silently stopped — an invisible failure hiding inside the freshness
machinery itself. Hook callers now pass `-NoWait` (this turn uses the previous
floor, the next gets fresh numbers) and the timeout is 60s.

**A transcript-derived estimate was built here and REMOVED the same day.**
Summing `message.usage` over a rolling window looked reasonable and produced
83.5% for a 5-hour window that was really at 46% - it would have stopped
autonomous work for no reason. A miscalibrated signal that gates work is worse
than no signal. It was also reinventing something `queue-run` had already
solved and verified; check the existing skills before building budget
machinery.

### Testing the gate must never touch the real queue

`scripts/test-hooks.ps1` covers this machinery (58 assertions), and runs as
part of `scripts/dev.ps1 all`. It exists because none of these hooks had any
tests until 2026-07-23, which is how both bugs above shipped — each was found
by reading the code after an incident, which does not scale to finding the
next one.

Two isolation rules, both learned the hard way:

- Set `OPAL_AUTOPILOT_QUEUE_DIR` to a throwaway directory. On 2026-07-21 a
  verification run ended by deleting `AUTOPILOT` plus both state files to
  clean up after itself, and killed the live autopilot - taking the session
  record with it, which is precisely what defeats the restore-on-delete
  protection. The maintainer had to restart the run manually. Isolate the
  test, don't clean up the real thing.
- Set `OPAL_RATE_LIMIT_STATUS` to a synthetic status file. Reading the real
  one makes assertions pass or fail depending on the maintainer's actual usage
  that hour.

When adding assertions, check they can actually fail — mutate the code under
test and confirm red. The expired-window rule was verified this way:
reintroducing the old bug fails three assertions across the reader, the
guard's behaviour, and the deny path.

### Known limitation: the budget data goes stale

`~/.claude/rate-limit-status.json` is written by the status line
(`~/.claude/statusline.py`), and **the status line does not run in
non-interactive sessions** - which is precisely where autopilot matters. On
2026-07-21 that file was found 10 hours stale, still reporting 1% / 15%.

That is why every reading is treated as a floor, and why an expired window
reads as unknown rather than as a number. The old `rate-limit-gate.ps1`, which
had no freshness check at all, is gone: its subagent-deny job moved into
`budget-guard.ps1` and now goes through the shared rule.

What remains genuinely unsolved is that no signal here is *live*. Between
hourly keep-warm refreshes the number does not move, so a fast-burning turn can
cross several rungs before the guard sees any of it. This is the reason the
whole design targets cheap failure rather than avoidance — do not treat any
gate here as a hard guarantee.

## 2. Model and effort

The maintainer is on **Claude Pro**, so budget is the binding constraint.

| Setting | Value | Where |
|---|---|---|
| Default model | `sonnet` | `~/.claude/settings.json` |
| Default effort | `medium` | `~/.claude/settings.json` |

**Escalate to Opus and/or high effort deliberately, not by habit.** Worth it
for: root-causing a live bug, designing a change that touches the crawl
path, or anything where being wrong costs a full re-verification cycle.
Not worth it for: routine implementation, doc edits, test writing, queue
bookkeeping.

The assistant **cannot change the main session's model or effort itself** -
`/model`, `/fast` and `/config` are interactive panels. So the rule is: when a
task genuinely needs more, say so plainly and let the maintainer flip it.
What the assistant *does* control is the model of any subagent it spawns;
those should default to something cheap for search/read work.

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

## 3. Staying inside 5h / 7d limits

The single most useful thing learned while measuring this repo:

> **Long live runs are nearly free in tokens; reading their output is not.**

A 5-minute full-account scrape is one tool call. What actually consumes the
budget is assistant reasoning turns and large tool outputs pulled into
context. Concretely:

- **Filter every command's output.** `grep`/`tail`/`Select-String` at the
  source, never dump a 900-line log and read it in context.
- **Run long jobs in the background** and wait for the completion
  notification. Never poll a file in a loop - each poll is a turn.
- **Batch verification.** One instrumented run that answers three questions
  beats three runs.
- **Prefer a decisive experiment over more reasoning.** The
  `AJAX_CALL_DONE` question was settled by two 5-minute runs, after
  considerably more than 5 minutes of speculation.

## 4. What still needs a human

Autopilot does not mean "decide everything". Move a task to
`.claude/queue/blocked/` with the open question written down, and continue
with the next one, when:

- The change would delete or overwrite the maintainer's real files.
- A stated project decision or principle would have to change.
- Credentials, 2FA, or anything requiring their account interactively.
- Two reasonable designs differ in a way only their preference settles -
  and only if reasoning it through genuinely does not resolve it.

Everything else: decide, do it, and report what was decided afterwards.
