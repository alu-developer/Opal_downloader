# Windows `setup.exe` Installer — Design Plan

**Status: built and shipping. This plan is history, not a to-do list**
(re-checked item by item on 2026-07-30). The line that stood here until then —
*"planning only. No installer code, `.iss`/`.wxs`/`.nsi` scripts, or CI changes
exist yet"* — was contradicted by the repo three sections further down its own
page, and by these files:

| The plan said it did not exist | What is actually in the repo |
|---|---|
| no `.iss` script | `installer/opal-downloader.iss`, bundling a staged Chromium cache into `%LOCALAPPDATA%\ms-playwright` |
| no CI changes | `.github/workflows/release.yml`, tag-triggered, builds the exe and runs `iscc` |
| implementation deferred | `README.md`'s "Download & Install (Windows installer)" section tells users to download `opal-downloader-setup.exe` |

Every row of Section 9's table is now done or deliberately obsolete — see the
status column there. Two items (3 and 8, Brave/Chrome detection and profile-path
prefill) were **removed rather than built**, because the real-browser-profile
option they served was deleted in full on 2026-07-14. The `.iss` documents that
removal in place, so the installer is consistent with the code; only this header
was not.

Read the design sections below as the reasoning behind what shipped. They are
preserved as written, and they are still the answer to "why Inno Setup, why
bundle Chromium" — but do not read any of it as work outstanding.

## 1. Problem statement

