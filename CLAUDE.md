# opal-downloader

Go program that logs into OPAL (Bildungsportal Sachsen, Shibboleth/TU-Fast
SSO) via Playwright browser automation, discovers course files by scraping
the DOM (no OPAL API/WebDAV — WebDAV PROPFIND was tried and dropped, see
`docs/webdav-propfind-research.md`), and syncs them to a local folder with
an incremental manifest. Ships as a single binary with two front ends: a
local web GUI (`internal/gui/`, launched by default when run with no
subcommand — see "GUI" in the open questions below) and a CLI (`init`,
`setup`, `status`, `login`, `list`, `sync`, `dump-links`) for scripting/
automation.

## Before editing this file

Ask the maintainer before making any change to this file — including
corrections you're confident about, and edits prompted by a queue-task
finding. Don't auto-apply edits here without asking first.

## Project philosophy: ease of use is the top priority, and other
non-negotiables

Friction reduction — both first-install and long-term/maintenance friction —
outranks almost everything else. The maintainer is open to ideas that
reshape the whole project (setup/distribution model, browser-profile
strategy, even walking back past architecture decisions) if they
meaningfully cut user or maintainer effort. Don't self-censor structural
proposals as "too big" for what's currently a small, mostly-single-maintainer
tool — surface them. See `docs/setup-friction.md`, `docs/installer-plan.md`,
and `docs/browser-profile-strategy.md` for the current state of this effort.

Beyond ease of use, these constrain every future decision regardless of
which specific initiative is being worked on:

- **Local-only tool.** Everything runs on the user's own machine. No
  opal-downloader-operated backend/cloud service exists today, and none is
  planned — not ruled out forever on principle, but no current need has been
  identified that would justify one.
- **Credentials and session data never leave the machine unscrubbed.**
  OPAL/Shibboleth login state, session cookies, and browser-profile data are
  handled entirely locally. Crash/error reporting is welcome and encouraged
  (it supports the reliability principle below) — but any report generated
  or transmitted must first be scrubbed of credentials, session
  tokens/cookies, and other sensitive data. This is a carve-out for
  crash/error diagnostics specifically, not a green light for general usage
  analytics or behavioral tracking, which stays out of scope.
- **Reliability over features.** The tool should not crash. A missing file
  or a selector break is an incident to fix (see `docs/OPERATIONS.md`), not
  an acceptable steady state. When trading off "ship a new capability" vs.
  "make the existing path more robust," robustness wins by default.
- **Safety matters but isn't the top principle.** The concrete safety bar
  that does matter: protect the user's login credentials/session data (see
  above). Beyond that, safety is balanced against ease-of-use and
  reliability, not treated as an automatic trump card.

## Who this is for

Three overlapping groups, in order of directness but not necessarily
priority:

1. **The maintainer** (personal use, TU Dresden).
2. **Other TU Dresden students.**
3. **Other Bildungsportal Sachsen users more broadly** — other Saxon
   institutions on the same OPAL platform.

