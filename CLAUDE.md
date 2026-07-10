# opal-downloader

Go CLI that logs into OPAL (Bildungsportal Sachsen, Shibboleth/TU-Fast SSO)
via Playwright browser automation, discovers course files by scraping the
DOM (no OPAL API/WebDAV — WebDAV PROPFIND was tried and dropped, see git
history), and syncs them to a local folder with an incremental manifest.

## Project philosophy: ease of use is the top priority

The maintainer has explicitly said friction reduction — both first-install
and long-term/maintenance friction — outranks almost everything else, and
that they're open to ideas that reshape the whole project (setup/distribution
model, browser-profile strategy, even walking back past architecture
decisions) if they meaningfully cut user or maintainer effort. Don't
self-censor structural proposals as "too big" for a single-maintainer tool —
surface them. See `docs/setup-friction.md`, `docs/installer-plan.md`, and
`.claude/queue/todo/plan-default-browser-profile-strategy.md` for the current
state of this effort.

## Package layout

- `cmd/opal-downloader/root.go` — Cobra CLI entry point (`init`, `login`,
  `list`, `sync`, `--dev`, `--force`).
- `internal/scraper/` — Playwright-driven browser automation. Split by
  concern: `session.go` (launch/login/auth-state), `profile.go` (browser
  profile handling, see below), `discovery.go` (find course cards on the
  dashboard), `crawl.go`/`navigation.go` (walk sections/subfolders within a
  course), `files.go` (extract downloadable file links per section),
  `download.go` (fetch a file), `course_filter.go` (match configured course
  names), `orchestrator.go` (ties discovery+crawl+download into one call).
- `internal/syncer/` — manifest-based incremental sync on top of
  `scraper.OpalScraper` (`SyncCourses`, `ListAvailableCourses`), diffing
  remote files against `.opal-sync.manifest.json`.
- `internal/config/` — `config.yaml` loading, validation, and `Save` (backs
  up the existing file before overwriting).

## Key design decisions worth knowing before touching this code

