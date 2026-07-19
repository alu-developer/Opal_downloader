# Operations Guide

This project is browser-automation heavy and depends on external website structure.

## Long-term checklist

- Keep Go and module dependencies updated regularly.
- Run CI checks on every pull request.
- Reinstall Playwright browser binaries after major updates.
- Keep `config.yaml` and session-state files out of version control.
- Keep selectors in scraper code reviewed when OPAL UI changes.
- Re-run login when the saved OPAL session expires.

## Suggested maintenance cadence

- Weekly: `scripts/dev.ps1 all`
- After touching `internal/scraper/` or `internal/syncer/`: `opal-downloader smoke-check`
  - a local, read-only, on-demand check (reuses the saved session, no
    credentials involved) that catches crawl/sync regressions right away
    instead of waiting for a human to notice or for the next scheduled run's
    failure banner. Add `--full-sync` to also test real file-download
    reachability (into a disposable scratch directory, never your real
    `download_path`). See `internal/smokecheck`'s package doc comment for
    what it checks and why.
- Monthly: dependency updates (`go get -u ./...`) and smoke sync run
- Semester start: validate course discovery and download selectors
- Periodically (or after README/config changes): `scripts/test-fresh-install.ps1`
  to re-validate the new-user setup flow (clone through `init`, no OPAL
  credentials needed). See [docs/setup-friction.md](setup-friction.md) for
  known friction points and [docs/manual-setup-checklist.md](manual-setup-checklist.md)
  for the credential-requiring login/sync tier.

## Per-subfolder download destinations

By default every file in a course downloads flat: `<download_path>/<course>/<file>`.
Three optional `config.yaml` settings (added in PR #19) let you split a
course's downloads into a subfolder per OPAL section (e.g. "Vorlesung",
"Übungen") and even redirect one specific section to an arbitrary path
outside `download_path` entirely - useful if, say, lecture slides for one
course should land directly in a folder you already sync elsewhere (Dropbox,
OneDrive, a shared drive).

These are editable both directly in `config.yaml` and, since this feature,
in the GUI Settings page (`opal-downloader gui` -> Settings -> "Subfolder
organization").

- **`use_section_subfolders`** (bool, default `false`) - the master switch.
  When `false` (the default), the other two settings below are parsed but
  have **no effect** - both the CLI (on every `status`/`list`/`sync`) and
  the GUI Settings page print/show a warning if they're set while this is
  off, so a misconfiguration doesn't fail silently.
- **`section_folder_names`** - maps an OPAL section-name pattern (same
  glob/substring matching as `course_folders`) to the local folder name to
  use instead of OPAL's own section wording. Sections that don't match any
  pattern keep OPAL's own (sanitized) name.
- **`subfolder_destinations`** - maps `"<course pattern>/<subfolder
  pattern>"` to an arbitrary destination path (can be outside
  `download_path`). Both halves of the key are matched independently, using
  the same pattern rules as `course_folders`.

### Worked example

Given this course structure in OPAL - course "Analysis I" with sections
"Vorlesung" and "Übungen" - and this `config.yaml`:

```yaml
use_section_subfolders: true

section_folder_names:
  "Übungen": "Exercises"

subfolder_destinations:
  "*Analysis*/*Vorlesung*": "D:/Elsewhere/AnalysisSlides"
```

- Files from the "Vorlesung" section of any course matching `*Analysis*` go
  to `D:/Elsewhere/AnalysisSlides/<file>` (the `subfolder_destinations`
  override wins, bypassing `download_path` and `section_folder_names`
  entirely for that one section).
- Files from "Übungen" go to `<download_path>/Analysis I/Exercises/<file>`
  (renamed via `section_folder_names`, still under the normal course
  folder).
- A section not covered by either map, in a course not covered by
  `subfolder_destinations`, falls back to
  `<download_path>/<course>/<OPAL section name>/<file>`.

If `use_section_subfolders` were left at its default `false` here, both
`section_folder_names` and `subfolder_destinations` would be ignored and
every file would land flat under `<download_path>/Analysis I/<file>` - this
is exactly the misconfiguration `opal-downloader status`/`list`/`sync` and
the GUI Settings page now warn about.

See `config.example.yaml` for the same fields with inline comments, and
`internal/config/config.go` (`ResolveSectionFolderName`,
`ResolveSubfolderDestination`, `Warnings`) for the resolution/validation
logic.

## Concurrency model: how many `sync`/`list` processes can run at once?

**Any number of `sync`/`list` processes can run concurrently, as long as
each has already finished establishing its session** (loading a still-valid
saved session, or completing an interactive login) - each process's crawl
runs against its own private, throwaway headless browser/context with no
shared state after that point. **Exactly one process at a time may be
inside session-establishment** (checking the saved `session_state_file`,
and - if that's missing/expired - launching the interactive-login browser
against the shared `~/.opal-downloader/login-profile`, waiting for
login, saving state, and relaunching headless) for a given real
profile+state-file pair; a second process reaching that phase concurrently
now blocks (waiting on a cross-process lock, see
`internal/scraper/session_lock_windows.go`) rather than racing a second
browser launch against the same profile directory. If the first process's
session-establishment doesn't finish within 6 minutes (`login`'s own wait
for interactive/TU-Fast login allows up to 5 minutes), a second process
waiting on the lock gives up and returns a clear "another opal-downloader
process appears to be logging in..." error instead of hanging forever.