Whether "Bildungsportal Sachsen users" means one shared OPAL instance (TU
Dresden's) or covers multiple differently-branded/configured OPAL
deployments across Saxon institutions is **not yet known** — see "Open
questions" below. Until resolved, don't assume the DOM-scraping selectors
need to be institution-generic; also don't assume they don't.

This scope directly affects how much onboarding/documentation friction is
worth eliminating — a stranger's first-run experience matters, not just the
maintainer's own repeat use.

## Open questions (future direction)

Genuinely undecided — don't build as if any of these were settled. Where a
doc under `docs/` has a recommendation but the maintainer isn't fully
committed to it yet, it's an open question here even if the doc reads
confidently.

1. **Browser/login strategy.** `docs/browser-profile-strategy.md` leans
   toward a dedicated second, never-copied profile
   (`~/.opal-downloader/login-profile`) as the future default over today's
   real-profile approach — see `docs/HISTORY.md` for why a literal
   *copy*-based profile doesn't work and how the second-profile finding
   differs from that. Not locked in: whether the real-profile path stays a
   permanent opt-out, or how strongly the dedicated profile is pushed,
   depends on how well it holds up once tried more.
2. **GUI vs. CLI, long-term.** Whether the CLI is kept indefinitely (for
   power users/automation/scripting) or eventually thinned/deprecated is
   **not decided** — keep the CLI fully functional until an explicit
   decision says otherwise.
3. **Multi-institution scope.** Does "Bildungsportal Sachsen users" (above)
   mean one shared OPAL instance, or multiple differently-configured OPAL
   deployments that would need generic/configurable scraping selectors?
   Unknown — needs investigation, not a guess.
4. **Local vs. cloud, long-term.** A cloud/server component is not ruled out
   on principle if a real need ever appears, but none has — don't propose
   one speculatively.

## Package layout

- `cmd/opal-downloader/root.go` — entry point, a plain `switch` over
  `os.Args[1]` (no CLI framework/Cobra — three direct Go deps total:
  `playwright-go`, `x/text`, `yaml.v3`). Subcommands: `init`, `setup`,
  `status`, `login`, `list`, `sync`, `dump-links`, `gui`. Running with no
  subcommand at all launches the GUI (see "Open questions" above). Also has
  a hidden `__panic-test` subcommand (intentionally left out of
  `printHelp`) that just panics on demand, so the panic-recovery wrapper in
  `Execute` can be live-verified without a real bug.
- `internal/gui/` — the local web GUI: HTTP server, settings page
  (read/write `config.yaml`), login trigger, sync/list/dump-links page with
  live progress.
- `internal/scraper/` — Playwright-driven browser automation. Split by
  concern: `session.go` (launch/login/auth-state), `profile.go` (browser
  profile handling, see "Key design decisions" below), `discovery.go` (find
  course cards on the dashboard), `crawl.go`/`navigation.go` (walk
  sections/subfolders within a course), `files.go` (extract downloadable
  file links per section), `download.go` (fetch a file), `course_filter.go`
  (match configured course names), `orchestrator.go` (ties
  discovery+crawl+download into one call).
- `internal/syncer/` — manifest-based incremental sync on top of
  `scraper.OpalScraper` (`SyncCourses`, `ListAvailableCourses`), diffing
  remote files against `.opal-sync.manifest.json`.
- `internal/config/` — `config.yaml` loading, validation, and `Save` (backs
  up the existing file before overwriting).
- `internal/timing/` — instrumentation for the perf-benchmark work
  (see `docs/OPERATIONS.md`/queue history for context).

## Key design decisions worth knowing before touching this code

- **Browser profile is opened directly against the user's real Chrome/Brave
  profile** (`browser_user_data_dir`/`browser_profile_directory` in
  config.yaml) — there is no working copy, and a copy-based approach does
  not work (HMAC integrity breakage). A dedicated *second*, never-copied
  profile is a live, promising direction currently being pursued as the
  friendlier default — see "Open questions" above. Full PR-by-PR history
  (PR #6, #15, #20, the HMAC finding, the second-profile finding): see
  `docs/HISTORY.md`. Practical implication either way: `login`/`sync`/`list`
  require a real (not remotely-hosted) browser profile on the user's own
  machine with the browser closed — see "Login/session automation" below
  for what that does and doesn't rule out for automated verification.
- **Session reuse**: `login` does an interactive Playwright login once and
  persists storage state (`session_state_file`); `sync`/`list` reuse it and
  only fall back to interactive login when it's expired.
- Course/file discovery is pure DOM scraping with heuristic selectors
  (`isBoilerplateCourseTitle`, `looksLikeFileLink`, `looksLikeShowAllControl`,
  etc. in `discovery.go`/`files.go`) — expect to need selector fixes whenever
  OPAL's UI changes. See `docs/OPERATIONS.md` for the incident playbook.

## Login/session automation

Login completes end-to-end (fixed and live-verified in
[PR #20](https://github.com/alu-developer/Opal_downloader/pull/20), see
`.claude/queue/done/fix-login-crash-and-missing-tu-fast.md`). **When TU-Fast
is installed and working in the real profile, it completes the
Shibboleth/2FA exchange itself — no human click is required.** The browser
window opens, TU-Fast drives the login, and `ensureSession` just waits for
the post-login course list to appear (up to 5 minutes). A human is only
needed if TU-Fast *isn't* working (not installed/enabled in that profile,
expired credentials inside TU-Fast itself) or the browser profile is locked
(another Brave instance open) — both of those surface as a clear
error/timeout, not a silent requirement.

This means `login`/`sync`/`list` are **not** inherently limited to
human-attended runs. They do need a real `browser_user_data_dir` on *this*
machine with Brave closed (profile copies don't work — see "Key design
decisions" above and `docs/HISTORY.md`), so this can't be exercised on a
remote/cloud machine without that real profile — but a `queue-run` agent
running locally (including in a `.claude/worktrees/` worktree — that's still
the same physical machine, not a remote sandbox) has everything it needs and
should just attempt the real command. Only report a criterion as unverified
if a live attempt actually failed, hung, or timed out — not merely because
the login/session path is involved. Verified `sync`/`list` runs are
especially cheap to attempt: they reuse `session_state_file` in a fresh
headless browser with no Brave/TU-Fast involved at all when the saved
session is still valid.

## Distribution direction: installer + in-app updates (not built yet)

Two structural decisions the maintainer has committed to as the intended
direction, even though neither is implemented yet — this is *the way I
want it to be, but it's currently just developing* (maintainer's words,
2026-07-09). Don't re-litigate these when picking up related work; treat
them as settled targets to build toward, gated only on the prerequisites
listed.

- **Update mechanism: T2 ("prompted, one-click apply")** is the committed
  v2 update mechanism — see `docs/update-mechanism-plan.md` for the full
  tradeoff analysis (T1/T2/T3) and design sketch. Version-embedding
  (`buildVersion` / `-X ...buildVersion=`, PR #37) and the release workflow
  are both done as of `add-release-build-workflow`
  (2026-07-10): `.github/workflows/release.yml` triggers on `v*` tags,
  builds the installer via `scripts/build-installer.ps1`, and publishes
  `opal-downloader-setup.exe` (unversioned name) plus a `.sha256` sidecar
  as GitHub Release assets — see `docs/installer-plan.md` Section 9 Task 4
  and `docs/update-mechanism-plan.md` Section 6 Task 2 for the full
  writeup and the tag/asset-naming conventions (`vX.Y.Z` tags, unversioned
  asset name).
  - **`internal/updater` package built (2026-07-10, task
    `add-internal-updater-package`)**: `CheckLatest(ctx, currentVersion)`
    hits the unauthenticated `GET
    https://api.github.com/repos/alu-developer/Opal_downloader/releases/latest`,
    parses `tag_name`, does a hand-rolled `.`-split integer comparison
    against the `currentVersion` the caller passes in (no import of
    `cmd/opal-downloader.buildVersion` — that would be a cycle), and picks
    the `opal-downloader-setup.exe` / `opal-downloader-setup.exe.sha256`
    assets by exact name match, matching release.yml's real (unversioned)
    asset shape. `Download(ctx, url, destPath)` streams the asset to disk;
    `VerifyChecksum(path, expectedSHA256)` and `VerifyChecksumSidecar(path,
    sidecarBytes)` check a SHA-256 digest, the latter parsing release.yml's
    exact sidecar format (`Get-FileHash`'s one-line, no-trailing-newline,
    two-space-separated `<hex>  <filename>`). Fully unit-tested against
    `httptest.Server` (no live GitHub/network dependency, no
    Playwright/browser dependency). See `internal/updater/updater.go`'s doc
    comment and `Example` in `updater_test.go` for the intended call
    pattern.
  - **GUI wiring built (2026-07-10, task
    `add-gui-update-checker-ui`)**: `internal/gui`'s `Run()` launches one
    `go srv.checkForUpdateOnce(ctx)` goroutine right after the listener
    starts (a one-shot check per process start, not a ticker — this is a
    short-lived local tool, not a daemon), caches the result on the
    `server` struct (`updateMu`/`updateResult`/`updateChecked`, same
    pattern as `loginActive`), and the landing page gets a second
    `.status` banner (same pattern as the login-state banner) plus a
    dedicated `/update` page when a newer release is available. `POST
    /update/start` downloads the asset and checksum sidecar via
    `internal/updater`, verifies the checksum, and only then attempts the
    installer hand-off.
  - **Installer hand-off finding: `cmd.Start()` can fail with "the
    requested operation requires elevation," and the code now handles this
    instead of assuming success.** The original design (start the
    installer, then unconditionally render "this app will close now" and
    exit) was live-tested with a stand-in installer executable and hit
    exactly this failure: launching an unsigned/unmanifested downloaded
    .exe from a non-elevated process can trigger Windows's
    installer-detection heuristic, which returns `ERROR_ELEVATION_REQUIRED`
    from `CreateProcess` outright when there's no interactive
    desktop/session available to show a UAC consent prompt (rather than
    showing that prompt, which is what would happen on a real interactive
    desktop session if the real `opal-downloader-setup.exe` requests
    admin — Inno Setup's default `PrivilegesRequired=admin` unless
    configured otherwise). The fix: `handleUpdateStart` now calls
    `launchInstaller` (wraps `exec.Command(path).Start()`) *before*
    rendering any response, and only renders "closing now" + calls
    `exitProcess` (the `os.Exit(0)` step) if that succeeded; a failure
    renders a real error page naming the downloaded-and-verified installer
    path so the user can run it manually, and leaves the GUI process
    running. The detach-and-survive-parent-exit mechanic itself (a process
    started via `.Start()` and never `.Wait()`'d does outlive the parent's
    `os.Exit`) was separately confirmed live with a plain manifested Win32
    executable.
  - **Follow-up (2026-07-10, queue-review live desktop verification): the
    elevation concern above does not apply to this project's real
    installer, and the full handoff has now been live-verified end-to-end.**
    `installer/opal-downloader.iss` sets `PrivilegesRequired=lowest` (installs
    per-user to `%LocalAppData%\Programs\...`, no admin needed) — the
    "requires elevation" failure only reproduced with an unmanifested
    stand-in exe that Windows's installer-detection heuristic assumed needed
    admin. Serving a real build of the `.iss` script from a local fake
    GitHub API and clicking "Download & install" in the actual native GUI
    window downloaded it, verified its checksum, and launched the real Inno
    Setup wizard with no UAC prompt at all, confirmed by the maintainer on
    their own desktop. Also found and fixed in the same pass: a "dev"
    `buildVersion` (unreleased/local builds) surfaced a raw "not a parseable
    numeric version" error on `/update` while the landing page simultaneously
    claimed "Running the latest version" - both now show a distinct, honest
    "Update checks are unavailable for development builds" message instead.
- **Installer bundles Chromium** — `docs/installer-plan.md` Section 3
  originally decided *against* bundling (to keep `setup.exe` small and
  avoid an assumed version-sync tax), but that call was reversed
  2026-07-09: the "sync tax" argument didn't actually hold up (it applies
  identically whether Chromium is fetched at install-time or bundled at
  build-time — bundling only moves *when* the fetch happens, not
  *whether*), and installer size in the low hundreds of MB was judged an
  acceptable one-time cost against removing a second internet-dependent,
  silently-failable step from first install. See that section's "Why this
  was revisited" for the full reasoning. No installer exists yet at all
  (still planning-only), so this hasn't been built either.

## Local task queue workflow (`.claude/queue/`, gitignored)

## Local task queue workflow (`.claude/queue/`, gitignored)

This repo uses `task-capture` / `queue-run` / `queue-review` (global Claude
Code skills) to queue and autonomously execute work. Conventions specific to
this repo:

- Queue-run-created branches are named `queue/<task-slug>` — no `fix-`,
  `perf-`, `feature/` prefixes for anything that came out of the queue.
- PRs are **never merged automatically unless every acceptance criterion was
  actually verified** — see the skill for the full rule. An `UNVERIFIED:` PR
  means a live attempt genuinely failed/hung/timed out, not that the task
  merely touched the login/session path — see "Login/session automation"
  above.
- **A `.claude/queue/done/` task means the work was completed and PR'd — it
  does not mean the PR is merged into `master`.** Several `done/` tasks
  (installer script, `internal/updater`, release workflow, ldflags version
  injection, and others) currently have no trace in `master` at all —
  check `gh pr list` / `git log` before treating a "done" task's changes as
  actually available to build on.
- Worktrees/branches are cleaned up by queue-run's housekeeping step once
  their PR merges or closes. If you see worktrees under `.claude/worktrees/`
  or stray `worktree-agent-*`/`agent-*` branches piling up, that step didn't
  run — don't manually pile up more, run `/queue-run` or clean up by hand.
  (A stray worktree at `.claude/worktrees/agent-aa47db6e61c2ebd5f` was
  observed 2026-07-10 and hasn't been cleaned up yet.)
- `.claude/queue/` itself is intentionally gitignored (task files may
  contain credentials/local paths) — its content is local-only and won't
  survive a fresh clone.
- **A task's `## Result` is not the end of the line if it changes or
  refines something this file already asserts** (most likely candidates:
  "Key design decisions," "Login/session automation," the non-negotiable
  principles, or "Open questions" above). Since `.claude/queue/` is
  gitignored, a finding written only in a task file effectively disappears
  once that task's branch/worktree is cleaned up — nobody reading this file
  later would know to look for it. Before moving such a task to `done/`,
  update the relevant section in the same PR/commit (or, for
  `queue-review`-driven decisions with no PR, commit the edit directly) so
  the finding survives independently of the queue.
  `investigate-independent-second-profile-for-login.md` (done 2026-07-08)
  was a miss on this — its HMAC/second-profile finding sat undocumented
  here until manually caught later; don't repeat that.

## Maintenance

- `scripts/dev.ps1 all` — local build/vet/test/lint, run before any PR.
- `scripts/test-fresh-install.ps1` — validates the no-credentials setup path
  (clone through `init`); see `docs/setup-friction.md` for known friction.
- `docs/manual-setup-checklist.md` — manual checklist for the
  credential-requiring tier (`login`/`list`/`sync`).
- `docs/OPERATIONS.md` — maintenance cadence and incident playbook.
