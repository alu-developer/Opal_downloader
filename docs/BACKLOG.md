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

### Sync speed: still ~5 minutes, maintainer says unacceptable
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

Still to walk *in the browser*: course selection, status, and changing a
setting afterwards — same WebView2 sandbox limitation noted elsewhere in
this file (e.g. the sync-page-buttons entry below) applies here too. Also
deliberately not run via `gui`/`main.exe gui` in this sandbox: `Run` always
calls `openNativeWindow` unconditionally on Windows, which would pop a real,
visible window on the maintainer's actual desktop with no warning - not
something to trigger from an unattended session.

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

### Decide: should a killed run restart itself? (needs the maintainer)
A turn killed by the usage limit is now cheap to lose — it is recorded, WIP is
captured, and the next session is handed `docs/RESUME.md` (see "Done recently").
What is still missing is *restarting*: the maintainer has to open a session,
which then immediately knows where it was.

Closing that gap means something outside the session spending budget
unattended — a scheduled headless `claude` that wakes after the quota resets
and picks the work back up. That is real recurring spend against a Pro plan, so
it is the maintainer's call, not a decision to make while they are away. The
previous answer here (a recurring `CronCreate` job, documented in the operating
model until 2026-07-23) is not a substitute: cron jobs are session-only and
only fire while the REPL is idle, so they cannot rescue the one case that
matters.

---

## Done recently

Newest first. Trimmed periodically — git history and PR bodies are the real
record.

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
