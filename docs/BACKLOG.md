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

_Nothing open right now — the 2026-08-19 target-path-collision fix and the
2026-08-20 weekly-review Part B panic-safety fix (`DownloadFile`'s
browser-fallback mutex) both shipped and are live/test-verified; detail
moved to `docs/BACKLOG-archive.md`._

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

**Maintainer decision, 2026-08-19 (`/decide` round): keep going, resume the
version/fork cause hunt** (deprioritized since 2026-08-17, unblocked now
that the policy half above shipped), **and the next cycle should try a
genuinely new approach**, not another pass of the same source-reading shape
that already spent 16 commits without shipping. Full reasoning and the
maintainer's own "~300s before this campaign started" recollection —
already close to recent numbers, worth an explicit end-to-end
re-measurement early next cycle — in `docs/sync-speed-model.md`'s "Next
experiment" section. Same round also decided the shape of the
retry-budget/slow-file question that cycle left open: look first for a
cheap way to skip the expensive browser-fallback chain entirely for
unchanged files (rather than shrinking its retry budget), communicate
clearly when a file genuinely needs the slow path, and — a hard
constraint, independent of how the rest resolves — **one slow file's
resolution must never block anything else in the sync.** Detail in the
same section, "Maintainer decision, 2026-08-19".

---

## Open findings

Found by using the tool as a normal user rather than reported by the
maintainer. Walk detail, expectations and named causes:
`docs/friction-campaign.md`. Tags: **blocker** / **wrong** / **friction** /
**bloat** / **question**.

### Friction campaign (GUI walks 1, 4, 5 & 7, CLI walks 2, 6, 8, 9, 11 & 13, first-run walks 3, 7, 10, 12 & 14)

