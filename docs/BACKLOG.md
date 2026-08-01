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

**Keep entries short.** An item says what is left and where the detail lives.
It is not the place to record what was already done — that goes in the commit
message, the relevant `docs/` file, or `docs/BACKLOG-archive.md`. This rule
exists because ignoring it grew the file to 1057 lines by 2026-07-31, most of
it closed work, until nobody could read it in one pass. Reintroducing history
here is the failure mode to watch for.

---

## Now

### Sync speed runs as an iteration loop again — reopened 2026-07-31
The campaign was closed on the strength of "every lever measured". The
maintainer's diagnosis is that the *working method* failed, not the levers:
try an idea, it fails, drop it, with no step in between where anyone
understands why. **`docs/sync-speed-model.md` is now the driver** — known
numbers, ranked open questions, and one experiment at a time with its
predicted number and kill criterion written down *before* the run.
`docs/sync-speed-campaign.md` is the archive.

Runs unattended as the `opal-downloader-sync-speed` scheduled task, one cycle
per run, reporting every fifth cycle with a keep-going-or-stop recommendation.
No cap on the campaign; the kill criterion sits per experiment. Nothing here
changes a default — every experiment goes behind an env flag and is diffed
byte-for-byte against the 345-file ground truth.

The top open question is the one nobody ever asked: **OpenOLAT is open source
and its source has never been read.** Ten days were spent guessing at the live
server what the code says out loud.

---

## Next

