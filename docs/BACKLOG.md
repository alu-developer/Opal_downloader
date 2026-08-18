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

### Friction campaign (GUI walks 1, 4, 5 & 7, CLI walks 2 & 6, first-run walks 3 & 7)

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
- **wrong — `~/.opal-downloader/last-sync.json` (and the "last sync" line the
  GUI builds from it) is config-independent, so *any* sync silently
  overwrites the maintainer's real status, including one run against a
  scratch `--config` for testing.** Found live, 2026-08-18, during Question
  44's own verification runs: two `sync --config tmp/policy-verify/...`
  calls against a throwaway `download_path` each called
  `statuslog.WriteLastSyncDefault` (`cmd/opal-downloader/root.go` line ~860,
  unconditionally, regardless of `--config`), leaving the maintainer's real
  GUI landing page reporting the scratch run's numbers ("1 downloaded, 348
  skipped") as if it were his real account state. Recovered this instance
  from `~/.opal-downloader/scheduled-run-history.jsonl`'s last real entry
  (2026-08-17, not fabricated), but that recovery path only exists because a
  *scheduled* run happens to also log to a second, append-only file -
  nothing would have caught a manual `sync --config` doing the same thing
  with no history file to fall back on. **Answers walk 7's own open question
  2** (`docs/friction-campaign.md`, "Do the GUI's other config-scoped-looking
  numbers share the same global-path leak the 'Last sync' line has now been
  shown twice to have") for the write side specifically: yes, and it is not
  just a display quirk on a not-yet-configured landing page (walk 7's finding,
  fixed 2026-08-15 by hiding the line) - the write itself is unconditional,
  so a fully-configured real install's status can be clobbered by an
  unrelated scratch run anywhere on the machine. `WriteLastSyncDefault`
  being machine-wide was a deliberate, named design choice at the time
  (walk 7); this finding is that the choice has a real cost the campaign's
  own scratch-config-for-experiments practice now pays. Same root cause
  likely reaches
  `login`/`list --scheduled` and any other command that writes through
  `statuslog` - not checked this pass. Fix is a maintainer call: either
  `WriteLastSyncDefault` should take `download_path` (or the whole config
  path) into its identity somehow, or every command that can run against a
  non-default `--config` needs to skip the shared status write entirely
  (today's assumption is baked in - the file's own doc comment calls it "the
  most recent outcome" with no notion of *whose* config that outcome
  belongs to). Until decided, running *any* experiment against a scratch
  config from a worktree is not actually side-effect-free the way
  `docs/friction-campaign.md`'s green/amber tiering assumes - `last-sync.json`
  needs to move from unlisted to the amber tier (snapshot-first) explicitly.

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