- **wrong — `README.md`'s "Commands" table never mentions `schedule` or
  `smoke-check`, so a first-timer following only the README has no way to
  discover that automatic daily syncing exists.** Walk 14, 2026-08-20. Both
  are real, working, `--help`-documented subcommands
  (`cmd/opal-downloader/root.go`'s `printHelp`) - `schedule` is literally
  "make this run on its own without me", the exact thing a first-timer
  reaches for right after their first successful manual sync - but
  `grep -ni schedule README.md` / `grep -ni smoke-check README.md` both
  return nothing. Cross-checked line-by-line against `printHelp`'s full
  command list: every other subcommand (`init`, `setup`, `status`, `gui`,
  `login`, `list`, `sync`, `dump-links`) is documented in README, so this
  isn't a generally-stale table - `schedule`/`smoke-check` were specifically
  never added when they shipped, and nothing (no test, no CI step) keeps
  `printHelp` and README in sync, so the same gap will recur for the next
  new subcommand unless something is added. Fix: add both to README's
  Commands table (and ideally a short "Automation" section pointing at
  `schedule enable`, matching the weight `docs/OPERATIONS.md` already gives
  it) - a small, self-contained doc fix. Full detail:
  `docs/friction-campaign.md` Walk 14.
- **wrong — walk 13's diagnostic-log filename fix only covers the manifest-key
  field; the same message's "technical detail" half still redacts filenames
  inside Playwright selector strings.** Walk 14, 2026-08-20, found while
  live-verifying walk 13's shipped fix. `printSyncError`'s own field
  (`internal/syncer/syncer.go`) is fixed and confirmed working live - but
  `internal/scraper/download.go`'s click-search loop builds the `(technical
  detail: ...)` half separately, embedding the target filename directly into
  Playwright locator strings (`a[href*='<name>.pdf']`,
  `getByText('<name>.pdf')`) that are never wrapped in
  `logging.ProtectPath` before reaching `logging.Detail` - so any filename
  with 32+ characters before its extension still comes back
  `[redacted].pdf` in that half of the line, live-confirmed in this walk's
  own log (`href-match a[href*='[redacted].pdf']`). One file in the same run
  escaped only because its name happened to be exactly 30 characters,
  under the threshold - not because anything protects it. Fix mirrors walk
  13's own shape: wrap the filename with `logging.ProtectPath` wherever
  `download.go` assembles the selector-error text, most likely inside
  `downloadFileViaBrowser`/`tryCandidatePagesInOrder`. Full detail:
  `docs/friction-campaign.md` Walk 14.
- **friction/bloat — `list --visit-report`'s "always empty" signal is
  buried under leaf-page nodes that were structurally never going to report
  a new file.** Walk 11, 2026-08-19. **Partially shipped and live-verified,
  2026-08-20 (autopilot):** every visit record now also carries
  `HadChildren` - whether the section's page had any subsection/folder
  links (queued, or skipped as a known non-file node type) - computed by
  reordering `appendSectionFolderTargets` before `recordSectionVisit` in
  both `internal/scraper/crawl.go` (browser path) and
  `internal/scraper/httpdiscovery_seed.go` (`discoverSectionsHTTP`, the
  default HTTP-first path). `visitlog.SectionStat.EverHadChildren`
  aggregates it across visits, and `FormatReport` appends "(container -
  files live in subsections)" to an always-empty row that ever had
  children, so a human no longer has to cross-reference the real course
  structure to identify *that* specific case. Deliberately does **not**
  attempt a "(leaf)" label for the rest - `EverHadChildren=false` is
  indistinguishable from "no data yet" on log entries written before this
  field existed, and asserting "leaf" on an absence of evidence risks being
  confidently wrong, which is worse than the status quo. Unit-tested
  (`TestAggregateSetsEverHadChildrenFromAnyVisit`,
  `TestFormatReportLabelsAlwaysEmptyContainerDistinctlyFromLeaf`,
  `TestDiscoverSectionsHTTPReportsVisitsForVisitLog`'s extended
  assertions). Live-verified against the real account (scratch
  `download_path`, fresh visit log): of 251 always-empty rows across 8
  courses, exactly 9 got the container label (course roots plus one
  `Algorithmen und Datenstrukturen/Materialien` folder) - the rest were
  genuine leaves (`Forum`, `Hausaufgabe 1`..`N`, one row per assignment)
  that never had children.

  **Still open, and this is most of the original complaint:** that live run
  shows the container label only resolves ~4% of always-empty rows - the
  other ~96% (`Hausaufgabe N`-shaped per-item leaves, matching Walk 11's
  `Vorlesung`/`Woche` examples) are genuine leaves with no children, and
  whether *that* is "structurally guaranteed, ignore it" or "a real gap
  worth a human's attention" is a real OPAL-course-content question this
  session has no grounds to answer from the crawl structure alone - a
  `Hausaufgabe N` page being empty could mean "this node is just a
  container for other things" or "the assignment isn't posted yet", and
  those look identical to the crawler. Chasing further needs either
  maintainer input on what these leaf pages are supposed to contain, or
  cross-referencing enough real course pages by hand to find a second
  structural signal - not more source-reading. Full detail:
  `docs/friction-campaign.md` Walk 11.
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
  Full diagnosis: `docs/friction-campaign.md` Walk 6.

  **Maintainer decision, 2026-08-19 (`/decide` round): install via the real
  installer** (`installer/opal-downloader.iss`, already built and shipping
  per `docs/installer-plan.md`) rather than adding a git-checkout override.
  Trying this surfaced two real, previously-undocumented installer problems
  rather than a clean install:

  - **Fixed and live-verified same session:** re-running the installer to
    upgrade over an existing install failed whenever the app was still
    running — either `opal-downloader.exe` itself or a
    `chrome-headless-shell.exe` it had started. Reproduced locally (built
    the installer with `scripts\build-installer.ps1`, installed once,
    started both processes, re-ran the installer): Inno's RestartManager
    check found the running files but couldn't close them gracefully, so a
    silent install aborted outright (`Some applications could not be shut
    down` → default Abort) and an interactive one would hit a genuine
    Windows access-denied error if the user pushed past the prompt. Fixed
    by adding `CloseApplications=force` to `installer/opal-downloader.iss`'s
    `[Setup]` section — re-ran the identical repro after rebuilding: log
    now shows `Shutting down applications using our files. (forced)`, all
    four locking processes gone, upgrade completes with exit code 0. See
    the `.iss` file's own comment at that line for the full trade-off
    (can interrupt an in-progress sync; accepted, since the download-phase
    backoff policy already treats an interrupted file as retry-next-sync,
    not data loss). Also added a regression test to
    `.github/workflows/release.yml`'s `workflow_dispatch`-only verify job
    ("Verify an upgrade succeeds while the app is still running") — starts
    the just-installed app and a `chrome-headless-shell.exe`, re-runs the
    installer over them, and fails CI if the upgrade doesn't exit 0 or
    either process survives, so a future edit that drops
    `CloseApplications=force` fails loudly instead of silently.
  - **Fixed:** `scripts\build-installer.ps1` (the one-command way to build
    `opal-downloader-setup.exe` locally, already used by the release
    workflow) was never mentioned in `README.md` — the maintainer had no
    way to discover it. Added a "Building `opal-downloader-setup.exe`
    locally" subsection to the README's "Build from source" section.

  The SmartScreen "check with the software publisher" warning the
  maintainer also hit while doing this is **expected, already documented**
  (README's "About the Windows security warning", the release workflow's
  own release notes) — not a new finding.

  **Still open:** installing for real and confirming `schedule enable` from
  the installed location reaches the real scheduled task with a working
  logon trigger — not done this session (this session's installs were
  disposable local test builds, uninstalled again afterward, not a real
  permanent install). That step still needs the maintainer to actually run
  the (now-fixed) installer on his own machine for real.
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

- **`TestSyncScheduledSkipsWhenAlreadySucceededToday` non-hermeticity: three
  mechanisms tried, all three ruled out — most likely explanation is now a
  misattributed real concurrent session, not a guard bug.** See
  `docs/BACKLOG-archive.md` "Settled" for the full trail (same-test
  contention, other-package live probes, and — 2026-08-19 (autopilot,
  live-verified) — a real background `sync` genuinely holding `sync.lock`,
  none of which make the test fall through). **Not fully closed:** no PID was
  captured for the process actually inside the original incident's run, so
  the misattribution explanation is well-evidenced, not proven. Downgraded
  from "real risk" to a documentation-only follow-up: nothing left to fix in
  the test unless it recurs with a captured PID.

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
