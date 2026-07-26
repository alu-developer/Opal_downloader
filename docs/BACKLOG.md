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

---

## Now

### Your daily sync is probably losing files right now (`course_concurrency: 2`)
Found while baselining the section-concurrency work (2026-07-26), against the
real account:

| run | files | Analysis | wall clock |
|---|---|---|---|
| `course_concurrency: 1` | **345** | 30 | 227.9s |
| `course_concurrency: 2` | **336** | 21 | 228.2s |

Two is **nine files short and not faster** — 0.3s apart, inside noise. The live
`config.yaml` is set to 2, so real syncs have been quietly missing Analysis
files. Nothing warns: the run reports success.

This contradicts the "Concurrency SOLVED" entry below and the
`DefaultCourseConcurrency = 2` decision it justified. It is *not* a new
discovery so much as a re-appearance: `docs/sync-speed-campaign.md`'s
2026-07-17 entry recorded the same course losing the same sort of count
("Analysis: -8 files in 3 of 4 runs").

**Not acted on yet, deliberately.** One run each. Changing a default on a
single pair of runs is exactly the mistake the campaign log keeps recording, so
this needs a repeat — but it should be repeated *soon*, and if it holds, the
default and the maintainer's config should both go back to 1, since 2 is
currently paying files for no speed at all.

### Sync speed: still ~5 minutes, maintainer says unacceptable
**Blocked:** the one unexplored axis (section-level concurrency) is a rewrite
of the crawl's concurrency model in the most correctness-sensitive part of
this codebase, with a documented history of *silent* file loss from past
concurrency changes - it needs the maintainer's explicit sign-off before being
built (see the last entry in `docs/sync-speed-campaign.md`). Nothing else
here is unblocked without new evidence; re-measuring or re-arguing already-
rejected approaches wastes a round trip.

Standing goal (2026-07-21, `docs/sync-speed-campaign.md`): a routine no-op
sync should feel instant, target ~30s. That file is the full decision log -
read it before touching this, several plausible-looking approaches (HTTP-fast-
path discovery, hash-based change caching, an OPAL notification signal) were
already built and live-tested against the real account, and are rejected for
concrete, measured reasons. Re-litigating those without new evidence wastes a
round trip.

Re-measured 2026-07-23 against the real account: 334.1s no-op sync, of which
discovery/crawling is ~322s (96.5%) and one course alone
(Softwaretechnologie, 160 of the account's 284 total sections) is 168s of
that. One free fix already applied: the live config still had
`course_concurrency: 1`, a stale value from before the campaign raised the
default to `2` - bumped to match.

One real, unexplored axis identified and written up in the campaign log:
section-level concurrency (parallelizing *within* a course's BFS crawl, not
just across courses) - every rejected/shipped attempt so far only ever
touched course-level concurrency or a change-detection signal. Not attempted
yet: it's a rewrite of the crawl's concurrency model in the most correctness-
sensitive part of this codebase (repeated history of *silent* file loss from
concurrency changes), so it needs the maintainer's sign-off before being
built, and the campaign's own rule (byte-for-byte against the known 344-file
ground truth, multiple runs) before being trusted at any level.

### Dogfood the whole first-run journey
**Blocked:** all four decisions below shipped on 2026-07-26 (first-run
introduction, "List courses" renamed, scheduling walked, picker explained),
plus one bug that only looking could find. What is left is the maintainer
opening the GUI and saying whether it now reads well — their eyes, not a test.
Everything an agent can check here is checked.

The four questions this was blocked on were answered by the maintainer on
2026-07-26. Their decisions, now delivered:

1. **A first run needs a real introduction.** Not just an unhidden picker — the
   start / no-courses-configured state should actually explain what to do.
   ("nein, es sollte beim start/nicht konfigurierten Kursen eine gute
   Einführung geben.")
2. **Rename "List courses".** The name is wrong twice over: it costs a full
   crawl, and listing courses is not really what it does. ("Ich meine, das
   macht es ja auch nicht.")
3. **Making it faster stays the dream, not this task.** The maintainer's
   position: past attempts were hard, but they believe it is possible — that is
   the sync-speed item above, which still needs their explicit sign-off before
   the section-level concurrency rewrite is attempted. Not folded in here.
4. **Walk scheduling.** Approved ("jo, mach mal"), including that it registers
   a real scheduled task on their machine.

Drive the GUI as a real first-time user — no config, through setup, login,
course selection, a sync, status, scheduling, then changing a setting — and
write down everything broken, confusing, or annoying. Findings get fixed if
trivial, filed here if not.

Explicitly from the perspective of a TU Dresden student who is *not* the
maintainer: a stranger's first run is in scope.

**First pass done (2026-07-23), no-credentials part only:** a fresh binary in
an empty folder, no config, walked through landing/settings/TU-Fast/sync in a
real browser. All findings from this pass are now fixed (see "Done recently"
below) except one deliberately left open: the "three status boxes" finding
led to hiding the login-state box before setup (it's meaningless with no
config to log into), but the update-check box was kept — knowing whether
you're on a stale binary seemed worth the one extra box regardless of setup
progress, and that's more a taste call than the login box was. Revisit if it
still feels cluttered.

**Scheduling/login/sync exercised for real (2026-07-23), but not through the
GUI:** fixing the scheduled-task working-directory bug (see "Done recently")
required actually triggering the real Windows Task Scheduler task against
the live account, which incidentally exercised login (interactive relogin
path), a real sync (2 downloaded, 342 skipped), and the scheduled-run path
end to end. That's real signal the underlying mechanics work, but it's not
the same as clicking through the GUI as a stranger would.

**The journey is now a permanent test (2026-07-26), not a one-off probe:**
`internal/gui/first_run_journey_test.go` walks no-config → landing → settings
form → course selection → save → landing/sync moving on → changing a setting
afterwards, against the real handlers and a real `config.yaml` in a temp dir.
Every other test in the package hits one handler with a prebuilt config; what
was missing was each step's on-disk result being the next step's input.

**Finding from writing it — a real structural fragility, now pinned.**
`parseSettingsForm` rebuilds `course_folders`, `subfolder_destinations` and
`section_folder_names` from the submitted rows *alone*; nothing in the handler
preserves them. The only thing standing between a returning user and silent
data loss is that `GET /settings` renders those rows as real server-rendered
form controls, which a browser then resubmits natively. That invariant was
load-bearing and completely untested — a template change dropping a `value=`
attribute would have made every save quietly wipe the user's mappings. It is
the same shape as the incident below, which is what made it worth looking for.
*Mutation-tested: removing one `value="{{$row.Key}}"` fails the test.*

**The browser walk is no longer blocked (2026-07-26).** The obstacle was
recorded as "the WebView2 window can't be driven here", but the window is only
a viewer for a plain local HTTP server, and the repo already ships Playwright's
Chromium. `internal/gui/browser_walk_test.go` serves the real mux over
`httptest` and drives it with headless Chromium: same pages, same JavaScript,
no window on anyone's desktop. Opt-in (`OPAL_GUI_BROWSER_WALK=1`), following
`internal/scraper`'s probe precedent, since a fresh clone has no browsers
installed; ~4s when it does run.
`Run`'s route table moved into `newMux` so the walk exercises the real routes -
wiring handlers up by hand in a test passes happily when a route is registered
at the wrong path or not at all.

**This is what the handler-level pass could not see.** Course selection is
largely JavaScript: "+ Add course" and "Find my courses" build their
`course_row_name[]` inputs client-side, so those rows exist in no
server-rendered HTML. *Mutation-tested, and the result is the argument for
keeping it: renaming the JS-created input to `course_row_name` (a row that
silently never submits) fails the browser walk and leaves the handler-level
journey test green.*

**First-run finding, not a bug:** with no config, `config.Load` defaults to the
wildcard course list, so "Sync all courses" renders checked and the whole
course picker is hidden behind it. Syncing everything is a reasonable
low-friction default, but a stranger who wants specific courses has to guess
that unticking a checkbox reveals the picker. Pinned in the walk as intended
behaviour rather than changed unilaterally - worth a maintainer's opinion.

**Every page in the nav now loads in a real browser too** (`/`, `/settings`,
`/sync`, `/tufast-setup`, `/update`, `/feedback`): HTTP 200, no uncaught
JavaScript on load, a heading, and a link home. That last one matters more than
it looks - the real window is WebView2 with no address bar and no back button,
so a page without a link home is a dead end the user cannot leave.
*No findings: all six were already clean. Mutation-tested by removing
`/feedback`'s back link, which fails the check.*
One thing worth knowing rather than fixing: `/sync` opens its SSE progress
stream on load and holds it, so the page never reaches network-idle even when
nothing is syncing. That is the page working; a walk that waits for idle there
times out on a healthy page.

**The live half is now covered too** (`internal/gui/live_list_walk_test.go`,
opt-in via `OPAL_GUI_LIVE_LIST=1`): "List courses" clicked in a real browser
against the real account, driving the parts the other walks stop short of —
reusing the saved session, the background job, the SSE progress stream, and the
result rendering. *Verified live: 6 courses reported, 345 remote files
discovered, and the run asserts the real session file is byte-identical
afterwards.* It reads the real `config.yaml` but writes its own in a temp dir
and works on a **copy** of the session state, so the 2026-07-23 config-wipe is
not repeatable here — there is no real path for it to write to.

**Finding: "List courses" is not a quick list.** It crawls every section of
every course, costing the same as a sync's discovery phase — measured at 210s
and 482s in two runs on the maintainer's account. The button sits next to
"Sync" and reads like a cheap lookup, so a user clicking it to see what is
there waits minutes with no warning. Worth either renaming, warning up front,
or serving from the dashboard listing rather than a full crawl; which of those
is a product call, so it is filed rather than decided.

**Scheduling walked (2026-07-26), and it turned up a hazard in the walks
themselves.** Rendering `/settings` calls `applyScheduleStatus`, which can
*write* to Task Scheduler during a page render (`repairDoomedSchedule`
re-points a registration whose executable looks doomed). A GUI test's
`os.Executable()` is a binary in the temp directory — so on a machine whose
task happened to look doomed, the browser walks committed earlier today would
have re-pointed the maintainer's real daily sync at a binary deleted seconds
later. It did not happen, because their task points at a stable path and the
repair never fired. That is luck. All GUI tests now stub the scheduler seams.

The walk itself (`TestBrowserSchedulingWalk`) ticks the box, sets a time,
saves, reloads, and unticks — asserting the toggle reflects the *scheduler's*
state rather than its own, since it is re-queried on every render. It runs
against stubs on purpose: `scheduler.TaskName` is a single global constant and
the maintainer's live daily sync is registered under it, so a real enable would
overwrite that task and a real disable would delete it — **the disable path has
no guard at all**. That is worth knowing independently of testing.

What *is* checked against the real Task Scheduler is the refusal
(`TestSchedulingRefusesToRegisterADisposableBinaryForReal`, opt-in via
`OPAL_GUI_LIVE_SCHEDULE=1`): enabling from a disposable binary must fail before
writing anything. *Verified live: the refusal rendered, and the real
registration was byte-identical before and after — still 08:00, same
executable.* Deliberately **not** mutation-tested: the mutation is "register
anyway", which would overwrite the maintainer's real scheduled sync. The
stubbed walk is mutation-tested instead (dropping the disable call fails it).

**Fixed a bug that only looking could find:** the secondary buttons on the
settings page — "Browse...", "+ Add course", "Suggest folders", "+ Add rule" —
rendered white text on a near-white fill, i.e. invisible. `pageStyle`'s base
`button` rule sets `color:#fff` for the blue primary buttons, and those class
rules overrode only `background`, leaving the inherited white. Every assertion
in the package passed throughout, because the markup was never wrong. Found by
screenshotting the page and reading the image. *Mutation-tested: dropping the
colour again fails the new test.*

Still genuinely unverified: nothing here is a human *looking* at the pages.
The walk asserts structure and behaviour, not that the result reads well, and
it runs headless so it would not catch a purely visual break. Also still not
run via `gui`/`main.exe gui`: `Run` calls `openNativeWindow` unconditionally
on Windows, which would pop a real window on the maintainer's desktop with no
warning - not something to trigger from an unattended session.

**Handler-level pass done (2026-07-23)** against the real account instead:
stood up the real mux/handlers over `httptest`, hit them with plain HTTP.
Landing, settings, and sync pages all render correctly; course discovery
against the real account found 8 courses (2 more than the 6 in the current
`courses` filter - "[WS25/26] Programmierung" and "Helfende DMS" - not a bug,
just means the account has courses the config doesn't track, which is
expected/normal). No UX issues found at this level, though this still isn't
the same as watching a stranger actually click through it.

**Incident during this pass, already resolved:** the probe's own settings-
POST round-trip check briefly wiped the real `course_folders`,
`subfolder_destinations`, and `use_section_subfolders` in the live
`config.yaml` (a bug in the probe's crude form-scraper, which only
resubmitted a handful of hardcoded field names and silently dropped the
`name="...[]"` array-style rows those settings actually use) — caught
immediately via a manual backup taken before the run, restored exactly, no
lasting damage. Confirmed by reading `settings.go`'s real POST handler
(`r.PostForm["subfolder_dest_key[]"]` etc.) that the actual shipped
settings page is NOT vulnerable to this: its rendered `<input name="...[]">`
rows are real, server-rendered form controls that any normal browser
submission includes natively, JS or not - this was purely an artifact of the
probe's own incomplete field list, not a product bug. The probe file was
deleted rather than fixed, since a settings-round-trip check isn't valuable
enough to justify keeping a known-hazardous test around.

---

## Next

(nothing queued right now)

---

## Done recently

Newest first. Trimmed periodically — git history and PR bodies are the real
record.

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