Before this was enforced (fixed 2026-07-16, queue task
`fix-login-window-stays-open-during-crawl`), this was **not actually
enforced** despite `isUserDataDirLocked`'s pre-flight check existing - that
check was a TOCTOU race (two processes could both observe "not locked"
before either had launched Chromium), and a separate race existed between
one process's `saveState()` write and another's concurrent read of the same
`session_state_file`. Both were live-reproduced against the real shared
profile on 2026-07-16 (see the queue task and
`docs/browser-profile-strategy.md`'s matching dated section) before being
fixed.

## Recurring-incident pattern: fixes merged without a live end-to-end watch

**2026-07-16: a "done" fix recurred because it was never actually watched
happen live.** PR #66 (`investigate-sync-list-not-headless`, merged
2026-07-14) added the headless-relaunch-after-interactive-login logic in
`ensureSession`, but was merged with its own PR comment flagging that the
live end-to-end scenario (real expired session -> real interactive login ->
confirm the browser window actually closes and the crawl proceeds headless)
was never actually observed - only unit-tested. Two days later the
maintainer reported the exact symptom that fix was supposed to prevent (a
persistent visible browser window during a crawl), filed as
`fix-login-window-stays-open-during-crawl`. Investigating that recurrence
found the real root cause was a different, previously-undocumented bug (the
cross-process session-establishment race described above), not a defect in
PR #66's own logic - but the point stands: an unverified "done" is
indistinguishable from a real fix until something re-triggers it, and by
then the original context (why it was risky, what wasn't checked) has to be
reconstructed from scratch. **If a queue task's acceptance criteria call
for a live/manual check that genuinely can't be done in the current
environment, say so explicitly (`UNVERIFIED:` prefix, stated plainly in the
PR) rather than merging as if it were checked** - this is already this
project's stated policy, but is repeated here as the concrete incident that
shows what skipping it costs.

**2026-07-19: 4th report of the same symptom, confirmed as stale build, not
a recurrence.** The maintainer reported (2026-07-17, via `/task-capture`) a
browser window visible throughout a crawl again, hours after PR #80 (merged
2026-07-17 same morning) had been thoroughly live-verified. Two hypotheses
were investigated (queue task
`investigate-visible-crawl-window-build-freshness-and-gui-scheduled-combo`):

1. *Stale build* - at report time the maintainer's `main.exe` predated
   PR #80 by 5 days.
2. *GUI/synclock gap* - whether `internal/gui/sync.go`'s GUI-triggered sync
   actually acquires PR #82's `internal/synclock` overlap guard, or only
   the CLI `--scheduled` path does.