- **Browser profile is opened directly against the user's real Chrome/Brave
  profile** (`browser_user_data_dir`/`browser_profile_directory` in
  config.yaml) — there is no working copy. An earlier design copied the
  profile into a private working copy at `~/.opal-downloader/browser-profile`
  so the user's everyday browser could stay open (PR #6, #15), but that
  approach was dropped in
  [PR #20](https://github.com/alu-developer/Opal_downloader/pull/20):
  Chromium's `Secure Preferences` file is HMAC-integrity-protected
  specifically to detect externally-copied extension state, so copying it
  into a new user-data-dir gets TU-Fast's permissions reset/dropped by
  Chromium on load — confirmed by live testing, with no viable way found to
  relax that protection. `session.go`'s `launchBrowser` now launches
  Playwright directly against the real `browser_user_data_dir`, with a
  pre-flight `isUserDataDirLocked` check (`profile.go`) that returns a clear
  "please fully close Brave first" error instead of a crash if another
  Chromium instance already holds it open. Practical implication:
  `login`/`sync`/`list` require the real profile on the user's own machine
  with the browser closed — see "Login/session automation" below for what
  that does and doesn't rule out for automated verification.
  - **Follow-up finding (2026-07-08,
    [investigate-independent-second-profile-for-login.md](.claude/queue/done/investigate-independent-second-profile-for-login.md)):**
    the HMAC problem above only breaks a profile that was *copied* from an
    existing one. A brand-new, never-copied `browser_user_data_dir` (e.g.
    `~/.opal-downloader/login-profile`, manually set up once by installing
    TU-Fast from the Chrome Web Store and logging into OPAL/Shibboleth
    directly inside it) is self-consistent from creation and works
    end-to-end — live-verified: `login` completes with no HMAC/extension-drop
    issue, `list` reuses the saved session headlessly, and the user's real
    Brave profile stays open and usable throughout. No code change needed —
    `browser_user_data_dir`/`browser_profile_directory` already support
    pointing at this second profile. **Not yet the default** — this is not
    wired into `init`/onboarding/docs anywhere; a dedicated second profile
    only exists if a user (or dev) sets one up by hand per the steps in that
    task file. Whether to make this the recommended/default setup is an open
    decision tracked in
    [plan-default-browser-profile-strategy.md](.claude/queue/todo/plan-default-browser-profile-strategy.md).
  - **Re-litigation attempt (2026-07-09, task
    `revisit-copy-based-browser-profile-approach`): PR #20's verdict still
    holds, re-confirmed live.** The maintainer asked whether the HMAC block
    could be worked around (e.g. by recomputing the MAC after copying)
    rather than accepted as final. Findings:
    - **Reconfirmed on this exact machine**: copying the real
      `Default/Secure Preferences` + `Default/Extensions` + `Local State`
      into a fresh directory and launching stock Brave against the copy
      still strips TU-Fast down to `{"active_bit":false,"allowlist":2}` (from
      a full `active_permissions`/`explicit_host`/`scriptable_host` entry) the
      moment Chromium loads it, and adds a `prefs.preference_reset_time`
      marker proving Chromium detected and reset an unverifiable preference.
      Same machine, same user, same Chrome build — so this isn't a
      cross-machine artifact, it reproduces purely from the directory move.
    - **Is the MAC theoretically forgeable?** Published research (the CANS
      2020 paper "HMAC and 'Secure Preferences': Revisiting Chromium-based
      Browsers Security", plus documented red-team techniques for forging
      extension entries) shows the HMAC seed is a hardcoded constant baked
      into `resources.pak` at build time (not a true per-install secret) and
      the "device ID" input is derived from machine-local values (e.g.
      NIC MAC addresses) via an algorithm Chromium doesn't expose as a
      public API but that has been reverse-engineered. So in principle a
      tool could recompute valid per-extension MACs and a valid
      `super_mac` after copying a profile, by extracting that build's seed
      and reimplementing the device-ID algorithm.
    - **Why this isn't a viable angle for this project anyway**: both of
      those inputs are undocumented Chromium/Brave implementation internals,
      not a stable contract — the seed's location/offset in `resources.pak`
      and the device-ID derivation are not guaranteed across Chrome/Brave
      versions, and Google has previously changed this exact mechanism
      specifically in response to published forgery techniques like the one
      above. Building opal-downloader's login path on reverse-engineered
      binary internals that can silently break on any Brave auto-update
      would trade one fragility (the current "close Brave first" real-profile
      requirement) for a strictly worse one (silent extension-permission loss
      after an unannounced Brave update, with no clear error). **Verdict
      unchanged: PR #20's direct-launch approach remains the design.** No
      code changed as a result of this task; see that task's PR for the
      full write-up.
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
human-attended runs. They do need the real `browser_user_data_dir` on *this*
machine with Brave closed (profile copies don't work, see above), so this
can't be exercised on a remote/cloud machine without that real profile — but
a `queue-run` agent running locally (including in a `.claude/worktrees/`
worktree — that's still the same physical machine, not a remote sandbox)
has everything it needs and should just attempt the real command. Only
report a criterion as unverified if a live attempt actually failed, hung, or
timed out — not merely because the login/session path is involved. Verified
`sync`/`list` runs are especially cheap to attempt: they reuse
`session_state_file` in a fresh headless browser with no Brave/TU-Fast
involved at all when the saved session is still valid.

## Distribution direction: installer + in-app updates (not built yet)

Two structural decisions the maintainer has committed to as the intended
direction, even though neither is implemented yet — this is *the way I
want it to be, but it's currently just developing* (maintainer's words,
2026-07-09). Don't re-litigate these when picking up related work; treat
them as settled targets to build toward, gated only on the prerequisites
listed.

- **Update mechanism: T2 ("prompted, one-click apply")** is the committed
  v2 update mechanism — see `docs/update-mechanism-plan.md` for the full
  tradeoff analysis (T1/T2/T3) and design sketch. Not built yet: no
  `internal/updater` package, no GUI update banner/routes. Version-embedding
  (`buildVersion` / `-X ...buildVersion=`, PR #37) and the release workflow
  are both done as of `add-release-build-workflow`
  (2026-07-10): `.github/workflows/release.yml` triggers on `v*` tags,
  builds the installer via `scripts/build-installer.ps1`, and publishes
  `opal-downloader-setup.exe` (unversioned name) plus a `.sha256` sidecar
  as GitHub Release assets — see `docs/installer-plan.md` Section 9 Task 4
  and `docs/update-mechanism-plan.md` Section 6 Task 2 for the full
  writeup and the tag/asset-naming conventions (`vX.Y.Z` tags, unversioned
  asset name) a future `internal/updater` package should assume. Gated on
  1-2 manual releases happening through this workflow first, per that
  doc's Section 7.
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
- Worktrees/branches are cleaned up by queue-run's housekeeping step once
  their PR merges or closes. If you see worktrees under `.claude/worktrees/`
  or stray `worktree-agent-*` branches piling up, that step didn't run —
  don't manually pile up more, run `/queue-run` or clean up by hand.
- `.claude/queue/` itself is intentionally gitignored (task files may
  contain credentials/local paths) — its content is local-only and won't
  survive a fresh clone.
- **A task's `## Result` is not the end of the line if it changes or
  refines something this file already asserts** (most likely candidates:
  the "Key design decisions" and "Login/session automation" sections
  above). Since `.claude/queue/` is gitignored, a finding written only in a
  task file effectively disappears once that task's branch/worktree is
  cleaned up — nobody reading CLAUDE.md later would know to look for it.
  Before moving such a task to `done/`, update the relevant CLAUDE.md
  section in the same PR/commit (or, for `queue-review`-driven decisions
  with no PR, commit the CLAUDE.md edit directly) so the finding survives
  independently of the queue. `investigate-independent-second-profile-for-login.md`
  (done 2026-07-08) was a miss on this — its HMAC/second-profile finding sat
  undocumented in CLAUDE.md until manually caught later; don't repeat that.

## Maintenance

- `scripts/dev.ps1 all` — local build/vet/test/lint, run before any PR.
- `scripts/test-fresh-install.ps1` — validates the no-credentials setup path
  (clone through `init`); see `docs/setup-friction.md` for known friction.
- `docs/manual-setup-checklist.md` — manual checklist for the
  credential-requiring tier (`login`/`list`/`sync`).
- `docs/OPERATIONS.md` — maintenance cadence and incident playbook.
