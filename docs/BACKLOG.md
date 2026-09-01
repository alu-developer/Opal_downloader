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
2026-08-20 (this run): `~/.opal-downloader/sync.lock` does not exist. The
2026-08-13 5.5-hour hold is closed, see `docs/BACKLOG-archive.md`._

_Nothing open right now — the walk 15 `smoke-check --full-sync` self-deadlock
(2026-08-19 target-path-collision fix and 2026-08-20 weekly-review Part B
panic-safety fix too) all shipped and are live/test-verified; detail moved
to `docs/BACKLOG-archive.md`._

---

## Next

`docs/sync-speed-model.md` holds the ranked list, re-ranked 2026-08-12 when
the maintainer redefined the speed target from "discovery" to "the whole
sync, start to `Done.`"

**Question 44 CLOSED 2026-09-01 (autopilot), both halves.** Policy half
shipped 2026-08-18 (a failed download now backs off instead of retrying
forever - see `docs/BACKLOG-archive.md` for the shipped mechanism). Cause
half closed 2026-09-01 by a genuinely different approach than the four prior
GitHub-source-reading passes: OPAL's own login page names its release
("OPAL 2026.08.2"), and a single web search on that turned up OPAL's
documented history - **it's an independently-developed proprietary fork BPS
split from OLAT 7.1 in 2011, not any version of `OpenOLAT/OpenOLAT`** (the
only repo this campaign ever read). No public OPAL source exists to read,
so this class of investigation is closed for good, not just deferred - see
`docs/BACKLOG-archive.md`'s "Settled" for the full finding and
`docs/sync-speed-model.md`'s Question 44 entry.

**End-to-end re-measurement done 2026-09-01 (autopilot):** a steady-state
no-op sync against a fresh scratch manifest, real account, all 6 courses -
`downloaded=0 skipped=349 errors=0 backing_off=49`, **Total 223.2s**. Below
the maintainer's own "~300s before this campaign started" recollection for
the first time this campaign has had a real number to check it against, but
still well above the ~30s target: **151.3s of that 223.2s comes from just 7
signal-less files** (`needsContentVerification` -
`internal/syncer/syncer.go:376`) whose byte-level verify has no direct link
and hits the same ~21.5s browser-fallback path as a failed download (7 ×
~21.5s ≈ 150.5s, matching the phase almost exactly) - the other 342 skip in
a fraction of a second each. Live-sizes the "signal-less-file verify path"
cost flagged as an open question 2026-08-18 and never picked up since, and
unlike the backed-off cluster this cost can't be reduced by backoff (these
files must be re-checked every sync, by definition). At ~68% of a no-op
sync from 2% of the files, **that cost is now the top-ranked item**, above
Question 43. Full numbers and the new question's exact framing:
`docs/sync-speed-model.md`'s "Next experiment".

**Question 43** (bulk-download-as-ZIP) sits second, still stalled on a
DOM-flakiness finding from 2026-08-12's Step B — two untried directions are
named in its own entry in `docs/sync-speed-model.md`. **Nothing on this
list is blocked on the maintainer** — Question 39 is decided and built, and
Question 5 is fully closed (see `docs/BACKLOG-archive.md`). Nothing further
is planned on the course-level HTTP concurrency thread — Question 41 closed
2026-08-11 as a no-go.

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

### Friction campaign (GUI walks 1, 4, 5 & 7, CLI walks 2, 6, 8, 9, 11, 13, 15 & 17, first-run walks 3, 7, 10, 12, 14 & 16)

- **wrong — `init`/`setup`'s printed "Next steps" and `status`'s "not logged
  in" line hardcode `config.yaml` and bare `opal-downloader login`/`sync`,
  ignoring the `--config <path>` the user actually passed.** Walk 17,
  2026-09-01. Ran `init --config tmp/friction/init-test.yaml` and got "Edit
  config.yaml... Run: opal-downloader login... Run: opal-downloader sync" -
  a file that doesn't exist and commands that, run as printed, would
  create/touch a different, default-path config instead of the one just
  initialized. Source-confirmed at four `fmt.Println`/`fmt.Printf` call
  sites in `cmd/opal-downloader/root.go`: `runInit` (313-320), `runSetup`
  (371-377), `printLoginStatus` (441), and a fourth at line 576 - all sit
  right next to a `configPath` variable already in scope but print a
  literal string instead of interpolating it. Anyone running more than one
  config (a second OPAL account, a scratch/test config, or simply following
  the README's own `--config` examples) gets instructions pointing at the
  wrong file. Open question this walk left: whether every site is a
  mechanical interpolate-`configPath` fix, or one of the four needs the
  variable threaded through first - not checked yet, read all four sites
  before assuming. Full detail: `docs/friction-campaign.md` Walk 17.
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
  of the code, so none of it counts as a persona walk. **Confirmed still
  blocked, walk 16, 2026-08-20:** this machine has no Inno Setup (`iscc`)
  installed, so `scripts\build-installer.ps1` cannot run here at all - a
  tooling gap, not a persona finding, and the second unattended run in a row
  to hit it. Needs either Inno Setup installed on this machine or a live
  session with it available.
- **Fixed, 2026-09-01 (autopilot):** the README's "Quick Start (Web UI)"
  section now describes the real Windows behavior (auto-opens a native
  WebView2 window, blocks until closed) and names the non-Windows fallback
  (prints the URL, waits for Ctrl-C) explicitly, instead of stating the old
  print-and-wait flow as universal fact. `docs/gui-concept.md`'s matching
  staleness (`:137`, `:346`) needed no edit - the file already opens with a
  "superseded... treat every open question past this point as historical
  framing" banner, so it doesn't mislead a reader the way the README did.
  Full detail: `docs/friction-campaign.md` Walk 16.

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
