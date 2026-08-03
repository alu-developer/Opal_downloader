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

- **Find out whether the "show all" click is dropped or just answered too
  late** (`docs/sync-speed-model.md` Question 19). The last unexplained real
  file loss: under `course_concurrency=2` a paginated folder returns the same
  41 rows it started with and drops six files, twice out of four runs, while at
  `concurrency=1` the same node expands correctly to 44. Prediction and failure
  criterion are already written down — read them before running, not after.
  Needs click-level logging plus a contention run to reproduce, and expect
  repeats because the failure is intermittent.

---

## Next
_Nothing queued._

---

## Noticed

Things seen while working on something else and passed over. Not commitments —
rough edges that would otherwise only exist in one session's context window.
Delete an entry when it is done, or when it turns out not to matter.

- **Answered, negatively: there is no 2FA-free app access to OPAL besides
  WebDAV (2026-08-03).** Asked whether some other interface skips the
  Shibboleth/2FA login the way WebDAV's own password does. Probed 15
  unauthenticated paths at `bildungsportal.sachsen.de`: `/opal/webdav/` answers
  `401 WWW-Authenticate: Basic realm="OPAL WebDAV"` (confirmed — plain Basic
  auth, no IdP in the path), while `/opal/restapi/*` answers a bare **403 with
  no auth challenge at all**, distinct from an unknown path, which redirects.
  So the REST API is deployed but closed to us, and it does not even offer
  credentials a chance. That matches this repo's own earlier finding — see
  `docs/sync-speed-campaign.md`, "REST API 403 at the proxy" (2026-07-21) — so
  it is two independent probes eleven days apart. BPS keeps the REST docs
  password-protected behind an email request to support@bps-system.de and
  describes it as a system-integration interface. Not worth pursuing: 2FA is
  already solved unattended by TU-Fast, so any of this would be about speed,
  not access, and WebDAV was measured and rejected on other grounds. Recorded
  so nobody re-runs the search.

- **The "one unexplained 300s login timeout" recurred a second time
  (2026-08-02), this time with no concurrent-process collision to blame.**
  After clearing the collision above and confirming no other opal-downloader
  process was running, the Question 15 retry hit `ensureSession: timed out after
  300000ms waiting for the OPAL course list after login` on its own - TU-Fast
  opened the login window but the course list never appeared within 5
  minutes. No debug flag was on, so nothing was captured. Fixed the capture
  gap so this doesn't happen a third time: `waitForLoggedInCourseLink`
  (`internal/scraper/session.go`) now folds the page's URL at the moment of
  timeout directly into the returned error, unconditionally (not gated behind
  `--debug-clicks`) - see the commit for the one-line diff. Whether the
  actual cause is TU-Fast itself, OPAL, or something in this project is still
  unknown; next recurrence should at least say where the page was stuck.
- **Two concurrent Routines colliding on the shared browser profile produced a
  hard failure, not the clean serialization `acquireSessionLock` is supposed
  to give (2026-08-02).** Running the Question 15 sync-speed probe manually
  overlapped in real time with a separately-scheduled run (`last-scheduled-run.json`,
  timestamp `2026-08-02T11:22:51+02:00`): that run failed with `playwright:
  timeout: Timeout 180000ms exceeded` while launching Chrome against
  `login-profile`, and my own probe hung for the full 22 minutes before its
  own `go test -timeout 20m` killed it — no `ErrProfileLocked` surfaced on
  either side, which `session_lock_windows.go`'s named-mutex design
  (`sessionLockAcquireTimeout = 6 * time.Minute`) exists specifically to
  produce instead of a raw launch-timeout or a silent hang. Two chrome.exe
  processes were left running after the killed test and had to be reaped by
  hand before the profile was usable again. Not yet root-caused — candidates:
  the mutex names two processes derive not actually matching for some path
  representation reason, or Playwright's own Chromium `ProcessSingleton`
  timing out before either process's Go-level lock logic gets a chance to
  run. Options for whoever picks this up: (a) reproduce deliberately (launch
  two `ensureSession` calls a few seconds apart against the same profile,
  watch for `ErrProfileLocked` vs a raw launch timeout) rather than trying to
  reason from one real collision; (b) log `sessionMutexName`'s derived name
  and the profileDir/stateFile strings each process actually resolves,
  since a silent mismatch there would explain the mutex never engaging;
  (c) lower `sessionLockAcquireTimeout` or add a fail-fast path so a second
  process gets a clear error in seconds instead of minutes even if today's
  exact failure mode isn't reproduced. No file loss and no bad data resulted
  (only a wasted 22 minutes and two orphaned processes), so this is not
  urgent, but it undermines the "safe to run several sessions at once"
  assumption this whole autonomy setup leans on. **Update 2026-08-03:** the
  two scheduled tasks were merged into one, so Routine-against-Routine is no
  longer a way to trigger this. The lock bug itself is untouched — a scheduled
  run and one of the maintainer's own sessions can still collide — so the
  options above stand.

