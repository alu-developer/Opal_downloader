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

- **Server load is a standing constraint, not a one-off check.**
  `docs/server-load.md` is the policy: a rate ceiling every OPAL navigation
  passes through (`internal/polite`, wired in via `gotoPolitely`), backoff when
  OPAL reports overload, and scheduled runs scattered across the hour instead
  of every install firing at 06:00. Read it before any change that makes the
  tool ask OPAL for more, or ask faster - it pulls directly against the
  sync-speed work, and that trade-off is written down there.
  **`docs/sync-speed-model.md` is that work's driver**: what is known, the
  ranked open questions, and one experiment at a time with its predicted
  number and kill criterion written down *before* the run.
  `docs/sync-speed-campaign.md` is the archive behind it — consult it so a
  measured rejection is not repeated, but a written conclusion there is not a
  result. Quote the measurement.
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
  `list` and `dump-links` **stay listed in `printHelp`** (maintainer's
  decision, 2026-07-23), even though the course picker removed the main
  end-user reason to run `list`. Anyone reaching for the CLI at all is
  someone who may want them; hiding a working command to tidy up help output
  is not a trade worth making.
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
- `internal/logging/` — the output path. Two axes, not one: a *level*
  (Debug/Info/Warn/Error, how bad) and an *audience* (user or diagnostic, who
  it is for), because "skipping section" is a genuine warning and also of no
  interest to a student who wants their slides. Two sinks read those
  independently — the console takes user-facing records plus every error
  (`--verbose` adds the rest), and a rotating file under
  `~/.opal-downloader/logs/` takes everything. Everything written to the file
  is scrubbed via `statuslog.SanitizeMessage` first, so the log is always safe
  to attach to a bug report without anyone remembering to check it. Call it
  with `logging.User/Detail/Warn/Error`; the printf shape is deliberate, since
  that is what every existing call site already looks like. The CLI's own
  `fmt.Println` output in `cmd/` is **not** migrated and should not be: a CLI
  printing its results to stdout is already exactly the user channel.

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

## How to write to the maintainer

Raised 2026-07-27: "es ist oft nicht klar, was fuer mich wichtig ist, welche
Fragen von mir beantwortet werden muessen, und es ist oft zu viel technisches
Detail."

The diagnosis: turn summaries were written as *reports* - organised by what got
done - when they should be *messages*, organised by what the reader has to do
about it. The clearest symptom was a turn that asked "which of these do you
want?" at the bottom, after roughly 500 words, when that question was the only
thing the turn actually needed.

Three rules:

1. **Anything you need from the maintainer goes first**, in the opening line,
   with a recommendation rather than a menu. If nothing is needed, say so
   plainly so they can stop reading. This is the one rule with a binary
   answer - the ask is either first or it is not.
2. **Then what changed, in plain language, short.** What is different for them
   now, not which files moved.
3. **Unverified work stays explicit, but briefly.** "UNVERIFIED: X because Y"
   is one line. The honesty requirement is satisfied by saying it, not by
   explaining it at length.

The detail does not disappear - it moves. Measurements, mutation tests, why an
approach was chosen over another: all of that belongs in the commit message,
`docs/`, or the backlog, where it is re-readable and where it already was.
Repeating it in chat means the maintainer reads it twice and finds the
important part neither time.

Technical depth is right when they asked a technical question, or when a number
is the answer. It is wrong as a default setting.

Known failure mode to watch for: the pull toward a long summary is strongest
after a lot of work has been done, because the work feels like it needs
showing. That instinct is exactly what produces the wall of text.

## Before you call something blocked, dead, or already-tried

Quote the measurement it rests on. A number, from a real run, with the
conditions it was taken under. If you cannot find one, it is an opinion
someone wrote down, not a result - and it does not block anything.

This exists because of a specific failure (2026-07-27). `docs/sync-speed-
campaign.md` records that every approach was tried and the target "is not
reachable by any approach identified so far". That was read twice, accepted
twice, and used to justify not looking - while the file itself named an
unexplored lead nobody had pulled on. Ten minutes of reading the actual
constants then turned up that ~170s of a ~227s run may be this tool's own
polling rather than OPAL being slow. Nobody had ever measured it.

Two things follow, and they are different:

- **Do not re-litigate a rejected approach without new evidence.** Still true.
  That rule is in this file for a reason and several approaches really were
  killed by real measurements.
- **"Do not re-litigate" is not "do not investigate".** A conclusion with no
  number behind it has not been tested, whatever its tone. Read the code, not
  the summary of the code.

The same applies to this project's own docs, including the ones written
confidently. A long, well-argued document is not evidence.

## How to organise yourself

`docs/agent-operating-model.md` is the standing answer. It is short now, and
the short version is shorter still:

**Stopping is not yours to decide.** If work remains in `docs/BACKLOG.md` and
nothing needs the maintainer's judgement, keep going. That is a rule you
follow, not a gate that catches you — the gates that used to enforce it were
removed on 2026-07-31 because they had become the thing that stopped the work.

