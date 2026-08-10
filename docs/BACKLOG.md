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

**Restructure the hybrid's phase 1 to discover over HTTP instead of by
walking the tree in a browser** — `docs/sync-speed-model.md` Question 36 Step
B2. Everything this needs is measured and closed: the course tree arrives
complete in each course root's own bytes (Steps A/A2, 261/261 URLs from 6
requests), and seeding from it then expanding with the crawl's own predicates
over plain HTTP reproduces **286 of 286 sections, 0 missing**, in 71.4s
against the same run's 173.8s browser crawl (Step B1, closed after run 1
failed at 4 sections and pagination was identified as a discovery boundary).
File extraction on that path was already verified diff=0 on all 6 courses
(2026-07-31).

What is left is production work, not a question: `scrapeCoursesHybrid`
(`orchestrator.go`) still runs the whole browser crawl first in *every* mode,
taking its section set from `VisitRecords`, because its own comment says the
browser is the only thing that can enumerate the tree — the sentence Question
34 refuted. Replace that with per-course root fetch → `ParseCourseTreeNodes`
seed → HTTP BFS, behind an env flag, and byte-diff against the 349-file
ground truth before it becomes a default.

Two things to carry over from the probe rather than rediscover: the seed must
apply `isNonFileSectionType` itself (it lives in
`appendSectionFolderTargets`, which a seed bypasses — 21 needless fetches
otherwise), and the expansion must follow `extractShowAllURLFromHTML`, because
rows past a section's pagination cap include sub-sections and not just files.

**This is one of the three paths that have silently lost files before, so it
goes as a PR per `CLAUDE.md`, not straight to master.**

---

## Next

**Raise `course_concurrency` past 2** — `docs/sync-speed-model.md`
Question 35. Maintainer asked for it in the same decision round. `3` has not
been measured since any of the 2026-08 work and `4` lost 9 files the last
time it was tried (2026-07-21), so this is a byte-for-byte parity sweep at 3
first, against a fresh same-session conc=2 baseline, with the same discipline
Questions 31–33 used. Do not skip to 4, and do not open a hunt if 3 comes
back short — stop at 2.

**Recommendation, 2026-08-10 — worth a moment before this is run:** it tunes
the browser crawl that Question 36 Step B2 would largely replace. If B2 lands,
these live runs were spent on soon-to-be-dead code. Options: (a) do B2 first
and re-ask whether concurrency 3 still matters afterwards — recommended, it
costs nothing but ordering; (b) run the sweep anyway as insurance in case B2
fails its byte-diff; (c) drop 35. Not blocking: this is a sequencing call and
(a) is the default unless the maintainer says otherwise.

---

## Noticed

Things seen while working on something else and passed over. Not commitments —
rough edges that would otherwise only exist in one session's context window.
Delete an entry when it is done, or when it turns out not to matter.

- **Question 23's raw-CDP preview blocker lost 33 files, all in one
  known-flaky section (2026-08-05).** Full mechanism write-up:
  `docs/sync-speed-model.md` Question 23 (closed, does not ship) and Question
  24 (the residual, ranked low). Short version: it isn't a new bug — Part-3 of
  "Softwaretechnologie (SoSe 26)" already carries Question 17's pre-existing
  "show all" expansion bug (Candidate B, still unfixed), which "fires more
  often under load," and this section is the single most preview-dense one in
  the account, so Question 23's own implementation loads it hardest. Not
  worth a retry cycle until Question 17's bug has an actual fix — that fix is
  the real prerequisite, for this and for `course_concurrency>1` both.
  `OPAL_BLOCK_FILE_PREVIEWS` stays off by default (already true, no user
  impact either way).

- **TU-Fast's stored credentials are obfuscated, not encrypted (2026-08-04).**
  Its AES key is derived from CPU/platform metadata only — no secret — and the
  store holds password *and* TOTP seed, which the transplant step then copies
  to a second profile. Accepted trade for unattended sync, but now written
  down: `docs/tufast-security.md`, incl. the mitigations that actually apply.