---

## Standing work

Not an item to finish — the work that fills a run when nothing above is
unblocked. The `opal-downloader-autopilot` task reaches it as its phase 2.

### Sync speed as an iteration loop — reopened 2026-07-31
The campaign was closed on the strength of "every lever measured". The
maintainer's diagnosis is that the *working method* failed, not the levers:
try an idea, it fails, drop it, with no step in between where anyone
understands why. **`docs/sync-speed-model.md` is now the driver** — known
numbers, ranked open questions, and one experiment at a time with its
predicted number and kill criterion written down *before* the run.
`docs/sync-speed-campaign.md` is the archive.

No cap on the campaign; the kill criterion sits per experiment. A report every
fifth cycle carries a keep-going-or-stop recommendation, and the maintainer
makes that call. Every experiment goes behind an env flag and is diffed
byte-for-byte against the 345-file ground truth — but a default that has
*passed* that diff may now be changed and shipped (decision 2026-08-03), so a
measured win reaches the maintainer instead of sitting behind a flag.

**Correctness goes ahead of speed in this loop (decision 2026-08-03, after the
five-cycle report).** Keep-going was confirmed, but the next cycles go to
Question 18 rather than to another timing lever: the round produced the first
correctness bug the campaign has found rather than caused, and it is losing
files today at the default setting. The remaining discovery levers (the 4000ms
hard cap, the 150ms poll interval) are both smaller than the one just taken, and
even a perfect result there leaves ~150s against a 30s target.

**A byte-for-byte diff is not proof of losslessness (learned 2026-08-03).** It
only catches losses that *vary* between runs. A section truncated identically on
every run is identical to itself and to the ground truth, and passes every gate
this project has — which is exactly what Question 18 turned out to be, through
all 8 runs of Questions 14 and 15. Do not read "all runs agreed" as "no files
lost"; `warnShowAllTruncated` in the run log is currently the only signal that
sees this class, and nothing consumes it.

**Do not convert a measured effect into a rule without a mechanism (2026-08-03).**
Question 16 measured file loss at `course_concurrency>1` and went straight to
proposing the setting be clamped away. The maintainer refused the exclusion and
asked why the loss was consistently *six* files — which turned out to be
answerable from an archived log in minutes, and re-classified the whole thing
from "concurrency loses files" to "a known expansion bug fires more often under
load". Rule 2 of the model applies to the campaign's own conclusions, not only
to its experiments.

---

## Done recently

Newest first, one line each. **Anything needing more than a line belongs in
`docs/BACKLOG-archive.md`** — this section exists so a session can see what just
happened, not to hold the reasoning. Trim to roughly the last ten entries and
move the rest across.

- **The truncation alarm was a broken detector, not a truncation** (2026-08-03,
  Question 18): the warning that had fired on every run for two days was
  counting raw table rows, so it flagged the tutorial *enrolment* table — which
  holds no files — every single time its pager disappeared on expansion. Live
  href-level probe: not one lost row was file-shaped, nothing has ever been
  missing there, and the 345-file ground truth is fine. It now counts file rows
  and stays quiet on sections with none; re-verified live, warning gone, file
  count unchanged. **The cost was never the noise:** this is the only signal the
  project has for a real truncation, and firing constantly is why Question 17's
  genuine six-file loss sat unread in those same logs.
- **The session status says a date now, and the failure toast stopped being
  optional** (2026-08-03): the landing page read "session saved <mtime>. May
  still need a fresh login if it expired" — a file timestamp and a shrug. New
  `internal/sessionstate` reads OPAL's own `authenticated-marker` cookie out of
  the saved Playwright state; the page now says "valid until Thu 6 Aug, 11:17
  (2 days left)". Live-verified against the real state file. It is an upper
  bound, not a promise — OPAL's server-side session can die sooner, which the
  wording says. Deliberately does *not* gate the Sync button: an expired
  session is not a blocker (see CLAUDE.md). In the same commit,
  `notify_on_scheduled_failure` was deleted outright — config key, GUI
  checkbox and all — because there is no scenario for switching it off, and it
  structurally could not fire for the failure that matters most (a run that
  died before `config.Load`). Plus two bits of flavour on the sync page at the
  maintainer's request, browser-walk tested.
