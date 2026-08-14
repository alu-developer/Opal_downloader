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
inconclusive, not confirmed. **No crisp next experiment is queued**; the
model file's own next-step suggestion is now open-ended source reading
(something at folder, not course, granularity) rather than a bounded live
test, so whoever picks this up next should read that entry's "what a next
pass should look for instead" note before choosing where to dig. Question 43
(bulk-download-as-ZIP) sits second, still stalled on the same DOM-flakiness
finding from 2026-08-12's Step B — two untried directions are named in its
own entry. **Nothing on this list is blocked on the maintainer** — Question
39 is decided and built, and Question 5 is fully closed (all three halves —
see `docs/BACKLOG-archive.md`). Nothing further is planned on the
course-level HTTP concurrency thread — Question 41 closed 2026-08-11 as a
no-go.

---

## Open findings

Found by using the tool as a normal user rather than reported by the
maintainer. Walk detail, expectations and named causes:
`docs/friction-campaign.md`. Tags: **blocker** / **wrong** / **friction** /
**bloat** / **question**.

### Friction campaign (GUI walks 1, 4, 5 & 7, CLI walks 2 & 6, first-run walks 3 & 7)

- **wrong — a fresh "first-time" setup silently reuses whatever OPAL login
  already exists on the machine, skipping the "log in once" step the GUI's
  own landing page promises, with no indication of whose session it is.**
  Walk 7, 2026-08-13 (GUI, true first-run: empty scratch dir, no
  `config.yaml`, no session file of its own). The landing page tells a new
  user "What you'll do once: ... Log in to OPAL once." A brand-new
  `config.yaml`, bootstrapped through Settings' own pre-filled-defaults flow
  exactly as README describes, was never used to log in — yet the very next
  page load reported `Logged in, valid until Sun 16 Aug, 13:59 (2 days
  left)`. Cause, source-confirmed: `internal/config.DefaultStateFile =
  "~/.opal_storage_state.json"` is a single fixed path outside `download_path`
  and outside the fresh config's own control; the Settings form writes this
  same literal default into every new `config.yaml` rather than leaving it
  config-scoped or unset. Predicts the same silent inheritance for anyone
  reinstalling, testing a second account on one Windows profile, or (this
  project's own common case) running a second worktree pointed at a
  different `config.yaml` while an earlier one already logged in. Distinct
  from the already-filed global-status-file findings below: this one is the
  actual authentication identity, not a reporting artifact. Fix direction not
  designed here - either scope `session_state_file`'s default per-install
  (e.g. next to `config.yaml`) or have the landing page say "using an
  existing OPAL session" instead of rendering identically to a session this
  setup actually created. Full walk: `docs/friction-campaign.md` Walk 7.
- **wrong — the same first-run landing page shows a stale, unrelated "Last
  sync" line right next to its own "First time here?" message.** Walk 7,
  2026-08-13, same scratch setup as above: before any config existed at all,
  the landing page read `First time here? This sets your download folder...`
  directly above `Last sync: 33 minutes ago – 49 file(s) failed` - two
  adjacent, contradictory claims about the same never-before-seen setup.
  Cause, source-confirmed (`internal/gui/gui.go`): `SetupNeeded`/
  `SyncBlockedReason` come from `config.Load(s.configPath)` (correctly
  config-scoped) but `LastSyncKnown`/`LastSyncWhen`/`LastSyncDetail` come from
  `statuslog.ReadLastSyncDefault`, a fixed global path unrelated to
  `s.configPath` - the same "global status file" root cause the "Optional,
  not a commitment" entry below already named for the *schedule* banner, now
  confirmed to reach the *landing page's own last-sync line* too, and visibly
  contradicting the adjacent first-run copy rather than just being stale.
  Lower severity than the login-reuse finding above (nothing is silently
  acted on, it is just confusing text) but same underlying cause family.
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
