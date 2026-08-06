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
_Nothing unblocked. Question 25 is next but currently blocked — see Noticed._

---

## Next
_Nothing queued._

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
  (2026-08-04).** Full write-up in `docs/opal-webdav-student-access.md`,
  including a send-ready letter to BPS. Two loose ends worth remembering: (a)
  OPAL hands *students* a token-authenticated personal RSS feed covering
  subscribed folders — a possible cheap change-detector that needs no browser;
  (b) if BPS ever answers the letter, the answer belongs in that doc.

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
- **Question 20 closed inconclusive, and said so plainly** (2026-08-04):
  raising the Wicket signal-wait ceiling to 15000ms produced 3 clean
  contention runs in a row (248/248/248 files) — but at this condition's
  ~33-50% historical failure rate that is not proof of "pure delay", just a
  plausible outcome either way. The diagnostic tool
  (`OPAL_WICKET_SIGNAL_TIMEOUT_MS_OVERRIDE`) is built and reusable; what's
  missing is an actual latency distribution, not another blind batch —
  Question 21, deliberately spread across more than one cycle to bound
  today's live server load (6 contention crawls already).
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