- **OPAL's WebDAV isn't broken — it never mapped participant course content
  (2026-08-04).** Full write-up in `docs/opal-webdav-student-access.md`.
  **Closed 2026-08-10 (decision round): the maintainer has sent a message to
  BPS himself and closed the letter thread** — the draft in that doc's §7 is
  no longer waiting on anybody. Nothing further to do unless BPS answers, in
  which case the answer belongs in that doc. Two loose ends worth
  remembering: (a)
  OPAL hands *students* a token-authenticated personal RSS feed covering
  subscribed folders — a possible cheap change-detector that needs no browser;
  (b) if BPS ever answers the letter, the answer belongs in that doc.
  **Update 2026-08-09 (autopilot, source reading, no live probe):** the REST
  API's admin-only framing below was only true for the course-level
  "Kursordner" endpoint. `BCWebService` (the actual Ordner-Kursbaustein REST
  service) gates its file listing on the same participant-visibility check the
  web UI uses, not an author/admin role — so opening it for students would be
  a network decision, not new engineering. Sharpened into the letter draft;
  full source citations in `docs/opal-webdav-student-access.md` §4.

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

  **Update 2026-08-09 (autopilot): third occurrence, and the capture gap fix
  paid off — the page was stuck at Shibboleth, not OPAL.** `login` (warming
  the session for Question 27) hit the identical error text and 5m05s
  duration, but this time named where: `page was at
  "https://bildungsportal.sachsen.de/opal/shiblogin;jsessionid=...?0"` — the
  Shibboleth IdP's own login-processing URL, before any redirect back to
  OPAL. No collision (clean tree, no other commits, no `chrome.exe` left
  over). An immediate retry, same profile, same session, succeeded in 5.95s —
  so whatever stalled was transient, not a standing block on this profile or
  account. That narrows "TU-Fast, OPAL, or this project" to the first two:
  the timeout fires while still inside the TU-Fast/Shibboleth handoff, before
  OPAL's own course-list page is ever reached, so this project's own
  post-login waiting logic was never in play for this failure. Not chased
  further — a single transient stall with no persisting evidence isn't worth
  a live-debugging session, and the capture gap fix already did its job by
  answering the question it was built for.

  **Update 2026-08-10 (autopilot): a mechanism that explains all three, and
  it was in this project's code after all.** The URL the capture-gap fix
  recorded turned out to be the whole clue. `loginSignals.stalled()` required
  `FieldCount > 0`, so a login-flow page with no input fields — exactly what
  `.../opal/shiblogin;jsessionid=...` is, an interstitial rather than a form —
  could never be classified as stalled, was therefore never reloaded, and ran
  out the full 300s `loginTotalBudgetMs`. That is the same 5-minute failure
  all three occurrences reported. So the earlier narrowing to "TU-Fast or
  OPAL, not this project" was half wrong: whatever *causes* the page to sit
  there is still theirs, but the reason it cost 300s instead of ~8s was ours.
  Fixed (see Done recently); verified only in the sense that a normal login is
  unaffected, since the stall has never been reproducible on demand. If a
  fourth occurrence appears, the thing to check is whether the run log now
  shows a `login-stall-reload` audit entry — its absence would mean the page
  was still moving enough to defeat `changedFrom`, which would be a new
  finding rather than a repeat.
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

  **Update 2026-08-06, a second live collision, this time with real (if
  circumstantial) evidence of what it costs.** Two Claude Code sessions were
  confirmed active in this exact checkout at the same time — this session's
  own autopilot run (Sonnet) working `docs/sync-speed-model.md` Question 22,
  and a second one (Opus, per its commit's co-author line) working the
  RSS/notification-signal thread in this file's own Noticed section. Evidence:
  commits `5c8956a`/`5ebfacd`/`b3d3e3d` landed directly on top of this
  session's own commit while it was still mid-run, and a 4-run real-account
  verification batch (`tmp/q22-fix-verify-run.log`) had its 1st run behave
  normally, then all 3 remaining runs report **0 files for both courses**
  with no error — a total, silent collapse right after the other session's
  commits landed, not the partial/intermittent loss this project's actual bugs
  produce. No `ErrProfileLocked` surfaced here either, same as the 2026-08-02
  incident. This is circumstantial, not proven (no direct log correlation
  attempted this session, and the crawl.go fix under test at the time is an
  alternative explanation not yet fully ruled out — though it cannot explain
  a *total* loss on both courses, only a partial one on the specific section
  it touches). **Consequence: Question 25 (`docs/sync-speed-model.md`,
  "Next experiment") — re-arming the Wicket watch on the context-destroyed
  reclick — is next but explicitly deferred until the profile is confirmed
  quiet.** Concrete check before running it: `tasklist` shows no `chrome.exe`,
  and `git log -3` shows no commit from another session in the last few
  minutes — cheap, and this session should have done it before the
  verification batch above, not just after.

  **Update 2026-08-06 (later the same day): the concrete check worked, no
  second collision this run.** Confirmed quiet before touching anything —
  `tasklist` showed no `chrome.exe`, `git log -3` showed nothing from another
  session in the last 16 minutes, tree clean — then ran Question 25's 4-run
  verification batch without incident: no `ErrProfileLocked`, no total-loss
  collapse, all 4 runs reported real counts. Not proof the lock bug itself is
  fixed (still open, options unchanged above), just confirmation that the
  cheap check is enough to avoid re-triggering it.

  **Update 2026-08-07 (autopilot, pure source reading, no live run): two of
  the three root-cause candidates from 2026-08-02 no longer fit the current
  code, and there is a better-supported fourth.** Re-read `ensureSession`,
  `acquireSessionLock`, `sessionMutexName` and their own regression tests
  (`session_lock_windows_test.go`, added since 2026-08-02) against the two
  original candidates:
  - **(b) mutex names silently not matching is refuted for this project's
    real config**, not just untested. `sessionMutexName` hashes
    `profileDir+stateFile`; `LoginProfileDir()` is a hardcoded
    `~/.opal-downloader/login-profile` (no per-config variation at all) and
    `config.yaml`'s `session_state_file: ~/.opal_storage_state.json` goes
    through `expandHome`, which resolves via `os.UserHomeDir()` to the same
    absolute path regardless of the calling process's cwd. Both existing
    tests (`TestAcquireSessionLockSerializesAcrossProcesses`,
    `TestAcquireSessionLockDoesNotSerializeDifferentProfiles`) also already
    prove the mutex itself serializes correctly once the strings do match. No
    plausible path-representation mismatch exists between any two
    opal-downloader processes on this machine today.
  - **(c) Chromium's `ProcessSingleton` racing ahead of the Go-level lock is
    refuted by control flow, not just untested.** `acquireSessionLock` is the
    *first* thing `ensureSession` does (`session.go:306`), strictly before
    `launchBrowser` or any Playwright/Chromium call. Nothing in the current
    interactive-login path can reach Chromium before the Go mutex has already
    been acquired.
  - **Neither incident is explained by the locked phase at all, and that is
    itself the clue.** Both 2026-08-02 and 2026-08-06 evidence shows *no*
    `ErrProfileLocked` and (2026-08-06) a *silent* collapse to 0 files that
    persisted across three subsequent runs, not a slow-but-eventually-correct
    serialization. `acquireSessionLock` only guards session *establishment*;
    once a process's saved session is already valid, it sails through the
    lock in milliseconds (`isAuthenticated()` true, no interactive-login
    branch) and enters the crawl - the phase `session_lock_windows.go`'s own
    doc comment calls safe to run "fully in parallel" because each process
    gets its own local browser context. **That claim is only true locally.**
    Every process's context is seeded from the identical
    `storage-state.json` cookie file (`ctxOpts.StorageStatePath`,
    `session.go:155`) - i.e. two or more local Chromium processes all
    presenting the *same* authenticated OPAL session identity to the server
    at once. OPAL's Wicket framework is stateful server-side per session
    (this project's own campaign has repeatedly hit Wicket AJAX
    call/DOM-completion races within a *single* crawl - see
    `docs/sync-speed-model.md` Questions 17/19/22/25); two crawls truly
    interleaving requests under one server-side session identity is a
    plausible way to get exactly the symptom seen: silent, total, and
    persisting past the collision window, because whatever broke is server-
    or session-side, not something a locally-restarted process would ever
    self-heal from mid-run.
  - **New candidate (D), ranked above the old options because it is the only
    one still consistent with both incidents' evidence:** OPAL's server-side
    session cannot safely be driven by two concurrent local browser
    processes at once, even though each has its own local Chromium context.
    The forensic log named in the original 2026-08-06 note
    (`tmp/q22-fix-verify-run.log`) no longer contains the 4-run batch it
    described - the filename was reused by a later, unrelated run the same
    day - so this is not re-confirmed against that specific evidence, only
    reasoned from the current source and the surviving written description.
  - **Cheap next step, no live run needed:** grep future collision incidents'
    logs for the responding page's actual HTML/URL at the moment a run
    returns 0 files (a redirect to a generic OPAL error/interstitial page
    would confirm server-side session interference; a normal-looking empty
    course page would point elsewhere). None of today's evidence captured
    that, because nobody was looking for it at the time.

  **Update 2026-08-10 (source reading, no live run): guarded, and a second
  mechanism found that explains the incident candidate D could not.** The
  collision is now blocked at a level above both locks, and the reason no
  amount of work on `acquireSessionLock` would have caught either incident is
  clear: **it was never the lock that could.** It covers session
  *establishment*; both incidents happened while at least one process was
  past it.
  - **Verified gap, the actual bug:** `list` did a full crawl and took **no
    overlap lock at all**, while `sync` has held `~/.opal-downloader/sync.lock`
    for its entire duration since PR #82. So a scheduled sync and a manual
    `list` were free to crawl concurrently, as were two probe runs — and the
    campaign's probe tests took no lock either, which is what actually
    collided both times.
  - **Candidate (E), new, and it fits 2026-08-02 where D fits 2026-08-06.**
    `shouldRelaunchHeadlessAfterInteractiveLogin` deliberately keeps the
    *visible persistent-context browser on the shared login profile* open
    past ensureSession's return for `login` (forceInteractive) and for
    `--dev` — i.e. after the session mutex has been released. A second
    process entering interactive login in that window has only
    `isUserDataDirLocked` (`profile.go`) between it and a bare
    `LaunchPersistentContext` against a profile Chromium already holds, and
    that check is explicitly best-effort ("not conclusive proof of a lock",
    its own comment). Its failure mode is a raw Playwright launch timeout,
    not `ErrProfileLocked` — which is exactly the 180000ms error 2026-08-02
    reported, and the campaign's own probe commands are documented as
    `list --dev --profile --debug-clicks`.
  - **Fix shipped (2026-08-10):** `list` and `login` now take the same
    `sync.lock` for their whole duration, and every live probe in
    `internal/scraper` takes it too — `captureProbeLogs` was renamed
    `beginLiveProbe` and does it centrally, so a new probe cannot be written
    without the guard. A second crawl gets `a sync is already running (PID …,
    started at …)` and exit code 4 in seconds. Tests:
    `cmd/opal-downloader/listoverlap_test.go` (refusal + correct exit code,
    and that `--visit-report` stays lock-free since it is offline);
    `internal/scraper/probeoverlap_test.go`.
  - **What is still not proven:** candidate D itself. The fix prevents the
    collision rather than demonstrating the server-side mechanism, and the
    "grep the responding page at the moment a run returns 0 files" step above
    is still the way to confirm it — but it now needs a *deliberate*
    collision to study, which is no longer something an accident will hand
    us. Given the guard, that is the right trade: leave D as
    well-supported-not-proven rather than re-triggering a bug that silently
    loses files. `session_lock_windows.go`'s doc comment carries the
    correction so nobody re-derives "safe to run fully in parallel" from it.

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

- **TU-Fast's missing fast reload traced to a real gap and fixed: a login page
  with no fields could never be called stalled** (2026-08-10, autopilot,
  option (a) as recommended): `loginSignals.stalled()` required
  `FieldCount > 0`, so the Shibboleth processing screen
  (`.../opal/shiblogin;jsessionid=...`) — an interstitial, not a form — was
  unreachable by the condition and waited out the full 300s budget. That is
  the shape of all three unexplained 300s login timeouts. The condition is now
  "on a login URL with nothing entered"; the guard protecting a human's typing
  is unchanged, because `FilledFields` is 0 whenever `FieldCount` is. The old
  test asserted the buggy behaviour in words and is reversed with the
  reasoning kept. **Verified live**: an interactive `login` completed
  unattended afterwards, and a full `list` returned the usual 349 files. Not
  verified against a real stall — it has never been reproduced on demand, so
  what a live run can show is that normal login is unaffected.
- **`netcheck.Check`'s two steps no longer share one deadline — and the fix is
  the opposite of the one that was suggested** (2026-08-10, autopilot): lookup
  and dial each get their own full `DefaultTimeout` instead of splitting it.
  The review proposed halving the shared budget; that was tried first and
  makes the reported symptom *worse*, because a slow-but-working DNS lookup
  then fails at 3s instead of 6s. Kept as a measured fact rather than an
  argument — `TestCheckGivesTheDialItsOwnBudgetAfterASlowLookup` fails under
  the split and passes under the full budget. Cost: a comprehensively broken
  network takes up to 12s to say so instead of 6s.
- **Both weekly-review bugs in the offline/dismiss set fixed** (2026-08-10,
  autopilot): the retry banner's reassurance sentence was appended *after*
  netcheck's `(technical detail: ` marker, and `bannerChrome` folds everything
  past that marker away — so the one line the feature existed to show was
  never visible. Fixed with `netcheck.AppendSentence`, which extends the
  sentence rather than the string and keeps the classification and cause for
  `errors.Is`; the regression test asserts the ordering by index, so the old
  `fmt.Errorf` form fails it. And Dismiss now sends the timestamp the page
  rendered, with the handler only writing the dismissal when it still matches
  — a run that landed while the banner was open can no longer be dismissed
  unseen. Verified live in the GUI against a real failed run, with the POST
  intercepted so the maintainer's actual notification was not consumed.
- **The browser turned out to be unnecessary for discovery: the whole course
  tree is already in the first response, and HTTP-first finds all 286 sections
  in 41% of the crawl's wall clock** (2026-08-10, autopilot, 3 live runs):
  Question 34 asked whether the served HTML conceals structure the crawl
  navigates for, and pre-registered "probably not" as its prediction. Wrong —
  every course page carries `var initial_data=[...]`, jstree's complete
  server-emitted course-node tree, which `isRenderChildren()` scopes only in
  the *rendered DOM*. 261 of 261 visited URLs across 6 courses from 6 requests
  (Steps A/A2), then 286 of 286 sections with 0 missing once expansion follows
  the pagination toggle (Step B1). New mechanism recorded: rows past a
  section's ~20-row cap include sub-sections, so pagination is a discovery
  boundary. Question 38 opened (fetch latency measured 208–228ms here vs 315ms
  on 2026-07-31; every floor projection uses that constant). `CLAUDE.md` gained
  the maintainer's note that needing a live crawl is never a reason to defer.
- **Four maintainer reports from 2026-08-10 fixed in one pass: an offline
  machine now says so, a scheduled run waits for the connection instead of
  losing the day, Dismiss stays dismissed, and the browser/Chromium jargon is
  gone from Settings and TU-Fast setup** (2026-08-10): new `internal/netcheck`
  classifies "you are offline" apart from "OPAL is down" and is checked once
  at the top of `ensureSession`, so every entry point (CLI, GUI, scheduled)
  fails in ~0.1s with a plain sentence instead of a raw Playwright
  `net::ERR_NAME_NOT_RESOLVED` dump; `sync --scheduled` retries over ~15
  minutes first and exits 5 if still offline. The banner's dismissal moved
  from the browser's localStorage to a file, because the GUI binds a fresh
  ephemeral port every launch and localStorage is per-origin — that, not the
  banner logic, was the whole "dismiss and it's back next time" bug.
- **The two-collision session-lock bug is guarded, and the reason no work on
  `acquireSessionLock` would ever have caught it is now written down**
  (2026-08-10, source reading, no live run): `list` did a full crawl while
  taking no overlap lock at all — `sync` has held `sync.lock` for its whole
  duration since PR #82 — and the campaign's own probe tests took none
  either, which is what actually collided both times. `list`, `login` and
  every live probe now hold that same lock; a second crawl gets
  `a sync is already running (PID …)` and exit code 4 in seconds instead of
  interleaving. Found candidate (E) on the way, which explains 2026-08-02's
  raw Playwright launch timeout where candidate (D) only explains
  2026-08-06's silent 0-file collapse: `login` and `--dev` keep the visible
  persistent context on the shared login profile open *past* the session
  mutex's release, guarded only by the explicitly-inconclusive
  `isUserDataDirLocked`. Full write-up in this file's Noticed section; the
  concurrency model is now a table in `docs/OPERATIONS.md`.
- **`course_concurrency=2` shipped as the new default, together with the
  concurrent settle debounce at 150ms** (2026-08-10, decision round): the
  maintainer chose to ship immediately rather than wait for the different-day
  confirmation run the recommendation asked for — the three clean 349-file runs
  are all from 2026-08-10, which is the residual risk.
  `DefaultCourseConcurrency` (`internal/config/config.go`) and
  `mutationObserverConcurrentDebounceMs` (`internal/scraper/navigation.go`) are
  one change and revert together. Also fixed `config.example.yaml`, which had
  been claiming a default of 2 on 2026-07-21's since-overturned reasoning the
  entire time the code default was actually 1.
- **Question 29 closed, no live run needed: the crawl's own `visited`/`queued`
  dedup makes a node re-fetch structurally impossible** (2026-08-10,
  autopilot, source reading): `appendSectionFolderTargets` checks both maps
  before ever queuing a child (`crawl.go:1328-1333`), so a node's `sectionKey`
  can enter the BFS at most once per course crawl. Residual, not chased: this
  assumes `sectionKey` normalizes every real URL variant for the same node,
  untested beyond the campaign's byte-diffs never showing a duplicate-content
  symptom. Full write-up in `docs/sync-speed-model.md` Question 29.
- **Questions 32/33 closed live at full 6-course scale: `course_concurrency=2`
  + debounce=150 loses 6 files with the existing override (hard cap
  unintentionally tightened too), but a new decoupled override
  (`OPAL_DEBOUNCE_MS_KEEPCAP_OVERRIDE`) passes the full byte-diff twice and
  cuts wall-clock ~36%** (2026-08-10, autopilot, 4 live runs): first
  `course_concurrency>1` config in the campaign to pass correctness *and*
  show a real speed win — now a maintainer decision, see "Now" above. Full
  write-up in `docs/sync-speed-model.md` Questions 32/33.
- **Question 31 closed at full 6-course/349-file scale: `course_concurrency>1`'s
  correctness objection is refuted (empty byte-diff both sides), but
  concurrency alone is not faster (17% slower in this run) — added
  `OPAL_COURSE_CONCURRENCY_OVERRIDE` to `filelist_probe_test.go` to test this
  without ever touching the maintainer's real `config.yaml`** (2026-08-09,
  autopilot, live run): this cycle's earlier 2-course probe found an ~85%
  wall-clock win, but that came from pairing concurrency with the 150ms
  debounce override, not from concurrency by itself — matches this project's
  own earlier finding (`course_concurrency=2` "lost 9 files and was no
  faster", 2026-07-26) on the speed half, while overturning it on the
  correctness half. Opened Question 32 (does the concurrency+debounce pairing
  replicate the win at full scale) as the campaign's current top-ranked live
  experiment. Full write-up in `docs/sync-speed-model.md` Question 31/32.
- **Question 24 closed live, same day the "one live-run batch per day"
  self-caution was retired: 6 trials (3 blocking-on, 3 blocking-off) of the
  one known-flaky section, 0 truncated in any of them** (2026-08-09,
  autopilot): the original prediction borrowed the wrong reference failure
  rate (Question 17/20/21's ~33–50% is a *contention*-condition number; this
  probe deliberately runs single-threaded) — corrected and closed per Rule 2
  rather than just reported as a miss. Real finding along the way: `go test`
  silently cached and replayed one trial instead of re-running it (identical
  env vars, no `-count=1`) — confirmed by a byte-identical log; every
  subsequent run forced `-count=1`. That hazard cannot be retroactively
  ruled out for Questions 20/21's older "N clean runs in a row" batches
  (their raw logs no longer exist), recorded as an open caveat, not a
  reopening. Opened Question 31: does the Question 25 fix also survive
  `course_concurrency>1` contention, potentially reopening a previously
  rejected speed lever. Full write-up in `docs/sync-speed-model.md`
  Question 24/31.
- **Question 30 opened and mostly closed same-cycle, no live run: OpenOLAT's
  folder browser does offer a participant-reachable bulk-ZIP-download of a whole
  `Ordner` subtree, but this project's own metadata parsing (`files.go`) only
  ever reads size/modified date off the currently-rendered page, so nested-folder
  discovery still needs one page load per level either way** (2026-08-09,
  autopilot, source reading of both OpenOLAT's and this project's own code, no
  live run): the lever is real but bounded to the ~86s first-sync download floor
  `docs/server-load.md` already named, not the 207s crawl floor. Ranked behind
  Question 24 (correctness first, per the standing rule). Full write-up and
  source citations in `docs/sync-speed-model.md` Question 30; fifth-cycle
  report appended there per the reporting cadence.
- **Question 6 closed, no live run needed: the "1 in 12 sections unstable" premise
  was already retracted by the campaign's own next entry, three days before this
  question was carried into `docs/sync-speed-model.md`** (2026-08-09, autopilot):
  the number came from fetching 12 section URLs twice back-to-back, but
  2026-07-30's own re-measurement of the condition that actually matters (a
  stored crawl-hash vs. a later crawl) found only 1/276 matching, and said
  plainly that the back-to-back condition "says nothing about the condition the
  feature runs in." The consequential half of "instability" (real files missing)
  is already a separate, tracked mechanism (Questions 17/19/22/25's Wicket
  "show all" bug); the feature this question was diagnostic for
  (`internal/sectionhash`) was deleted 2026-07-31, so nothing survives to test.
