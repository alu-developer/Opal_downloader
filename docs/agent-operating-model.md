# Agent operating model

How the AI assistant working on this repo organises itself: when it keeps
going on its own, what restarts it between sessions, and what survives a run
being killed.

**The rules are here. The incidents that produced them are in
`docs/agent-incidents.md`**, and the retrospective that produced *this*
version is `docs/work-quality.md`. Read those when a rule looks arbitrary or
you want to remove one — not to find out what to do.

This file was 529 lines on 2026-07-31, describing eleven PowerShell files. Ten
of them are gone. If it starts growing again, that is the symptom, not the
progress.

## 0. The one rule this document exists for

**Stopping is not the assistant's decision.** Not on budget, not on session
length, not on "the next task deserves a fresh start". If stopping looks
right, say so in the reply *and keep working*.

It is a rule, not a mechanism. Between 2026-07-23 and 2026-07-31 it was a
mechanism — a `Stop` hook that refused to let the turn end — and the measured
result was that the mechanism became the stopping condition: runs ended on its
timers and caps rather than on the work being done. Every guard added to keep
the agent working was a new place for it to stop. That is the whole lesson.

## 1. Working without being prompted

`docs/BACKLOG.md` is the list. Read it, take the top unblocked item, do it,
update the file in the same commit. Nothing arms this and nothing gates it.

**When autonomy stalls, the cause is almost always the backlog's *content*,
not a mechanism.** On 2026-07-31 all four "Now" items were marked
**Blocked:** on the maintainer, so there was correctly nothing to run — which
is indistinguishable from the automation being broken. Check that first; it is
the cheapest thing to verify.

Which means the bar in §4 matters. Applying it too widely converts the backlog
into a wall, and the wall looks like a bug.

## 2. Restarting between sessions

A process cannot revive itself, so something external has to start the agent.
That is the one unavoidable piece, and since 2026-07-31 it is **one
first-party Claude Code Desktop scheduled task**, not a Windows scheduled task
driving 23 KB of PowerShell gating.

- Prompt: `~/.claude/scheduled-tasks/opal-downloader-autopilot/SKILL.md`
- Managed in the Desktop app under **Routines**: schedule, working folder,
  model, permission mode, run history, and per-task tool approvals.

**It only fires while the Desktop app is open and the machine is awake.** That
is the price of running locally, and running locally is required: verification
here needs the maintainer's real OPAL account and real files, which a cloud
routine cannot reach. Missed runs are caught up once on wake (one catch-up for
the most recent miss, not one per miss), and skipped runs are visible in the
task's history with the reason.

Why the previous design was replaced, in one line each — the long version is
in `docs/agent-incidents.md`:

- It died silently twice while looking healthy, and its own log could not show
  it, because the log only recorded decisions the gate reached.
- Modern Standby made an `InteractiveToken` task refuse to launch; a refusal is
  never replayed, so the `missed` counter reached 19 while the task read
  `State: Ready`.
- A frozen run left a 0-byte log and no commits while the log still said
  "launched". Nine of nineteen run logs were 0 bytes.

The Desktop task has catch-up, a real run history, and a visible session per
run, so all three failure modes are observable without building anything.

## 3. When the usage limit is hit mid-run

A usage-limit kill stops the turn dead. No `Stop` hook runs, so the design goal
is not avoidance — the data cannot support prediction — but that **being killed
costs the current turn and nothing more.**

What survives:

- **`docs/RESUME.md`** — tracked in git, deliberately messy, holds the thought
  that would otherwise live only in a context window. Keep it current *while*
  working; the end of a task is the part that does not always arrive.
- **Uncommitted work** — `.claude/hooks/turn-failure-checkpoint.ps1`
  (`StopFailure`) runs `git stash create` and points
  `refs/wip-checkpoints/<unix>` at the result, without touching the working
  tree, index, stash, or any branch. It keeps the newest 40 refs and deletes the
  rest on every fire.
