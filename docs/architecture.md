# Architecture

Reference, not required reading. `CLAUDE.md` carries the rules; this file
carries the facts you can also get by reading the code, collected here so you
don't have to. Moved out of `CLAUDE.md` on 2026-07-31 — see the note at the
bottom for why.

## What the tool is

Go program that logs into OPAL (Bildungsportal Sachsen, Shibboleth/TU-Fast
SSO) via Playwright browser automation, discovers course files by scraping the
DOM (no OPAL API/WebDAV — WebDAV PROPFIND was tried and dropped, see
`docs/webdav-propfind-research.md`), and syncs them to a local folder with an
incremental manifest. Ships as a single binary with two front ends: a local web
GUI (`internal/gui/`, launched by default when run with no subcommand) and a
CLI (`init`, `setup`, `status`, `login`, `list`, `sync`, `dump-links`) for
scripting/automation.

## Package layout

- `main.go` — the only `package main`. Build the binary with `go build .`;
  `go build ./cmd/opal-downloader` silently produces a library archive, not an
  executable, and the resulting "not a valid application for this OS platform"
  is a confusing way to learn that.
- `cmd/opal-downloader/root.go` — entry point, a plain `switch` over
  `os.Args[1]` (no CLI framework/Cobra — three direct Go deps total:
  `playwright-go`, `x/text`, `yaml.v3`). Subcommands: `init`, `setup`,
  `status`, `login`, `list`, `sync`, `dump-links`, `gui`. Running with no
  subcommand launches the GUI. Also has a hidden `__panic-test` subcommand
  (intentionally left out of `printHelp`) that just panics on demand, so the
  panic-recovery wrapper in `Execute` can be live-verified without a real bug.
  `list` and `dump-links` **stay listed in `printHelp`** (maintainer's
  decision, 2026-07-23), even though the course picker removed the main
  end-user reason to run `list`. Anyone reaching for the CLI at all is someone
  who may want them; hiding a working command to tidy up help output is not a
  trade worth making.
- `internal/gui/` — the local web GUI: HTTP server, settings page (read/write
  `config.yaml`), login trigger, sync/list/dump-links page with live progress.
- `internal/scraper/` — Playwright-driven browser automation. Split by concern:
  `session.go` (launch/login/auth-state), `profile.go` (browser profile
  handling), `discovery.go` (find course cards on the dashboard),
  `crawl.go`/`navigation.go` (walk sections/subfolders within a course),
  `files.go` (extract downloadable file links per section), `download.go`
  (fetch a file), `course_filter.go` (match configured course names),
  `orchestrator.go` (ties discovery+crawl+download into one call),
  `embeddedpage.go` and `groupfolders.go` (the two places a course keeps
  files that are not in any file table — see "Where files actually live"
  below).
- `internal/syncer/` — manifest-based incremental sync on top of
  `scraper.OpalScraper` (`SyncCourses`, `ListAvailableCourses`), diffing remote
  files against `.opal-sync.manifest.json`. Manifests carry a
  `schema_version`; bump `ManifestSchemaVersion` (`syncer.go`) whenever
  manifest *key derivation* changes, and see `migrate.go` for the remap/warn
  pass that salvages an old-scheme manifest instead of silently re-downloading
  everything and orphaning the old copies.
- `internal/config/` — `config.yaml` loading, validation, and `Save` (backs up
  the existing file before overwriting).
- `internal/foldersuggest/` — proposes a `course_folders` destination per
  course by scanning the download root. Pure matching + a filesystem walk, no
  scraping and no writes; the GUI's `/settings/suggest-folders` is its only
  caller. Deliberately withholds a suggestion rather than risk a wrong one
  (`MinScore`/`MinMargin` in `match.go`).
- `internal/polite` — the rate ceiling every OPAL navigation passes through,
  wired in via `gotoPolitely`. Policy lives in `docs/server-load.md`.
- `internal/timing/` — instrumentation for the perf-benchmark work.
- `internal/logging/` — see below.

## Where files actually live

A course's files are not all in file tables, and two of the three places they
can be are invisible to anything that only walks the course-node tree. Both
were found live on 2026-11-04, each in a course that had been reporting zero
files since the day it was added.

1. **File tables** — the ordinary case: a folder node renders a `<tr>` per
   file with `data-file-name`, a size cell and a "zuletzt geändert" cell.
   `httpdiscovery.go` parses it, and change detection gets real signals.