- **Question 2 closed, no live run needed: the abandoned HTTP-first crawler never
  reached the empty courses' file sections, it wasn't about client-side rendering**
  (2026-08-09, autopilot): re-connected Question 1, Question 9's
  `MenuTreeRenderer.isRenderChildren()` finding, and `httpdiscovery.go`'s own design
  comment — a course's tree is only ever revealed one navigation per newly-opened
  branch, never whole in one response, so an implementation that skipped the browser's
  tree walk (22s, in the same range as a bare per-section HTTP fetch with zero tree
  navigation) could only ever see default-open branches. Predicts exactly the two
  courses that came back empty/near-empty (TUDMATH NuMa, Softwaretechnologie) against
  the three that came back perfect. Corroborated, not just inferred: the only
  HTTP-discovery code ever committed is built to always take URLs from the browser's
  own walk, and was verified byte-for-byte correct on all 6 real courses 2026-07-31.
  Opened Question 29 (real-account load, waits with Question 24 for a fresh day) —
  full write-up in `docs/sync-speed-model.md`.
- **Question 28 closed: `go test`'s own build/cache-staleness check, not raw
  compilation or process spin-up, is the noise source behind Question 27's
  unaccounted 6s** (2026-08-09, autopilot, local only, no account): precompiled
  binary showed near-zero variance (refuting the literal prediction), but the
  `go test` wrapper itself showed a 3.6s gap between a cache-cold and a cached
  invocation of the identical test. Sharper mechanism than originally guessed;
  full write-up in `docs/sync-speed-model.md`.
