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
this file (e.g. the sync-page-buttons entry below) applies here too; this
sandbox can only verify those at the handler/HTTP level, not by actually
watching the native window.

---

## Next

### A cheap recurring review pass
Roughly weekly: a correctness review plus a simplification pass, scoped to
what changed since last time so cost stays flat. Output lands here as backlog
items, not as an unread report. Keep it light — a heavy ritual gets skipped.

---

## Blocked / needs evidence

### Scheduled sync failed with "a sync is already running" 4 seconds after another started
Ruled out so far: duplicate scheduled tasks (one task, one trigger), a
double-acquire inside one run (a single lock call site exists), the
smoke-check path (its own subcommand, not invoked by `sync --scheduled`), and
a recycled PID (that was a real bug, fixed in #120, but the 4-second gap means
a genuinely fresh lock rather than an inherited number).

Remaining hypothesis: two real processes starting ~4s apart — the GUI and the
scheduled run colliding, or the task being launched twice by something outside
Task Scheduler's trigger list.

**Blocked because it cannot be reproduced.** The schedule has since been
re-registered, and the scheduled-run status file keeps only the single most
recent run, so there is no history to mine. **Do this first:** add a small
rolling log of scheduled-run outcomes. Otherwise the next occurrence is
equally un-diagnosable.

---

## Done recently

Newest first. Trimmed periodically — git history and PR bodies are the real
record.

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
