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

**`sync.lock` has been held for 5.5+ hours by a stuck live run, blocking every
sync/list/login on the account.** Found 2026-08-13 by the weekly-review pass's
Part C attempt: `main.exe list --config config.yaml` with
`OPAL_HTTP_DISCOVERY=verify` failed immediately with `Error: a sync is already
running (PID 14804, started at 2026-08-13T06:25:32Z)`. `~/.opal-downloader/sync.lock`
confirms the same PID/timestamp; `Get-Process -Id 14804` confirms it is
genuinely still alive (not a stale/crashed holder `internal/synclock` would
have reclaimed) — `main.exe` in worktree
`.claude/worktrees/suspicious-pare-359a30`, running since 08:25:32 local with
no sign of stopping. That worktree's own `docs/RESUME.md` still reads the
placeholder ("_Nothing in flight._"), so whatever it launched (most likely
Question 44's "open one Part-3 file in the visible browser" live step, named
in this file's own Next section as "in progress 2026-08-13, blocked mid-attempt
... retry once it clears") left no checkpoint. Corroborating evidence: the
`opal-downloader-autopilot` routine's own schedule shows a run at 11:49:53
today with no new commit since 08:35:38 — its most recent cycle almost
certainly hit this same lock and had nothing to show for it. Not fixed here
(out of scope for this pass, and killing another session's process isn't this
pass's call) — the maintainer should look at PID 14804 / that worktree
directly. `internal/synclock` is working as designed (a genuinely-alive PID is
correctly not reclaimed); the actual problem is whatever is inside that run
that isn't finishing or reporting progress.

**`internal/scraper/httpdiscovery_seed.go`'s `discoverSectionsHTTP` skips
non-file sections silently — the browser path logs the same skip.** Found by
this pass's Part B code review (window `bbc782d..07a7d0d`, 90 commits). The
browser crawl path (`crawl.go` line ~207-219, `appendSectionFolderTargets`)
explicitly logs every section it skips as structurally file-less (OPAL
enrollment/Einschreibung nodes) via `logging.Detail(...)`, with a comment
saying plainly "Auditable, not silent." `discoverSectionsHTTP` — the function
HTTP-first discovery uses, HTTP-first having been the default sync path since
2026-08-11 — skips the same class of node in two places (the tree-seed loop
at line 128-135, and the `appendSectionFolderTargets` call at line 195) and
logs neither: the tree-seed skip has no logging call at all, and line 195
discards the call's `[]skippedSection` return value with `_`. No files are
lost (enrollment nodes never hold files), but the audit trail this project
built specifically because "skips must be auditable, not silent" is silently
absent on the path everyone now runs by default. Cheap fix: thread the
discarded `skipped` slice at line 195 through to a caller-supplied logger the
same way `onSectionError`/`onSectionVisited` already are, and log the
tree-seed loop's own skip the same way.

---

## Next

`docs/sync-speed-model.md` holds the ranked list, re-ranked 2026-08-12 when
the maintainer redefined the speed target from "discovery" to "the whole
sync, start to `Done.`" **Question 44 is now the top item** (opened by that
same re-ranking): a no-op sync spends 1097.1s of its 1147.2s (96%) failing to
download 49 files that answer with HTML instead of bytes, 33 of them
concentrated in one course's "Part-3" folder; the failures write no manifest
entry, so every sync retries the same 18 minutes of dead weight. Cheapest
next step (registered, not yet run): open one Part-3 file in the visible
browser and watch what the server actually answers, before touching any
code — in progress 2026-08-13, blocked mid-attempt only by the account's own
one-crawl-at-a-time lock (another sync was already running); retry once it
clears. Question 43 (bulk-download-as-ZIP) drops to second, still stalled on
the same DOM-flakiness finding from 2026-08-12's Step B — two untried
directions are named in its own entry. **Nothing on this list is blocked on
the maintainer** — Question 39 is decided and built, and Question 5 is fully
closed (all three halves — see `docs/BACKLOG-archive.md`). Nothing further is
planned on the course-level HTTP concurrency thread — Question 41 closed
2026-08-11 as a no-go.

---

## Open findings

Found by using the tool as a normal user rather than reported by the
maintainer. Walk detail, expectations and named causes:
`docs/friction-campaign.md`. Tags: **blocker** / **wrong** / **friction** /
**bloat** / **question**.

### Friction campaign (GUI walks 1, 4 & 5, CLI walks 2 & 6, first-run walk 3)

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

---

## Noticed

Rough edges seen while working on something else, that would otherwise exist
only in one session's context window. Not commitments. An entry leaves in one
of two directions: up into the work above, or into `docs/BACKLOG-archive.md`
once it is done, decided, or shown not to matter.

_(Empty as of 2026-08-12 — everything that stood here was already closed or
decided and now sits in the archive's "Settled" section.)_

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
