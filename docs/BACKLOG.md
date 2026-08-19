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

**Only open work belongs here: what is being worked on, and what is blocked.**
The moment an item is done, decided, or ruled out it leaves — closed work goes
to `docs/BACKLOG-archive.md` under "Done recently", answered-and-shut questions
go to the same file under "Settled". No history, no post-mortems, no "Fixed
2026-xx-xx" entries in this file; an entry says what is *left* and where the
detail lives. Ignoring that grew this file past 900 lines twice, most of it
closed work, until nobody could read it in one pass.

Where the detail lives: `docs/BACKLOG-archive.md` (closed work, settled
questions), `docs/sync-speed-model.md` (the speed campaign's ranked open
questions and its rules), `docs/friction-campaign.md` (walk findings),
`docs/installer-plan.md` (distribution, signing, releases).

---

## Now

_Nothing currently blocking a sync/list/login on the account — checked
2026-08-14: `~/.opal-downloader/sync.lock` does not exist. The 2026-08-13
5.5-hour hold is closed, see `docs/BACKLOG-archive.md`._

---

## Next

`docs/sync-speed-model.md` holds the ranked list, re-ranked 2026-08-12 when
the maintainer redefined the speed target from "discovery" to "the whole
sync, start to `Done.`" **Question 44 is still the top item.** Seven live
experiments on 2026-08-13 fully excluded every concurrency- and order-based
lever this project controls (download-side concurrency, discovery-side
course concurrency, and discovery order all tried and ruled out) and
isolated the trigger to **which course is paired with Softwaretechnologie
in the same session** - Algorithmen und Datenstrukturen reproduces all 49
original failures (including its own `Vorlesung` folder), Analysis
reproduces the two largest (Part-3: 33, Part-1: 6) but not the smaller
Part-2 (4), independent of which course is discovered first. A first
source-reading pass the same day (`gh search code --repo OpenOLAT/OpenOLAT`)
found a real candidate mechanism (OpenOLAT's `DTabs` per-session tab cap)
but it does not cleanly fit the evidence - checked and registered as
inconclusive, not confirmed. **A second source-reading pass (2026-08-15)
found why: the account's live folder-browser HTML fires genuine Apache
Wicket AJAX (`Wicket.Ajax.ajax`, class `pager-showall`), but current
`OpenOLAT/OpenOLAT` master has zero trace of Apache Wicket anywhere -
neither the legacy `Table` component nor the modern `FolderController`
matches what's actually served.** Every OpenOLAT-source finding on this
question to date (DTabs, Question 43's `FolderController`, this pass's own
component-id/pagination search) is true of current master but unconfirmed
against whatever OPAL actually runs - a version/fork gap, not a dead end.
**Next step is finding which version/fork is actually deployed** (see
`docs/sync-speed-model.md`'s "Next experiment" for the checked-but-
inconclusive commit-search attempt and the cheaper follow-ups it names)
before more time goes into reading master for mechanisms that may not exist
in the running code. Question 43
(bulk-download-as-ZIP) sits second, still stalled on the same DOM-flakiness
finding from 2026-08-12's Step B — two untried directions are named in its
own entry. **Nothing on this list is blocked on the maintainer** — Question
39 is decided and built, and Question 5 is fully closed (all three halves —
see `docs/BACKLOG-archive.md`). Nothing further is planned on the
course-level HTTP concurrency thread — Question 41 closed 2026-08-11 as a
no-go.

**Weekly review finding (self-imposed, 2026-08-17):** Question 44's cause
half has now run at least 16 investigation-only commits since 2026-08-13
(seven live experiments, three OpenOLAT source-reading passes, a live-server
Wicket fingerprint, a branch/tag sweep, a mirror check) with nothing
shipped — past the line `docs/work-quality.md` ("The sync-speed campaign,
measured") draws for itself: *"a campaign that reaches five investigation
commits with nothing shipped is failing — say so rather than continuing to
measure."* The chase is now for which OpenOLAT/Wicket fork Sachsen runs, and
`docs/sync-speed-model.md`'s own "Next experiment" section admits a real
dead end is possible (a private, unpublished fork with no public source).
Question 44's *policy* half — a negative-manifest-entry-with-backoff for a
file that fails the same way every time — has been named "unblocked by any
of the above" and sufficient by itself to hit the question's own kill line
(~120s no-op sync, down from 1097s) at least three times in
`docs/sync-speed-model.md`, and was never implemented. Nothing breaks by
dropping the version/fork hunt tomorrow: the policy half reaches the
measured target on its own, regardless of whether the cause is ever found.
Ship the policy half next, ranked above resuming the cause hunt.

**2026-08-18 (autopilot): shipped and live-verified — closed.** A failed
download now writes a `FileRecord` with `FailCount`/`FailedAt` instead of no
manifest entry at all, and the next sync skips a file still inside its
backoff window (6h / 24h / 3d / capped at 7d) without attempting it — see
`internal/syncer/syncer.go`'s `downloadRetryAt`/`recordDownloadFailure`.
`force` still bypasses everything, the same escape hatch it already was.
This is a download-phase policy change, not a discovery change, so it
shipped directly rather than behind a flag.

Two live runs against a scratch `download_path` on the real account
confirmed the mechanism exactly: run 1 (fresh manifest) reproduced the
known 49 failures plus one new one (50 total, all recorded as negative
manifest entries); run 2 (same manifest, run immediately after) skipped all
50 via backoff — `downloaded=1 skipped=348 errors=0 backing_off=50` — cutting
the download phase from 1374.2s to 346.7s (~75%, right-sized for removing
~50 retries at ~20s each). **The ~120s total-wall-clock kill line was
missed anyway** (517.1s), but for two separate, already-known reasons this
change was never scoped to fix, not because the backoff failed — see
`docs/sync-speed-model.md`'s "Next experiment" for the full diagnosis and
the two new open questions it left (discovery-time variance; the
signal-less-file verify path's own cost when it needs the browser
fallback).

---

## Open findings

Found by using the tool as a normal user rather than reported by the
maintainer. Walk detail, expectations and named causes:
`docs/friction-campaign.md`. Tags: **blocker** / **wrong** / **friction** /
**bloat** / **question**.

### Friction campaign (GUI walks 1, 4, 5 & 7, CLI walks 2, 6, 8 & 9, first-run walks 3, 7 & 10)

- **wrong — `smoke-check` and `dump-links` never take `sync.lock`, so either
  can run a real crawl concurrently with a real `sync`/`list`/`login` -
  exactly the condition that already caused two silent-failure incidents
  before the lock existed.** Walk 10, 2026-08-19: source-swept every
  `scraper.New(` call site in `cmd/opal-downloader/root.go` against every
  `acquireCrawlOverlapLock` call site. `login` and `list` call it directly;
  `sync` locks one layer down in `internal/syncer.SyncCoursesWithProgress`
  (confirmed live - this walk's own `login` attempt was refused by a real
  `sync` holding the lock); `smoke-check` (`runSmokeCheck`) and `dump-links`
  (`runDumpLinks`) call it nowhere. `docs/OPERATIONS.md`'s own words for why
  the lock exists: "concurrent crawls present one authenticated identity to
  a Wicket backend that is stateful server-side per session," naming two
  real incidents this caused (2026-08-02 raw Playwright launch timeout,
  2026-08-06 silent 0-file collapse - both `docs/BACKLOG-archive.md`).
  `smoke-check` is the higher-risk of the two: a real, unattended-safe,
  TU-Fast-triggering command anyone (or automation) could run at any time,
  including the exact minute a scheduled `sync` is already crawling - this
  walk's own collision shows that overlap happens roughly daily, not
  rarely. `dump-links` is lower-risk (maintainer-only debugging tool, no
  documented end-user workflow) but has the identical gap and the fix
  should cover both. **Possibly explains Walk 9's still-open
  Softwaretechnologie-dropout question** (2026-08-18: two full `sync`s and
  two `smoke-check`s against the real account within about an hour,
  unexplained course-level dropout on the second `smoke-check`) - this
  finding doesn't confirm those runs actually overlapped in wall-clock time,
  only that nothing would have stopped them from doing so, which walk 9 had
  no reason to check for at the time. Fix: `runSmokeCheck` and
  `runDumpLinks` should call `acquireCrawlOverlapLock`/`defer
  releaseOverlap()` the same way `runList` already does - a proven,
  one-line-per-site pattern, not new mechanism. **Left one open question
  before the mechanical fix lands:** is the omission deliberate (does
  smoke-check need to run *during* a sync to test something about
  concurrent access?) or an oversight - worth a maintainer call given Walk
  9's course-filter finding turned out to be intentional design once asked.
  Full detail: `docs/friction-campaign.md` Walk 10.
- **friction — a first-time user who hits the real single-instance lock
  gets a bare PID and a timestamp, with no guidance on what to do.** Walk
  10, 2026-08-19: `opal-downloader login`, run from a genuinely fresh clone
  as a first-timer following the README, refused instantly with `Error: a
  sync is already running (PID 39684, started at 2026-08-19T09:06:47Z)` -
  the real daily scheduled sync happened to be running at that exact
  moment, confirmed via `Get-Process`. The lock itself is correct and
  already documented for maintainers (`docs/OPERATIONS.md`: "that is the
  intended outcome, not a bug - wait for the first to finish"), but that
  context reaches nobody who only sees the CLI's own message - no ETA, no
  "this is probably today's scheduled sync," no suggestion to retry in a
  few minutes. `synclock.ErrHeld` (`internal/synclock/synclock.go`) is one
  shared message returned identically by `sync`, `list`, and `login`, so
  every command that touches the account hits the same bare text under the
  same daily-recurring condition. Fix, if wanted: fold a sentence of the
  `docs/OPERATIONS.md` framing into the CLI message itself. Full detail:
  `docs/friction-campaign.md` Walk 10.
- **wrong — `/schedule`'s on-logon catch-up promise is false for the real
  task, and cannot become true until the app is installed somewhere
  permanent.** Walk 6, 2026-08-13: the page states as fact that a missed run
  is retried "the moment you next log in"; the real `OpalDownloaderScheduledSync`
  task has only its daily trigger, no logon trigger, confirmed via
  `Get-ScheduledTask`. Walk 1's Finding 1 repair (b) (the LogonTrigger) is
  real, shipped code, but both places that could push it onto the real task -
  `schedule enable` and the GUI's `repairDoomedSchedule` self-heal - refuse
  whenever the executable (registered *or* currently running) sits inside a
  git working tree, which every way this project runs today does. Nothing is
  installed at any of the obvious permanent locations (checked, not assumed).
  Fix needs a maintainer call, not more code: run the real installer once and
  re-enable the schedule from there, or add an override that trusts a git
  checkout anyway. Full diagnosis: `docs/friction-campaign.md` Walk 6. This
  also **downgrades the previous line here** ("repair (b) shipped and closes
  the failure mode") - shipped as code, not yet live on the machine it was
  meant to fix.
- **Optional, not a commitment:** an outcome-independent "when did a sync last
  actually *succeed*" staleness signal — walk 1's Finding 1, repair (a). Still
  just a broader defence-in-depth layer on top of (b) - see the entry above
  for why (b) itself isn't fully landed yet either.
- **The installer surface is still unwalked by the campaign proper**, and
  walk 6 sharpens why that now matters beyond general thoroughness: the
  on-logon-trigger finding above is blocked on exactly that surface. The
  2026-08-11 installer work was engineering verification with full knowledge
  of the code, so none of it counts as a persona walk.
- **question — Softwaretechnologie (SoSe 26) course discovery itself, not
  just individual file downloads, was unstable across two `smoke-check` runs
  eight minutes apart on the same account.** Walk 9, 2026-08-18: run 1 found
  it with `0 files (0.3s)` (all its other 210 files were found minutes
  earlier by a real `sync` - see the Question 44 verification above); run 2
  dropped it entirely - not "0 files", genuinely absent from both the
  per-course discovery output and the final baseline breakdown, despite
  "Found 8 course links" (matching run 1's course-link count) printing first.
  Nothing in the visible CLI output explains the gap - no error line, no
  timeout message, no course-skipped notice. **Caveat that matters for
  reading this honestly: this session ran two full syncs and two smoke-checks
  against the same real account inside about an hour, entirely for its own
  testing purposes** (Question 44's live verification, this walk) - self-
  inflicted, unusually heavy load this account does not normally see in a
  day, and run 2 also needed an interactive TU-Fast relogin mid-run (the
  saved session had expired between the two smoke-checks), which is itself
  more session churn than a normal daily use pattern produces. This may be
  nothing more than that - not a claim of a new bug, and not chased further
  this walk. Filed because it sits close enough to Question 44's still-open
  cause question (`docs/sync-speed-model.md`: "which course pairs with
  Softwaretechnologie", "a second course's discovery perturbs
  Softwaretechnologie's state") that a future cause-hunt cycle should know
  this data point exists: **course-level dropout, not just file-level HTML
  responses, may be part of the same family** - worth a clean, isolated
  repro (a single smoke-check against a *rested* session, no other activity
  that hour) before reading anything more into it.

---

## Noticed

Rough edges seen while working on something else, that would otherwise exist
only in one session's context window. Not commitments. An entry leaves in one
of two directions: up into the work above, or into `docs/BACKLOG-archive.md`
once it is done, decided, or shown not to matter.

- **`TestSyncScheduledSkipsWhenAlreadySucceededToday` (`cmd/opal-downloader/
  root_test.go`) is not hermetic — under contention it falls through its own
  dedup guard into a real live sync against the real OPAL account.** Found
  2026-08-19 while verifying the last-sync.json fix above: with a stray
  concurrent `go test` process already holding the real `~/.opal-downloader/
  sync.lock`, this test - which fakes `readScheduledStatusForDedup` to
  report "already succeeded today" specifically so it should return
  `errAlreadySucceededToday` before touching the network - instead ran a
  full ~166s discovery/download pass against the real account (8 courses,
  349 files, real TU-Fast login), landing only in a scratch temp
  `download_path` so no real files were touched. Reproduced identically on
  unmodified `master` (not something this session's changes caused). Passes
  cleanly in isolation with no contending process. Root cause not
  diagnosed - plausible mechanism is that `alreadySucceededToday`'s
  in-process fake and the real filesystem-backed `sync.lock` are two
  different guards, and something about lock contention or its retry path
  reaches the real scraper before the dedup check runs, but that is a guess,
  not confirmed by reading the code. Real risk: any future test run that
  overlaps another (two sessions, this project's own worktree-per-session
  habit, a CI matrix) can trigger an unplanned real crawl during `go test`.
  Worth a source-reading pass to find why the guard doesn't hold under
  contention, or making the test's scraper fully faked so a guard miss fails
  loudly instead of degrading into a real network call.

---

## Standing work

Not an item to finish — the work that fills a run when nothing above is
unblocked. The `opal-downloader-autopilot` task reaches it as its phase 2.

### Sync speed as an iteration loop

**`docs/sync-speed-model.md` is the driver** — known numbers, ranked open
questions, the three rules, and one experiment at a time with its predicted
number and kill criterion written down *before* the run.
`docs/sync-speed-campaign.md` is the archive. There is no cap on the campaign;
the kill criterion sits per experiment. A report every fifth cycle carries a
keep-going-or-stop recommendation, and the maintainer makes that call.

Two standing decisions govern it. Every experiment goes behind an env flag and
is diffed byte-for-byte against the 345-file ground truth, but **a default that
has passed that diff may be changed and shipped** without asking (2026-08-03),
so a measured win reaches the maintainer instead of sitting behind a flag. And
**correctness goes ahead of speed** where the two compete (2026-08-03). The
corollaries the campaign learned the hard way — including why a byte-for-byte
diff is not proof of losslessness — sit with the rules in
`docs/sync-speed-model.md`.