- **`.claude/queue/LAST_FAILURE.json`** plus an append-only `turn-failures.log`,
  written by the same hook. That directory is gitignored scratch space and holds
  nothing else any more. The autopilot task reads `LAST_FAILURE.json` at the top
  of a run; nothing else does, so a change that stops it looking there makes the
  file write-only again.

Two things this is *not*, both measured 2026-07-31:

- **It is not the main thing that saves work.** A usage-limit kill does not
  delete anything — the working tree is exactly as the killed turn left it, and
  the next autopilot run refuses to start on a dirty tree rather than clobber
  it. What actually dies is the *context*, which is why `RESUME.md` and small
  commits carry this and the ref is a backstop for the narrow case where
  something later discards the tree.
- **It is not a substitute for `/rewind`.** Claude Code checkpoints code *and*
  conversation before every prompt, keeps them 30 days, and restores after a
  resume — strictly more than this hook, for any session a human is sitting in.
  The hook's one non-overlapping job is the unattended runs, where nobody is
  there to press anything. Keep it that small.

The prune above is the correction of a real defect, not a tidy-up: the first
version required *both* a count floor and 14 days of age, and had therefore
never deleted a single ref. 535 had accumulated at 26-201/day, each pinning a
whole tree against gc. A bound that the write rate outruns is not a bound.

**Staying savable is about how often you commit, not how much you attempt.**
There is no budget hook now. There used to be, and its own wording — "avoid
starting work that only pays off if a long turn completes" — is the documented
cause of the half-finished changes the maintainer complained about on
2026-07-30. Do not rebuild it.

**Do not build a rate-limit estimator.** One was written and removed the same
day for reporting 83.5% against a real 46%. A miscalibrated signal that gates
work is worse than no signal.

### What does *not* survive: a background process

A backgrounded command belongs to the session that started it, and both die
together. So:

- A long verification run must write its result to a file under `tmp/` or
  `docs/`, not only to stdout.
- `docs/RESUME.md` must never call a background process "in flight" without
  saying where its result will land.
- An unattended run commits first and verifies second. A commit survives; a
  working tree survives by luck.

## 4. What still needs a human

Mark the item **Blocked:** in `docs/BACKLOG.md` — with the open question
written down *and concrete options to choose between* — and continue with the
next item, when:

- The change would delete or overwrite the maintainer's real files.
- A stated project decision or principle would have to change.
- Credentials, 2FA, or anything requiring their account interactively.
- Two reasonable designs differ in a way only their preference settles, and
  reasoning it through genuinely does not resolve it.

Everything else: decide, do it, report afterwards.

**A blocked entry without options is not finished work.** "Please look at the
GUI" costs the maintainer 45 minutes and therefore waits weeks; the same
question as three concrete alternatives costs ten seconds and gets answered.
Whoever marks an item blocked owes it the options. `/decide` collects them.

**Watch the ratio.** If several sessions in a row end with every backlog item
blocked, the bar above is being applied too widely.

## 5. Model and effort

The maintainer is on **Claude Pro**, so budget is the binding constraint.
Escalate to Opus and/or high effort deliberately: root-causing a live bug,
designing a change touching the crawl path, anything where being wrong costs a
full re-verification cycle. Not for routine implementation, doc edits, or test
writing.

The assistant **cannot change the session's own model or effort** — those are
interactive panels. Say plainly when a task needs more and let the maintainer
flip it. Subagent models it does control; default those to something cheap.

## 6. Staying inside 5h / 7d limits

The single most useful thing learned while measuring this repo:

> **Long live runs are nearly free in tokens; reading their output is not.**

A 5-minute full-account scrape is one tool call. What consumes budget is
reasoning turns and large tool outputs pulled into context.

- **Filter every command's output** at the source. Never dump a 900-line log.
- **Run long jobs in the background** and wait for the completion
  notification. Never poll in a loop — each poll is a turn.
- **Batch verification.** One instrumented run answering three questions beats
  three runs.
- **Prefer a decisive experiment over more reasoning.**

## 7. The standing rule

**Wanting to build a gate is the signal to do the work instead.**