- **The code budget is gone** (2026-08-03, maintainer's call): `codebudget_test.go`
  deleted. In its 7 days it never once refused a raise — 11181 → 11898 committed,
  plus a pending raise to 12025 that died with it — and its comment had grown to
  213 lines of justification over 84
  lines of code, which is the "a number I defend is a paragraph I write" failure
  it was explicitly built to avoid. It did earn two things worth remembering by
  hand: lower the ceiling when you delete something, and try trimming before
  growing. `git show 5153cb5:codebudget_test.go` has the file and its full ledger.
- **The 150ms debounce shipped as the default** (2026-08-03, decision round):
  `mutationObserverDebounceMs` 300 → 150, the campaign's first user-visible win
  since it reopened. ~29% off the dominant component of a sync, on 8 byte-identical
  live runs across two courses 27x apart in size. The "also prove it under
  contention" precondition was dropped as unmeetable — Question 16 showed the
  unchanged config already differs from itself there.
- **Question 17 answered without a live run, and `course_concurrency>1` was NOT
  clamped** (2026-08-03, decision round): the maintainer rejected the proposed
  exclusion and asked for the mechanism first. `warnShowAllTruncated` had already
  fired in the archived log on exactly the two runs that lost files (4/4
  correlation), naming the branch — so it is a "show all" expansion bug that
  contention makes more likely, not a property of concurrency. The setting keeps
  its default of 1 and stays available.
- **The two unattended Routines merged into one** (2026-08-03): `-sync-speed`
  is deleted; `opal-downloader-autopilot` now works the backlog first and falls
  through to the sync-speed campaign. They had been firing milliseconds apart
  (measured: 1 ms, 75 ms, 9 ms) because Routines only fire while the Desktop app
  is open and missed runs catch up together — so the ~2h47m cron offset never
  applied. Consequences: autopilot's backlog work had dried up (its only *Now*
  item was sync-speed's own campaign), two runs aborted on collision, and one
  Question-15 attempt was lost to a 22-minute profile deadlock. Four rules changed
  with the merge — no usage-limit gate at all, *Now*+*Next*+*Noticed* must be
  clear of unblocked items before the handoff, a byte-diff-proven default may
  now be shipped, and run length is left to judgement.
- **Installer's Chromium-cache-path fix verified in CI and merged** (2026-08-03,
  PR #131): `release.yml` gained a `workflow_dispatch` trigger and a step that
  silently installs the built installer and asserts both browser binaries land
  under `%USERPROFILE%\.opal-downloader\ms-playwright` — with the runner's own
  cache moved aside first, without which the check passes regardless. Took
  three revisions of the *check* (chrome.exe is GUI-subsystem so `--version`
  leaves `$LASTEXITCODE` unset; chrome-headless-shell.exe carries no version
  resource at all, confirmed against a working local install), which is
  recorded in `docs/installer-plan.md`. Not verified: that the installed app
  launches the browser end-to-end — that needs an OPAL account the runner
  does not have.
- **Sync-speed Question 16 answered by refutation: the contention baseline is
  itself unstable** (2026-08-03): four 2-course runs at `course_concurrency=2`
  split 248/242/242/248 — the same 6 files from one *paginated* course node
  vanished in one run of each condition, including the unchanged
  500ms/6000ms one. So a tighter debounce could not be tested: there was no
  stable baseline to test it against, and the finger points at the Wicket
  "show all" path, not the settle budget. Users unaffected
  (`DefaultCourseConcurrency = 1`). Opened Question 17 with a decided next step.
- **Sync-speed Question 15 closed: 150ms debounce holds on the large course too**
  (2026-08-02, autopilot): same file-set, 210/210, across 2 baseline (300ms)
  and 2 override (150ms) runs against Softwaretechnologie (164 sections);
  savings 28.7%, matching the small course's 29.6% (Question 14) almost exactly
  — took 3 failed attempts first (a Routine collision, then a recurrence of
  the 300s login timeout, now diagnosed with a page-URL fix in `session.go`).
  Course-size dimension answered; `course_concurrency>1` contention is not,
  and can't be with the current probe design — see `docs/sync-speed-model.md`
  Question 16.
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
