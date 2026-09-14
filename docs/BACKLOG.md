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

`docs/sync-speed-model.md` holds the ranked list and the full history (it is
the permanent record - nothing below duplicates it, this is a pointer plus
current state). Re-ranked 2026-08-12 when the maintainer redefined the speed
target from "discovery" to "the whole sync, start to `Done.`". Steady-state
baseline (2026-09-01, real account, all 6 courses, no-op sync): **223.2s**
against a ~30s target, of which 151.3s (~68%) was, at the time, 7
"signal-less" files each paying a ~21.5s browser-fallback cost every sync
(`needsContentVerification`, `internal/syncer/syncer.go:376`) - that
cost-concentration is Question 45. Question 44 (the discovery-phase
version/fork investigation) and Question 41 (course-level HTTP concurrency)
are both closed for good - see `docs/BACKLOG-archive.md`'s "Settled" and
`docs/sync-speed-model.md`'s own entries for either.

**Question 45** (why signal-less files pay the browser-fallback cost every
sync) is down to one open subset. The `So26 Programmieren` `Woche` cluster
is closed to a maintainer call, confirmed 2026-09-14: these are `node-st`
pages whose files are plain links in lecturer-authored free text, with no
server-side metadata anywhere to route around - options A (7-day
`VerifiedAt` TTL cache, recommended), B (non-blocking only, no TTL) or C
(accept the cost), detailed in `docs/sync-speed-model.md` Question 45, need
the maintainer's staleness call. The `2026 LA20/Übungen` subset is resolved
**without** a maintainer call - it rides on Question 43's bulk-ZIP result
below instead of needing its own fix. One loose end: `Kapitel5.pdf`
(signal-less, same course) is not actually in `Übungen` - which section it's
really in is still unfound.

**Question 43** (bulk-download-as-ZIP) is the top unblocked speed item.
Proven at whole-section scale on a real account (2026-09-14: 15 files,
1.586s total, 15/15 real per-file timestamps, ~200x today's per-file
browser-fallback cost for that cluster), with the selection mechanism
pinned: click each row checkbox individually - the header "select all"
control visually works but does not fire the AJAX callback the download
button's enabled state needs. **What remains: sketch the `internal/syncer`
integration** - when to trigger a bulk fetch vs. per-file downloads, how a
zip entry's mtime maps onto `remote.Modified`, and the byte-diff that would
be needed before shipping it even behind a flag. Full ranked history:
`docs/sync-speed-model.md` Questions 43 and 45.

---

## Open findings

Found by using the tool as a normal user rather than reported by the
maintainer. Walk detail, expectations and named causes:
`docs/friction-campaign.md`. Tags: **blocker** / **wrong** / **friction** /
**bloat** / **question**.

### Friction campaign (GUI walks 1, 4, 5 & 7, CLI walks 2, 6, 8, 9, 11, 13, 15, 17, 19 & 21, first-run walks 3, 7, 10, 12, 14, 16, 18 & 20)

- **friction — no per-invocation course selector on `sync` or `list`, so
  "grab just this one course right now" forces a persistent `config.yaml`
  edit.** Walk 19, 2026-09-02 (CLI everyday use). Persona: a student who
  wants only the new Analysis exercise sheet, not a crawl of all six
  configured courses. `sync`'s flags are `--force --dev --profile
  --debug-clicks --concurrency --course-concurrency --section-concurrency
  --no-skip-enrollment-sections --scheduled`; anything else is
  `unknown option for sync`. `--course-concurrency` is a false friend (a
  parallelism knob, not a picker). The only way to scope to one course is
  editing `config.yaml`'s `courses:` list and remembering to revert it;
  `--help` gives no hint that this is the workflow. **Named cause:** the
  configured `courses:` list is the sole expression of "which courses" and
  it lives in a persistent settings file - no surface has a per-run scope
  override. **Predictions, all confirmed cheap:** `list` has the identical
  gap; `--force` is also all-or-nothing (no path/course argument, so one
  corrupt file means re-forcing the whole sync); the GUI is not an escape
  hatch either - `/sync` runs `syncer.SyncCoursesWithProgress(..., loaded.App, ...)`
  on the whole config (`internal/gui/sync.go:196`), no per-course button.
  The gap is tool-wide. **Open question (needs a maintainer product call):**
  is a per-run `--course <pattern>` selector wanted, or is "edit the
  config's `courses:` list" the intended source-of-truth workflow? If
  wanted: (a) glob match like `course_folders` or exact match like the
  config list, and (b) does it imply `--force <course>`/`--force <file>`
  narrowing too. Full detail: `docs/friction-campaign.md` Walk 19.
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
  worth a human's attention" is a real OPAL-course-content question that
  cannot be answered from the crawl structure alone - a `Hausaufgabe N`
  page being empty could mean "this node is just a container for other
  things" or "the assignment isn't posted yet", and those look identical to
  the crawler.

  **Blocked on a product call. Three options, recommendation first:**

  - **(A, recommended) Ship the report as-is and close this finding.** The
    container label already covers the one case a reader could not
    otherwise diagnose (a course root or folder that *should* have surfaced
    files). Every remaining always-empty row is a known per-item leaf whose
    emptiness is expected the vast majority of the time; a label there would
    fire on ~96% of rows and train the reader to ignore it, which is worse
    than no label. The residual risk - a genuinely missing file on a leaf
    page that looks the same as an unposted assignment - is already covered
    by the byte-diff ground truth and the discovery-count smoke check, not
    by this human-readable report. Cost: none. Downside: a reader who wants
    "is *every* empty page really supposed to be empty" still has to check
    OPAL by hand.
  - **(B) Add a collapsed-by-default "N always-empty leaves (expand to
    list)" footer.** Keeps the signal reachable without burying the report.
    Cost: ~half a day in `visitlog.FormatReport` plus a flag to expand.
    Downside: still no way to tell "expected empty" from "missing file", so
    the expanded list is only a manual-check worklist.
  - **(C) Find a second structural signal by hand.** Cross-reference ~20-30
    real leaf pages against what the course actually shows a logged-in
    student, looking for something the crawler sees on a "really has content
    coming" page that it does not see on a "just a container" page (a
    date-gated element, a submission widget, an empty-state string). Cost:
    an hour of live crawling plus analysis, no guarantee a signal exists.
    Only worth doing if (A) is rejected.

  Full detail: `docs/friction-campaign.md` Walk 11.
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

---

## Noticed

Rough edges seen while working on something else, that would otherwise exist
only in one session's context window. Not commitments. An entry leaves in one
of two directions: up into the work above, or into `docs/BACKLOG-archive.md`
once it is done, decided, or shown not to matter.

_Nothing currently._ (The `TestSyncScheduledSkipsWhenAlreadySucceededToday`
non-hermeticity note moved to `docs/BACKLOG-archive.md` "Settled" on
2026-09-01 — three mechanisms ruled out, nothing left to fix in the test's
own guard logic unless it recurs with a captured PID.)
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
