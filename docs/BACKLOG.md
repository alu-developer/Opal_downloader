# Backlog

The current state of work on opal-downloader. **This file is the answer to
"what should I do next?"** — read it at the start of a session, pick the top
item that isn't blocked, and get on with it.

Kept in git deliberately, so it survives a fresh clone, a reinstall, and a
lost `~/.claude` directory. Update it in the *same commit* as the work it
describes; a backlog that lags the code is worse than none.

Keep personal specifics out of this file — the repo is public. Absolute
paths, account details, and measured numbers that only make sense for one
machine belong in local memory, not here.

---

## Now

### Decide whether `list` (and `dump-links`) should be hidden from end users
Now that setup has a course picker, the one plausible end-user reason to run
`list` — "what are my courses called, so I can type them in?" — is gone. The
landing page no longer names them (#118). Remaining question is whether to
drop them from `printHelp` too (compare the hidden `__panic-test`
subcommand). `dump-links` should be judged in the same pass.

---

## Next

### Dogfood the whole first-run journey
Drive the GUI as a real first-time user — no config, through setup, login,
course selection, a sync, status, scheduling, then changing a setting — and
write down everything broken, confusing, or annoying. Findings get fixed if
trivial, filed here if not.

Explicitly from the perspective of a TU Dresden student who is *not* the
maintainer: a stranger's first run is in scope.

### Clean up legacy manifest orphans
Around two dozen manifest entries still use an old absolute-path key scheme
that current key derivation no longer produces. They are dead weight: never
matched, never updated, never cleaned. Decide whether to prune them during
the existing migration pass or leave them inert.

### A cheap recurring review pass
Roughly weekly: a correctness review plus a simplification pass, scoped to
what changed since last time so cost stays flat. Output lands here as backlog
items, not as an unread report. Keep it light — a heavy ritual gets skipped.

---

## Blocked / needs evidence

### Scheduled sync failed with "a sync is already running" 4 seconds after another started
Ruled out so far: duplicate scheduled tasks (one task, one trigger), a
double-acquire inside one run (a single lock call site exists), the
smoke-check path (its own subcommand, not invoked by `sync --scheduled`), and
a recycled PID (that was a real bug, fixed in #120, but the 4-second gap means
a genuinely fresh lock rather than an inherited number).

Remaining hypothesis: two real processes starting ~4s apart — the GUI and the
scheduled run colliding, or the task being launched twice by something outside
Task Scheduler's trigger list.

**Blocked because it cannot be reproduced.** The schedule has since been
re-registered, and the scheduled-run status file keeps only the single most
recent run, so there is no history to mine. **Do this first:** add a small
rolling log of scheduled-run outcomes. Otherwise the next occurrence is
equally un-diagnosable.

---

## Done recently

Newest first. Trimmed periodically — git history and PR bodies are the real
record.

- **Repair a scheduled sync that points at a disposable binary**, instead of
  telling the user to repair it themselves. Finishes what #122 started: that
  one only stopped new doomed registrations being created. *The repair branch
  is unobserved in the wild — verified live only in its refusing-to-repair
  form, since triggering the repair means rewriting a real Task Scheduler
  entry.*
- **Suggest a per-course download folder** from the folder tree the user
  already keeps coursework in — abbreviation-aware, so `Algorithmen und
  Datenstrukturen` finds `AlgData`. *Matching is tuned against the
  maintainer's own mappings only; a stranger's folder naming is untested, and
  the threshold may turn out to be too strict or too loose in the wild.*
- **#124** Reload a login page TU-Fast has not acted on, instead of waiting
  out the full timeout. *The stall itself was never reproduced; the reload
  branch is unobserved in the wild.*
- **#123** Verify files OPAL reports no size or date for by comparing bytes,
  instead of assuming they are unchanged. Closes the second half of the
  never-updating-file bug.
- **#122** Refuse to register a scheduled sync against a disposable binary.
- **#121** Discover courses so they can be ticked in setup, not typed.
- **#120** Don't treat a recycled PID as a running sync.
- **#119** Report what the crawl is doing while it runs.
- **#118** Put a primary "Sync now" action on the GUI start page.
- **#117** Heal manifest entries that carry no size/modified signal. First
  half of the never-updating-file bug.