2. **A section's prose** (`embeddedpage.go`) — an OPAL "Einzelne Seite"
   (node-sp) renders none of its content into the section page. The section
   page holds only `<iframe src="/opal/FolderResource/<repo>/<page>.html">`,
   and the lecturer's links live in *that* document, relative to its own
   directory. The maintainer's AN30 course is nothing but two such pages, so
   this was not a missing file but a missing course. Two consequences worth
   knowing: the link text on a prose page is a sentence, not a file name
   ("Übungsblatt 0"), so the name is taken from the URL instead; and a prose
   page has no size/date columns, so these files are signalless and get
   byte-verified on every sync rather than compared by signal.

3. **A learning group's Ordner** (`groupfolders.go`) — enrolling in a course
   can put you in a *Lerngruppe* that has its own file area, which is not a
   course node at all. The maintainer's English course has no file-bearing
   node anywhere; all of its material is in one group. Enumeration is free:
   every course page carries a second jstree payload (`"id":"idgroups"`)
   listing this user's groups *for that course*, so the course root body
   already in hand yields the group URLs, and the Ordner is that URL plus
   `/menu/MENU_FOLDER`. A group with no Ordner answers 200 with an empty
   table, so no probe is needed. The response is an ordinary file table, so
   (1)'s parser, the size/date columns and the download path all apply
   unchanged.

This is also the correction to a conclusion recorded in `section_type.go`:
skipping `node-en` enrollment nodes is still right, they really cannot hold
files — but "the enrollment node has no files" never meant the course has
none. The files are one level sideways, in the group the enrollment put you
in.

## Logging

Two axes, not one: a *level* (Debug/Info/Warn/Error, how bad) and an *audience*
(user or diagnostic, who it is for), because "skipping section" is a genuine
warning and also of no interest to a student who wants their slides. Two sinks
read those independently — the console takes user-facing records plus every
error (`--verbose` adds the rest), and a rotating file under
`~/.opal-downloader/logs/` takes everything. Everything written to the file is
scrubbed via `statuslog.SanitizeMessage` first, so the log is always safe to
attach to a bug report without anyone remembering to check it. Call it with
`logging.User/Detail/Warn/Error`; the printf shape is deliberate, since that is
what every existing call site already looks like.

The CLI's own `fmt.Println` output in `cmd/` is **not** migrated and should not
be: a CLI printing its results to stdout is already exactly the user channel.

## Login/session automation

Login/sync/list always use Playwright's bundled Chromium against a single
hardcoded dedicated profile at `~/.opal-downloader/login-profile`
(`scraper.LoginProfileDir`). There is no "point opal-downloader at your real
Brave/Chrome profile" option — removed in full on 2026-07-14.

The user either logs in manually (credentials + 2FA by hand) in that profile,
or, once, installs the TU-Fast extension from the Chrome Web Store into that
same profile (via the GUI's `/tufast-setup` page or by hand during `login`),
after which TU-Fast completes the Shibboleth/2FA exchange itself on every
future login — no human click needed, `ensureSession` just waits for the
post-login course list. A human is only needed if TU-Fast isn't installed in
the dedicated profile yet, or the profile is locked (another opal-downloader
process has it open) — both surface as a clear error/timeout.

**Consequence, and the reason this section matters:** `login`/`sync`/`list` are
**not** inherently limited to human-attended runs. An agent running locally
(including in a `.claude/worktrees/` worktree — same physical machine) should
just attempt the real command rather than assuming it needs a human. Only
report a criterion as unverified if a live attempt actually failed, hung, or
timed out. Verified `sync`/`list` runs are especially cheap: they reuse
`session_state_file` in a fresh headless browser with no TU-Fast involved at
all when the saved session is still valid.

## Who this is for

Three overlapping groups, in order of directness but not necessarily priority:
the maintainer (personal use, TU Dresden); other TU Dresden students; other
Bildungsportal Sachsen users at other Saxon institutions on the same platform.

**Open question:** whether "Bildungsportal Sachsen" means one shared OPAL
instance (TU Dresden's) or several differently-branded/configured deployments
is not known. Until resolved, don't assume the DOM-scraping selectors need to
be institution-generic; also don't assume they don't.

This scope decides how much onboarding friction is worth eliminating — a
stranger's first-run experience matters, not just the maintainer's repeat use.

## Why this file exists

`CLAUDE.md` was 358 lines, roughly half of it descriptions of the codebase that
an agent can read for itself. Measured evidence says that costs rather than
helps: Gloaguen et al., *Evaluating AGENTS.md* (arXiv 2602.11988, ETH Zurich /
LogicStar, Feb 2026) found across four coding agents and hundreds of real
GitHub issues that repository context files do not generally improve task
success while raising inference cost by over 20% — and specifically that
**instructions are followed but repository overviews are not helpful**. So the
instructions stayed in `CLAUDE.md` and the overview moved here.
