# Backlog archive

Everything that has left `docs/BACKLOG.md`, in two sections: **Settled** —
questions answered, options ruled out, incidents diagnosed — and **Done
recently**, closed work newest first.

Split out on 2026-07-31, when the live backlog had grown to 1057 lines of which
678 were closed work and the file that answers "what should I do next?" had
become unreadable in one pass; widened on 2026-08-12 to take the Noticed
section too, on the rule that `docs/BACKLOG.md` now holds open work only.

Nothing here is actionable. Git history and PR bodies remain the real record;
this is the human-readable version of why things are the way they are.

---

## Settled — investigated, decided, closed

Questions that were answered, options that were ruled out, and incidents that
were diagnosed and fixed. Kept so nobody re-runs the search or re-proposes the
option; a few carry a "if this recurs, check X" note. Nothing here is work.
Moved out of `docs/BACKLOG.md`'s "Noticed" section on 2026-08-12, when that
file was cut back to open work only.

- **`TestSyncScheduledSkipsWhenAlreadySucceededToday`
  (`cmd/opal-downloader/root_test.go`) non-hermeticity, three mechanisms
  tried and ruled out (2026-08-19, autopilot) — likely a misattributed real
  session, not a test bug.** Original report (2026-08-19, while verifying
  the last-sync.json fix): with a stray concurrent process already holding
  the real `~/.opal-downloader/sync.lock`, this test — which fakes
  `readScheduledStatusForDedup` to report "already succeeded today"
  specifically so it should return `errAlreadySucceededToday` before
  touching the network — instead ran a full ~166s discovery/download pass
  against the real account (8 courses, 349 files, real TU-Fast login),
  landing only in a scratch temp `download_path`. Reproduced on unmodified
  `master`; passed cleanly in isolation.
  **Pass 1 — same-test contention, ruled out:** read `alreadySucceededToday`
  (`cmd/opal-downloader/root.go`) — it runs before `config.Load`, long
  before `sync.lock` is touched (that happens later, inside
  `internal/syncer`), and decides purely from the in-process
  `readScheduledStatusForDedup` fake, with no code path to `sync.lock` at
  all. Ran two genuinely simultaneous
  `go test -run TestSyncScheduledSkipsWhenAlreadySucceededToday` processes
  against each other: both passed in 0.00s/0.01s.
  **Pass 2 — other-package live probes, ruled out:** checked all 24
  `internal/scraper/*_probe_test.go` files (grepped every `t.Skip(` /
  `os.Getenv("OPAL` call) — every one requires its own explicit `OPAL_*` env
  var before touching the real account, unset by default (confirmed the
  shell had none set). A bare `go test ./...` — the original incident's
  invocation — cannot make any of them run; `.github/workflows/ci.yml` relies
  on exactly this to run `go test ./...` on GitHub Actions with no OPAL
  credentials and stay green.
  **Pass 3 — real sync.lock contention, ruled out, live-verified:** built the
  worktree's own `main.exe` (from repo root — `cmd/opal-downloader` is
  `package opaldownloader`, not `main`; the real entry point is the
  repo-root `main.go`), started a real background `sync` against a scratch
  config (`tmp/friction/scratch-download`, gitignored), confirmed via
  `~/.opal-downloader/sync.lock` that it was genuinely running (mid-crawl,
  courses being scanned), then ran the dedup test while that lock was held.
  `TestSyncScheduledSkipsWhenAlreadySucceededToday` passed in 0.01s, network
  untouched, while the real sync kept crawling underneath it.
  **Conclusion:** all three candidate mechanisms for "contention makes the
  guard fall through" are now individually disproven. The better-fitting
  explanation is that the original run's ~166s crawl was a genuine, separate
  concurrent `sync` (matching `scheduled-run-history.jsonl`'s real PID
  collisions logged in almost exactly that window, 2026-08-18T21:24 to
  2026-08-19T00:24 local — PIDs 15412 and 36856) whose output or timing got
  misattributed to the test run rather than caused by it. Not proven (no PID
  was captured for the process actually inside the original run), so kept as
  a documentation note rather than fully closed — but nothing in the test's
  own guard logic needs fixing unless it recurs with a captured PID.
- **Walk 11's `list --visit-report` umlaut-mojibake finding fixed 2026-08-19
  (autopilot, phase 1).** `internal/visitlog/visitlog.go`'s `truncate`
  sliced `s[:max-1]` on the raw byte string; any 2-byte UTF-8 character
  (every German umlaut/ß) whose second byte landed past the cutoff produced
  invalid UTF-8, rendered as `�`. Fixed by slicing `[]rune(s)` instead of the
  byte string; regression tests added (`TestTruncateSplitsOnRunesNotBytes`,
  `TestFormatReportTruncatesLongUmlautNameWithoutMojibake`) using the exact
  reported course name. **Left open, not closed by this fix:** whether the
  GUI has its own separate name-truncation logic that makes the same
  mistake — not checkable from an unattended session (no browser tool
  reachable here); worth a GUI walk picking this up specifically.
- **Walk 9's Softwaretechnologie course-discovery dropout question closed
  2026-08-19 (autopilot, phase 1) — the rested-session repro it asked for
  came back clean, no dropout.** With `sync.lock` free and no other
  opal-downloader activity that hour, a single live `smoke-check` found all
  8 courses including `Softwaretechnologie (SoSe 26): 210 files (41.1s)` -
  its full known count, no 0-file result, no absence from the output.
  Confirms walk 9's own caveat was the likely explanation all along: that
  session's dropout came from unusually heavy self-inflicted load (two full
  syncs plus a smoke-check inside an hour, one mid-run TU-Fast relogin), not
  a standing discovery bug in the same family as Question 44. One clean run
  doesn't prove "never happens under load," only "doesn't happen normally" -
  but there is no positive evidence left pointing at a real mechanism, and
  nothing further is planned unless it recurs. Full detail:
  `docs/friction-campaign.md` Walk 9's closing note.
- **`sync.lock` held 5.5+ hours by PID 14804 (worktree
  `suspicious-pare-359a30`) on 2026-08-13 — closed, self-resolved, root cause
  of the *duration* unconfirmed and not further pursued.** `internal/synclock`
  worked as designed throughout: the PID was genuinely alive the whole time
  (never a stale holder to reclaim), and the lock released on its own once
  that run finished — confirmed cleared by 2026-08-14 (`sync.lock` absent).
  What was never confirmed: why that particular run took 5.5+ hours for what
  this file's own Next section frames as a short live step. No trail survived
  to answer it — `suspicious-pare-359a30`'s `docs/RESUME.md` is still the
  placeholder, its last commit (`4980552`, 2026-08-12, already merged to
  master) predates the incident, and no diagnostic log from that run was
  found in the worktree. Treated as a dead end rather than reopened: nothing
  left to read. If a run wedges the lock for hours again, check
  `Get-Process` on the holder PID immediately (don't wait — this is what
  would have caught it live) rather than reasoning about it after the fact.