Today, getting `opal-downloader` running requires (per `docs/setup-friction.md`
and `README.md`'s Installation section):

1. `git clone` the repo (with a README URL casing bug already logged as
   friction finding #1).
2. Install Go 1.23+.
3. Run the Playwright browser install (`go run
   github.com/mxschmitt/playwright-go/cmd/playwright@... install`).
4. `go build -o opal-downloader.exe .`.
5. Run `init` (or the newer `setup` meta-command) to create `config.yaml`,
   then hand-edit it or use `gui` to configure it.

That's a developer workflow, not an end-user install. The goal here is a
single downloadable `setup.exe` that a non-technical TU Dresden student can
double-click, click through a short wizard, and end up with the app
installed, Playwright's Chromium ready, and the GUI open in their browser —
no Go toolchain, no `git`, no terminal.

This plan explicitly targets **Windows only** (the primary user base per
`env`/repo history), consistent with the rest of the project's Windows-first
tooling (`scripts/dev.ps1`, `scripts/test-fresh-install.ps1` are PowerShell).

## 2. Installer technology choice: **Inno Setup**

Recommendation: **Inno Setup** (free, script-driven `.iss` compiler, produces
a single self-contained `setup.exe`).

Rationale, considered against the alternatives:

| Option | Verdict |
|---|---|
| **Inno Setup** | **Chosen.** Purpose-built for exactly this shape of deliverable: one binary + a few support files, a short wizard (welcome → install dir → optional shortcuts → finish), and a post-install hook to run an arbitrary command. Its Pascal-like scripting (`[Code]` sections) is enough to run `opal-downloader.exe setup` after file copy and to check whether the bundled Chromium cache landed (`NeedsPlaywrightSetup`). (This originally also cited "detect Brave/Chrome presence" as a requirement; that need disappeared with the real-browser-profile option — see Section 5. The choice of Inno Setup is unaffected.) Single `.iss` text file, no XML, compiles to one `setup.exe` with no runtime dependency beyond what Windows already has. Large existing user base for "simple app installer" use cases, so tutorials/troubleshooting are easy to find. |
| **WiX Toolset** | Rejected for v1. Produces industry-standard `.msi` packages with strong enterprise/Group Policy/uninstall-tracking support, but that's overkill for a single-maintainer open-source tool with no enterprise deployment story. XML authoring and the WiX build pipeline (candle/light, or the newer WiX v4 CLI) are meaningfully more setup/learning overhead than Inno Setup for the same result. Worth revisiting only if the project later needs MSI-specific features (e.g. SCCM/Intune deployment at TU Dresden IT scale) — not needed now. |
| **NSIS** | Close second, also viable (also free, also produces a single `.exe`, arguably more flexible/lower-level via its scripting language). Passed over in favor of Inno Setup mainly because Inno's declarative `[Files]`/`[Tasks]`/`[Run]` sections map more directly and readably onto this installer's actual needs (copy binary + assets, optionally run `setup`, optionally launch `gui`) with less boilerplate than NSIS's more macro-heavy scripting style. Either would work; this is a mild preference, not a hard technical requirement. |
| **Go-based self-extracting wizard** (a custom Go program with an embedded archive that extracts itself and shows a minimal UI) | Rejected. Appealing in the abstract ("stay in the language the rest of the project already uses"), but it means building and maintaining a GUI wizard framework (welcome screen, directory picker, progress bar, uninstall registration, Windows Add/Remove Programs entry, admin-elevation prompts) essentially from scratch — all things Inno Setup already provides out of the box, tested across two decades of Windows versions. Not a good use of effort for a single-maintainer project; would only make sense if the project needed cross-platform installer parity from one codebase, which is not a current goal (this repo is Windows-first per `env`/`CLAUDE.md`). |

**Decision: Inno Setup**, single `.iss` script, compiled to
`opal-downloader-setup.exe`.

## 3. What gets bundled vs. installed at install-time

Two install-time cost centers exist beyond the Go binary itself: (a) the
compiled `opal-downloader.exe` and its static assets (GUI templates/static
files, `config.example.yaml`), and (b) Playwright's Chromium browser cache
(`%LOCALAPPDATA%\ms-playwright`, historically 150–300+ MB depending on
Chromium version).

**Decision (revised 2026-07-09, overrides the original "do not bundle"
call below): bundle Chromium into `setup.exe`.** The maintainer weighed the
original rationale and rejected it — see "Why this was revisited" below.
The installer copies the pinned Chromium build into
`%LOCALAPPDATA%\ms-playwright` (or wherever `playwright-go` v0.6100.0
expects it) as a normal `[Files]` entry, so a fresh install needs no
Chromium download step and works fully offline after the `setup.exe`
download completes.

Consequences for the rest of this plan:

- `runSetup`'s Playwright-install step (Section 9 task 1) becomes a
  **skip-if-already-present** check rather than the primary install path
  for installer-based installs — it stays as-is for the manual
  clone-and-build dev flow (where bundling doesn't apply), but the
  installer's post-install `[Run]` step no longer needs it to succeed.
- The release-build process (Section 9 task 4, and the CI job this plan
  assumes exists per `docs/update-mechanism-plan.md` Section 6 task 2) must
  fetch the Chromium build matching the pinned `playwright-go` version
  *before* compiling `setup.exe`, and embed it as a `[Files]` source. This
  is a bounded, one-time addition to a release workflow that doesn't exist
  yet anyway (see task 4b below) — not an ongoing tax beyond what already
  exists today, since the pin only changes when the maintainer deliberately
  bumps it, same as any other dependency bump that would already trigger a
  new release.
- Installer size grows by roughly the Chromium cache size (150–300+ MB) —
  accepted; a one-time larger download is judged worth it against the
  friction of a fresh install silently depending on a second internet
  fetch that can fail, stall, or degrade with no visible progress.
- The offline-degradation handling from the rejected design (skip/report
  Chromium as failed, tell the user to re-run `setup` later) is no longer
  needed for the installer path, since Chromium ships in the package. It's
  kept for the manual dev-build path, where `setup` is still how Chromium
  gets installed.

### Why this was revisited

Original rationale (kept below for the record) argued bundling would (a)
roughly triple-plus the installer size, and (b) create an ongoing
maintenance tax of "keeping bundled Chromium in lockstep with the
`playwright-go` version pin." On review:

- (a) was judged an acceptable trade — installer size in the low hundreds
  of MB is unremarkable for a one-time download in 2026, and this project
  has no bandwidth/hosting cost constraint (GitHub Releases hosting is
  free).
- (b) does not actually hold up: the "lockstep with the pin" sync cost
  exists identically whether Chromium is bundled or fetched at
  install-time — either way, whenever `playwright-go`'s pin bumps, *some*
  process has to fetch the new Chromium build that matches it. Bundling
  only moves *when* that fetch happens (CI build time, once per release)
  rather than *whether* it happens (user's install time, once per
  install). Moving it to build time is arguably less fragile: one fetch
  per release instead of one fetch per user-machine, with no
  offline/flaky-network failure mode exposed to end users at all.

Given ease-of-use is this project's explicitly stated top priority
(`CLAUDE.md`'s "Project philosophy" section), removing a second
internet-dependent, silently-failable step from first install is worth
more than the avoided installer size — so the decision is reversed.

<details>
<summary>Original (2026-xx-xx) rationale — superseded, kept for history</summary>

**Original decision: do not bundle Chromium. Trigger
`opal-downloader.exe setup` (already exists, see
`cmd/opal-downloader/root.go`'s `runSetup`) as a post-install step,
downloaded over the internet at install time.**

Rationale:

- `runSetup` already wraps exactly this step (`go run
  .../playwright-go/cmd/playwright@v0.6100.0 install`, run as a subprocess of
  the *installed* binary — no Go toolchain needed on the user's machine,
  since the pinned `playwright` driver download is independent of `go run`
  requiring a local Go install... **caveat**: `runSetup` currently shells out
  via `exec.Command("go", "run", ...)`, which *does* require a Go toolchain
  on PATH. That's a real blocker worth flagging as a prerequisite fix (see
  Section 8) — either the installer's post-install hook needs its own
  Chromium-fetch mechanism independent of `go run`, or `runSetup` needs to
  be reworked to invoke the Playwright driver executable directly (the
  `playwright-go` module can install its own driver without `go run` via the
  library's `playwright.Install()` API, which is what `runSetup` should
  probably call instead of shelling out to `go run ... install`). This plan
  flags it; fixing it is out of scope for this planning-only document.
- Bundling Chromium would roughly triple-plus the installer size and,
  worse, would need updating in lockstep with the `playwright-go` version
  pin (`v0.6100.0` today) every time it's bumped — an ongoing maintenance
  tax the project doesn't currently have tooling for.
- Fetching at install time keeps the installer itself small (just the Go
  binary + assets, likely low tens of MB) and matches the project's existing
  "internet-required for one step" precedent (README already documents this
  as a one-time step for the manual dev path).
- Trade-off accepted: install-time internet access is required. If it's
  unavailable or the download fails, the installer should not hard-fail the
  whole install — it should install the binary successfully, skip/report the
  Chromium step as failed, and tell the user they can re-run it later via
  `opal-downloader.exe setup` from a shortcut or terminal. This mirrors how
  `setup` already degrades today (it's rerunnable/idempotent per its
  existing "skip if exists" pattern for config).

</details>

## 4. Config bootstrap during install

**Decision: the installer does not collect `download_path`, course patterns,
or any other `config.yaml` field. It defers entirely to the GUI's first-run
settings page.** (This originally also named
`browser_user_data_dir`/`browser_profile_directory`; those keys no longer
exist — see Section 5. The decision itself is unaffected.)

Rationale:

- The GUI (`internal/gui/settings.go`) already detects a missing config file
  (matches on `"config file not found"` from `internal/config`) and offers to
  create one — this is exactly the onboarding surface a wizard-style install
  flow should hand off to, and it's already implemented (unlike the
  currently-blocked `gui-primary-entrypoint` task, this detection is a
  pre-existing capability of the settings page, not something this plan is
  waiting on).
- Config fields like `download_path`/course glob patterns require
  path pickers, validation, and explanatory text that are far better suited
  to a web form (already built) than to Inno Setup's limited wizard-page
  scripting. Duplicating that logic in Pascal Script inside the `.iss` file
  would be redundant, harder to maintain, and would drift from the GUI's own
  validation over time.
- The one thing worth the installer's own wizard page: **the install
  directory and the "create a desktop/start-menu shortcut" choice** — both
  are standard Inno Setup wizard pages requiring no custom code.
- Post-install flow: last installer step runs `opal-downloader.exe setup`
  (Playwright install, per Section 3) then optionally launches
  `opal-downloader.exe gui`, which opens the user's browser straight into
  the settings page — if `config.yaml` doesn't exist yet (fresh install,
  the common case), the GUI's existing missing-config flow takes over from
  there. This means the installer's job ends at "the app runs and shows you
  a working GUI," and the GUI's existing onboarding does the rest — no new
  config-writing code needed in the installer itself.

## 5. Browser constraint (rewritten 2026-07-31 — the old one no longer exists)

**What this section said until 2026-07-31, and why it was wrong:** it stated
that `login`/`sync`/`list` launch Playwright against the user's *real*
Brave/Chrome profile (`browser_user_data_dir`/`browser_profile_directory`), and
derived a whole installer design from that — a Brave/Chrome detection step, a
"Browser Requirement" wizard page, an optional profile-path prefill passed to
the GUI. That option was **removed in full on 2026-07-14** (queue task
`chromium-only-login-remove-real-browser`, the maintainer's explicit decision;
see `docs/browser-profile-strategy.md`'s "Chromium-only login: Strategy 1
removed outright"). The installer was updated at the time; this section was
not, so it kept describing a constraint the code had stopped having.

### What is actually true

`login`/`sync`/`list` always launch **Playwright's bundled Chromium**, against
a **single hardcoded profile** at `~/.opal-downloader/login-profile`
(`scraper.LoginProfileDir`, `internal/scraper/profile.go:23`). Verified in
`internal/scraper/session.go`'s `launchBrowser`:

- Interactive login (`!headless && !useSavedState`) opens a persistent context
  against exactly that directory with extensions enabled (`session.go:68-95`).
- Headless `sync`/`list` with a saved session launches a fresh anonymous
  Chromium and loads cookies from `session_state_file` — no profile directory
  at all (`session.go:145-146`).
- `ExecutablePath` is never set anywhere. There is no code path that launches
  an installed browser executable.

The config keys are gone too: `internal/config` ignores
`browser_executable`/`browser_user_data_dir`/`browser_profile_directory` if an
old `config.yaml` still carries them (regression test
`TestLoadCredentialsIgnoresRemovedBrowserFields`, `internal/config/config_test.go:326`).

### What this means for the installer

**The constraint is not "don't touch the user's browser" — it's that the user
does not need a browser at all.** A machine with no Brave and no Chrome is a
fully supported install. Concretely:

- **Nothing to detect, so nothing to detect *for*.** The Brave/Chrome
  detection step, the "Browser Requirement" wizard page, and the
  `--suggested-browser-user-data-dir` prefill (`BrowserDetected`,
  `GetSuggestedBrowserProfileArg`) were **deleted, not deferred** —
  `installer/opal-downloader.iss` documents the removal in its `[Code]`
  section. Section 9 rows 3 and 8 are marked obsolete for this reason.
  Re-adding any of them would surface a prerequisite that no longer exists and
  would mislead the user into thinking their everyday browser is involved.
- **The installer still must not install, configure, or modify Brave/Chrome or
  the TU-Fast extension** — but now because it is unnecessary, not because it
  is dangerous. TU-Fast, if the user wants it, is installed from the Chrome Web
  Store *into opal-downloader's own dedicated profile*, through the GUI's
  consent-gated `/tufast-setup` page or during `opal-downloader login`. The
  post-install `[Run]` step already launches the GUI, which is the right
  handoff point.
- **First login works without TU-Fast.** The user types credentials and does
  2FA by hand once in the dedicated profile. TU-Fast only removes that manual
  step on subsequent logins. So the installer has no hard prerequisite to gate
  on and no informational page it owes the user.
- **The one remaining real-browser touchpoint is in the GUI, not the
  installer.** `/tufast-setup` may *detect* an existing Brave/Chrome profile
  root (`detectBrowserUserDataDir`, `internal/gui/tufast_setup.go:93`) and
  offer to copy TU-Fast's own stored login/2FA data out of it into the
  dedicated profile (`scraper.TransplantTUFastLoginData`). That is read-only
  detection behind an explicit consent gate, it is optional, and it happens
  after install. The installer must not pre-empt, pre-answer, or automate it.
- **The `Secure Preferences` HMAC finding still stands, and still isn't the
  installer's problem.** Copying a whole profile (`Preferences`/`Secure
  Preferences`/`Local State`/`Extensions`) breaks Chromium's integrity check
  and strips the extension's permissions — the PR #20/#41 finding. The
  transplant above copies only TU-Fast's `Local Extension Settings/<id>`
  leveldb folder, which is outside that HMAC chain (live-verified 2026-07-12,
  `docs/browser-profile-strategy.md` "Transplanting TU-Fast login data"). Both
  facts live in the GUI's flow; the installer touches neither.
- **`isUserDataDirLocked` (`internal/scraper/profile.go:64`) still exists**,
  but it no longer guards against "the user's Brave is open" — it guards
  against *two opal-downloader processes* opening a persistent context on the
  same dedicated profile (`session.go:79-85`). Unchanged conclusion: a runtime
  check, with a clear error, that the installer neither needs to duplicate nor
  can interfere with.

## 6. Code signing / SmartScreen

An unsigned `setup.exe` triggers a Windows SmartScreen "Windows protected
your PC" warning on first run (and on the downloaded file's "unblock"
prompt), which reads as scary/untrustworthy to a non-technical user — exactly
the audience this installer is meant to serve.

Trade-off:

| | Buy a code-signing cert | Ship unsigned + document workaround |
|---|---|---|
| Cost | EV code-signing certs run roughly $300–600+/year (standard/OV certs are cheaper but SmartScreen reputation still needs to build up over time/downloads even when signed); ongoing renewal burden | $0 |
| User experience | No SmartScreen warning (EV certs get instant reputation; standard certs still need download-volume reputation to build) | SmartScreen warning every fresh download until enough users click through and Microsoft's reputation system whitelists the hash — which won't happen at this project's likely download volume |
| Maintainer burden | Cert renewal, secure key storage, signing step in the release process | None beyond a doc note |
| Fit for this project | A single-maintainer open-source tool with an unknown/small user base (TU Dresden students) | Matches the project's current scale and budget — no other paid infrastructure exists today (no CI signing secrets, no cert vault) |

**Recommendation: ship unsigned for v1, document the SmartScreen "More info →
Run anyway" workaround prominently** (in the GitHub release notes and a short
section in the README/installer download page). Revisit code signing only if
the user base grows enough that the SmartScreen friction becomes a real
adoption blocker — at that point a standard (non-EV) cert is the pragmatic
next step, since EV's main advantage (instant reputation) is proportionally
less valuable than its cost for a project this size.

## 7. Update story

**Recommendation: re-run the installer manually for v1** (download the
latest `setup.exe` from GitHub Releases, run it again — Inno Setup installs
support upgrade-in-place by default when the same `AppId` is reused, so
re-running over an existing install just updates files rather than
duplicating the Start Menu entry).

Rationale: an in-app update checker is real additional scope — it needs a
version-check endpoint or GitHub Releases API polling, a download-and-apply
flow, and a decision about auto-vs-prompted updates, none of which exist
today. Manual re-run is zero-cost to build (it falls out of Inno Setup's
default behavior) and is consistent with how the project already treats
Playwright's own version pin (`v0.6100.0`) — bumped manually by the
maintainer, not auto-updated.

This can change later: an in-app "check for updates" button on the GUI
(hitting the GitHub Releases API, showing "v0.2.0 available, download here")
is a reasonable, low-effort follow-up once there's more than one release to
update *to* — flagged in Section 8, not designed further here.

## 8. Relationship to existing work

- **Supersedes, for end users:** the multi-step manual clone → Go install →
  Playwright install → build → `init`/`gui` flow in `README.md`'s
  Installation/Quick Start sections, and the friction items in
  `docs/setup-friction.md` that are specifically about *that* flow (findings
  #1, #2, #5, #7 in particular — wrong clone URL, `.exe` suffix gotcha,
  silent Playwright install, "no single meta-command" — all become
  non-issues once `setup.exe` exists, since the installer subsumes what the
  `setup` subcommand already does and removes the clone/build steps
  entirely).
- **Does NOT replace:** the documented manual dev setup. Contributors who
  need to build from source (to modify the code, run tests, `scripts/dev.ps1
  all`, etc.) keep using the existing README instructions unchanged — the
  installer is an end-user distribution artifact built *from* a contributor's
  `go build`, not a replacement for the build process itself.
- **`gui-primary-entrypoint` has since landed** (PR #31, commit `5fcccd0`,
  `.claude/queue/done/gui-primary-entrypoint.md`) — running the bare binary
  with no subcommand now defaults to `gui`, not just `opal-downloader.exe
  gui` explicitly. This section originally noted the installer's pitch
  ("double-click, get the GUI") would be undercut if the GUI were still one
  extra subcommand away from default, and treated that as a release-gating
  condition, not a technical dependency — that condition is now satisfied,
  so it's no longer a reason to hold back a public release once the
  installer itself exists. The installer's post-install `[Run]` step can
  still invoke `opal-downloader.exe gui` explicitly either way.

## 9. Effort/complexity estimate and suggested follow-up order

**All eight rows are settled — verified against the code 2026-07-30.** The
original sizing and dependency columns are kept below as the record of how this
was planned; this table is what actually happened.

| # | Status, and how it was checked |
|---|---|
| 1 | **Done.** `runSetup` calls `playwright.Install(&playwright.RunOptions{...})` directly (`cmd/opal-downloader/root.go:287`), not `exec.Command("go", "run", ...)`, so the target machine needs no Go toolchain. |
| 2 | **Done.** `installer/opal-downloader.iss` exists and bundles the staged Chromium cache. |
| 3 | **Obsolete, not pending.** The Brave/Chrome detection page served the real-browser-profile option, deleted in full on 2026-07-14. `BrowserDetected`/`GetSuggestedBrowserProfileArg` and the "Browser Requirement" wizard page were removed with it; the `.iss` documents this at its post-install section. Building it now would re-add a page for a capability that no longer exists. |
| 4, 4b | **Done.** `.github/workflows/release.yml`, tag-triggered, fetches the Chromium build matching the pinned `playwright-go` version and runs `iscc`. |
| 5 | **Done.** Landed via PR #31 (`5fcccd0`); was never a gate after that. |
| 6 | **Done.** `README.md`'s "Download & Install" section plus `docs/release-notes-template.md`, both present. |
| 7 | **Done.** `internal/updater` exists and is wired into both front ends — `updaterCheckLatest` in `cmd/opal-downloader/root.go:59` and `updaterClient`/`checkLatest` in `internal/gui/gui.go`. Listed here as "later, optional"; it shipped. |
| 8 | **Obsolete**, same removal as row 3. |

Original planning table, kept for the reasoning in its dependency column:

| Task | Effort | Depends on |
|---|---|---|
| 1. Fix `runSetup`'s Playwright install to not require a Go toolchain on the target machine (call `playwright-go`'s install API directly instead of `exec.Command("go", "run", ...)`) | Small–Medium | None — this is a prerequisite bug, not installer-specific, worth fixing regardless of the installer; still needed for the manual dev-build path even though the installer itself no longer depends on it (Section 3 revision) |
| 2. Write the `.iss` script: file list (binary, GUI assets, `config.example.yaml`, LICENSE, **bundled Chromium cache**, per Section 3's revised decision), wizard pages (install dir, shortcuts), post-install `[Run]` entries (`gui`; `setup` only as a skip-if-present fallback) | Medium | None (no longer gated on Task 1, since the installer bundles Chromium instead of depending on the post-install fetch working) |
| 3. Add the Brave/Chrome detection informational page (Section 5) | Small | Task 2 |
| 4. Wire installer build into a release process (manual `iscc` invocation is fine for v1; CI automation is explicitly out of scope per this task's constraints, but worth a follow-up) | Small | Task 2 — **done**, see below |
| 4b. Release-build step: fetch the Chromium build matching the pinned `playwright-go` version and stage it for the `.iss` `[Files]` entry (Section 3) | Small–Medium | Task 4 — folded into the release workflow's Chromium-fetch step, see below |
| 5. ~~Wait on / land `gui-primary-entrypoint` before the first public release of `setup.exe`~~ — **done**, landed via PR #31 (commit `5fcccd0`); no longer a gate | N/A | — |
| 6. Document the SmartScreen workaround in the release notes / README | Trivial | Task 2 (done — see `README.md`'s "Download & Install" section and `docs/release-notes-template.md`) |
| 7. (Later, optional) In-app update checker | Medium | A second release existing to update to |
| 8. (Later, optional) Suggested-profile-path prefill from browser detection into the GUI (Section 5) | Small | Task 3 |

**Task 4 update (2026-07-10, task `add-release-build-workflow`):** a tag-triggered
release workflow now exists at `.github/workflows/release.yml` (a new
workflow file, not a job added to `ci.yml`, to keep the tag-only trigger
cleanly separate from the existing push/PR test job). On a `v*` tag push it:
builds `opal-downloader.exe` with the tag injected via `-ldflags` (the
`buildVersion` mechanism from PR #37); fetches a Chromium cache matching the
pinned `playwright-go` version (`go.mod`) via the same driver command
`runSetup` uses, since a fresh GitHub-hosted Windows runner has no
pre-existing `%LOCALAPPDATA%\ms-playwright`; installs Inno Setup via
Chocolatey (preinstalled on `windows-latest`); invokes
`scripts/build-installer.ps1` to stage the cache and run `iscc`; and uploads
the resulting `opal-downloader-setup.exe` plus a `.sha256` sidecar as GitHub
Release assets via `gh release create`.

**Conventions this establishes** (also documented in the workflow's own
header comments): tag format is `vX.Y.Z` (e.g. `v0.2.0`); the installer
asset is uploaded **unversioned** as `opal-downloader-setup.exe` (not
`opal-downloader-setup-v0.2.0.exe`), matching the name
`docs/update-mechanism-plan.md` Section 2.2 already assumed for the planned
`internal/updater` package, so that package can always fetch the same fixed
asset name off `GET .../releases/latest` without parsing a version out of
the filename. Live end-to-end verification (pushing a real tag and watching
the run) status is recorded in that task's PR — see there before relying on
this workflow for a real release if it wasn't marked verified.

**Overall estimate: small-to-medium** for a working v1 installer (tasks 1–4,
6) — roughly a few days of focused work for someone already familiar with
this codebase, dominated more by the `runSetup`/Go-toolchain fix (task 1)
than by the Inno Setup scripting itself, which is templated and
well-documented. This is explicitly **not urgent** — per the task framing,
this is long-term/low-priority and should not block other in-flight work.
Follow-up tasks (1–4, 6 first; 7–8 later) should be captured separately in
the local task queue (`.claude/queue/`) rather than implemented as part of
this planning task.

## Addendum (2026-08-01): the bundled Chromium landed where nothing looked

Every `%LOCALAPPDATA%\ms-playwright` reference in Sections 1–9 above was
correct when written, but the ground under it moved on 2026-07-13
(`EnsurePlaywrightBrowsersPath`, `internal/scraper/session.go`, commit
`b352143`) and this plan was never updated: `PLAYWRIGHT_BROWSERS_PATH`
defaults to `%USERPROFILE%\.opal-downloader\ms-playwright` now, to dodge an
NTFS-junction failure seen under `%LOCALAPPDATA%` on at least one machine
(`docs/OPERATIONS.md`). The installer chain still copied Chromium to, and
probed for it at, the old `%LOCALAPPDATA%` path — so a fresh install's
bundled ~680MB landed where the running app never reads, and
`NeedsPlaywrightSetup`'s stale probe found it "present" there anyway and
skipped the one fallback (`opal-downloader.exe setup`) that would have
recovered. That defeated Section 3's entire bundling decision without ever
failing loudly.

Fixed (not yet verified against a built `setup.exe` — no Windows Chromium
cache was available to stage in the environment that made this fix):
`installer/opal-downloader.iss` (`[Files]` DestDir and `NeedsPlaywrightSetup`
now both point at `{%USERPROFILE}\.opal-downloader\ms-playwright`),
`scripts/build-installer.ps1` (`$ChromiumCacheSrc` default moved to match),
and `.github/workflows/release.yml`'s "Fetch Playwright Chromium" step (now
sets `PLAYWRIGHT_BROWSERS_PATH` explicitly before the bare `playwright
install` CLI call, which otherwise still falls back to *its own*
`%LOCALAPPDATA%` default since it never goes through
`EnsurePlaywrightBrowsersPath`). Also fixed in passing: the `[Run]` fallback
and `build-installer.ps1`'s missing-cache warning both still said the
`setup` fallback needs a Go toolchain — it hasn't since `runSetup` switched
to calling `playwright.Install()` directly (Section 9 task 1).

**Still needs a real verification pass**, ideally the next time an actual
release is cut: confirm a locally-built `setup.exe` stages Chromium into the
new path, that `NeedsPlaywrightSetup` correctly finds it, and that a tag
push through `release.yml` produces an installer with Chromium bundled
(check the resulting `.exe`'s size — a few hundred KB means the cache
silently didn't stage, ~250-300MB means it did).