- **Question 27 confirmed: warm-session delta is 4.03% total, but only 1.14%
  lives inside the crawl itself — the rest is `go test` overhead, opening
  Question 28** (2026-08-09, autopilot): second clean full-account pass (349
  files both sides, empty diff, no truncation). Full decomposition and the
  incidental third occurrence of the Shibboleth login timeout (this time
  showing where it got stuck) in `docs/sync-speed-model.md` and this file's
  login-timeout Noticed entry.
- **Question 26 confirmed live, zero-diff, and preview-blocking shipped as the
  default** (2026-08-07, autopilot): full real-account before/after
  (`filelist_probe_test.go`) found 349 files both ways, empty diff, no
  truncation anywhere including the section that lost 33 files on Question
  23's first attempt — closing the causal chain Questions 17→25→26 have
  chased since 2026-08-03. `attachInlinePreviewBlocker` (`previews.go`)
  flipped from opt-in (`OPAL_BLOCK_FILE_PREVIEWS=1` to enable) to opt-out
  (`OPAL_BLOCK_FILE_PREVIEWS=0` to disable), per the standing shipping rule
  below. Build, vet, and the full non-account test suite pass. Opened Question
  27 (the 6.8% timing delta measured is confounded by a fresh-login baseline,
  needs a warm-session rerun) and re-ranked Question 24 up (the residual risk
  to Question 17's still-unfixed Candidate-B bug now applies to every user,
  not just a retest account) — full write-up in `docs/sync-speed-model.md`.