- **Walk 1's questions 3 and 4 both closed 2026-08-13, neither needing a code
  change.** Question 4 (do the three `download_path` slash conventions -
  forward-slash absolute, backslash absolute, relative - behave identically,
  and does that interact with the known `default_course_folder` doubled-path
  bug?): walk 4 had already spot-checked backslash absolute; four new
  real-filesystem tests close the remaining cases
  (`TestSyncCoursesForwardSlashAbsoluteDownloadPathBehavesLikeBackslash`,
  `TestSyncCoursesRelativeDownloadPathResolvesAgainstCWD`,
  `TestSyncCoursesForwardSlashAbsoluteDefaultCourseFolderLandsOnDisk`, all in
  `internal/syncer/syncer_test.go`) - all pass, no doubling, no divergent
  behavior. `handleSettings` (`internal/gui/settings.go`) does no path
  normalization on save at all (whatever string is typed is written to
  config.yaml verbatim), so this was really a question about
  `filepath.Join`/`os.MkdirAll` downstream: they already treat forward and
  backslash identically on Windows, and a relative `download_path` resolves
  against the process's current working directory at invocation time (worth
  knowing, not itself a bug - CLI, GUI, and the scheduled task can each have a
  different CWD). Question 3 (is the 08:00 default schedule time actively
  hostile to Finding 1's logged-off failure?): moot rather than answered -
  the code's actual default is `06:00` (`internal/scheduler/scheduler.go`,
  `DefaultTime`; the maintainer's real config's `08:00` was a hand-set value,
  not what a fresh install gets), and more importantly Finding 1's own
  recommended repair (an on-logon catch-up trigger) already shipped
  2026-08-11 (see this file's own entry below) - it catches the
  not-yet-logged-on case regardless of what hour the daily trigger fires at,
  which is a strictly better fix than moving the default later (that would
  only trade one exposure window for the visible-browser-flash problem
  `DefaultTime`'s own doc comment picked early morning to avoid).
- **What a `sync` does with a `download_path` that goes bad *mid-run* (walk
  1's follow-up) — closed 2026-08-13, neither "fails clearly" nor "appears to
  succeed".** `status` already catches a path broken before a sync starts;
  this answered the other half - a path (or one course's subfolder) that goes
  bad *after* the sync has already begun, e.g. a removable drive unmounted or
  a OneDrive folder renamed mid-run. Confirmed with a real-filesystem test
  (`TestSyncCoursesWithProgress_DownloadPathGoesUnwritableMidSync`,
  `internal/syncer/syncer_test.go`) rather than injected fake errors: after
  the path breaks, every remaining file is retried individually and fails the
  same way - no hang, no crash, no early abort - and each is counted into
  `stats.Errors` and reported via `printSyncError`/`EventError`.
  `SyncCoursesWithProgress`'s own return is `nil` regardless - it never
  treated per-file failures as fatal, mid-sync-path-death included. That nil
  is not silence, though: the last-sync status file is written
  unconditionally for *every* run (scheduled or not - see `runSync`'s own
  comment), so a plain interactive `sync` gets a "Synced with N file
  error(s)" record in `status`/the GUI the same as a scheduled one does,
  classified `OutcomePartial`. The one real gap: the plain interactive `sync`
  command's own **process exit code** stays 0 even when every remaining file
  failed - `--full-sync` already returns an error (and a non-zero exit) when
  `stats.Errors > 0`, `sync` does not. Deliberately not changed to match: the
  same `err` also drives `sync --scheduled`'s status classification, and its
  own comment explains partial file errors are treated as routine by design
  (no failure toast, to avoid notification fatigue on a flaky download) - a
  change that made `err` non-nil on `stats.Errors > 0` would flip the
  scheduled classification from `OutcomePartial` to `OutcomeFailure` and
  start toasting on routine hiccups, which is what the maintainer explicitly
  didn't want. If this needs revisiting, it is a request to *decide*
  (accept a script that checks `sync`'s exit code seeing 0 on 100% file
  failure, or add a second signal decoupled from the scheduled classification)
  rather than a bug to silently fix.
- **The GUI process's ~5-minute exit was an artefact of how agents launched
  it, not a bug users can hit — closed 2026-08-12 by the one test no agent
  could run.** Walk 1 saw the GUI process die ~5 minutes after launch; across
  walks 1, 4 and 5 that became 2 deaths and 1 survival over three
  background-shell launches, plus a clean survival under a properly detached
  launch. Never reliable enough to name a cause, and every data point came
  from a process an agent had started from a shell it also owned. The
  maintainer settled it by double-clicking the GUI like a user (PID 20576,
  15:59:30) while a watcher polled it: **it survived the full 25-minute
  window and was still running at 26 minutes**, five times past the failure
  window. Both outcomes had been written into the backlog entry *before* the
  result came back, so this closed on a rule rather than on a judgement call
  made after seeing the answer.
  **What stays:** automated GUI walks default to `Start-Process` or
  equivalent full detachment — still the only launch method with a perfect
  record, and correct regardless of the root cause. No code change; nothing
  in the app was ever wrong. **If it recurs:** it will be under an
  agent-started launch, and the thing to examine is the parent shell's
  lifetime, not the GUI.

- **What a sync does with an unwritable `download_path`: fails clearly, both
  at the top and per file — investigated 2026-08-12, walk 1 follow-up now
  closed.** `SyncCoursesWithProgress` (`internal/syncer/syncer.go:431`) calls
  a fresh `os.MkdirAll(cfg.DownloadPath, 0o755)` right after acquiring the
  sync lock, on every run — it never trusts an earlier `status` check, so the
  "goes bad between the check and the sync" race collapses to "the same check,
  run again, immediately before the same work." Live-verified: pointed a
  scratch config's `download_path` at a location blocked by a file instead of
  a directory and ran `sync` — clean single `Error: mkdir ...` to stderr, exit
  1, before any login or network activity (`tmp/friction/` scratch env, no
  files of the maintainer's touched). For a path that goes bad *mid-run*
  instead (root deleted after the top-level check passed), each file's own
  `os.MkdirAll(filepath.Dir(localPath), 0o755)` (`syncer.go:593`) fails on its
  own turn, increments `stats.Errors`, emits `EventError`, and is never
  queued as a job — confirmed by source reading, all three write call sites
  (`internal/scraper/download.go:76,162,222`) propagate `os.WriteFile`/
  `SaveAs` errors rather than swallowing them, and the result loop
  (`syncer.go:660-665`) only logs `downloaded:` and marks the manifest on
  `err == nil`. Surfaced at the top as `Done. ... errors=N` and
  `statuslog.OutcomePartial`, visible in both the CLI and the GUI's status
  read. The one real gap is cosmetic, not silent: a vanished root reports as
  N individual file errors rather than one "download_path disappeared"
  message — not worth chasing, since the underlying failure is never hidden
  or mislabeled a success.

- **winget distribution: investigated 2026-08-11, decided against, do not
  re-propose as a free SmartScreen workaround.** It looks like one — publish a
  manifest to `microsoft/winget-pkgs`, users run `winget install`, no
  shell-launched `.exe` carrying a Mark-of-the-Web. It isn't. SmartScreen still
  blocks an unsigned installer under winget, and winget reports the install as
  *successful* while nothing happens
  ([vim/vim-win32-installer#319](https://github.com/vim/vim-win32-installer/issues/319)),
  which is strictly worse than a blue screen the user can click through. The
  `winget-pkgs` validation pipeline also runs SmartScreen reputation checks on
  submitted URLs and sandbox-installs under Defender, which a zero-reputation
  240MB unsigned installer would not survive. Full writeup in
  `docs/installer-plan.md` Section 6. **Revisit only after signing exists** —
  it is then a small follow-up, never a substitute.

- **Code signing has one plausible route and it is currently closed to us.**
  Also 2026-08-11: EV certificates no longer grant instant SmartScreen
  reputation (Microsoft's own docs now say so outright), so no certificate at
  any price removes the warning on a fresh file. Azure Artifact Signing
  (~$10/month, no hardware token, signs from GitHub Actions) is the sane option
  and would at least let reputation carry across releases — but individual
  sign-up is USA/Canada only, and the organization path needs a business
  entity. Worth re-checking once a year, or if the project ever gets an entity
  behind it. `docs/installer-plan.md` Section 6 has the comparison.

- **Two sessions independently built the same backlog item, five hours
  apart, and neither noticed the other's PR (2026-08-11 investigation).**
  Question 36 Step B2 was handed off as open work in commit `9ceb88b` ("Five-
  cycle report, backlog handoff to Step B2"). Both PR #133
  (`restructure-hybrid-http-first-discovery`, first commit 13:13) and PR #134
  (`http-first-discovery-b2`, first commit 18:08) branched from the exact
  same master commit (`865820c`) and built the same algorithm from scratch.
  This was not the git-collision hazard already tracked above (no
  simultaneous `chrome.exe`/session-lock contention — the two sessions never
  ran at the same time) — it was a **visibility gap**: PR #133 finished at
  16:22 and its last commit rewrote `docs/BACKLOG.md`'s Now section to point
  at itself, but that edit lived only on its own branch. Master's own
  `docs/BACKLOG.md` still read "Step B2 open work" the whole time, because
  nobody had merged or even referenced the PR back into master yet. A fresh
  session at 18:08 read master's backlog (accurately, as far as it could
  tell), saw open work, and redid it — a `gh pr list` before starting would
  have caught it, but nothing in this project's workflow prompts a fresh
  session to check for in-flight PRs on the item it's about to start, only
  for in-flight *branches/processes* (the existing collision-hazard entry
  above). Then a later session, editing master directly at 20:22, picked
  #134 as the Now item's PR without ever checking whether an older #133
  covering the same ground existed. Consequence: ~5 hours of duplicate work,
  caught only because both PRs happened to sit open at once when this
  decision round ran. **Resolved 2026-08-11:** #133 merged (it had already
  found and fixed a real bug — see `docs/sync-speed-model.md` Question 36
  Step B2 — that #134 still carried, unfixed, un-triggered), #134 closed as
  superseded. Not fixed at the process level: a PR-open-work check before
  starting a backlog item that's already "Now" would close this gap, but
  that is itself the "another gate" failure mode `CLAUDE.md`'s global
  instructions warn against building reflexively — recorded here as a known
  gap instead, to weigh against if it recurs.

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
  RSS/notification-signal thread now in this file's Settled section. Evidence:
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

## Done recently

Newest first. Trimmed periodically — git history and PR bodies are the real
record.

- **`smoke-check` and `dump-links` now take `sync.lock` before touching the
  account, closing walk 10's finding (2026-08-19, autopilot).** Both called
  `scraper.New` with no overlap guard at all, so either could run a real
  crawl concurrently with a real `sync`/`list`/`login` — the exact condition
  behind the 2026-08-02 and 2026-08-06 collision incidents
  (`docs/BACKLOG-archive.md`). Fixed exactly as the finding named it: both
  now call `acquireCrawlOverlapLock`/`defer releaseOverlap()`, the same
  proven pattern `runList`/`runLogin` already used — no new mechanism.
  `smoke-check`'s lock is acquired *before* `scraper.EnsureTUFastPresent`,
  ahead of every other check, so a concurrent run is refused as cheaply as
  possible and the new test doesn't depend on the test machine having
  TU-Fast set up. Treated the finding's own open question (deliberate
  omission vs. oversight) as a judgment call rather than a maintainer
  block: a "smoke test" designed to *require* running during a live sync to
  exercise concurrent access would be a surprising, undocumented design for
  a command whose own doc comment calls it "read-only, safe to run often,"
  and the lock's entire documented purpose is preventing exactly this
  overlap — nothing in the code or docs suggested smoke-check was meant to
  be the exception. New `TestSmokeCheckRefusesWhileAnotherRunHoldsTheOverlapLock`/
  `TestDumpLinksRefusesWhileAnotherRunHoldsTheOverlapLock`
  (`cmd/opal-downloader/listoverlap_test.go`) pin both, following
  `TestListRefusesWhileAnotherRunHoldsTheOverlapLock`'s existing pattern.
  Also folded in walk 10's second finding, the bare lock-held message: since
  `synclock.ErrHeld`'s message (`internal/synclock/synclock.go`) is shared by
  every caller, one edit now gives `sync`/`list`/`login`/`smoke-check`/
  `dump-links` all a sentence of guidance ("likely today's scheduled sync or
  another opal-downloader command; wait for it to finish and try again")
  instead of a bare PID and timestamp — `docs/OPERATIONS.md`'s lock-holder
  table updated to list the two new callers. `go build ./...` clean;
  `go vet ./...` clean; `go test ./...` passes across every package (skipped
  only the pre-existing, separately-tracked non-hermetic
  `TestSyncScheduledSkipsWhenAlreadySucceededToday` — a real `sync.lock` was
  held by another process at test time, which is exactly that test's known
  trigger condition, see the Noticed entry it left; not re-run here to avoid
  reproducing it).
- **`smoke-check`'s account-wide course scope is now documented instead of
  silent** (2026-08-19, autopilot, closing walk 9's friction finding,
  2026-08-18). Decided to keep the behavior rather than change it - checking
  every enrolled course regardless of `config.yaml`'s `courses:` filter is a
  defensible goal for an account-reachability smoke test, and narrowing it
  to match `list`/`sync` would make it test less, not more, correctly. Fixed
  the actual complaint (nothing said this out loud) instead: the top-level
  command list and the `smoke-check`-only options block in `--help`
  (`cmd/opal-downloader/root.go`) now both state it plainly, and
  `internal/smokecheck/smokecheck.go`'s `Run` carries a comment at the
  `[]string{"*"}` call. No behavior change, so no new test - existing
  smoke-check tests were unaffected by construction.
- **A scratch `sync --config` can no longer clobber the maintainer's real
  "last sync" record** (2026-08-19, autopilot, closing the last-sync.json
  finding found live 2026-08-18 during Question 44's verification runs).
  Root cause: `statuslog.WriteLastSyncDefault` is machine-wide by design (see
  its own doc comment), but both places that called it -
  `cmd/opal-downloader/root.go`'s `runSync` and `internal/gui/sync.go`'s
  sync handler - called it unconditionally regardless of which `--config`
  the run actually used, so a scratch `sync --config tmp/.../config.yaml`
  overwrote the real record with a throwaway run's numbers. Picked the
  finding's second named option (skip the shared write for a non-default
  config) over widening the record's identity: it needed no format/schema
  change and left the "machine-wide record of the real account" contract
  intact for the common case. New `config.IsDefaultPath(configPath)`
  compares an absolute `configPath` against `cwd/config.yaml` (the fallback
  every CLI/GUI entry point already uses when `--config` is omitted); both
  call sites now skip the write unless it returns true. Pinned by new
  `TestIsDefaultPath` (`internal/config/config_test.go`) and verified by
  reading the two call sites' guards - not re-run live, since the bug itself
  only reproduces by comparing a real run's on-disk effect against a
  scratch one over two separate invocations, and the mechanism (a path
  comparison gating an existing, already-tested write) doesn't need a fresh
  live crawl to confirm. `go build ./...` clean; `go test` passes across
  `internal/config`, `internal/gui`, `internal/statuslog`, and
  `cmd/opal-downloader` (excluding one pre-existing unrelated flake, see
  `docs/BACKLOG.md`'s Noticed section). `/preview`'s own pages were not
  checked for the same pattern - walk 7's open question 2 stays open for
  that half.
- **The first-run landing page no longer shows a stale, unrelated "Last
  sync" line next to its own "First time here?" message** (2026-08-15,
  autopilot, same session as the fix above, closing Walk 7's second finding).
  Root cause was structurally different from the login-reuse bug even though
  both trace to a global, unscoped path: `statuslog.WriteLastSyncDefault` is
  *by design* a machine-wide "last sync of any kind" record - the CLI,
  scheduled task, and GUI's own sync button all write the same file
  regardless of which config.yaml ran them - so scoping the reader alone
  (the session_state_file fix's approach) would have desynced it from an
  equally-global writer, and rescoping the writer too is a real product
  decision (would the scheduled-task status banner still want cross-config
  visibility?) left alone rather than decided unilaterally. Instead,
  `internal/gui/gui.go`'s `applyLastSync` now returns immediately when
  `data.SetupNeeded` is true (set by the already-config-scoped
  `applySyncReadiness`, which runs first) - the machine-wide line is
  suppressed only on the page that is simultaneously telling the user
  nothing has been configured yet, everywhere else it renders unchanged.
  New test `TestApplyLastSyncSaysNothingWhenSetupNeeded` pins the fix; all
  existing `applyLastSync` tests use a zero-valued `landingData{}` (so
  `SetupNeeded` is false) and were unaffected. Live-verified the same way as
  the companion fix: confirmed `~/.opal-downloader/last-sync.json` exists on
  this machine, then loaded a brand-new scratch config's landing page and
  confirmed the line no longer renders next to "First time here?". The
  schedule banner's own use of the same global file (the "Optional, not a
  commitment" entry, and Walk 6's on-logon finding) is unrelated and still
  open in `docs/BACKLOG.md` - this only touched the landing page's last-sync
  line.
- **A fresh config.yaml no longer silently inherits whatever OPAL session
  already exists on the machine** (2026-08-15, autopilot, fixing friction
  campaign Walk 7's most severe finding, 2026-08-13). Root cause was
  `internal/config.DefaultStateFile = "~/.opal_storage_state.json"`, a single
  fixed path every config.yaml that left `session_state_file` unset resolved
  to - which was all of them, since the Settings form never exposed the
  field. New `config.PerInstallStateFile(configPath)` scopes the implicit
  default to configPath's own directory instead
  (`<config dir>/.opal_storage_state.json`); wired into
  `credentialsFromRaw`/`fromRaw` (so `config.Load`/`LoadCredentials` resolve
  it for any config.yaml that omits the field) and into
  `internal/gui/settings.go`'s Settings-form save and
  `internal/gui/gui.go`'s pre-config landing-page check (both previously
  hardcoded to the same global constant). An explicit `session_state_file` in
  an existing config.yaml still overrides this, unchanged - only configs that
  relied on the implicit default move. Live-verified, not just unit-tested:
  built the real binary, pointed it at a brand-new scratch config directory
  with no session file of its own, and drove the actual Settings-save HTTP
  flow. Before submitting the fix, confirmed the real global session file
  (`~/.opal_storage_state.json`) exists and is one day old on this machine -
  exactly the state that would have made the bug reproduce. After the fix,
  the landing page correctly reported "Not logged in yet" for the fresh
  config instead of falsely inheriting that session. Full build, vet, and the
  `internal/config`/`internal/gui` test suites all green; two config_test.go
  tests updated to pin the new intentional divergence between
  `Defaults()` (no configPath, falls back to the machine-wide default) and
  `Load(configPath)` (scoped) rather than papering over it.
  `internal/gui/tufast_setup.go`'s interactive-browser-launch call site was
  checked and deliberately left on the global constant: read
  `OpenInteractiveBrowserAt`/`scraper.New` directly and confirmed that call
  path never reads or writes the state file at all (the TU-Fast install
  browser's identity lives in the separate, genuinely-shared login profile
  directory), so scoping it would have been a no-op change to dead-for-that-
  path code, not a fix. The companion finding (a stale global "Last sync"
  line on the same landing page, from `statuslog.ReadLastSyncDefault`) is a
  separate root cause and still open in `docs/BACKLOG.md`.
- **`discoverSectionsHTTP` (HTTP-first discovery, the default sync path since
  2026-08-11) now logs the structural section-type skips it was silently
  discarding** (2026-08-13, autopilot, weekly-review Part B finding). The
  browser path (`crawl.go`) has always logged every enrollment/Einschreibung
  node it skips via `logging.Detail`, with a comment stating the reason
  plainly: "Auditable, not silent." `discoverSectionsHTTP`
  (`internal/scraper/httpdiscovery_seed.go`) skipped the identical node class
  in two places - the tree-seed loop, and the `appendSectionFolderTargets`
  call whose `[]skippedSection` return value was discarded with `_` - and
  logged neither. No files were ever lost (these nodes never hold files), but
  the audit trail this project built specifically for this class of skip was
  silently absent on the path everyone now runs by default. Both sites now
  call the same `logging.Detail` line the browser path uses, verbatim. Build,
  vet, and the full test suite (including the existing
  `TestDiscoverSectionsHTTPSkipsNonFileSectionAtSeed`, which already covers
  the functional skip and is unaffected) all green; no live run needed since
  this only adds a log line to an already-tested code path.
- **Real per-file download errors no longer leak raw Playwright internals to
  the CLI's stdout or the GUI's live `/sync` log** (2026-08-12, friction
  campaign walk 5's finding). `internal/scraper/download.go`'s browser-
  fallback error now embeds a `"(technical detail: ...)"` marker, matching
  `internal/netcheck`'s own established convention (`AppendSentence`), so a
  short user-facing clause and the Playwright locator/timeout call log three
  past investigations (PRs #35/#89/#95) needed are no longer glued into one
  string with no split point. `internal/syncer`'s new `printSyncError` /
  `splitTechnicalDetail` print only the short clause to the CLI's stdout and
  route the full detail to the diagnostic log instead (`logging.Detail`) -
  nothing is thrown away, it just no longer reaches a surface built for the
  user. The GUI's `/sync` page mirrors the same split client-side
  (`splitTechnicalDetail`/`describeDetail` in `internal/gui/sync.go`),
  folding the detail into a collapsed `<details>` row exactly like the
  connectivity banner already does for this same marker - `textContent`
  only, never `innerHTML`, since the text comes from the account being
  synced. Also added the comment `docs/BACKLOG.md`'s other open finding
  asked for: `internal/config/config.go`'s `rawConfig` now points future
  field additions at `parseSettingsForm`, so the next one does not silently
  get dropped on every GUI settings save the way `opal_url`/
  `session_state_file` deliberately already do.

- **The installer's unverified post-uninstall message is closed on the
  compile alone** (2026-08-12, engineering decision - option (2) of the two
  the item itself had already written up). The `v0.1.1` release build
  compiled `CurUninstallStepChanged`'s `MsgBox` successfully, proving the
  script and its `ExpandConstant` usage are sound; what remains unverified is
  only whether the dialog *reads* correctly and fires at all, which needs a
  real Windows uninstall neither this environment nor the maintainer's own
  machine can currently run (no `iscc`/Inno Setup on either). Accepted rather
  than left open indefinitely: the worst case is a confusing sentence during
  an uninstall, not data loss, so the cost of being wrong is low and the only
  way to close it further is the maintainer's own ~1-minute install/uninstall
  of `v0.1.1` - available any time, not blocking anything else.

- **Question 39's monthly discovery-verify spot-check is built** (2026-08-12).
  `OPAL_HTTP_DISCOVERY=verify` mode already existed and already ran the
  independent browser-vs-HTTP comparison this question wanted
  (`internal/scraper/orchestrator.go`'s `scrapeCoursesHybrid`); nothing called
  it periodically. Added a new Part C to the local
  `opal-downloader-weekly-review` scheduled task
  (`C:\Users\alois\.claude\scheduled-tasks\opal-downloader-weekly-review\SKILL.md`
  — not part of this repo) that runs it, guarded by its own
  `docs/last-verify-run.txt` timestamp at ~30 days so it fires roughly
  monthly, separately from that pass's normal 2-day Parts A/B guard.
  Deliberately not wired into `sync` or any daily path, per the decision.
  Files a `## Now` backlog item only when the diff's `missing` total is
  greater than 0 for some course (a real regression) or the run itself fails;
  a clean run (the expected case) writes nothing, matching Parts A/B's
  existing "if there is nothing, write nothing" rule. No code changed in this
  repo — the verify mode was already there; only the caller was missing.

- **Question 5 is fully closed** (2026-08-12). All three halves: the CLI and
  GUI `list`-only silence were already fixed; the "last sync" timestamp line
  shipped and is live-verified; and the remaining background-run half —
  option (C), a real background `list` triggered by the GUI opening — was
  deliberately left unbuilt. It would need its own opt-out and `sync.lock`
  interaction, and there was no evidence GUI opens are frequent enough to
  justify it. Reopen only if the "last sync" timestamp has been in use for a
  while and "feels like one click" still visibly fails.

- **The GUI's explanatory-paragraph sweep is done** (2026-08-12, maintainer's
  ask: *"Es ist so nervig, immer einen Absatz zu haben, der einen Button
  erklärt."*). Deleted the Settings page's "Configured elsewhere" block
  entirely (`internal/gui/settings_page.go`, plus the now-unused `.elsewhere`
  CSS rule in `chrome.go`) and swept all ~26 `<p class="hint">` paragraphs
  across `settings_page.go`, `tufast_setup.go`, `schedule_page.go`, `sync.go`,
  and `logs.go`. Applied the maintainer's rule case by case: relabel a
  checkbox/heading and delete the paragraph where the control can say it
  itself (e.g. "Sync all courses" → "Sync all courses (untick to pick
  specific ones below)"); delete outright where the paragraph only restated
  a label or a visible placeholder (e.g. the "24-hour, e.g. 06:00" hint next
  to an input whose placeholder already says `06:00`); shorten and keep where
  a hint carries a real consequence a label can't (subfolder reorganization
  moving existing downloads, the "Fill in folders for me" confidence
  caveat); and collapse into a `<details class="more">` (following
  `feedback.go`'s pattern) where the load-bearing content was long — the
  Automatic Sync page's catch-up/TU-Fast/notification behavior, previously
  three separate paragraphs, is now one "How this works" disclosure. Verified
  with `go test ./...` (all green except a pre-existing, unrelated Windows
  timing flake in `internal/scraper`) and a live GUI smoke test against a
  scratch config. `gui.go`'s privacy paragraph and the landing page's
  last-sync line were kept, per the item's own exceptions.

- **The diagnostic log now rides along with a bug report by itself**
  (2026-08-12, maintainer's ask: can the log be attached automatically?).
  It cannot be *attached* — a prefilled GitHub issue carries its body in the
  URL, and GitHub has no way to upload a file from a link — so the log is
  inlined instead: `handleFeedbackPage` prefills a collapsed, **editable**
  field with the last 60 log lines, and `handleFeedbackOpen` builds the body
  from whatever comes back in it.
  The engineering content is the length problem. GitHub answers an over-long
  prefill URL with `414 URI Too Long`: the user lands on an error page and
  the report is simply lost, which would have made this feature actively
  worse than the link it replaced. `report.FitIssueURL` measures the built,
  *encoded* URL against `report.IssueURLBudget` (6500, under GitHub's ~8 KB)
  and drops log lines oldest-first until it fits — never the newest lines,
  never the user's own words. Measured on the maintainer's real log
  2026-08-12: 60 lines / 9032 chars in, 41 lines kept, final URL 6493 chars,
  and the result page said which 19 lines it left out and linked the full
  download.
  Editable rather than readonly is the privacy design, and deliberate: the
  log is already stripped of credentials and session tokens, but it names
  courses and files, and this now reaches a public issue tracker
  automatically where before the user had to go and attach it. Rather than a
  checkbox plus a paragraph explaining the trade-off, the text sits in a
  field the user can clear. Detail: `internal/report/report.go`'s package
  comment, which records this as its one deliberate widening of the
  scrubbing rule.

- **Second round of GUI flavour: the tab carries the run** (2026-08-12,
  maintainer's request: *"mehr gimmickkkkkks... die sind super witzig"*).
  Five additions on top of the 2026-08-03 pair. The one that is more than
  decoration: while a run is in flight the **tab title and the favicon** carry
  it — `(3/6) Syncing`, an opal-gradient progress ring drawn on a canvas, and
  `✓ Done` / `✕ Failed` afterwards until you look at the page. A sync takes
  minutes and the whole point is that you go elsewhere meanwhile, so the tab
  strip was the one surface that said nothing. Rules kept from round one:
  nothing touches `#status`, everything is derived from events the page has
  already shown, and it restores itself completely. `Working` rather than
  `Syncing` when the page cannot know the kind (a run started elsewhere after
  it connected sends events but no state frame) — a preview downloads nothing.
  Making the ring honest during **discovery**, the long half of a run, needed
  the only non-GUI change in the batch: `syncer.EventDiscovery` now carries
  `CourseIndex`/`TotalCourses`, which `scraper.DiscoveryProgress` already had
  and the relay was dropping. The two phases each count 1..N over the same
  courses, so they map onto one half of the ring each and it never resets
  mid-run. The rest: the quip pool went 10 → 20; the **Konami code works on
  the sync page** too and swaps in a sillier pool (`konamiEasterEgg`'s
  detector split out as `konamiWatcher`, so there is still one place that
  implements "never eat a keystroke"); and the landing page's logo shimmers on
  hover. Browser-walk tested throughout — real keypresses, real hover, a real
  `<link rel=icon>` — and live-verified on the real account: the tab read
  `(6/6) Syncing` with a PNG data-URL favicon through a 6-course run, and the
  `✕ Failed` path came for free when `sync.lock` correctly refused a second
  concurrent run.

- **The GUI landing page now says when the last sync was — Question 5's
  "feels like one click" half** (2026-08-12, maintainer's call: *"du kannst ja
  irgendwo (mainbildschirm/sync-feld) hinschreiben, wann der letzte sync
  war."*). The larger half of the change was that the timestamp did not
  exist: only `sync --scheduled` recorded an outcome, so a line read from
  `last-scheduled-run.json` alone would have shown a days-old scheduled run
  right after a manual sync — most wrong exactly when the user had just done
  the thing it reports on. `statuslog` gained a separate `last-sync.json`
  (kept separate so the GUI's scheduled-failure banner does not start
  announcing failures the user watched happen live), written by both places a
  sync can start — the CLI's `runSync` and the GUI's in-process job. A `list`
  never counts; a user-cancelled run records as a failure, not a success; a
  missing or corrupt record renders no line at all. Live-verified end to end
  2026-08-12: a real 39-file sync into a scratch folder wrote the record, and
  the landing page rendered "Last sync: just now". Question 5's remaining
  background-run half stays open in `docs/BACKLOG.md`.

- **`v0.1.1` published — the first release whose `login`/`sync` can actually
  start a browser** (2026-08-12, maintainer's call in a `/decide` round).
  `v0.1.0` (2026-07-14) shipped an installer that staged Chromium into
  `%LOCALAPPDATA%\ms-playwright` while the binary in that same release had
  already moved to `%USERPROFILE%\.opal-downloader\ms-playwright`, and
  `NeedsPlaywrightSetup` probed the same wrong path, so it reported "present"
  and skipped the `setup` fallback that would have recovered. Fixed on master
  by `9e9ac47` on 2026-08-03 and then simply never tagged for three weeks —
  the gap this entry exists to remember. Release run
  `31604913539` green; assets `opal-downloader-setup.exe` and its `.sha256`
  are published. Two things landed with it: `iscc` finally compiled the
  post-uninstall `MsgBox` that had only ever been written from source (so the
  compile half of that finding is closed — a real uninstall is still
  unwitnessed), and `379645d` fixed a version mismatch found while preparing
  the tag. The `.iss` hard-coded `MyAppVersion "0.1.0"` and nothing overrode
  it, so `v0.1.1` would have installed itself as "0.1.0" in Apps & Features
  while `--version` said `v0.1.1` — aimed squarely at the one question this
  release exists to answer. The tag now drives both; confirmed in the CI log
  (`Building opal-downloader.exe version v0.1.1 (installer AppVersion
  0.1.1)`).

- **Sync-speed Question 5's second half fixed: the GUI's `list`-only job
  streams per-course progress too, and both discovery paths now say what
  happened to enrolled courses that came back with 0 files** (2026-08-12,
  autopilot, live-verified in a real headless browser against the real
  account and via a live CLI run): the GUI's `sync` job already streamed live
  via a pre-existing `DiscoveryProgress`/`SetDiscoveryProgress` hook
  (`internal/scraper/progress.go`) that `internal/syncer.SyncCoursesWithProgress`
  was already wired to - a correction to the first entry below, which had
  implied the signal was wholly new rather than already used on one of two
  GUI paths. The GUI's `list`-only job (`internal/gui/sync.go`) was not wired
  to it at all and shared the CLI's old batching gap; now is, same event,
  same fix shape. Same pass closed the walk 3 "8 links, 6 courses, no
  explanation" friction finding's remaining half: both `ListAvailableCourses`
  (`internal/syncer/syncer.go`) and the GUI's list job now count
  `PhaseCourseDone` events with `FileCount == 0` and print/publish "(N of M
  enrolled courses had no files)" when it's non-zero. Full mechanism:
  `docs/sync-speed-model.md` Question 5 (two experiments, same day).
- **Sync-speed Question 5's first (cheap) half fixed: the CLI's discovery
  phase now prints a line per course as it completes, instead of the
  ~3-minute silent stretch friction-campaign Walk 3 measured the same day**
  (2026-08-12, autopilot, live-verified against the real account, 8 courses):
  the discovery line had run dry (Question 39 blocked, Question 41 closed),
  which the maintainer's 2026-08-03 decision makes Question 5 fair game for.
  Source reading found `collectCourseFilesConcurrently`
  (`internal/scraper/orchestrator.go`) already learns each course's result
  the instant that course's crawl finishes — the same point `PrintProfileLine`
  and the `onResult` download-candidate merge already fire from — nothing was
  surfacing it to a user without `--profile`. New always-on
  `timing.PrintCourseProgress` covers both `list` and `sync`, both discovery
  paths (one shared function). Bonus: the verification run named both
  courses walk 3 found "missing" from `list`'s course-count summary
  (`[WS25/26] Programmierung`, `Helfende DMS`) as genuinely 0-file courses,
  closing that walk's uncertainty too — see the next entry. Question 5's
  only remaining half (whether/when a background run before the click is
  worth building) is open in `docs/BACKLOG.md`, ranked in
  `docs/sync-speed-model.md` Question 5.
- **Friction campaign walk 3 (first run from zero): 3 findings, and a stray
  debug file deleted from the repo root** (2026-08-12, autopilot):
  `sync_run.log` (UTF-16, three lines, someone's local test-run transcript)
  had sat next to `README.md`/`go.mod` in every fresh clone since the initial
  commit `18f875d` (2026-07-02) and was the first thing a new user's file
  browser showed. `git rm`'d, nothing referenced it. The walk's other two
  findings are both fixed too, in the two entries above: the silent
  ~3-minute CLI discovery phase, and 8 course links reported as 6 courses
  with no explanation (the missing 2 confirmed genuinely empty, and both
  discovery paths now say so). Walk detail: `docs/friction-campaign.md` Walk
  3.
- **`list --visit-report` now flags intermittently-empty sections, not just
  always-empty ones** (2026-08-12, autopilot, verified live against the real
  account's visit log, ~344 sections): 34 of 344 sections are empty on some
  visits but not others (one on 85 of 86 visits, another on 62 of 89) — a mix
  of "material posted partway through the semester" and, for the ones closest
  to `Visits`, a plausible echo of the still-open Wicket "show all" expansion
  bug (Questions 17/19/22/25). None of it was visible: only the always-empty
  case got a Notes annotation, so an interesting row looked identical to a
  fully reliable one unless a human compared two number columns across ~278
  always-empty rows sorted ahead of it. `FormatReport`
  (`internal/visitlog/visitlog.go`) now also flags `0 < EmptyVisits < Visits`.
  Notes-column addition only, no sort-order or schema change; two new unit
  tests. This answers the walk-1 question about whether any row ever has
  `Empty < Visits` — yes.
- **The GUI no longer claims "Running the latest version" for a version it
  never compared** (2026-08-12, autopilot, unit test rendering the real
  template): `isDevBuildVersion` only short-circuits the plain `dev`/`""`
  default, so a git-describe build tag like `v0.1.1-dev-99c2fca` reached
  `IsNewerVersion`, which errors on an unparseable current version — and the
  landing template branched only on `UpdateAvailable`/`UpdateDevBuild`, so a
  real comparison failure fell through to the same "latest" branch as a
  genuine "checked, not newer". New `UpdateCheckFailed` field gets its own
  branch. The `/update` page already handled this correctly; only the landing
  banner was wrong.
- **`config.example.yaml` ships a `download_path` that works** (2026-08-12):
  `"D:/Uni/OPAL"` was one machine's real path and `init` copied it verbatim,
  so a CLI-first user's first `status`/`list`/`sync` reported `(BROKEN: ...)`.
  Now `"./downloads"`, matching `config.Defaults()`. The
  `section_folder_names`/`subfolder_destinations` examples are commented out
  too — they were live while `use_section_subfolders: false` sat right above
  them, producing two "has no effect" warnings out of the box. Verified: an
  `init`'d config passes `status` with no warnings.
- **`docs/setup-friction.md`'s closing paragraph described a `list` that no
  longer exists** (2026-08-12, autopilot, source reading): Findings 3/4's prose
  is deliberately preserved historical dry-run text (the doc says so), but the
  closing "still genuinely rough" paragraph was present tense and three
  defaults stale — `ensureSession` runs an offline reachability pre-check
  before anything browser-related, and discovery is HTTP-first, so a reachable
  OPAL with no saved session is the only path left that opens a browser.
  Updated in place with a dated note.
- **The installer is findable and verifiable, and winget is ruled out**
  (2026-08-11): release workflow, README and release-notes template rewritten
  so a download can be checked before it is run; the winget investigation and
  the code-signing comparison are in `docs/installer-plan.md` Section 6 and in
  the Settled section above.
- **Installer walked end to end from a real build: 28 of 30 checks passed**
  (2026-08-11): built `opal-downloader-setup.exe` from `99c2fca` and installed
  it the way a new user would — real silent install, real Start Menu shortcut,
  first run, first save, uninstall — with the maintainer's own Playwright cache
  moved aside first, so "Chromium is present afterwards" could only be true
  because the installer put it there. Both misses were the test's outdated
  expectations, not the app's behaviour. **Deliberately not filed as a
  friction-campaign walk**: it was engineering verification of a build
  artifact, run with full knowledge of the code and of the bug being checked
  for — no expectation registered before the click (campaign Rule 1), insider
  knowledge used at every step (Rule 4). The defects and stale docs it turned
  up stand without the persona and went to `docs/BACKLOG.md`; the installer
  surface itself is still unwalked by the campaign proper.
- **Fresh install flipped a live-confirmed default: the first Settings save
  wrote `skip_enrollment_sections: false`** (2026-08-11): front ends were
  hand-listing defaults, so the GUI's first save silently overrode one.
  `config.Defaults()` now exists and both the render and save paths use it.
- **Friction campaign walk 2 (CLI): `status`'s login line now reports
  session validity, not just file presence** (2026-08-11, autopilot,
  verified live against the real account's own session file): `status`
  said `Logged in: session state file present (...)` regardless of whether
  that session was minutes or weeks old - the same "checks presence, not
  substance" pattern walk 1 already found and fixed for `download_path` in
  this same command. `internal/sessionstate.Inspect` has answered "am I
  still logged in, and until when" from one offline file read (OPAL's own
  `authenticated-marker` cookie expiry) since 2026-08-03 and the GUI has
  used it the whole time; `status` was simply never wired to it. Now
  reports one of the GUI's own four states in matching wording. Verified:
  output now reads `Logged in: valid until Fri 14 Aug, 21:22 (2 days
  left)`, matching the GUI exactly for the same file. Four new
  `cmd/opal-downloader` test cases plus a `humanizeDuration` unit test,
  full suite green. Full write-up (including two new open questions) in
  `docs/friction-campaign.md` Walk 2.
- **Two more friction-campaign GUI-walk-1 findings fixed, and Finding 3's
  original diagnosis corrected on the way** (2026-08-11, autopilot, verified
  live in the GUI against the real `~/.opal-downloader/` status files,
  Amber-tier snapshot/restore): Finding 3 ("raw Playwright internals in the
  banner") turned out to already be fixed by 2026-08-10's `netcheck` work —
  the exact banner text the walk quoted is the real, still-current
  `last-scheduled-run.json`, timestamped 2026-08-10T08:38, which is **before**
  `netcheck` landed that day (09:42). Confirmed by reproducing the walk's own
  green-tier test (`opal_url` pointed at a dead host): current code answers
  `No internet connection... (technical detail: ...)`, cleanly split by the
  banner's existing marker logic — not the raw dump. What *was* still a real,
  live gap: a genuine local Chromium **launch** failure (as opposed to a
  network failure) was never wrapped at all — `internal/scraper/session.go`'s
  two `Chromium.Launch`/`LaunchPersistentContext` call sites returned the raw
  Playwright error verbatim, argv and all, matching the "full Chromium
  command line" the walk saw in the 01/08 and 02/08 history (both from
  before this fix, and before `netcheck` too). Both call sites now wrap with
  a friendly sentence and the same `(technical detail: ...)` marker the
  banner already folds away. Finding 4 (banner never expires): a
  network-classified failure now softens from red to the existing `.success`
  styling with a reassurance sentence once `navigator.onLine` says the
  connection is back — client-side only, no new endpoint, matching
  `netcheck.Describe`'s three stable sentence shapes. Deliberately does not
  apply once the staleness warning fires (>= 2 days since any run): that is
  a separate, still-true, more urgent problem (Finding 1) that a resolved
  network cause does not fix. Verified both branches live: a fresh
  network-classified failure turns green with the reassurance line, a stale
  one stays red with the staleness sentence, browser-online in both cases.
  Full test suite green.
- **Finding (bloat) fixed: `/settings`'s glob-pattern rules collapsed behind
  an "Advanced" disclosure** (2026-08-11, autopilot, verified live in the
  GUI against both an empty and a populated config): section-name rewrites
  and subfolder destination overrides no longer sit in the same flat flow
  as "where do my files go" - both tables now live inside a single
  `<details>`, closed by default, that opens automatically when either
  already has a row (so an existing user's configuration is never hidden
  from them). Nothing about the fields, their names, or their submission
  changed - purely a visual regrouping. Confirmed collapsed with a fresh
  scratch config and expanded with the real config.yaml's existing
  subfolder override.
- **Finding 1's recommended repair (b) shipped: an on-logon catch-up trigger,
  guarded against over-firing** (2026-08-11, autopilot, live-verified against
  real Task Scheduler under a scratch task name): the scheduled task now
  registers a `LogonTrigger` alongside the existing daily `CalendarTrigger` -
  `docs/scheduled-sync-plan.md` section 4's original "not both, since
  STARTWHENAVAILABLE gives a logon-like catch-up for free" reasoning is
  corrected in place, with the walk's own event-332 evidence (3 of 5 days
  silently unsynced) as the refutation. New `errAlreadySucceededToday` guard
  (`cmd/opal-downloader/root.go`) is the "already ran today" dedup the
  original design doc said a logon trigger would need, checked before
  anything else in the `--scheduled` path (TU-Fast presence, config load,
  network wait) so a machine locked/unlocked repeatedly costs one cheap
  status-file read per extra logon, not a resync. Only a recorded
  **success** suppresses the guard - a failure or partial run earlier today
  still lets the next trigger retry. Verified live: registered the generated
  XML under a scratch task name (`schtasks /Create`), confirmed both
  triggers round-trip through a real `schtasks /Query`, deleted it - the
  real `OpalDownloaderScheduledSync` task was never touched. `/schedule`
  page copy updated to match. Full test suite green.
- **Finding 2 fixed by teaching the existing doomed-schedule repair machinery
  about git checkouts** (2026-08-11, autopilot): `CheckExecutableStable`
  (`internal/scheduler/exepath.go`) already rejected go-build-cache and
  system-temp-dir paths (task #122) but had no concept of "this path is
  inside a git working tree" - so a plain `go build .` output sitting in
  the repo (exactly what was actually registered, 19 days stale) passed as
  "stable" and the maintainer's own `repairDoomedSchedule` self-heal
  (`internal/gui/schedule.go`) never triggered for it. New
  `findGitWorkingTreeRoot` walks up from the executable's directory looking
  for a `.git` entry (file or directory, so a linked worktree counts too),
  bounded to 12 levels. No new UI or registration-time code needed - the
  existing GET-render repair/warn path (already covers the go-build-cache
  and missing-executable classes) now also catches and either repairs or
  clearly warns about this one, the next time the maintainer opens
  `/schedule`. Verified via the existing mock-driven repair tests (no real
  `schtasks.exe` involved) plus two new cases using this package's own real
  git-tracked working directory rather than a hardcoded path.
- **Question 41 closed: course-level HTTP concurrency's second confirming
  run lost 6 files, overturning the first run's clean result and closing
  the promotion question as a no-go** (2026-08-11, autopilot, 2 live runs):
  `OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2` dropped one paginated section's
  6 files (`Algorithmen und Datenstrukturen` -> `Vorlesung`) on identical
  code to the first (clean) run — confirms the exact "shared
  `APIRequestContext` under concurrent load" hazard Question 40's own
  implementation notes had flagged as unproven. No production impact, the
  override was never wired to a default. Full mechanism in
  `docs/sync-speed-model.md` Question 41.
- **Four friction-campaign GUI-walk-1 findings fixed** (2026-08-11,
  autopilot): `status` now runs the same `os.MkdirAll` a sync calls first, so
  a broken `download_path` (typo'd drive letter, path under a file) is
  caught at `status` time instead of surfacing minutes into a sync
  (`(BROKEN: ...)`, tested both ways). Landing page: the primary "Sync now"
  button states duration ("Takes several minutes"); the nav link that named
  only "developer tools" for a page that's also home to Preview and Force
  re-download now names those too; the two-window disclaimer no longer
  reuses "it"/"this window" for both windows. Full suite green. Remaining
  friction-campaign items (on-logon trigger, gitignored task binary, raw
  Playwright internals in errors, banner expiry, settings bloat) still open —
  see the friction campaign section above.
- **Question 40 live-verified: `scrapeCoursesHTTPFirst` course-level
  concurrency (`OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2`) is empty-diff clean
  and cuts discovery 27%** (2026-08-11, autopilot, 2 live runs): 349/349
  files both sides, 41.6s vs a fresh 56.7s serial baseline, squarely inside
  the pre-registered 35-45s prediction. Not promoted to default — one run
  against this project's own two-clean-runs bar (Step B2 precedent) — opened
  Question 41 for a different-day confirming run and the promotion decision.
  Full write-up in `docs/sync-speed-model.md` Question 40/41.
- **HTTP-first discovery silently stopped feeding `internal/visitlog`'s
  cross-run log the moment it shipped as the default; fixed, and Question
  35's downgrade sharpened into two new ranked questions** (2026-08-11,
  autopilot, 1 live run): found while re-running Question 36 Step B1's own
  probe for Question 38 - `scrapeCoursesHTTPFirst` never called
  `recordSectionVisit` (only the browser path did), so every real sync/list
  run since PR #133 merged recorded 0 section visits, silently (no error -
  `persistVisitLog`'s own no-op-on-empty design). Not a file-loss bug, just a
  dried-up diagnostics resource. Fixed: `discoverSectionsHTTP` now takes an
  `onSectionVisited` callback, wired to `s.recordSectionVisit`; regression
  test added; live-verified (298 sections recorded, not 0). Also confirmed by
  reading `orchestrator.go` directly: `course_concurrency` was only ever
  wired into the browser path, never into `scrapeCoursesHTTPFirst` - so
  Question 35 (already downgraded, see below) is fully moot for what ships,
  not just lower priority. Opened Question 39 (is HTTP-first's correctness
  still cross-validated by anything, now that shipping it as default removed
  the browser-crawl comparison every live run used to get for free) and
  Question 40 (does the now-fully-serial `scrapeCoursesHTTPFirst` benefit
  from its own course-level concurrency). Question 38 itself parked, not
  closed: three data points (55.93s/303req=184.6ms, 59.97s/314req=191.0ms,
  both today) cluster with 2026-08-10's four against 2026-07-31's 315ms
  outlier, but the *why* is still unnamed and no longer worth a live run to
  chase. Full write-up in `docs/sync-speed-model.md` Questions 35/38/39/40.
- **`OPAL_HTTP_DISCOVERY=2` (HTTP-first discovery, Question 36 Step B2)
  shipped as the default** (2026-08-11, decision round): merged PR #133,
  closed duplicate PR #134 as superseded (it carried the same unfixed
  20-minute-hang bug #133 already found and fixed, just never triggered it —
  see the Settled section for how the duplicate happened). Maintainer chose to
  ship now rather than wait for a different-day confirmation run, matching
  the `course_concurrency=2` precedent. `OPAL_HTTP_DISCOVERY=0` forces the
  old plain browser crawl as a rollback path. Downgraded Question 35 (raise
  `course_concurrency` past 2) — the path it would tune is no longer the
  default one.
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
  `isUserDataDirLocked`. Full write-up in this file's Settled section; the
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
  show a real speed win — now a maintainer decision, decided the same day. Full
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
  login-timeout entry in this file's Settled section.
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
- **Question 7 closed, no live run needed: it was already answered by data
  collected for Questions 13-15, just never connected back** (2026-08-06,
  autopilot): the two remaining candidates for "what fills the settle wait, if
  not network transfer" (browser parsing/layout, or a JS widget) both predict
  real browser-side work should dominate the unexplained ~70%. Question 13's
  CDP profiling had already measured that ceiling directly — Script+Layout+
  RecalcStyle at 11.4%, even the broadest `TaskDuration` metric at 24.4% (and
  flagged as an overestimate). Question 14/15 then clinched the mechanism:
  halving `mutationObserverDebounceMs` lost zero files over 8 real-account
  runs on two courses 27x apart in size — only possible if the removed 150ms
  was margin the browser had already finished before, not time it needed. So
  the settle wait is mostly the crawler's own fixed timers outliving actual
  completion, not browser work of any kind. Full write-up:
  `docs/sync-speed-model.md` Question 7.
- **Question 20 closed inconclusive, and said so plainly** (2026-08-04):
  raising the Wicket signal-wait ceiling to 15000ms produced 3 clean
  contention runs in a row (248/248/248 files) — but at this condition's
  ~33-50% historical failure rate that is not proof of "pure delay", just a
  plausible outcome either way. The diagnostic tool
  (`OPAL_WICKET_SIGNAL_TIMEOUT_MS_OVERRIDE`) is built and reusable; what's
  missing is an actual latency distribution, not another blind batch —
  Question 21.
- **Question 19 closed, its own prediction wrong: the Wicket signal doesn't
  arrive late, it doesn't arrive at all** (2026-08-04): new
  `wicket-expand-signal` audit line (`crawl.go`) plus a contention probe
  (`showallsignal_probe_test.go`) caught `expansionSignalled=false` in both
  runs that lost the Vorlesung folder's tail — the click fires, Wicket's
  `AJAX_CALL_DONE` just never shows up within the 4000ms budget. Refutes
  Candidate B (late signal), reopens Candidate A split into "pure delay" vs.
  "never issued" — Question 20.
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
- **Question 25 confirmed live, 3/3: rearming the Wicket watch on the
  context-destroyed reclick recovers the section** (2026-08-06, autopilot):
  `crawl.go`'s reclick fallback now mirrors the sibling `AJAX_CALL_FAILURE`
  retry — rearm before reclicking, await the signal, only fall back to the
  generic wait if it doesn't come. All 3 live `context-destroyed` hits across
  a 4-run contention batch recovered cleanly (`expansionSignalled=true` on the
  retry), zero truncation warnings, all 4 runs at the full 248/248 files. Closes
  the causal chain Questions 17→25 have chased since 2026-08-03. Opened
  Question 26: retest Question 23's shelved preview-blocking win now that its
  named prerequisite is live-tested fixed — full write-up and the deferred
  run's cost/prediction in `docs/sync-speed-model.md`, "Next experiment".
- **Question 21's first cycle caught its own wrong assumption within the same
  day** (2026-08-04): timestamped the Wicket signal wait (`signalMs` on the
  `wicket-expand-signal` audit line) expecting to distinguish a bimodal
  latency distribution from a smooth one; 2 live samples were too few for
  that, but both showed `expansionSignalled=false` resolving in ~200ms — the
  same order as a successful signal, not the 4000ms timeout the code's own
  comment (written hours earlier, same commit's predecessor) assumed a
  failure would consume. `awaitWicketExpansionDone` was discarding the actual
  wait error; now it returns and classifies it (`signalWaitErr`). Opens
  Question 22: does it say `context-destroyed`, tying this to the fallback
  `waitForInteractiveLinks` already has for exactly that.
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

- **This file was too big to read in one go (~1500 lines) because closed
  sync-speed measurements were accumulating here instead of in
  `docs/sync-speed-campaign.md`.** The "Sync speed" entry carried ~270 lines
  of A/B tables and blow-by-blow history (the `ctx.Route` tax investigation,
  the file-preview-blocking A/Bs, the skip-settle-wait experiment) that had
  never actually been migrated to the campaign doc despite this file saying
  in multiple places that closed measurements belong there. Moved all of it
  (verbatim, nothing lost - verified every table survived) into
  `docs/sync-speed-campaign.md` as dated entries continuing its existing
  2026-07-27 narrative, and rewrote the BACKLOG entry down to current status
  + a 3-point summary + a pointer to the log. 1429 → 1130 lines overall.

- **Every live probe test in `internal/scraper` now routes its diagnostic
  Warn/Detail logs somewhere visible, not just `TestFileListSnapshot`.**
  `captureProbeLogs(t)` added to the three `htmlstability_probe_test.go`
  functions, `network_trace_probe_test.go`, `httpdiscovery_probe_test.go`, and
  `mutationmarker_probe_test.go` - a one-line addition each, matching the
  pattern `TestFileListSnapshot` already used. Live-verified, not just
  compiled: running the mutation-marker probe with the fix in place surfaced
  a real, previously-silent diagnostic ("show all" control expansion capped a
  section at 17 rows, later files missing) that the probe's own summary line
  never mentioned - exactly the class of information the 2026-07-29 incident
  (`probelogging_test.go`) was about.

- **`scripts/dev.ps1 all` no longer fails outright when a live probe (or the
  test harness's own parent process) is running in this tree — it skips just
  the two assertions that cannot be evaluated, and says so.** The busy-check
  precondition in `test-hooks.ps1` used to `Assert-That` (i.e. FAIL) when it
  found a live `go test`/`.test.exe`/repo-path-bearing process, which blocked
  even a docs-only push for however long that process ran, with no action for
  the reader beyond "wait and retry". A new `Skip-Assertion` helper (separate
  pass/fail/skip counters) marks it `SKIP` instead, and the final summary line
  now reports a skip count. Verified live both ways: a normal run shows
  `209 passed`; forcing a busy fixture mid-run (a backgrounded `cmd.exe` with
  the repo path on its command line, held alive for the whole run) shows
  `207 passed, 2 skipped` and still exits 0.

  **Worth remembering independent of the fix:** an earlier version of this
  entry blamed the suite's own `taskkill` fixture for an unrelated flake,
  based on "perfect correlation across 5 runs, 3 of 5 failed" - reasoning
  that was wrong twice over (9 later clean runs refuted the correlation, and
  the accused `ping` child carries no repo path so could never have tripped
  this assertion anyway). A cause was asserted from a correlation without
  reading the assertion's own message, which already named the real
  offending processes - exactly what `CLAUDE.md` says not to do.

- **All three unbounded local accumulators this project had are now pruned;
  `tmp/` was the last.** `.claude/queue/`'s resume-run logs and
  `refs/wip-checkpoints/` were fixed earlier 2026-07-30 (age + count-floor
  pruning on each invocation of the process that writes them).
  `scripts/dev.ps1 all` now also prunes `tmp/` of files older than 14 days on
  every run - no count floor needed there, unlike the other two, since `tmp/`
  is written by hand and sporadically rather than by an automated process
  that could plausibly race a burst of writes into deleting something recent.
  Verified live: pruned exactly the 10 files from 27-day-old abandoned
  research (`dump-links.*`, `opal-course-1-*.json`,
  `opal-resource-courses-*.json`), left everything from the last two weeks
  (including the section-cache A/B's own recent output) untouched, and a
  second run found nothing left to prune.
- **The section-detection cache is deleted (2026-07-30), for the second time
  it was rejected on the same measurement.** `internal/sectioncache`,
  `internal/sectionhash`, `internal/scraper/sectioncachewiring.go`,
  `sectionpayload.go`, the `crawl.go` probe loop, the `syncer.go` load/save,
  `OPAL_SECTION_CACHE`, and `FileRef.SectionURL` are gone; code budget back
  down 11902 -> 11577. Warm 273.3s against a 241.0s control, 3.9% hit rate -
  the same rejection the 2026-07-21 attempt already earned, reached again by
  a rebuild that didn't measure the hit rate until the end. The maintainer
  also declined a third-round volatility diagnostic ("mir wurscht") -
  recorded so nobody reopens it looking for a decision that was never made.
  Evidence lives in `docs/sync-speed-campaign.md`; reversible in one
  `git show` if anyone wants to re-measure. What survived and matters more
  than the feature did: `ctx.Route` costs ~30% of a run just by existing,
  and the settle wait is the cheap completion signal, not overhead to cut.
- **The Windows binary's running window now shows opal-downloader's own
  icon, not the Go default.** `scripts/build-icon.ps1` rasterises `logoSVG`
  into `internal/gui/assets/icon.ico` (16-256px) via WPF - `Geometry.Parse`
  already speaks the SVG path mini-language, so the "needs an SVG renderer"
  blocker recorded 2026-07-27 didn't need a new dependency to clear.
  `window_windows.go` embeds the file and `WM_SETICON`s it onto the window
  right after creation. Live-verified by screenshotting the real running
  window. The `.exe`'s own Explorer icon is a separate, not-yet-decided
  follow-up (see "Next").
- **Self-monitoring (2026-07-30 maintainer request): 2 of 3 named symptoms now
  have a real detector, both wired into `session-start-autopilot.ps1` and
  tested in `scripts/test-hooks.ps1` (209 hook assertions, up from 191).**
  - **A silently dead hook.** `.claude/hooks/hookbeat.ps1`: every wired hook
    writes a liveness beat; `Test-HookLiveness` flags `autopilot-gate`/
    `noticed-gate`/`budget-guard` (the three that fire on almost every turn)
    as dead if their last beat predates the newest commit - a comparison
    that can't false-positive, since a commit only lands inside a turn that
    made a tool call and then hit Stop. Surfaced as `SELF-AUDIT: possible
    dead hook(s)`.
    **Found and fixed along the way:** `hookbeat.ps1` ignored
    `$env:OPAL_AUTOPILOT_QUEUE_DIR`, so every test run was overwriting the
    *real* liveness beats with test timestamps - which would have made the
    check permanently blind, since the test suite kept "healing" the exact
    thing it exists to catch.
  - **Budget spent with nothing to show for it.** `session-start-autopilot.ps1`
    persists the budget floor + HEAD commit each session start; the next one
    compares, and flags a same-window floor rise of 15+ points against 0
    commits in between as `SELF-AUDIT: ... 'too many tokens for too little'`.
  - **Not built:** a detector for "too little actually worked on" in
    *interactive* sessions specifically (unattended runs already get this
    from `unattended-run.ps1`'s existing verdict). Judged fuzzier than the
    other two - "too little" has no clean definition for a session the
    maintainer is actively driving - and lower priority; see `docs/RESUME.md`
    if picking this back up.
  - Neither detector has been observed catching a real incident yet, only
    tested against synthetic state - same starting position every hook in
    this family began from.
  - `docs/work-quality.md` (separate, same request): names why "no
    acceptance authority" and "half-changes by default" happen, and drafts a
    definition of done. Explicitly does NOT include any hook that grades
    code quality - self-grading was ruled out on purpose (see that file).
- **The "Noticed" section has a consumer now.** The maintainer asked what
  happens to the notes ("was passiert eigentlich mit den notizen?") and the
  honest answer was: nothing, unless somebody happened to read them. That is
  the same gap that made autopilot look dead the same evening — an all-blocked
  "Now" had the gate concluding there was no work while real entries sat lower
  in the file.
  The gate now falls back to Noticed when nothing under "Now" is actionable,
  and says which list the work came from. **Second-class on purpose:** the
  section describes its own entries as "not commitments", so they never outrank
  a real item and never make a finished backlog look busy. An all-blocked
  backlog with no notes still ends the run — a gate that never lets go is worse
  than one that stops early.
  *Seven new assertions (129 total), and the wiring is tested end to end rather
  than just the parser: `OPAL_AUTOPILOT_BACKLOG` was added for exactly that,
  since whether the fallback fires otherwise depends on the repo's own backlog
  happening to be all-blocked. Testing the parser alone is how the stall
  watchdog shipped connected to nothing.*

- **Autopilot had been dead all session, and nothing said so.** The maintainer
  asked "wo hook? warum muss ich dich schon wieder anschreiben?" — and they
  were right: `.autopilot-state.json` had not been touched since 14:08, so the
  Stop gate never blocked once during a session lasting hours.
  **Two independent causes, both found by looking rather than guessing.**
  First, every item under "Now" was marked `**Blocked:**`, including sync speed
  — whose blocker (a hand-run `login`) the maintainer had cleared that very
  evening, while the heading was never updated. The gate reads only the
  heading's first line, so it correctly concluded there was no work, which is
  indistinguishable from being broken. Second, the marker and session record
  later vanished with nothing recording why, after which the gate allowed every
  stop *silently*.
  The silence is the part that was fixed, because it is what made this cost an
  evening: autopilot ending now always writes `.autopilot-ended.json` with a
  reason, and a gate that finds no config but sees a state file — proof it once
  ran here — blocks **once** to say so and explains how to re-arm. A repo that
  never armed autopilot stays a complete no-op, and the report writes its record
  before blocking, so a confused state can never trap anyone in a loop.
  *Five new hook assertions covering exactly those three cases (122 total).*
  **Still unattributed:** what removed the marker. The off switch was absent and
  the arithmetic does not obviously fit the expiry either. Recording the reason
  is what makes the next occurrence answerable instead of guessed at — which is
  the honest fix available, since the evidence for this one is gone.

- **Four things the maintainer hit running the GUI (2026-07-27 evening).**
  `/feedback` asked people to attach the diagnostic log and offered no way to
  obtain it - it linked to a viewer and left them to go find the file. There is
  a download now (`/logs/download`), serving the whole file rather than the
  page's tail, because a bug report wants all of it; no log yet returns an
  explanation instead of an empty file somebody would attach believing it held
  something. A `go run` build is no longer told it lives in "a temporary
  location" - accurate, but it reads as a fault when it is just how `go run`
  works - and now names the command that fixes it. The schedule page's error
  box said "Could not update", which reads as a failed app update rather than a
  schedule that did not change. And `/settings` had two `<h2>` sections styled
  exactly like real ones that contained nothing settable, only pointers
  elsewhere; they are one secondary line at the foot of the page now.
  *Verified in a real headless browser, not only asserted - the last GUI bug
  here was invisible white-on-white text that every assertion passed.*

- **A cancelled run reports as cancelled, not as a broken tool.** The
  maintainer cancelled the 18:31 run themselves - "da war alles normal" - and
  got a course-listing failure plus advice to leave their browser window open.
  Cancelling tears the browser down, so every source fails; `scrapeCoursesBrowser`
  checked `ctx.Err()` before discovery and after the crawl but not around
  discovery's own error, which is exactly the window a cancel lands in.
  **This also corrects that turn's own reporting**, which presented a
  cancellation as an incident.
  *The wiring is the part that breaks here: removing the call site passes the
  unit test, the build and `go vet`. So the probe cancels for real against a
  real browser, with a server that blocks until the cancel has landed so the
  earlier guard cannot catch it first. Mutation-tested: reverting the call site
  reproduces the exact message the maintainer would have seen.*

- **A run that read nothing no longer reports as a healthy empty account.**
  Found by the maintainer running `go run . gui` (2026-07-27 18:31): login
  succeeded, then all three course-listing sources failed with Playwright's
  `target closed` and the run finished as `Found 0 course links / Discovered
  0 remote files` — which is exactly what a successful sync of an empty
  account looks like. `discoverCourseLinks` warned per source, `continue`d,
  and returned an empty list with a **nil error**; "every source failed" and
  "you have no courses" were the same value to every caller.
  Now all-sources-failed is an error. A *partial* failure stays a warning on
  purpose — the sources overlap, and one transient navigation failure
  aborting a whole sync would be a worse bug than the one being fixed. An
  empty result with no failures also stays fine, since `courses:` can
  legitimately filter everything away.
  **Nothing was lost or damaged by the bad run**: checked rather than
  assumed — the syncer's only `os.Remove` is a temp file, and it never
  removes a local file on the strength of a remote listing.
  The likely trigger is worth knowing on its own: in developer mode the crawl
  keeps running in the *same visible window* the interactive login used, and
  nothing tells you to leave it open. So the error names that case
  specifically when the failure looks like a closed browser.
  *Verified against a real headless browser
  (`TestDiscoveryAgainstARealBrowser`, opt-in via
  `OPAL_SCRAPER_BROWSER_PROBE=1`), which reproduces the incident by closing
  the page mid-run — the unit tests cover the predicates, and this covers
  that `discoverCourseLinks` actually calls them, the gap the stall watchdog
  fell into. Mutation-tested: making the predicate always return false
  reproduces the original message verbatim. Both directions covered — a
  readable listing still discovers its course and does not error, and a plain
  timeout must not be reported as a closed window.*

- **`course_concurrency` default confirmed at 1, live config now matches.**
  Re-measured the real account five times: serial 227.9s/345 files; `2` came
  back 228.2s/**336 files** (`Übungsblätter` 29 → 20, one "show all" expansion
  silently not happening) and otherwise 345 across three more runs at
  230.4s/219.6s/229.4s — no longer faster (mean 226.9s vs. serial 227.9s,
  inside noise) and still loses files about one run in five. Root cause fixed:
  `expandShowAllInSection` now warns instead of silently returning a truncated
  section (`scripts/compare-visit-runs.ps1` turns that into a one-command
  diagnosis), though the warning itself is unverified in the wild — a
  deliberately lossy `--section-concurrency 4` run produced zero warnings
  while losing 160 files, a different failure mode (the file table never
  renders at all, so there's no "show all" control to find). The maintainer's
  own `config.yaml` explicitly set `course_concurrency: 2`; confirmed
  2026-07-27 it now reads `1`, so the measured-correct default reaches them.
- **Fixed the hook-output mojibake noticed in the previous session.** Root
  cause: `docs/RESUME.md` and `docs/BACKLOG.md` have no BOM, and
  `Get-Content` without an explicit `-Encoding` reads a BOM-less file as the
  system ANSI codepage in Windows PowerShell 5.1 — so a UTF-8 em dash
  (`E2 80 94`) was read as three separate CP1252 characters and then
  re-encoded as UTF-8 on the way out, doubling the corruption. Fixed in the
  three call sites where this prose actually reaches the model:
  `session-start-autopilot.ps1` (embeds `RESUME.md` in its `additionalContext`),
  `resume-runner.ps1` (reads `RESUME.md` to decide if there's work), and
  `budget-lib.ps1`'s `Get-BacklogItems` (titles feed directly into
  `autopilot-gate.ps1`'s Stop-hook reason text). *Verified by running the
  hook directly and inspecting the raw output bytes before and after: the em
  dash arrived as the single correct 3-byte sequence, not six mangled bytes.
  Mutation-tested in `scripts/test-hooks.ps1`: a non-ASCII backlog title now
  round-trips byte-exact through `Get-BacklogItems`.*

- **Removed the stale agent worktree flagged above.**
  `.claude/worktrees/agent-ae4c52c8caec1f5e0` (branch
  `worktree-agent-ae4c52c8caec1f5e0`) was a 2026-07-23 prototype ("Add
  section-level flattened crawl (shared frontier across courses)") that
  predates and was superseded by the per-course tab-pool section concurrency
  that actually shipped and was measured on 2026-07-26 (`ca299c5` "Build
  section-level concurrency" onward) — confirmed by comparing commit dates
  and `git merge-base` before removing anything. Uncommitted changes in the
  worktree (`.gitignore`, `section_crawl.go`) were an earlier iteration of
  the same dead approach. `git worktree remove --force` + `git branch -D`;
  nothing pushed, nothing referenced elsewhere.

- **The folder picker corrupted any path with a non-ASCII character.** Chased
  from a mojibake spotted in a live `config.yaml` (`...\Analysis\<U+FFFD>bung`,
  should be `Übung`) and it turned out to be a real bug, not a typo.
  `browseForFolder` runs a PowerShell script and reads its stdout, and
  PowerShell encodes stdout in the **console's OEM code page** — 850 on a
  German Windows, where `Ü` is the single byte `0x9A`. Go reads those bytes as
  UTF-8, `0x9A` is not valid UTF-8, and it becomes U+FFFD. So the user picks a
  real folder with the file browser and the tool stores a path that points at
  nothing — silently, with a successful-looking picker.
  One line fixes it (`[Console]::OutputEncoding` before anything is written).
  *Measured, not reasoned: under code page 850 the path arrives as
  `...,92,154,98,117,110,103` without it and `...,92,195,156,98,117,110,103`
  with it. Both directions are tests — one asserts the round trip survives, the
  other asserts the corruption still happens without the guard, so the guard
  cannot quietly stop being load-bearing.*
  **Why it hid:** it does not reproduce on a console already at 65001, which is
  what an interactive shell here happened to have. The machine's real OEM code
  page had to be read out of the registry to see it.
  The maintainer's own `config.yaml` was repaired in place (backup left beside
  it); the `Übung` folder it should have pointed at already existed.

- **The diagnostic log can be reached from the GUI now.** It was written to
  `~/.opal-downloader/logs/`, named in the CLI's `--help`, and mentioned
  nowhere in the GUI — which is how most people use this, and the case where
  the log matters *most*, since a windowed app's stdout goes nowhere. A
  diagnostic nobody can find is close to no diagnostic.
  `/logs` shows the path, the end of the file, and a button that reveals it in
  the file manager, linked from `/feedback` because a bug report is exactly
  when someone needs it. Showing the contents in a page is safe by
  construction, not by judgement: everything in that file has already been
  through `statuslog.SanitizeMessage`.
  Both the log path and the file-manager call are **seams stubbed in every
  test** — the same hazard as the scheduler one: a test must not open Explorer
  on the maintainer's desktop or depend on their real log.
  *Verified in a real browser (it is in `TestBrowserEveryPageLoads` now) and
  screenshotted, since the last GUI bug here was invisible white-on-white text
  that every assertion passed. Mutation-tested in two directions: dropping the
  tail cap and removing the feedback link both fail.*

- **Refused to schedule a daily sync when there is nothing to sync.** Found
  while building the `/schedule` page and left open at the time. Enabling the
  daily run with no `config.yaml` registered a Windows task that does not fail
  once — it fails *every morning*, silently, unless the failure notification
  happens to be on, in which case it becomes a daily toast about a job the user
  cannot tell they set up wrong. Pre-existing (the old settings-page handler did
  the same), but the new page shows the form right next to a "set up first"
  warning and then let you ignore it.
  Now the enable path refuses before writing anything and says what to do
  instead. **Disabling is deliberately never blocked**: somebody whose config
  has gone missing still has a task running every morning, and refusing to let
  them remove it would strand them with it.
  *Mutation-tested: dropping the guard re-registers the doomed task. All three
  directions covered — refused without a config, still works with one, and
  disable unaffected.*

- **The sync page notices when a run stops moving.** A sync was reported stuck
  once (2026-07-26) and the only evidence was a status line that had not
  changed — nothing noticed, and nothing could have, because the page rendered
  the last event it received and had no opinion about how long ago that was.
  After three minutes of silence during a run it now says how long it has been
  and points at Cancel. Deliberately not an alarm: a large section legitimately
  goes quiet for a while, so it reports elapsed time rather than declaring a
  fault, and any event clears it so it cannot latch on and cry wolf.
  A second bug fell out of writing the test: the page learned "a run is in
  flight" only from the SSE frame sent when it connects, so a run that started
  *after* the page was open was never watched — which is exactly the run worth
  watching. Events arriving now count as proof of a run in flight.
  *Verified in a real browser (`TestBrowserSyncPageNoticesAStalledRun`), which
  also checks the idle page stays quiet and the notice clears on activity.*
  **The other half is now closed too** (`internal/scraper/stallwatch.go`): a
  watchdog inside the crawl logs, every 30s of silence past 3 minutes, *which
  section it was on* — course, title and URL. That covers CLI and scheduled
  runs, which had nothing at all, and it records the thing that was actually
  missing the one time this happened: somewhere to go and look. It only logs;
  cancelling a crawl on suspicion would risk killing a slow-but-healthy run,
  and losing work to a false positive is worse than the stall.
  *Mutation-tested in three directions, and the third is the interesting one:
  deleting the call from `scrapeCoursesBrowser` passed every other test,
  because they all invoke `watchForStall` directly and so check the machinery
  rather than whether anything uses it. The watchdog now records that it was
  started, and the scrape is asserted to start it. Its position moved to the
  top of the function as a result — a watcher stopped by an early return costs
  microseconds, and starting there means nothing can later be added above it
  that hangs unwatched.*
  The original hang has still never been reproduced.
- **Server load is bounded, and the bound is written down.** The maintainer
  asked for this to be set up long-term rather than checked once. Three parts,
  in rough order of how much they matter:
  **Scattering the scheduled runs** is the cheapest and largest. Every install
  proposed `06:00`, so a few hundred of them would start several hundred page
  loads on the same tick — a spike created entirely by a default, for no
  benefit. The minute is now derived from the hostname: scattered but stable,
  so opening the page twice shows the same time.
  **A rate ceiling** every navigation passes through (`internal/polite`, via
  `gotoPolitely`, all fifteen call sites), defaulting to ~4 requests/second —
  about three times looser than what the crawl does on its own. The looseness
  is the design: a limiter that binds during normal operation makes every
  future performance measurement a measurement of the limiter. Its job is to
  stop a *future* change speeding past a defensible rate by accident.
  **Backoff** when OPAL reports overload (429/503), easing off again on a clean
  response. A transport error is deliberately not treated as overload — backing
  off on flaky wifi would turn a bad network into an ever-slower sync.
  `docs/server-load.md` is the policy and is referenced from `CLAUDE.md`,
  including the part that has to be said out loud: this pulls directly against
  `docs/sync-speed-campaign.md`, and the distinction that matters is asking for
  *more things* versus asking for the *same things faster*.
  *Measured live, not assumed: `284 navigation(s), 0 delayed, 0s held in
  total`, on a run that took 226.9s against 211.9s and 223.4s unthrottled. An
  intermediate run measured 244.6s and briefly looked like the ceiling binding
  — the instrumented run settled it. The limiter counts its own interference
  and a scrape logs it, so this stays checkable rather than becoming folklore.*

- **The stalled-login reload watches the page instead of a clock.** Reported as
  "der refresh bei tu-fast braucht viel zu lange" — and that was a description
  of the design, not a tuning complaint. The old code waited a flat 45 seconds
  before reloading, whether or not anything was happening, so a TU-Fast that
  never fired always cost 45 seconds of staring at a page that was never going
  to move. It now reads the login page between short probes and reloads after
  **8 seconds of no change at all** — no navigation, no field being filled, no
  change in how many fields there are.
  **This also fixes a real bug in the old behaviour, not just its speed.** A
  human typing their password by hand stays on a login URL, which was the only
  thing the timer checked — so after 45 seconds it would reload the page and
  wipe what they had typed. A non-empty field now means somebody or something
  is working, and the page is left alone.
  The reading counts fields and how many are non-empty. It never reads their
  contents, and an unreadable page (closed, mid-navigation, evaluation failed)
  counts as activity rather than as a stall — acting on a reading that could
  not be taken is how a working login gets interrupted. The retargeting the old
  code did by hand for flows that open a new tab now falls out for free, since
  the active page is re-read every pass.
  *Mutation-tested: dropping the "nothing typed in it" condition fails the
  test that pins the wiped-password case. The DOM reading is verified against a
  real headless browser and a real Shibboleth-shaped form
  (`OPAL_SCRAPER_BROWSER_PROBE=1`), because a wrong type assertion there fails
  silently as "unknown", which reads as "busy" and would disable stall
  detection entirely.*
  **Not yet seen in the wild:** the stall itself has never been reproduced on
  demand, so the 8-second threshold is reasoned, not measured. If TU-Fast is
  ever observed taking longer than that to fire on a page it *does* eventually
  act on, the reload is harmless (it acts on the reloaded page) but the
  threshold is worth revisiting.

- **Course selection is one list now.** The maintainer's words were "es gibt so
  mehrere stellen und so weiter.. fühlt sich weird an", and they were right
  about the cause: a box of discovered checkboxes, a separate table of
  configured rows, a "+ Add course" button producing a third kind of thing, and
  the user left to join them up mentally. Every course now appears exactly once,
  with its tickbox and its folder on the same line, under three plainly-named
  actions ("Refresh this list from OPAL", "Add one by hand", "Fill in folders
  for me").
  **Unticking no longer deletes the row.** The old version did, which is why it
  had to refuse with an `alert()` when the row carried a folder override — it
  was protecting the user from a deletion it had chosen to do. Keeping the row
  greyed out removes the deletion, the alert and the special case: unticked rows
  are dropped when the form is submitted, and until then the decision is free to
  change. The wire format is untouched, so `parseSettingsForm` did not have to
  learn anything new.
  Also: choosing "pick specific courses" now fetches the list straight away
  instead of leaving a button to be found, and a failed automatic lookup reads
  as "log in first, then refresh" rather than as an error, because on a first
  run that is exactly what it is.
  *Verified in a real browser (`TestBrowserCoursePickerIsOneList`) and
  screenshotted. Mutation-tested: making submit keep the unticked rows fails
  it.*

- **Automatic sync got its own page.** The maintainer's read was that Settings
  is really folder configuration and a daily schedule is a different kind of
  thing; they offered "own page or fold it into sync options" and left the
  call. Own page — `/schedule` — because `/sync` is where you make something
  happen *now*, and putting "run every day at 06:00" beside a button that runs
  immediately invites exactly the mis-click it sounds like.
  The move also fixed something that was never a layout problem: Settings had
  **two independent forms with two save buttons**, one of which did not save
  the schedule and the other of which did not save the settings. And "Notify me
  if a scheduled sync fails" sat under a *Notifications* heading in the
  settings form, about a feature configured further down the page in the other
  form. It now saves with the thing it is about, under one save button.
  **A data-loss hazard came with it, and is pinned by a test.**
  `parseSettingsForm` rebuilds the config from submitted form fields, and an
  unchecked checkbox is indistinguishable from an absent one — so once the
  notification input left the settings page, reading it there would have
  silently switched the preference off *every time anyone saved their folder
  settings*. It is now carried over from disk, and
  `TestSavingSettingsDoesNotClearTheScheduledFailureNotification` fails if that
  regresses. This is the same shape as the invariant already flagged in the
  first-run journey notes below.
  *All five browser walks pass against the new route, and the page was
  screenshotted rather than only asserted on.*

- **Gave the tool real logging, and moved the developer chatter into it.**
  Raised by the maintainer relaying their father's point that a long-lived
  project needs logging with more than one layer. Until now there was exactly
  one channel — `fmt.Printf` to stdout — doing two unrelated jobs: talking to
  the person running the tool, and recording what a crawl did. It served
  neither. The user read text written for a developer, and the developer's text
  scrolled away, or was never visible at all, because the GUI runs as a window
  and nobody sees its stdout.
  `internal/logging` splits it on two axes rather than one: a **level** (how
  bad) and an **audience** (who it is for), because "skipping section" is a
  genuine warning *and* of no interest to a student who wants their slides. Two
  sinks read those independently — the console takes user-facing records plus
  every error, and a rotating file under `~/.opal-downloader/logs/` takes
  everything. `--verbose` (any command) adds diagnostics to the console;
  `--debug-clicks` implies it, since asking for a trace and not being shown it
  would be absurd. Built on stdlib `log/slog`, so no fourth dependency, with a
  printf-shaped facade because that is what every existing call site looks like.
  The scraper's 25 prints are routed by audience. The CLI's own `fmt.Println`
  results are deliberately **not** migrated: a CLI printing its results to
  stdout is already the user channel.
  **Two bugs the first real log caught, which no test would have.** The shared
  credential scrub redacts any 32+ character run of the base64 alphabet — and
  `/` is in that alphabet, so every OPAL URL collapsed to
  `https://bildungsportal.sachsen.[redacted]`. The section URL is precisely
  what `scripts/compare-visit-runs.ps1` needs to answer "which section lost the
  files", so the log was being stripped of the one field that makes it worth
  keeping. URLs are now held out of the scrub and put back with their query
  string dropped — path identifies a course node, query is where a jsessionid
  would live. Second: migrated messages kept their literal `Warning: ` prefix,
  which now doubled up against `level=WARN`.
  *Verified live against the real account: a `list` run wrote user lines to the
  console and diagnostics only to the file. Rotation is mutation-tested
  (reversing the backup shift fails it), as is the audience split.*

- **Rewrote the sync log for a user instead of a developer.** The maintainer's
  account is ~345 files of which almost none change, so a routine sync printed
  ~345 `skipped: course / file` rows and buried the handful of lines that say
  what the run actually did. Worse, the live status line named whichever file
  was being checked, so it sat on one arbitrary filename for minutes — which
  reads as a hang, and was reported as one (`hybrid_quicksort.ipynb`, the
  separate hang item below). Now an already-up-to-date file is counted, not
  listed: the status line shows a running "N files checked, M downloaded" total
  that visibly ticks, downloads and errors still get their own rows, and the
  closing summary is a sentence ("Everything was already up to date (345 files
  checked)") rather than `downloaded=0 skipped=345 errors=0`, which made a
  successful no-op look like a run that did nothing for an unclear reason.
  *Verified in a real browser (`TestBrowserSyncLogIsWrittenForAUser`) by
  publishing events into the real job and letting the real SSE stream drive the
  real JavaScript — a live sync takes minutes and cannot produce an error on
  demand. Mutation-tested: restoring the per-file rows fails it.*

- **Warn before settings edits are thrown away.** Reported by the maintainer
  (2026-07-26): change a field, click away, and it is gone with nothing said.
  Three layers, because no one of them covers every way out of a page — a
  persistent bar while anything is unsaved (the layer that actually helps: it
  removes the need to remember, rather than interrupting at the moment of
  leaving), a confirmation on in-page links (how the user navigates in the real
  WebView2 window, which has no address bar and no back button), and
  `beforeunload` for closing the window. Dirtiness is measured against a
  snapshot taken on load rather than "has anyone typed", so an edit that is
  undone leaves the page clean — a warning that cries wolf gets clicked
  through. Re-checked on a timer as well as on input, because every change this
  page makes in JavaScript (added rows, "Suggest folders", "Browse...") assigns
  `.value`, which fires no event and is invisible to a MutationObserver too.
  *Verified in a real headless browser (`TestBrowserUnsavedChangesWalk`) and
  screenshotted, since the last GUI bug here was invisible white-on-white text
  that every assertion passed through. Mutation-tested: removing the guard
  fails the walk.*
- **Stopped shipping mojibake, and made it detectable.** The sync page's
  preview hint rendered its em-dashes as three junk characters each; two more
  sat in `config.go`'s comments. A human found the first by reading the running
  program, which is the only way any of them could have been found — the damage
  is invisible in review, because the reviewer's terminal renders the broken
  bytes as the characters they were mistaken for. So the fix is a guard
  (`encoding_test.go`) rather than three edits: it scans git-tracked text for
  the lead characters that produce essentially all mojibake, in combinations
  that cannot occur in German or English. Tracked files rather than a directory
  walk — a plain walk also reads the gitignored `tmp/` dumps of real OPAL
  pages, 77 findings this repo cannot fix, which would have made the guard
  useless on its first run. *Mutation-tested both directions.*

- **Made the self-resume runner able to actually start a session.** It never
  once did. Reported by the maintainer (2026-07-26) as "hasn't worked so far";
  `.claude/queue/resume-runner.log` showed six `launch-failed` lines over two
  days, every one of them `%1 is not a valid Win32 application`. All five gates
  were correct — they decided a resume was warranted, and then the launch died.
  Cause: `Start-Process -FilePath "claude"` does not resolve a bare name the way
  the shell prompt does. The prompt walks PATHEXT and finds `claude.cmd`;
  Start-Process hands the raw string to the Windows loader, which takes the
  first PATH match by name — npm's extensionless POSIX shim, not a PE binary.
  A second bug was sitting behind it, never reached: the multi-line prompt was
  passed as a `-ArgumentList` argument, and a `.cmd` runs under cmd.exe, which
  ends its command line at the first newline. It would have delivered line one
  and tried to *execute* the rest — `--model sonnet` included, so the run would
  not even have been on the intended model. The prompt now goes over stdin,
  which has no quoting or newline rules.
  **Why it stayed invisible for two days:** the runner's only output is a log
  line, and nothing reads that file. `SessionStart` now reports unacknowledged
  `launch-failed` entries to the next interactive session, once each — a
  watchdog whose failures are silent is worse than none, because it looks like
  a working safety net.
  The tests were fully green throughout: every resume-runner assertion used
  `-DryRun`, which returns before `Start-Process` is reached. The launch path is
  now testable via an `OPAL_RESUME_CLAUDE_CMD` stub, and `-WhichClaude` lets the
  suite ask the runner what it would execute rather than reimplementing the
  resolution and asserting two copies of the same idea agree.
  *Verified live end-to-end: the real runner launched the real `claude`, which
  read its prompt over stdin and replied — run in an isolated `OPAL_RESUME_REPO_ROOT`
  so an unattended agent was not turned loose on the working tree. Both bugs are
  mutation-tested: restoring the bare `claude` fails the resolution assertion,
  and restoring the argument form fails four, with the stub capturing the prompt
  truncated at line one exactly as predicted. 90 hook assertions, `dev.ps1 all`
  green.*
- **Stopped the resume runner joining a session already working in the tree.**
  Found by the fix above working: the very next hourly fire launched a real
  unattended agent into this worktree while an interactive session was editing
  it. Two agents, one tree, no lock between them — the run was killed before it
  committed anything, tree clean. The existing gate only asked whether a
  *previous unattended run* was alive, which says nothing about a human's
  session. Now `budget-guard.ps1` stamps `.session-heartbeat.json` on every tool
  call and the runner skips while that stamp is under 20 minutes old.
  The obvious implementation — "is any `claude` process alive?" — would have
  deadlocked: the keep-warm process is permanently alive and idle, so it would
  have vetoed every launch forever. Stamping from the tool-call hook separates
  *working* from *running*, and a stamp that ages out means a session dying
  cannot wedge the runner shut. An idle open session is the accepted false
  negative; it isn't editing anything.
  *Verified live: the real runner now reports `a session is active in this tree
  (0m since its last tool call)` against this session's own heartbeat. Both
  directions mutation-tested — removing the gate fails the "won't launch" test,
  and making the heartbeat immortal fails the ages-out test — plus a third
  proving the stamp really comes from `budget-guard` on a healthy budget, where
  it returns early.*
- **Made stopping an unattended run actually stop it.** Same night, same
  incident, third bug: the recorded pid is the `cmd.exe` wrapper, not the agent.
  Killing it left `claude.exe` orphaned and still editing the worktree for five
  more minutes, and its changes landed in an unrelated commit before anyone
  noticed. `resume-runner.ps1 -Stop` now kills the recorded pid *and its
  descendants*, and says which.
  That orphan's own half-finished work was kept rather than reverted — it was
  sound (stop counting `**Blocked:**` backlog items as work an unattended run
  can do, so an all-blocked backlog no longer forces hourly relaunches with
  nowhere to go) — but it had been killed before writing a single test for a
  change to the gate that decides whether autopilot keeps running. That gap is
  now closed: `Get-BacklogItems` has its own tests, including that the real
  `docs/BACKLOG.md` still parses into items, since a formatting change that made
  it parse as zero would stop autopilot dead in silence.
  *Verified: the orphan-kill is mutation-tested by reverting it to a plain
  `Stop-Process`, which reproduces the incident exactly (`orphaned: 38980`).
  `Get-BacklogItems` is mutation-tested in both directions — never flagging
  blocked, and flagging on any mention anywhere in the body. 107 hook
  assertions.*
- **Made work resume by itself once the budget recovers.** Closes the
  "should a killed run restart itself?" question — the maintainer asked for it
  directly (2026-07-23) after being told the cost. An hourly Windows scheduled
  task runs `.claude/hooks/resume-runner.ps1`, whose five gates (off switch,
  already-running, 2h cooldown, budget rung, is-there-work) all run in
  PowerShell and cost **zero tokens**, so a quiet hour is free and a `claude`
  process starts only when all five pass. Unattended runs are bounded by
  construction: 5 autopilot iterations instead of 20, `--model sonnet`, and a
  cooldown so a run that dies on startup cannot become a relaunch loop.
  An in-session cron job was considered as a second layer and rejected: its
  only advantage is preserving this conversation's context, which after a kill
  costs more to resume than a fresh session reading `docs/RESUME.md`.
  Set up / inspect / remove with `scripts/register-resume-task.ps1`.
  **A deadlock nearly shipped here**, caught by the maintainer asking where the
  runner gets fresh numbers from: `rate-limit-status.json` is only written by a
  live session's status line, and this runner exists for when no session is
  running. Once both windows' `resets_at` pass, every reading is unusable — and
  that is exactly when the quota came back. Giving up there meant needing fresh
  numbers to justify starting a session, while only a session produces fresh
  numbers: it would have logged `refusing to guess` hourly, forever, silently.
  An unusable reading now forces a keep-warm sync and re-reads; a usable one
  never does.
  *Verified live: the real registered task was triggered and correctly logged
  `skip  budget not recovered` without spawning anything, and fired again on its
  own hourly schedule. `keepwarm -Force` tested for real — killed the stale
  process, resynced in 14s, file genuinely updated. The deadlock fix is
  mutation-tested: removing the refresh reproduces `refusing to guess` exactly.
  **The launch path was flagged unverified here, and was in fact broken** — see
  the entry above; "tests cover it only in `-DryRun`" was the whole problem, not
  a caveat.*
- **Watch the token budget during a turn, not just between turns.** A run was
  killed mid-turn by the 5-hour limit (2026-07-23) and left no trace;
  diagnosing it meant comparing commit timestamps against window-reset
  arithmetic. Every guard lived on the `Stop` hook — *between* turns — so one
  long turn ran past the budget unwatched, with 1–2 autopilot continuations
  used against a cap of 20. A usage-limit kill never reaches `Stop`, so none of
  the existing guards could ever have fired.
  Now: `budget-guard.ps1` (`PreToolUse`, every tool call) escalates advice as
  the budget floor climbs — commit, update `docs/RESUME.md`, and at the top
  rung no new subagents; `turn-failure-checkpoint.ps1` (`StopFailure`) records
  the kill and captures uncommitted work as a `refs/wip-checkpoints/` commit
  without touching the working tree; `SessionStart` hands the next session the
  failure record and the resume note, and won't arm a full autonomous stretch
  on a budget the `Stop` gate would veto immediately.
  It deliberately does **not** try to predict the limit — the data is a floor
  that can be an hour stale, and the one precise estimator attempted here was
  removed the day it was written for reporting 83.5% against a real 46%. The
  goal is that a kill costs one turn, not a session's train of thought.
  Two latent bugs fixed on the way: keep-warm's 42s cold-launch wait sat inside
  a 15s `Stop` hook timeout and was silently ending autopilot, and
  `rate-limit-gate.ps1` (now deleted, folded into `budget-guard.ps1`) had no
  freshness check and would gate on an already-rolled-over window.
  *Verified: `budget-guard` fired live at rung 3 on a real tool call during
  this work; 58 new assertions in `scripts/test-hooks.ps1`, now part of
  `dev.ps1 all`, and mutation-tested to confirm they fail when the code is
  wrong. **Unverified:** `StopFailure` has not been observed firing for real —
  that needs an actual API kill; tests drive the script directly via synthetic
  stdin, which covers everything except whether the harness invokes it.*
- **Set up the recurring review pass as an actual weekly cron**, not just a
  backlog note. A scheduled cloud routine (Monday 06:00 UTC) reviews only the
  commits since its own last run (tracked via `docs/last-review-commit.txt`),
  looks for correctness bugs and simplification opportunities in that diff,
  files genuine findings here, and commits/pushes directly — matching how
  this repo already operates. "Nothing to report" is treated as a fine
  outcome, not padded with invented findings. Maintainer confirmed the
  ongoing-cost tradeoff (a real recurring cloud-agent run against their Pro
  plan budget) before this was created rather than assuming it.
- **Stopped treating "another sync already running" as a scheduled-sync
  failure.** This closes what used to be the "blocked, needs evidence" sync-
  lock-contention item above: reported live again (2026-07-19, "PID 34084,
  4 seconds after another"), and reading the code showed the GUI's own "Sync
  now" job runs a sync in-process (same PID as gui.exe) using the identical
  `synclock` lock a scheduled run acquires - so this is routine overlap
  between the GUI and the daily trigger, not an incident, and there was
  nothing actionable for the user regardless of which process actually won
  the race. Added `statuslog.OutcomeSkipped`, distinct from `OutcomeFailure`,
  for exactly this case (`synclock.ErrHeld`); it's still recorded in the
  status file/history for diagnosis but no longer fires the failure toast or
  GUI banner. The rolling history log added earlier the same day turned out
  not to be needed to close this - the fix didn't require catching another
  occurrence, just correctly classifying the one already reported.
- **Fixed the tufast-setup page's inconsistent "Home" link** — every other
  page uses "&larr; Back", this one alone said plain "Home" with no arrow.
- **Decided: leave legacy manifest orphans inert, don't prune.** Checked the
  real manifest (2026-07-23): 26 entries still use the pre-migration
  absolute-path key scheme (`_2. Semester/...`, `_4. Semester/...`), matching
  the count from the original migration run. `delete(manifest.Files, ...)` is
  used exactly once in the whole codebase, immediately followed by
  re-inserting under the new key (a rename, not a deletion) — nowhere does
  the manifest ever forget an entry outright, for files removed from OPAL or
  otherwise. Adding a prune path would break that invariant for 26 dead JSON
  keys in a 370-entry file: no perf or correctness cost either way. Not
  revisiting unless the manifest's never-delete design changes for other
  reasons.
- **Set the scheduled task's working directory.** Task Scheduler launches an
  action with no working directory set to `C:\Windows\System32`, not the
  exe's own folder; every subcommand resolves `config.yaml` relative to the
  current working directory, so a scheduled run failed with `config file not
  found: C:\windows\system32\config.yaml` — caught live on the maintainer's
  machine (2026-07-23), even though the registered exe path itself was
  already stable (a different failure than the still-doomed-path repair
  logic below covers). *Verified live end-to-end: rebuilt, re-registered the
  real scheduled task, triggered it, watched it complete
  (`LastTaskResult: 0`, "2 downloaded, 342 skipped").*
- **Hid the pre-setup landing page's login-state box.** A first run with no
  config yet can't be logged in - there's no OPAL URL or credentials to log
  into - so "Not logged in yet" above the setup button was noise, not signal.
  Comes back automatically once a config exists.
- **Auto-arm autopilot on session start**, instead of requiring the marker
  file to be created by hand (in practice it rarely was, so autopilot rarely
  ran even for sessions opened correctly in this directory). Does not help a
  session opened outside this directory - see the "gates are absent" section
  above, unchanged.
- **Gave the dev-build update note its own neutral status-box style**,
  instead of reusing "up to date"'s green on the landing page or the
  error/warn red on `/update`.
- **Gate the `/sync` page's own Sync/List buttons on the same readiness check
  the landing page already applies**, instead of leaving them live when no
  config exists or nobody is logged in. *Verified via handler-level tests
  (exact rendered HTML/disabled state); not exercised in a live browser
  window - this sandbox can't run the native WebView2 binary.*
- **Repair a scheduled sync that points at a disposable binary**, instead of
  telling the user to repair it themselves. Finishes what #122 started: that
  one only stopped new doomed registrations being created. *The repair branch
  is unobserved in the wild — verified live only in its refusing-to-repair
  form, since triggering the repair means rewriting a real Task Scheduler
  entry.*
- **Suggest a per-course download folder**, now measured against a real
  account and tree: 6 of 6 course→folder mappings correct, after a first pass
  that got 0 of 6. Three fixes made the difference — excluding the tool's own
  `default_course_folder` dumping ground (it name-matches perfectly and
  shadowed the real folders), and two tie-breaks for folders a name cannot
  separate (the `…/Downloads` convention, then recency, so this semester's
  "Analysis" beats last semester's). *A stranger's naming is still only as
  good as these signals; the thresholds are tuned to one real tree.*
- **#124** Reload a login page TU-Fast has not acted on, instead of waiting
  out the full timeout. *The stall itself was never reproduced; the reload
  branch is unobserved in the wild.*
- **#123** Verify files OPAL reports no size or date for by comparing bytes,
  instead of assuming they are unchanged. Closes the second half of the
  never-updating-file bug.
- **#122** Refuse to register a scheduled sync against a disposable binary.
- **#121** Discover courses so they can be ticked in setup, not typed.
- **#120** Don't treat a recycled PID as a running sync.
- **#119** Report what the crawl is doing while it runs.
- **#118** Put a primary "Sync now" action on the GUI start page.
- **#117** Heal manifest entries that carry no size/modified signal. First
  half of the never-updating-file bug.
