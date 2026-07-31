# Agent operating model

How the AI assistant working on this repo organises itself: when it keeps
going on its own, which model/effort it runs at, and how it stays inside the
maintainer's Claude Pro budget.

**The rules are here. The incidents that produced them are in
`docs/agent-incidents.md`.** That split happened on 2026-07-31, when this file
had reached 529 lines and was mostly history — read the incidents file when a
rule looks arbitrary or you want to remove one, not to find out what to do.

Written 2026-07-21 to fix two recurring problems: the assistant stopping at
"natural" checkpoints and waiting to be told *weiter*, and model/effort being
picked ad hoc with no policy behind it.

## 0. The one rule this document exists for

**Stopping is not the assistant's decision.** Not on budget, not on session
length, not on "the next task deserves a fresh start" — those were the actual
rationalisations used to end three runs early on 2026-07-21. If stopping looks
right, say so in the reply *and keep working*.

Everything below is machinery serving that one rule. When the machinery and
the rule disagree, the machinery is wrong.

## 1. Autopilot: working without being prompted

A `Stop` hook (`.claude/hooks/autopilot-gate.ps1`) refuses to let the turn end
while unblocked work remains in `docs/BACKLOG.md`, and tells the assistant to
pick up the next item itself. It engages only when `.claude/queue/AUTOPILOT`
exists, so ordinary conversation is unaffected.

### Every gate here is silently absent unless the session started in this repo

`.claude/settings.json` is *project* configuration, loaded from the directory
the session was opened in. Open a session elsewhere and point it here by path
and **none of these hooks exist** — not the Stop gate, not the pre-push gate,
not the budget guard. Nothing announces it.

So: **start the session in the repo directory.** If you cannot tell whether the
hooks are live, assume they are not and run `scripts/dev.ps1 all` yourself
before every push. Autopilot not engaging is the visible symptom; the missing
pre-push gate is the part that matters.

### Starting and stopping it

Armed automatically by a `SessionStart` hook (matcher `startup` only, so it
never resets an in-flight session) unless `.claude/queue/AUTOPILOT.OFF` exists.
Default stretch: **12 hours, 60 continuations.** By hand:

```powershell
# arm a specific stretch
$exp = [DateTimeOffset]::UtcNow.AddHours(8).ToUnixTimeSeconds()
"{`"expires_at`": $exp, `"max_iterations`": 40}" |
  Set-Content .claude/queue/AUTOPILOT -Encoding utf8