**Two hooks remain, both enforcement, never behaviour:**
`pre-push-gate.ps1` blocks a push that hasn't passed `scripts/dev.ps1 all`,
and `turn-failure-checkpoint.ps1` saves uncommitted work when a usage-limit
kill ends a turn dead. Neither tells you what to do.

**A usage-limit kill is unpredictable and unpreventable**, so the goal is that
being killed costs the current turn and nothing more: commit small, and keep
**`docs/RESUME.md`** current *while* working. Budget pressure is never a
reason to stop, to pick a smaller task, to skip the harder half, or to avoid
subagents.

**Both hooks only exist if the session was started in this repo's
directory.** `.claude/settings.json` is project configuration; a session
opened elsewhere and pointed here by path gets neither, and says nothing about
it. Run `scripts/dev.ps1 all` by hand before every push unless you know the
gate is live.

**Keep this machinery small.** It is infrastructure for building
opal-downloader, not the project. If a session's output is another gate,
another doc about gates, or another entry in a backlog about gates, that is
the failure mode — not diligence. On 2026-07-31 that ratio was measured at
102 of 193 commits in seven days touching only `docs/`, `.claude/` or
`scripts/`; the cleanup that followed is why this section is this short.

**`docs/work-quality.md` is why these rules exist** — the measured
retrospective of how the workflow drifted, including the three rules that came
out of it: a rejection with no diagnosed cause is not a rejection; a campaign
with five investigation commits and nothing shipped is failing; and wanting to
build a gate is the signal to do the work instead. Read it before proposing any
new hook, doc, or process here.

## Task tracking: `docs/BACKLOG.md`, not the old queue

Work is tracked in `docs/BACKLOG.md` (see "Start here" above). Plain prose,
tracked in git, updated alongside the code.

`docs/RESUME.md` is its short-lived companion: what is in flight *right now*,
kept current while working rather than at the end. The backlog stays tidy and
says what should happen; the resume note is allowed to be messy and says where
you actually are. Clear it back to its placeholder line when the work lands.

The older `.claude/queue/` workflow — `task-capture` / `queue-run` /
`queue-review` against `todo/`, `in-progress/`, `done/`, `blocked/` — was
retired for this repo on 2026-07-22 and the skills themselves were deleted on
2026-07-31, having gone unused since 20 July. If you find an old
`.claude/queue/` directory on a machine, it is debris: `docs/BACKLOG.md` is
authoritative.

Why it was retired: the queue was gitignored, so the only record of in-flight
work couldn't survive a fresh clone; and it needed a skill to be *invoked*
before anything happened, which made autonomy depend on ceremony rather than
on simply owning the backlog.

## Branches, commits and PRs

**Default: commit small and push straight to `master`.** No branch, no PR.

That is already what happens, and the numbers say it is right. All 130 PRs this
repo has ever had were opened and merged by the same person; 121 of them lived
under three minutes and not one was reviewed by anybody. They stopped on
2026-07-22 at #130 — not by a decision, but because `.claude/queue/` was retired
that day and `queue-run` was the thing that opened them. The 227 commits pushed
straight to master in the nine days since caused no incident. `ci.yml` triggers
on `push` to master as well as on `pull_request`, so a PR runs no check a push
does not, and releases fire on a `v*` tag rather than on master — a briefly red
master ships nothing to any user.

**Branch and open a PR in exactly two cases:**

1. **You could not verify it.** Anything you would label `UNVERIFIED` to the
   maintainer goes on a branch, with that word in the PR title, instead of onto
   master. Pushing is still right — the work should survive the session — but
   master stays the line that has actually been checked.
2. **The change can silently lose the user's files.** `internal/scraper/crawl.go`,
   manifest key derivation in `internal/syncer`, and anything that deletes or
   renames files under the download root. All three have a documented history of
   silent loss; a PR buys a diff worth reading and one clean revert.

**Do not merge your own PR under either trigger.** Both exist so that somebody
looks — merging it yourself puts back exactly the ceremony this section removes.
Leave it open, name it in the turn summary, and go on to the next backlog item.
This deliberately narrows the maintainer's standing "merge PRs once checks pass"
autonomy for this repo, and only for these two cases; everything else still goes
to master without asking.

Anything else — "it feels big", "to be safe", a tidy-looking branch name — is not
a trigger. A PR nobody reads is a slower push.

## Maintenance

- `scripts/dev.ps1 all` — local build/vet/test/lint, run before every push
  (the pre-push gate enforces it, but only if the session was started in this
  directory — see "How to organise yourself").
- `scripts/test-fresh-install.ps1` — validates the no-credentials setup path
  (clone through `init`); see `docs/setup-friction.md` for known friction.
- `docs/manual-setup-checklist.md` — manual checklist for the
  credential-requiring tier (`login`/`list`/`sync`).
- `docs/OPERATIONS.md` — maintenance cadence and incident playbook.