Hypothesis 2 was ruled out by reading the code: `synclock.AcquireDefault`
is called from `internal/syncer.SyncCoursesWithProgress` itself (added in
PR #82's original commit `2c44314`), which is the function both the CLI
`sync` command and the GUI's `handleStart`/`runJob` call - there was never
a code path where GUI sync skipped the lock. This was then live-confirmed:
a real GUI sync (started via `/sync/start`, real account, real session)
held the lock while a concurrent `sync --scheduled` was started from the
CLI - the scheduled run failed fast with `Error: a sync is already running
(PID ..., started at ...)` (exit code 4) instead of racing it, exactly as
designed.

Hypothesis 1 was then tested directly: rebuilt from current master
(`c7c4dd0`, includes PR #91/#92) and ran that real GUI sync (6 real
courses, 168 files discovered) with process-window monitoring throughout
(`Get-Process | Where MainWindowTitle -ne ""`, checked 8+ times spanning
~5 minutes of discovery/crawl and the lock-contention moment above) -
zero visible browser windows at any point; only the app's own intended
native WebView2 window ("Opal Downloader") and title-less
`chrome-headless-shell`/`msedgewebview2` helper processes ever appeared.
**The symptom did not reproduce on a confirmed-current build** - stale
build was the cause, consistent with hypothesis 1, and no code change was
needed. See PR
`queue/investigate-visible-crawl-window-build-freshness-and-gui-scheduled-combo`
for the full investigation. One process note for next time: building this
repo's binary is `go build -o main.exe .` from the repo root (the actual
`package main`) - `go build ./cmd/opal-downloader` silently builds the
`opaldownloader` library package instead (produces a Go archive, not a
runnable `.exe`); this cost time during this investigation before being
caught.

## Incident playbook

If sync suddenly returns too few files:

1. Run `opal-downloader list` and compare expected course count.
2. Re-authenticate with `opal-downloader login`.
3. Check OPAL page changes and update selectors in `internal/scraper/scraper.go`.
4. Run one forced sync: `opal-downloader sync --force`.

### "Course crawled successfully but found 0 files" / a course is missing from `Found N course links`

**Check `course_concurrency` in `config.yaml` first, before re-investigating
from scratch.** This exact pair of symptoms (a course reporting 0 files
despite genuinely having content, or a whole course vanishing from
discovery) was root-caused (PR #64/#65, live-tested against the real TU
Dresden account) to an AJAX-render race specific to *concurrent* course
crawling - not to a per-section retry bug. `course_concurrency=3` (the old
default) silently lost 21% of files across 2 whole courses; `=5` lost 76%.
The code default is `course_concurrency=1` (serial) since PR #65, and queue
task `fix-course-level-crawl-flakiness` (2026-07-13) re-confirmed this live:
three consecutive `list --dev --profile --debug-clicks --course-concurrency
1` runs against the real account produced byte-identical per-course file
counts every time (341 files, same 8 courses discovered, same 7
content-bearing courses), with zero section-level Goto/extraction failures
in any run.

**A config.yaml with an explicit `course_concurrency: 3` (or higher) silently
overrides the safer code default** - `internal/config/config.go`'s `Load()`
only substitutes `DefaultCourseConcurrency` when the field is unset or
non-positive, so an old config file written before PR #65 keeps running at
the data-lossy concurrency level even though a fresh/default config
wouldn't. This was found live during the above investigation: the
maintainer's own real `config.yaml` still had `course_concurrency: 3`. If
you're chasing this symptom, check the actual `course_concurrency` value in
the config file being used (not just the code default) before assuming it's
already at the safe setting.

If flakiness genuinely reproduces at `course_concurrency=1` (not just at a
higher, already-known-unsafe value), that is a *new* finding distinct from
the above and needs fresh live investigation - don't assume it's already
covered by this section.

**Update (2026-07-13, queue task fix-concurrent-crawl-ajax-race-and-raise-
concurrency): the race at `course_concurrency>1` was root-caused further and
substantially (not fully) fixed - don't assume "check course_concurrency,
done" is still the whole story.** Four distinct bugs behind it were found
and fixed: a fixed-duration per-section content wait that could elapse
before OPAL's AJAX-rendered content actually finished under concurrent
contention; that fix's own initial version (stop at one non-growing read)
still not being enough because OPAL/Wicket renders a section in stages and
one non-growing read can land on a false plateau between stages (fixed by
requiring several *consecutive* non-growing reads); a "show all" pagination
click timeout too short under concurrent load; and simultaneous multi-tab
creation worsening contention (now serialized/staggered). See
`DefaultCourseConcurrency`'s doc comment in `internal/config/config.go` for
the full writeup and exact code pointers. These fixes hold up in
light-to-moderate concurrent load, but **repeated live full-account
re-tests still intermittently lost a small number of files (~1-2% of 341,
down from the original 21-76%) at `course_concurrency` 2 and 3** when the
account's one much-larger/slower course (198 files, several minutes) was
crawling concurrently with smaller courses that have paginated sections -
so the default stays at 1. If you're chasing this symptom on a config with
`course_concurrency>1` explicitly set, that residual risk is real and
expected, not necessarily a new regression; compare against a
`course_concurrency=1` serial run to confirm before deep-diving further.