# stop it (maintainer only - this is the off switch)
New-Item .claude/queue/AUTOPILOT.OFF -ItemType File
```

**Deleting `AUTOPILOT` does nothing** — the hook keeps a session record and
restores it. Interrupting the session works, and always wins.

### Guards

Every guard ends the turn rather than continuing; the hook **fails open**,
because trapping the maintainer in a loop is worse than stopping early.

| Guard | Behaviour |
|---|---|
| No `AUTOPILOT` marker | No-op. Normal conversation. |
| `expires_at` passed | Stops, and deletes the marker so it cannot linger. |
| `max_iterations` reached (per session) | Stops. Default 60. |
| Rate limit 5h ≥ 90% or 7d ≥ 92% | Stops. |
| Rate-limit data stale (>30 min) | Treated as *unknown*, capping the stretch at 25 continuations. |
| No unblocked item in `docs/BACKLOG.md` | Stops — there is nothing to do. |
| Any error, unreadable JSON, unexpected state | Stops. |

**The failure mode to know:** a backlog whose every "Now" item is marked
**Blocked:** makes the gate correctly conclude there is no work, which looks
exactly like autopilot being broken. Check the backlog before debugging hooks.

The thresholds were 75%/80% and 4h/20 until 2026-07-31. They were raised
because they had stopped being a safety allowance and become the actual
stopping condition — runs were ending on a timer with a quarter of the budget
unused, and the maintainer had to restart what was still in flight.

### The budget signal is a floor, never a measurement

`~/.claude/rate-limit-status.json` is written only by the status line, which
does not run in non-interactive sessions. It has been found **18 hours stale,
reporting 1% against a real 46%**. `.claude/hooks/rate-limit-keepwarm.ps1`
keeps it fresh with a hidden idle `claude`; the gate invokes it with `-NoWait`.

- **Every reading is a lower bound.** "60%" means "at least 60%, possibly an
  hour's worth more". It never means "40% left".
- **Staleness is per window.** A window whose `resets_at` has passed is
  meaningless and must be ignored, or a stale-and-expired reading gates every
  future run forever. A window still inside its period is usable even when old,
  since usage only climbs.
- That rule lives in `.claude/hooks/budget-lib.ps1` and nowhere else. It used
  to be copied per hook, and the copies had drifted.
- **Do not build an estimator.** One was written and removed the same day for
  reporting 83.5% against a real 46%. A miscalibrated signal that gates work is
  worse than no signal.

### Testing the gate must never touch the real queue

`scripts/test-hooks.ps1` covers this machinery and runs inside
`scripts/dev.ps1 all`. Set `OPAL_AUTOPILOT_QUEUE_DIR` to a throwaway directory
and `OPAL_RATE_LIMIT_STATUS` to a synthetic status file — a verification run
once cleaned up after itself by deleting the *live* `AUTOPILOT` marker and its
session record. When adding assertions, mutate the code under test and confirm
they can actually fail.

## 2. Model and effort

The maintainer is on **Claude Pro**, so budget is the binding constraint.
Default `sonnet` at `medium` effort (`~/.claude/settings.json`).

Escalate to Opus and/or high effort deliberately: root-causing a live bug,
designing a change touching the crawl path, anything where being wrong costs a
full re-verification cycle. Not for routine implementation, doc edits, or test
writing.

The assistant **cannot change the session's own model or effort** — those are
interactive panels. Say plainly when a task needs more and let the maintainer
flip it. Subagent models it does control; default those to something cheap.

## 3. When the usage limit is hit mid-run

A usage-limit kill stops the turn dead. **No `Stop` hook runs**, so every guard
in §1 is bypassed. The design goal is therefore *not* avoidance — the data
cannot support prediction — but that **being killed costs the current turn and
nothing more**.

| Piece | Hook | Job |
|---|---|---|
| `budget-guard.ps1` | `PreToolUse` | The only check running *during* a turn. As the floor climbs, tells the assistant to commit and write down where it got to. |
| `turn-failure-checkpoint.ps1` | `StopFailure` | Fires *because* the turn died. Records what happened, captures uncommitted work. |
| `session-start-autopilot.ps1` | `SessionStart` | Hands the next session the failure record and `docs/RESUME.md`. |

**The rungs** (worst window wins, thresholds are on the floor):

| Rung | 5h | 7d | Effect |
|---|---|---|---|
| 1 | ≥50% | ≥65% | Silent. |
| 2 | ≥70% | ≥80% | Commit what is correct now, even mid-task. Update the resume note. |
| 3 | ≥80% | ≥85% | Same, more urgently. |

Advice is throttled: once when the rung rises, then at most every 15 minutes.

**None of these rungs is a stop condition, and none of them shrinks the task.**
They say "stay savable" — which is about how often you commit, not about how
much you attempt. Rung 1 was made silent and rung 3's subagent deny was removed
on 2026-07-31, because between them they had turned a savability mechanism into
a general instruction to do less.

### What survives a kill

- **`docs/RESUME.md`** — tracked in git, deliberately messy, holds the thought
  that would otherwise live only in a context window. Keep it current *while*
  working; the end of a task is the part that does not always arrive.
- **Uncommitted work** — `turn-failure-checkpoint.ps1` runs `git stash create`
  and points `refs/wip-checkpoints/<unix>` at it, without touching the working
  tree, index, stash, or any branch. The next session is told the SHA.
- **`.claude/queue/LAST_FAILURE.json`** plus an append-only `turn-failures.log`.

### What does *not* survive: a background process

A backgrounded command belongs to the session that started it, and both die
together. So:

- **A long verification run must write its result to a file** under `tmp/` or
  `docs/`, not only to stdout.
- **`docs/RESUME.md` must never call a background process "in flight"** without
  saying where its result will land.
- **An unattended run commits first and verifies second.** A commit survives; a
  working tree survives by luck.

### Automatic resumption

The `OpalDownloader-ResumeRunner` scheduled task restarts work between
sessions — it survives closed terminals, reboots and logouts, and stops only
when unregistered or when `AUTOPILOT.OFF` exists. Set it up with
`scripts/register-resume-task.ps1` (`-Status` to inspect, `-Remove`).

**What makes it affordable:** `.claude/hooks/resume-runner.ps1` gates entirely
in PowerShell — off switch, already-running, cooldown, budget, is-there-work —
so **a quiet hour costs no model turn.** An in-session cron was considered and
rejected for exactly this: every fire is a model turn, including one that
concludes there is nothing to do.

Its bounds, because nobody is watching: `OPAL_UNATTENDED_RESUME=1` arms 15
iterations / 4h, `--model sonnet`, a 2-hour cooldown, mains power required, and
it resumes only at rung 0–1 on the **worst** window. It skips while
`.session-heartbeat.json` is under 20 minutes old or the worktree was modified
in the last 30 minutes, so it never joins a tree someone else is working in.

`.claude/hooks/unattended-run.ps1` wraps the agent: it holds a wake lock so
Modern Standby cannot freeze the run, and writes what the run actually achieved
(`finished` / `run-died-early` / `run-left-uncommitted`), because a dead run and
a working one used to be indistinguishable in the log.

## 4. Staying inside 5h / 7d limits

The single most useful thing learned while measuring this repo:

> **Long live runs are nearly free in tokens; reading their output is not.**

A 5-minute full-account scrape is one tool call. What consumes budget is
reasoning turns and large tool outputs pulled into context.

- **Filter every command's output** at the source. Never dump a 900-line log.
- **Run long jobs in the background** and wait for the completion notification.
  Never poll in a loop — each poll is a turn.
- **Batch verification.** One instrumented run answering three questions beats
  three runs.
- **Prefer a decisive experiment over more reasoning.**

## 5. What still needs a human

Autopilot does not mean "decide everything". Mark the item **Blocked:** in
`docs/BACKLOG.md` with the open question written down, and continue with the
next one, when:

- The change would delete or overwrite the maintainer's real files.
- A stated project decision or principle would have to change.
- Credentials, 2FA, or anything requiring their account interactively.
- Two reasonable designs differ in a way only their preference settles — and
  only if reasoning it through genuinely does not resolve it.

Everything else: decide, do it, and report what was decided afterwards.

**Watch the ratio.** If several sessions in a row end with every backlog item
blocked, the bar above is being applied too widely — that is the state the
backlog was found in on 2026-07-31, where all four items waited on the
maintainer and autopilot therefore had nothing to run.
