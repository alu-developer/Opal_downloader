# opal-downloader

## Start here

**`docs/BACKLOG.md` is the current state of work.** Read it at the start of a
session, pick the top item that isn't blocked, and get on with it — without
being asked to. Update it in the same commit as the work it describes.

The maintainer works by thinking out loud: they describe problems, ideas and
annoyances, and expect whoever is listening to turn that into maintained
software. Turning a passing remark into a backlog entry is part of the job,
not something to ask permission for. Do not wait for a task to be handed over
in a particular format.

Go program that logs into OPAL (Bildungsportal Sachsen, Shibboleth/TU-Fast
SSO) via Playwright browser automation, discovers course files by scraping
the DOM (no OPAL API/WebDAV — WebDAV PROPFIND was tried and dropped, see
`docs/webdav-propfind-research.md`), and syncs them to a local folder with
an incremental manifest. Ships as a single binary with two front ends: a
local web GUI (`internal/gui/`, launched by default when run with no
subcommand) and a CLI (`init`, `setup`, `status`, `login`, `list`, `sync`,
`dump-links`) for scripting/automation.

## Before editing this file

The former "ask before every edit to this file" rule was lifted by the
maintainer on 2026-07-21: routine edits (keeping the package layout,
workflow notes, and architecture facts accurate) can be applied directly,
no check-in needed. Still flag the change in the turn summary, and still
ask first when an edit would change a stated *decision* or project
principle rather than just describe reality.

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
deployments across Saxon institutions is **not yet known**. Until resolved,
don't assume the DOM-scraping selectors need to be institution-generic;
also don't assume they don't.

This scope directly affects how much onboarding/documentation friction is
worth eliminating — a stranger's first-run experience matters, not just the
maintainer's own repeat use.

## Package layout

- `main.go` — the only `package main`. Build the binary with `go build .`;
  `go build ./cmd/opal-downloader` silently produces a library archive, not
  an executable, and the resulting "not a valid application for this OS
  platform" is a confusing way to learn that.
- `cmd/opal-downloader/root.go` — entry point, a plain `switch` over
  `os.Args[1]` (no CLI framework/Cobra — three direct Go deps total:
  `playwright-go`, `x/text`, `yaml.v3`). Subcommands: `init`, `setup`,
  `status`, `login`, `list`, `sync`, `dump-links`, `gui`. Running with no
  subcommand at all launches the GUI. Also has a hidden `__panic-test`
  subcommand (intentionally left out of
  `printHelp`) that just panics on demand, so the panic-recovery wrapper in
  `Execute` can be live-verified without a real bug.
- `internal/gui/` — the local web GUI: HTTP server, settings page
  (read/write `config.yaml`), login trigger, sync/list/dump-links page with
  live progress.
- `internal/scraper/` — Playwright-driven browser automation. Split by
  concern: `session.go` (launch/login/auth-state), `profile.go` (browser
  profile handling), `discovery.go` (find
  course cards on the dashboard), `crawl.go`/`navigation.go` (walk
  sections/subfolders within a course), `files.go` (extract downloadable
  file links per section), `download.go` (fetch a file), `course_filter.go`
  (match configured course names), `orchestrator.go` (ties
  discovery+crawl+download into one call).
- `internal/syncer/` — manifest-based incremental sync on top of
  `scraper.OpalScraper` (`SyncCourses`, `ListAvailableCourses`), diffing
  remote files against `.opal-sync.manifest.json`. Manifests carry a
  `schema_version`; bump `ManifestSchemaVersion` (`syncer.go`) whenever
  manifest *key derivation* changes, and see `migrate.go` for the
  remap/warn pass that salvages an old-scheme manifest instead of silently
  re-downloading everything and orphaning the old copies.
- `internal/config/` — `config.yaml` loading, validation, and `Save` (backs
  up the existing file before overwriting).