### The installer bundles Chromium into a directory the app never reads
Fix proposed in [PR #131](https://github.com/alu-developer/Opal_downloader/pull/131)
(branch `fix-installer-playwright-cache-path`) — **UNVERIFIED, not merged**.
`opal-downloader.iss`, `build-installer.ps1`, and `release.yml` all pointed at
`%LOCALAPPDATA%\ms-playwright`, which stopped matching
`EnsurePlaywrightBrowsersPath`'s actual default
(`%USERPROFILE%\.opal-downloader\ms-playwright`, since commit `b352143`,
2026-07-13) — so a fresh install's bundled Chromium landed where the app never
looks. PR moves all three to the correct path. Needs a real build (Inno Setup
+ a populated local Chromium cache, neither available in the environment that
made the fix) before it can merge — see the PR's test plan and
`docs/installer-plan.md`'s 2026-08-01 addendum.

---

## Noticed

Things seen while working on something else and passed over. Not commitments —
rough edges that would otherwise only exist in one session's context window.
Delete an entry when it is done, or when it turns out not to matter.

- **One unexplained 300s login timeout on 2026-07-31.** `ensureSession: timed
  out after 300000ms waiting for the OPAL course list after login` during a
  sync-speed cycle. It was written off as "needs 2FA, unattended runs can't do
  that", which is wrong — the same path auto-logged-in fine on 2026-08-01, so
  the cause is still unknown. If it recurs, capture the browser state instead
  of blaming 2FA. Note in `docs/sync-speed-model.md`.

---

## Done recently

Newest first, one line each. **Anything needing more than a line belongs in
`docs/BACKLOG-archive.md`** — this section exists so a session can see what just
happened, not to hold the reasoning. Trim to roughly the last ten entries and
move the rest across.

- **Scheduler disable-path "no guard" Noticed item closed as investigated,
  not fixed** (2026-08-01, autopilot): `scheduler.Disable()` itself still
  performs no ownership check (it deletes whatever `schtasks` has under
  `TaskName`), but both real callers — the CLI's `schedule disable` and the
  GUI's Settings form handler — are reachable only via explicit user action,
  and no installer/uninstaller script or background path calls it. The actual
  residual risk is dev/test sessions in this repo, already mitigated by
  `live_schedule_guard_test.go`'s env-var gating and the `scheduleDisableFunc`
  test double. Left a doc comment on `Disable()` spelling out the obligation
  for future callers instead of adding speculative validation logic.
- **Installer's Chromium-bundling path bug found and a fix opened as a PR**
  (2026-08-01, autopilot): see Next — `%LOCALAPPDATA%` vs
  `EnsurePlaywrightBrowsersPath`'s actual `%USERPROFILE%\.opal-downloader`
  default. UNVERIFIED (no Inno Setup / local Chromium cache available here),
  so it's a PR (#131), not a merge.
- **Two "your call" backlog items closed on their own stated recommendation**
  (2026-08-01, autopilot): the 2026-07-26 feedback-batch visual-check and the
  first-run-journey dogfood item both said "can be closed without you,
  recommended: close on the strength of existing automated coverage" — acted
  on that instead of leaving it sitting. The `internal/scraper/crawl.go`
  stays-unsplit decision they recorded stands. The scheduler-disable-path gap
  they surfaced moved to Noticed.
- **Weekly-review pass's two Next items closed** (2026-07-31, autopilot):
  `pre-push-gate.ps1`'s stale test-suite comment fixed, and both settings
  course-list visibility bugs (opacity-stacking contrast, note flash) fixed.
- **Sync-speed campaign closed for real this time** (2026-07-31): reopened
  autonomously via the ground-truth diff instead of needing a live human
  (confirmed `internal/syncer` never deletes files, so a regression here is
  recoverable and the byte-for-byte diff is a sufficient check); built and
  ran the tree-walk-only wait against the real account, diagnosed it as a
  measurement artifact (the content tree is JS-rendered at *every* level, not
  just the dashboard), then measured the HTTP mode=1 alternative live (47s
  slower, not faster). 30s stays unreachable loss-free; ~207s is the real
  ceiling. Full trail in `docs/sync-speed-campaign.md`.
- **The .exe has its own Explorer icon** (2026-07-31): `rsrc_windows_amd64.syso`
  is generated from `internal/gui/assets/icon.ico` and checked in, so building
  needs no new tool and `go.mod` is untouched — which is what the "needs the
  maintainer's OK for a build-time dependency" block was really about.
- **The course list is always on the settings page now** (2026-07-31):
  "Sync all courses" still starts ticked, but it mutes the list and says the
  ticks are inactive instead of hiding it. Folder inputs stay live, because
  `course_folders` applies under the wildcard too.
- **Deleted the self-built autonomy machinery** (2026-07-31): ten of twelve
  PowerShell files, the `OpalDownloader-ResumeRunner` Windows task, the
  keep-warm process, and every accumulated file in `.claude/queue/`. Replaced by
  first-party Claude Code Desktop scheduled tasks. Kept only the two hooks that
  *enforce* (pre-push gate, turn-failure checkpoint — which still writes
  `LAST_FAILURE.json` there). Trigger: 102 of 193 commits in seven days touched
  only `docs/`, `.claude/` or `scripts/`. The follow-up on 2026-07-31 deleted
  `docs/agent-operating-model.md` and `docs/agent-incidents.md` too: of 189
  lines, one section was neither duplicated in `CLAUDE.md` nor describing
  deleted scripts. Survivors folded into `CLAUDE.md` and
  `docs/work-quality.md`.
- Closed the last Noticed item (the User-Agent-fix theory) with a decision
  rather than another probe — a higher-volume burst at the real OPAL server is
  what `docs/server-load.md` exists to prevent, curiosity included.
- Closed the "unattended run can't wait for a background job" item as a rule,
  not a detector: behaviour lives in the prompt, hooks enforce only.
- Fixed a stdin BOM silently breaking JSON parsing in 5 hooks (2026-07-31).
- **Closed the sync-speed campaign's remaining question with a decision:** the
  verified HTTP-hybrid (diff=0, all 6 courses) ships as an opt-in diagnostic;
  the actual speedup would need an unreviewed change to the crawl's
  highest-risk code for an estimated ~60-90s that still misses the 30s target.
  Reliability outranks features. Reopen only with the maintainer watching the
  diff live, not by re-measuring. Reasoning in `docs/sync-speed-campaign.md`.
- Corrected two of my own same-session claims with real evidence: the Logon
  trigger did fire (Task Scheduler event 119), and the "overlapping launches"
  I had called confirmed were the cheap gate script, not real launches.
- Moved 678 lines of closed work into `docs/BACKLOG-archive.md` (2026-07-31).
- Routed every live probe's diagnostic logs somewhere visible.
- Found a real completion-signal candidate: jsTree's `aria-busy`, across 4
  courses.
- Wired the app icon into the running window (WM_SETICON), rasterised from
  logoSVG.
- Deleted the section change-detection cache, budget and all.