**Update (2026-07-17, queue task close-residual-concurrent-crawl-ajax-race):
PR #81 (2026-07-16) replaced the per-section content wait with a
MutationObserver-based debounce (a genuine load-completion signal, not a
timing guess - see `waitForInteractiveLinks`'s doc comment in
`internal/scraper/navigation.go`) and got a clean 344/344 on one live test
at `course_concurrency=2`. That single clean run was not conclusive on its
own, since the residual race documented above was itself intermittent (2 of
4 repeated runs in PR #78's own sample, not every run). Repeating the same
live methodology (four consecutive `list --dev --debug-clicks --profile
--course-concurrency 2` runs against the real account, serial ground truth
345 files/7 content-bearing courses) found the race is still open - and
this sample was worse than PR #78's: all 4 of 4 runs lost files (337, 339,
331, 333 of 345), always from the same two previously-flagged courses
(Analysis, Softwaretechnologie) plus Algorithmen und Datenstrukturen once,
never a different one, and always the same lost-file count for a given
course when it was lost (not randomly varying).**

**The MutationObserver wait is confirmed NOT the failure point**: across
all 4 runs it resolved via "settled-no-mutations" every time (zero
hard-cap fallbacks, zero injection-failure fallbacks), and all 5 "show
all" pagination clicks per run succeeded on the first try in every run,
including the runs that still lost files. The loss happens downstream, in
`waitForStableSectionContent`/`candidateStabilityPoll`
(`internal/scraper/crawl.go`) - left deliberately unchanged by PR #81. Those
polls were observed settling on a *stable* read that was nonetheless short
of the serial ground truth (`sectionContentMaxPolls=20` was never exhausted
in any run, ruling out "poll budget too short" as the mechanism) - the
section's own client-side render plateaus incomplete under concurrent
contention. `DefaultCourseConcurrency` stays at 1; see its doc comment in
`internal/config/config.go` for the full run-by-run breakdown.

**Update (2026-07-17): `--debug-clicks` now also writes a persistent JSONL
log**, not just stdout. Every investigation into this race so far (PR
#73/#78/#81/#84 above) had to re-run a full live crawl to get fresh raw data,
and even then only ever reported aggregate stats (poll counts, hard-cap
frequency) in the PR/task file, not the actual per-poll/per-wait trace - the
raw data that would answer "was the count still climbing when the poll gave
up, or flat from the start" was always thrown away with the terminal session
that produced it. `internal/scraper/audit.go`'s `EnableDebugLogFile` (wired
automatically whenever `--debug-clicks` is passed - see
`cmd/opal-downloader/root.go`) now writes the same lines `auditLog` prints to
stdout into a timestamped file at `~/.opal-downloader/debug-logs/debug-
<timestamp>.jsonl` too (path printed at the start of the run). Deliberately
global, not under a config's `download_path`, since live-verification runs
routinely point at a scratch/throwaway `download_path` that gets discarded
after the test - a log that followed it would get lost with it. Files
accumulate across runs; cleanup is manual (delete old ones), same as
`internal/visitlog`'s cross-run log. Any future investigation of this race
(or anything else needing a click/wait trace) should read these files
instead of re-deriving aggregate stats from a fresh live run and discarding
the trace again.

Separately, `internal/scraper/crawl.go`'s `collectCourseFiles` and
`internal/scraper/discovery.go`'s `discoverCourseLinks` were both hardened
(same task) so a section/source-page whose Goto or extraction fails outright
no longer looks identical to a genuinely empty/complete result: a course
where *every* attempted section failed now surfaces as a distinct crawl
error (see `sectionsVisited`/`sectionsFailed` in crawl.go) instead of "0
files, crawled successfully", and a discovery source page now retries once
and logs a warning instead of silently dropping its courses from the list.

