# opal-downloader

Go tool that logs into OPAL (Bildungsportal Sachsen) with Playwright, scrapes
course files from the DOM, and syncs them to a local folder. Single binary, two
front ends: a local web GUI (default) and a CLI.

What the packages are and how login works: `docs/architecture.md`. Read it when
you need it, not by default.

## Start here

**`docs/BACKLOG.md` is the current state of work.** Read it, pick the top item
that isn't blocked, get on with it — without being asked to. Update it in the
same commit as the work it describes.

**Stopping is not yours to decide.** If work remains and nothing needs the
maintainer's judgement, keep going. Only four things need it: a change that
would delete or overwrite their real files, a stated project decision or
principle that would have to change, a login that genuinely needs them at the
keyboard (see below — an expired session does not), and a genuine fork between
two designs that reasoning does not settle. Mark those **Blocked:** in the
backlog *with concrete options to choose between*, and carry on with the next
item.

**An expired session is not a blocker.** TU-Fast is installed in the dedicated
login profile and completes credentials and 2FA by itself; `login`/`sync`/`list`
trigger it automatically when the saved session is stale, with nobody at the
machine. Live-verified 2026-08-01: expired state → auto-login → 8 courses in
3.7s, no click. So never report "needs the maintainer for 2FA/fresh cookies" —
just run the command. Only a run that actually failed is a blocker, and then
quote its error. How it works: `docs/architecture.md`.

The maintainer thinks out loud. A passing remark about a problem or an
annoyance is a real input — turning it into a backlog entry is part of the job,
not something to ask permission for.

`docs/RESUME.md` says what is in flight *right now*. Keep it current while
working, not at the end; clear it to its placeholder when the work lands. A
usage-limit kill is unpredictable, so commit small — being killed should cost
one turn and nothing more. Budget pressure is never a reason to stop, to pick a
smaller task, or to skip the harder half.

## Standing constraints

- **Ease of use outranks almost everything**, first-install and long-term.
  Structural proposals are welcome — setup model, distribution, walking back an
  architecture decision. Don't self-censor them as "too big". Current state:
  `docs/setup-friction.md`, `docs/installer-plan.md`,
  `docs/browser-profile-strategy.md`.
- **Server load is a standing constraint, not a one-off check.** Policy:
  `docs/server-load.md`. Read it before any change that makes the tool ask OPAL
  for more, or ask faster — it pulls directly against the sync-speed work, and
  that trade-off is written down there. `docs/sync-speed-model.md` drives that
  work: open questions ranked, one experiment at a time, predicted number and
  kill criterion written down *before* the run.
- **Credentials and session data never leave the machine unscrubbed.** Crash
  and error reports are encouraged, but scrub credentials, cookies and session
  tokens first. That carve-out is for diagnostics only — usage analytics and
  behavioural tracking stay out of scope.
- **Reliability over features.** A missing file or a broken selector is an
  incident to fix (`docs/OPERATIONS.md`), not an acceptable steady state. When
  trading "ship a capability" against "make the existing path robust",
  robustness wins by default.
- **Local-only.** No backend, no cloud service, none planned.

## Quote the measurement

Before calling anything blocked, dead, or already-tried: quote the number, from
a real run, with the conditions it was taken under. No number means it is an
opinion someone wrote down, and it blocks nothing.

