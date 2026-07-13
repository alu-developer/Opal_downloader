# opal-downloader

Go program that logs into OPAL (Bildungsportal Sachsen, Shibboleth/TU-Fast
SSO) via Playwright browser automation, discovers course files by scraping
the DOM (no OPAL API/WebDAV — WebDAV PROPFIND was tried and dropped, see
`docs/webdav-propfind-research.md`), and syncs them to a local folder with
an incremental manifest. Ships as a single binary with two front ends: a
local web GUI (`internal/gui/`, launched by default when run with no
subcommand) and a CLI (`init`, `setup`, `status`, `login`, `list`, `sync`,
`dump-links`) for scripting/automation.

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
deployments across Saxon institutions is **not yet known**. Until resolved,
don't assume the DOM-scraping selectors need to be institution-generic;
also don't assume they don't.

This scope directly affects how much onboarding/documentation friction is
worth eliminating — a stranger's first-run experience matters, not just the
maintainer's own repeat use.

## Package layout

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
  remote files against `.opal-sync.manifest.json`.
- `internal/config/` — `config.yaml` loading, validation, and `Save` (backs
  up the existing file before overwriting).
- `internal/timing/` — instrumentation for the perf-benchmark work
  (see `docs/OPERATIONS.md`/queue history for context).

## Login/session automation

Login/sync/list need a Playwright browser session against a real,
unlocked (browser closed) profile on the machine running them — a profile
*copy* does not work (HMAC integrity breakage). A dedicated, never-copied
second profile is shipped and working: one-click setup (CLI and GUI)
transplants TU-Fast's login/2FA state into a fresh profile without
repeating the manual install+2FA flow — see
`docs/browser-profile-strategy.md` and `docs/HISTORY.md`. Not yet the
default; the user's real everyday Chrome/Brave profile still is, and
whether/when that changes is undecided. Whichever profile is configured,
when TU-Fast is installed and working in it, it completes the
Shibboleth/2FA exchange itself — no human click needed, `ensureSession`
just waits for the post-login course list. A human is only needed if
TU-Fast isn't working in that profile or the profile is locked (another
browser instance open) — both surface as a clear error/timeout.

This means `login`/`sync`/`list` are **not** inherently limited to
human-attended runs, and a `queue-run` agent running locally (including in
a `.claude/worktrees/` worktree — same physical machine) should just
attempt the real command rather than assuming it needs a human. Only report
a criterion as unverified if a live attempt actually failed, hung, or timed
out. Verified `sync`/`list` runs are especially cheap: they reuse
`session_state_file` in a fresh headless browser with no browser/TU-Fast
involved at all when the saved session is still valid.

## Local task queue workflow (`.claude/queue/`, gitignored)

This repo uses `task-capture` / `queue-run` / `queue-review` (global Claude
Code skills) against `.claude/queue/`, which is gitignored and local-only
(won't survive a fresh clone).

- **A `.claude/queue/done/` task means it was completed and PR'd — not that
  the PR is merged into `master`.** Check `gh pr list` / `git log` before
  treating a "done" task's changes as actually available to build on.

## Maintenance

- `scripts/dev.ps1 all` — local build/vet/test/lint, run before any PR.
- `scripts/test-fresh-install.ps1` — validates the no-credentials setup path
  (clone through `init`); see `docs/setup-friction.md` for known friction.
- `docs/manual-setup-checklist.md` — manual checklist for the
  credential-requiring tier (`login`/`list`/`sync`).
- `docs/OPERATIONS.md` — maintenance cadence and incident playbook.
