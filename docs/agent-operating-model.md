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

### Starting and stopping it

```powershell
# start a 4-hour autonomous stretch, max 20 continuations
$exp = [DateTimeOffset]::UtcNow.AddHours(4).ToUnixTimeSeconds()
"{`"expires_at`": $exp, `"max_iterations`": 20}" |
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

### The budget signal: real data first, local estimate second

`~/.claude/rate-limit-status.json` is accurate but is written *only* by the
status line, which does not run in non-interactive or `claude-desktop`
sessions. It was found **18 hours stale on 2026-07-21, still reporting "1%"
while the account was minutes from its 5-hour limit.**

Every other source was checked and ruled out: the CLI exposes no usage
command, `claude -p` does not refresh the file, the desktop app keeps no
readable usage state, and `~/.claude.json`'s `lastModelUsage` is only written
at session end.

What *is* always available: Claude Code's own session transcripts record
`message.usage` per assistant message.
`.claude/hooks/usage-estimate.ps1` sums those over a rolling 5-hour window
(output + cache-creation + input tokens; cache reads are excluded, since they
dominate raw counts — 374M against 1M output in one session — while costing a
fraction). It runs in ~2s over ~8,500 messages.

**Calibration, and its limits.** The 5-hour ceiling (7.6M weighted tokens) is
taken from the one moment the limit was actually hit, on 2026-07-21. One
observation, one plan — a trend indicator, not the provider's accounting. The
gate therefore prefers the real file whenever it is fresher than 30 minutes,
and only falls back to the estimate otherwise, at a **lower** threshold (70%
against 75%) precisely because it is less trustworthy.

**No 7-day ceiling is defined, deliberately.** That limit has never been hit,
so any number would be invented — a first attempt at 40M promptly reported
125% while the account was nowhere near its weekly cap. The 7-day total is
emitted raw for a human to read, and nothing gates on it.

### Known limitation: the budget data goes stale

`~/.claude/rate-limit-status.json` is written by the status line
(`~/.claude/statusline.py`), and **the status line does not run in
non-interactive sessions** - which is precisely where autopilot matters. On
2026-07-21 that file was found 10 hours stale, still reporting 1% / 15%.

That is why the hook checks the file's age and treats stale data as unknown
rather than as "plenty of budget left". The `PreToolUse` rate-limit gate for
subagent launches (`.claude/hooks/rate-limit-gate.ps1`) has the same blind
spot and does **not** check freshness - it fails open on stale data. Worth
fixing; until then, do not treat either gate as a hard guarantee.

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

Hitting the limit stops the turn dead. Nothing restarts it, so historically
the maintainer had to notice and write "continue" — the exact manual step
this whole model exists to remove.

**Whenever autopilot is enabled, also schedule a resume check.** A recurring
`CronCreate` job (~every 23 minutes) that:

- does nothing at all if `.claude/queue/AUTOPILOT` is missing, expired, or
  the queue is empty — it must not burn tokens deciding that;
- otherwise resumes the highest-value queued task, reading
  `docs/sync-speed-campaign.md` and the task file to reconstruct state
  instead of re-running measurements.

While the limit is in force the fire simply fails; once the quota resets, the
next fire picks the work back up on its own.

**Known limits of this mechanism, do not oversell it:**

- Cron jobs are **session-only** — in memory, gone if the session ends, and
  auto-expiring after 7 days. It rescues a rate-limited session, not a dead
  one.
- Jobs only fire while the REPL is idle.
- The budget data itself is unreliable (see the stale-status-file note
  above), so this cannot *predict* a limit. It only recovers from one, which
  is the achievable half.

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