"Do not re-litigate a rejected approach without new evidence" is still true.
But it is not "do not investigate" — including for this project's own docs. A
long, confidently-argued document is not evidence. Read the code, not the
summary of the code. (`docs/sync-speed-campaign.md` is the archive that taught
this the hard way; consult it, but don't mistake its conclusions for results.)

## Budget goes on reading output, not on running things

Measured here: **a long live run is nearly free in tokens; reading its output is
not.** A 5-minute full-account scrape is one tool call. What costs budget is
reasoning turns and large tool outputs pulled into context.

- Filter every command's output at the source. Never dump a 900-line log.
- Run long jobs in the background and wait for the completion notification.
  Never poll in a loop — each poll is a turn.
- A backgrounded command dies with the session that started it. A long
  verification run must write its result to a file under `tmp/` or `docs/`, not
  only to stdout, and `docs/RESUME.md` must never call a job "in flight"
  without saying where its result will land.
- Batch verification — one instrumented run answering three questions beats
  three runs. Prefer a decisive experiment over more reasoning.

## Do the work, don't build machinery to do the work

Hooks enforce, never instruct. Two exist, both enforcement: `pre-push-gate.ps1`
blocks a push that hasn't passed `scripts/dev.ps1 all`, and
`turn-failure-checkpoint.ps1` saves uncommitted work when a turn dies. Neither
tells you what to do — behaviour belongs in the prompt.

Both only exist if the session was started in this repo's directory. A session
opened elsewhere and pointed here by path gets neither and says nothing about
it, so run `scripts/dev.ps1 all` by hand before pushing unless you know the
gate is live.

Prefer the first-party feature over your own version of it. Unattended runs are
first-party Claude Code Desktop scheduled tasks, not scripts: the prompts live
in `~/.claude/scheduled-tasks/<name>/SKILL.md` (`opal-downloader-autopilot`,
`-weekly-review`) and everything else — schedule, model, permissions, run
history — is managed in the Desktop app under **Routines**. Autopilot works the
backlog first and falls through to the sync-speed campaign when nothing there
is unblocked; it absorbed the separate `-sync-speed` task on 2026-08-03, after
the two fired within milliseconds of each other and raced on the shared browser
profile.
They fire only while the app is open and the machine awake, which is the price
of running locally; local is required because verification here needs the
maintainer's real OPAL account and real files. **Wanting to build a gate is the
signal to do the actual work instead.** If a session's output is
another gate, another doc about gates, or another backlog entry about gates,
that is the failure mode. When the same mistake happens twice, add a test or a
lint rule — not a paragraph here. `docs/work-quality.md` is the measured
retrospective of how this went wrong before; read it before proposing any new
hook, doc or process.

## Branches, commits and PRs

**Default: commit small, push straight to `master`.** No branch, no PR. `ci.yml`
runs on push to master as well as on PRs, and releases fire on a `v*` tag, so a
briefly red master ships nothing to anyone.

**Branch and open a PR in exactly two cases:**

1. **You could not verify it** — anything you would label `UNVERIFIED`, with
   that word in the PR title. Push it so the work survives, but keep master the
   line that has actually been checked.
2. **The change can silently lose the user's files** —
   `internal/scraper/crawl.go`, manifest key derivation in `internal/syncer`,
   and anything that deletes or renames files under the download root. All
   three have a history of silent loss.

**Do not merge your own PR under either trigger** — both exist so somebody
looks. Leave it open and name it in the turn summary. This narrows the
maintainer's standing "merge once checks pass" autonomy for these two cases
only.

Nothing else is a trigger. A PR nobody reads is a slower push.

## How to write to the maintainer

1. **Anything you need from them goes first**, in the opening line, as a
   recommendation rather than a menu. If nothing is needed, say so plainly so
   they can stop reading.
2. **Then what changed, in plain language, short.** What is different for them
   now — not which files moved.
3. **Unverified work stays explicit, but brief.** "UNVERIFIED: X because Y" is
   one line.

Technical depth is right when they asked a technical question, or when a number
is the answer. It is wrong as a default. Measurements, rejected alternatives
and rationale go in the commit message, `docs/`, or the backlog — repeating
them in chat means they read it twice and find the important part neither time.

The pull toward a long summary is strongest after a lot of work, because the
work feels like it needs showing. That instinct is what produces the wall of
text.

## Editing this file

Routine edits — keeping facts and workflow notes accurate — go in directly, no
check-in needed; just flag them in the turn summary. Ask first when an edit
would change a stated *decision* or principle.

Keep it short. It is loaded into every session, and instructions that don't
earn their place cost tokens and dilute the ones that do.

## Commands

- `scripts/dev.ps1 all` — build/vet/test/lint. Before every push.
- `go build .` — not `go build ./cmd/opal-downloader`, which silently produces
  a library archive instead of an executable.
- `scripts/test-fresh-install.ps1` — the no-credentials setup path (clone
  through `init`).
- `docs/manual-setup-checklist.md` — manual checks for `login`/`list`/`sync`.
- `docs/OPERATIONS.md` — maintenance cadence and incident playbook.