**Update (2026-07-19, queue task
fix-candidate-stability-poll-concurrent-crawl-race): the race is now
root-caused and fixed - `DefaultCourseConcurrency` is 2.** PR #84's writeup
above named `candidateStabilityPoll` as the culprit but only ever traced
`waitForStableSectionContent` (the main per-section content poll after a
`Goto`) - correctly cleared, but the wrong call site. Adding equivalent
`--debug-clicks` logging to `waitForStableExpandedCandidates` (the "show
all" pagination EXPANSION poll, run after a *click* rather than a `Goto` -
see `expandShowAllInSection`, `internal/scraper/crawl.go`) exposed the real
mechanism on the first re-run that reproduced the loss: every lost
section's expansion poll read the exact same pre-expansion candidate count
on every single poll from #1 onward - flat from the start, never partway
through growing - while a same-window serial run's main-content-poll
numbers matched the concurrent run's exactly, section for section. The
trigger, confirmed across 12 "show all" clicks over 3 live concurrent runs
with a perfect 12/12 correlation: every lost section's post-click
`waitForInteractiveLinks` call had hit the recoverable "execution context
was destroyed" condition and retried; every section whose post-click wait
did *not* retry kept its full count. Mechanism: a click's AJAX response is
tied to the JS execution context live when the click fired; unlike a `Goto`
(whose own destroyed-context event is an expected side effect of navigating,
and whose response already contains the complete content), if OPAL/Wicket
tears that context down under concurrent-tab rendering contention before
the click's response is applied, the response has nowhere left to land and
the expansion is silently dropped for good - no amount of further polling
recovers content that was never going to arrive, which is why
`sectionContentMaxPolls`/`showAllExpansionMaxPolls` never showing
exhaustion (PR #84's finding) didn't rule this out.

**Fix**: `expandShowAllInSection` now re-issues the "show all" click
whenever its own post-click content-settle wait hits this signal (the
direct-navigate branch, which doesn't share this failure mode, is
untouched). **Live-verified byte-for-byte clean across 6 repeated
full-account runs** - 4 at `course_concurrency=2`, 2 at
`course_concurrency=3` - all matching a same-day serial baseline (339/339
files); one of the concurrency=2 runs directly caught the fix engaging
(the same two sections that had lost files in every prior unfixed run this
task tested hit the retry signal, got re-clicked, and came back with their
full correct counts).

**Wall-clock caveat**: on this account (one ~200-file course dwarfing the
other five), `course_concurrency` 2/3 measured the *same or slower*
wall-clock than serial in these verification runs (516-551s at
concurrency=2, ~500s at concurrency=3, vs. 312s serial) - the wider
MutationObserver/stability-poll budgets used at concurrency>1 are a fixed
per-section cost paid whether or not that section was ever actually
contended, and with the crawl dominated by one course regardless of worker
count, concurrency's parallelism has little left to win back. Anyone
opting into `course_concurrency>1` expecting a speed win should verify that
holds for their own course-size mix; see `DefaultCourseConcurrency`'s doc
comment in `internal/config/config.go` for the full numbers.

### "response is HTML" download failures — course "2026 LA20" no longer reproduces (2026-07-19 investigation)

Queue task `investigate-2026-la20-session-sensitive-html-response-failures`
followed up on a residual gap PR #89 (`fix-html-response-download-
fallback-failures`) flagged but didn't chase: during that task's
2026-07-17 live verification, *every* file in course "2026 LA20" failed
its fast-path GET and the browser-click fallback with "response is HTML,
browser fallback click did not find downloadable link" when LA20 was
crawled as part of a full multi-course run, but succeeded cleanly when
LA20 was crawled alone. This is a different failure shape from the
show-all gap PR #89 fixed (LA20 has no "show all" pagination at all), and
was suspected - but never confirmed - to relate to the session-wide Wicket
AJAX download mechanism PR #63/#35
(`fix-fast-path-download-history-counter`, see `download.go`'s and
`download_refresh.go`'s doc comments) somehow being sensitive to other
courses' requests sharing the same session.

Live reproduction (2026-07-19, current master, includes PR #89-#94): a
real forced `opal-downloader sync --force --debug-clicks --profile`
against the maintainer's actual 6 configured courses (LA20 plus the other
5, `course_concurrency: 1`, `download_concurrency: 3` - matching the real
`config.yaml` verbatim), redirected only to a disposable scratch
`download_path` so the real synced folders/manifest weren't touched.
**Result: LA20 did not fail at all.** Every one of its ~39 files
downloaded successfully - a normal mix of clean fast-path hits and a
handful of browser-fallback retries, same shape as any other course - with
zero errors, while running alongside the other 5 courses in the same
browser session exactly as the original report's scenario. (One unrelated
single-file failure was observed elsewhere in the same run,
`Algorithmen und Datenstrukturen/Vorlesung_9_10.pdf`, same generic
"response is HTML" message - a single file, not a whole-course failure,
and not LA20; out of scope for this task and not chased further given the
cost of another live cycle.)

**No code change made - closing as "does not currently reproduce," which
this task's own acceptance criteria calls out as a valid outcome.** The
exact root cause was never conclusively isolated (the same trap PR #89
and PR #35 both flagged: reproduction costs a full live account cycle, and
repeated probing risks perturbing the very session state under
investigation, so this task budgeted for one confident live attempt, not a
bisection across historical commits). The most plausible - but unconfirmed
- explanation is that one or more fixes that landed since the 2026-07-17
report incidentally stabilized whatever session-side condition was
corrupting LA20's requests: PR #89's own show-all-click retry, and/or PR
#94's fix for a click-AJAX-response-dropped-under-concurrent-tab-
contention race. Neither is a confirmed match - PR #94's race is a
discovery-time mechanism (concurrent-tab JS execution-context teardown)
distinct from LA20's symptom (download-time, fast-path GET/counter-refresh
failing outright), and LA20 itself has no show-all sections for PR #89's
fix to touch - so treat this as circumstantial, not verified.

If this symptom recurs: check first whether it's isolated to one course
again (matching this exact shape) before re-investigating from scratch -
a fresh regression is more likely to spread across several courses. Reuse
`~/.opal-downloader/debug-logs/*.jsonl` from the failing run (see the
`--debug-clicks` JSONL update above) rather than re-deriving raw data from
a second live run. One build gotcha hit again during this investigation,
already documented above (PR #93) but worth repeating since it cost real
time twice now: `go build -o file.exe ./cmd/opal-downloader` silently
builds the `opaldownloader` library package (a Go archive, not a runnable
`.exe`) - build `go build -o file.exe .` from the repo root instead.

### Chromium fails to launch with "the application has failed to start
### because its side-by-side configuration is incorrect"

Root-caused 2026-07-13 on the maintainer's machine: `%LOCALAPPDATA%\ms-playwright`
(Playwright's default browser install directory, which `playwright.Install()`
and `playwright.Run()` both used implicitly via `PLAYWRIGHT_BROWSERS_PATH`
being unset) had silently become an NTFS junction into an unrelated packaged
app's private storage (`...\Packages\<pkg-id>\LocalCache\Local\ms-playwright`)
- created by that app's own sandboxing, not by opal-downloader, OPAL, or
anything the user did. Launching `chrome.exe` through that junction failed
with the SxS error every time; an identical byte-for-byte copy of the same
`chrome-win64` folder placed in a plain (non-redirected) directory launched
fine. So this was never Chromium/Playwright corruption or a real Windows
SxS/WinSxS problem - it was the browser's install directory sitting behind a
reparse point that broke the OS loader's assembly-manifest resolution for
that specific process.

Fix (`internal/scraper/session.go`'s `EnsurePlaywrightBrowsersPath`, called
from both `launchBrowser` and `runSetup` in
`cmd/opal-downloader/root.go`): default `PLAYWRIGHT_BROWSERS_PATH` to
`~/.opal-downloader/ms-playwright` whenever the user hasn't already set that
env var themselves, instead of leaving it to playwright-go's own default of
`%LOCALAPPDATA%/ms-playwright`. This sidesteps `%LOCALAPPDATA%` entirely, so
it doesn't matter whether that specific directory is ever redirected again
by some other app's sandboxing/virtualization on a given machine.

If this resurfaces (same SxS error, any machine): check whether
`~/.opal-downloader/ms-playwright` (or `$PLAYWRIGHT_BROWSERS_PATH` if the
user has set it explicitly) itself is a reparse point/junction/symlink
before assuming a Chromium/Playwright regression - `Get-Item <path> -Force`
in PowerShell shows `LinkType`/`Target` if it is one. Re-running
`opal-downloader setup` reinstalls the browsers at whatever path is
currently active.
Neither failure path was live-triggered during this task's testing (OPAL was
stable across all 3 runs), so this hardening is defensive/untested-live, not
a fix for a reproduced bug - it closes a gap the acceptance criteria called
for regardless.