- `internal/foldersuggest/` — proposes a `course_folders` destination per
  course by scanning the download root. Pure matching + a filesystem walk, no
  scraping and no writes; the GUI's `/settings/suggest-folders` is its only
  caller. Deliberately withholds a suggestion rather than risk a wrong one
  (`MinScore`/`MinMargin` in `match.go`).
- `internal/timing/` — instrumentation for the perf-benchmark work
  (see `docs/OPERATIONS.md`/queue history for context).

## Login/session automation

Login/sync/list always use Playwright's bundled Chromium against a single
hardcoded dedicated profile at `~/.opal-downloader/login-profile`
(`scraper.LoginProfileDir`) — there is no more "point opal-downloader at
your real Brave/Chrome profile" option (removed in full, queue task
`chromium-only-login-remove-real-browser`, 2026-07-14). The user either
logs in manually (credentials + 2FA by hand) in that profile, or, once,
installs the TU-Fast extension from the Chrome Web Store into that same
profile (via the GUI's `/tufast-setup` page or by hand during `login`),
after which TU-Fast completes the Shibboleth/2FA exchange itself on every
future login — no human click needed, `ensureSession` just waits for the
post-login course list. A human is only needed if TU-Fast isn't installed
in the dedicated profile yet, or the profile is locked (another
opal-downloader process has it open) — both surface as a clear
error/timeout.

This means `login`/`sync`/`list` are **not** inherently limited to
human-attended runs, and a `queue-run` agent running locally (including in
a `.claude/worktrees/` worktree — same physical machine) should just
attempt the real command rather than assuming it needs a human. Only
report a criterion as unverified if a live attempt actually failed, hung,
or timed out. Verified `sync`/`list` runs are especially cheap: they reuse
`session_state_file` in a fresh headless browser with no TU-Fast
involved at all when the saved session is still valid.

## How to organise yourself (autopilot, model/effort, budget)

`docs/agent-operating-model.md` is the standing answer to "when do you keep
working on your own, what model/effort do you run at, and how do you stay
inside a Pro plan's limits". Read it before deciding to stop and wait for
input — stopping to ask "shall I continue?" is the specific failure it
exists to prevent.

Short version: a `Stop` hook (`.claude/hooks/autopilot-gate.ps1`) keeps the
turn going while `.claude/queue/AUTOPILOT` exists and queued work remains,
with expiry/iteration/rate-limit guards that all fail open. **Ending a run is
not yours to decide** — deleting the marker does nothing (the hook restores
it); only those guards or the maintainer's `.claude/queue/AUTOPILOT.OFF` end
it. Default model is
Sonnet at medium effort; escalating to Opus/high is a deliberate call the
maintainer has to make, so ask for it explicitly when a task needs it.

## Task tracking: `docs/BACKLOG.md`, not the old queue

Work is tracked in `docs/BACKLOG.md` (see "Start here" above). Plain prose,
tracked in git, updated alongside the code.

The older `.claude/queue/` workflow — `task-capture` / `queue-run` /
`queue-review` against `todo/`, `in-progress/`, `done/`, `blocked/` — is
**retired for this repo** (2026-07-22, maintainer's decision). It is not
deleted: the skills stay installed and are still reasonable for a repo where
a formal queue earns its ceremony. Don't reach for them here.

Why it was retired: the queue was gitignored, so the only record of in-flight
work couldn't survive a fresh clone; and it needed a skill to be *invoked*
before anything happened, which made autonomy depend on ceremony rather than
on simply owning the backlog. If you find an old `.claude/queue/` directory
on a machine, treat `docs/BACKLOG.md` as authoritative.

## Maintenance

- `scripts/dev.ps1 all` — local build/vet/test/lint, run before any PR.
- `scripts/test-fresh-install.ps1` — validates the no-credentials setup path
  (clone through `init`); see `docs/setup-friction.md` for known friction.
- `docs/manual-setup-checklist.md` — manual checklist for the
  credential-requiring tier (`login`/`list`/`sync`).
- `docs/OPERATIONS.md` — maintenance cadence and incident playbook.
