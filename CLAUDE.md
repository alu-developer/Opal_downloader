# opal-downloader — things I got wrong

Go tool that logs into OPAL with Playwright, scrapes course files, and syncs them
to a local folder. Single binary, two front ends: a local web GUI (default) and a
CLI. Architecture: `docs/architecture.md`. **Current work: `docs/BACKLOG.md`** —
read it, take the top unblocked item, get on with it.

This file is a list of beliefs I acted on here that turned out to be false. Each
one cost real time or real files. The correction is the rule; the evidence is why
it isn't up for re-litigation.

---

## Running the tool

**"Unattended runs can't log in, because 2FA needs the maintainer."**
False. TU-Fast is installed in the dedicated login profile and completes
credentials *and* 2FA by itself — `login`/`sync`/`list` trigger it automatically
when the saved session is stale, with nobody at the machine. Live-verified
2026-08-01: expired state → auto-login → 8 courses in 3.7s, no click. So never
report "needs the maintainer for 2FA/fresh cookies", and never treat an expired
session as a blocker. Just run the command. Only a run that actually failed is a
blocker, and then quote its error. This belief cost a whole sync-speed cycle
before it was killed.

**"`go build ./cmd/opal-downloader` builds the binary."**
It silently produces a library archive instead of an executable. Use `go build .`.

**"Running things is what costs budget."**
Backwards. A 5-minute full-account scrape is one tool call and nearly free;
what costs is reasoning turns and large tool outputs pulled into context. So:
filter every command's output at the source, never dump a 900-line log, run long
jobs in the background and wait for the completion notification instead of
polling, and prefer one instrumented run answering three questions over three
runs. A backgrounded command dies with the session that started it — a long
verification run must write its result to a file under `tmp/` or `docs/`, not
only to stdout.

**"Asking OPAL for more, or faster, is free."**
It is not, and server load is a standing constraint rather than a one-off check.
Read `docs/server-load.md` before any change that increases request volume or
rate. It pulls directly against the sync-speed work and that trade-off is
written down there.

## The code

**"The file count matched, so nothing was lost."**
Both of this project's known data losses would have passed a file-count check.
Anything touching discovery needs a **byte-for-byte diff** against the 345-file
ground truth (`scripts/compare-visit-runs.ps1`). A count is not evidence.

**"A manifest key is a path."**
It is not. Assuming so triggered a migration that moved 96 files and left 26
orphans (2026-07-21).

**"Wicket's `AJAX_CALL_DONE` means the DOM is complete."**
It does not. Reading at that signal lost 52 files (2026-07-21). The stability
poll stays, and "faster" gets proven by a byte-for-byte diff, not by the signal
firing earlier.

**"Concurrency is free speed."**
Remeasured 2026-07-26: `course_concurrency=2` lost 9 files **and was no faster**.
Section-level concurrency lost 38% and is off. `DefaultCourseConcurrency = 1` for
this reason. As of 2026-08-03 there is a named mechanism: under contention a
paginated section's "show all" click path drops its second page — the loss is not
in the settle budget (`docs/sync-speed-model.md`, Question 17).

**"If the change signals are missing, on-disk size will do as a fallback."**
It will not. That nil-guard froze 33% of manifest entries forever (PR #117,
2026-07-22). Signalless files are byte-verified instead (PR #123). On-disk size
is not a usable third signal.

**"Reliability can wait until the feature ships."**
Wrong order. A missing file or a broken selector is an incident to fix
(`docs/OPERATIONS.md`), not an acceptable steady state. When trading "ship a
capability" against "make the existing path robust", robustness wins by default.

## Judging evidence

**"It's written down in our own docs, so it's established."**
A long, confidently-argued document is not evidence. Read the code, not the
summary of the code. Before calling anything blocked, dead, or already-tried:
**quote the number, from a real run, with the conditions it was taken under.** No
number means it is an opinion someone wrote down, and it blocks nothing.
`docs/sync-speed-campaign.md` is the archive that taught this — consult it, but
don't mistake its conclusions for results.

**"The file I read is current."**
Not necessarily. On 2026-08-03 docs read 18 commits out of date produced a whole
round of already-answered questions. Check `git log` for the file when its
content drives a decision.

**"That approach was rejected, so it's dead."**
A rejection with no diagnosed cause is not a rejection. HTTP-first discovery was
killed two hours into the campaign as "fast, and it silently loses courses"; ten
days later a session that actually diagnosed it verified diff=0 across all 6
courses. "Don't re-litigate a rejected approach without new evidence" is still
true — but it is not "don't investigate".

**"The measurement is what the instrument reported."**
Check the instrument first. The first network probe read `Content-Length` on
chunked Wicket responses — it would have reported "0 bytes" for every section
page and falsely refuted a live hypothesis, not because the responses were small
but because the instrument was blind.

## How I work

**"The tests are green, so the work is done."**
The tests are written by the same agent, in the same turn, as the code — so a
half-change arrives with tests asserting exactly the half that was built, and the
suite goes green *because* the work was scoped down. Green means internally
consistent and nothing else. Verify the way the change's own failure mode demands:
a UI change by looking at it, a data-loss-capable change by a byte diff, a hook by
a test that fails when the hook is removed. `docs/work-quality.md` is the measured
retrospective.

**"It's wired up, so it runs."**
Three separate times a correct-looking gate had never once produced its effect.
Verify a hook by finding its output in the real repo, never by reading its code.
A watchdog whose failures are silent is worse than none, because it looks like a
working safety net.

**"More machinery will make me more autonomous."**
It did the opposite. Every complaint that I had stopped working produced a new
gate, and each gate became a new place to stop — 102 of 193 commits in one week
touched only `docs/`, `.claude/` or `scripts/`. Hooks enforce, never instruct;
two exist, both enforcement (`pre-push-gate.ps1`, `turn-failure-checkpoint.ps1`).
Prefer the first-party feature: unattended runs are Desktop Routines managed in
the app, with prompts in `~/.claude/scheduled-tasks/<name>/SKILL.md`, not scripts.
**Wanting to build a gate is the signal to do the actual work instead.**

**"'Implement X' can quietly become 'document X'."**
No. Narrowing the scope of a task is the maintainer's call, not mine. If part of
the work is genuinely blocked, finish everything else and say explicitly what was
left out and why.

**"It's marked done, so it's on master."**
Often it is not — a bug reported "again" is frequently already fixed on an
unmerged UNVERIFIED branch. Check open PRs before re-diagnosing.

**"Checks passed, so I can merge my own PR."**
Not for the two cases that require a PR at all. **Default: commit small, push
straight to `master`** — no branch, no PR; CI runs on push to master and releases
only fire on a `v*` tag. Branch and open a PR in exactly two cases: (1) work I
could not verify, with `UNVERIFIED` in the title; (2) a change that can silently
lose files — `internal/scraper/crawl.go`, manifest key derivation in
`internal/syncer`, and anything that deletes or renames files under the download
root. **Do not merge your own PR under either trigger** — both exist so somebody
looks. Leave it open and name it in the turn summary.

**"Budget pressure is a reason to stop, or to pick the smaller task."**
It is not, and neither is session length. Commit small so a kill costs one turn
and nothing more, keep `docs/RESUME.md` current *while* working rather than at the
end, and stage explicit paths — never `git add -A`, since another session's files
may appear mid-run. Stopping is only mine to decide for four things: a change that
would delete or overwrite the maintainer's real files, a stated project decision
that would have to change, a login that genuinely needs them at the keyboard (an
expired session does not — see the top of this file), and a genuine fork between
two designs that reasoning does not settle. Mark those **Blocked:** in the backlog
*with concrete options to choose between*, and carry on with the next item.

---

## Standing facts that aren't mistakes

- **Ease of use outranks almost everything**, first-install and long-term.
  Structural proposals are welcome — setup model, distribution, walking back an
  architecture decision. Don't self-censor them as "too big". State:
  `docs/setup-friction.md`, `docs/installer-plan.md`,
  `docs/browser-profile-strategy.md`.
- **Local-only.** No backend, no cloud service, none planned.
- **Credentials and session data never leave the machine unscrubbed.** Crash and
  error reports are encouraged, but scrub credentials, cookies and session tokens
  first. That carve-out is diagnostics only — usage analytics stay out of scope.
- **Commands:** `scripts/dev.ps1 all` (build/vet/test/lint, before every push) ·
  `scripts/test-fresh-install.ps1` (no-credentials setup path) ·
  `docs/manual-setup-checklist.md` · `docs/OPERATIONS.md`.
- The two hooks only exist if the session was started in this directory. A session
  opened elsewhere and pointed here by path gets neither and says nothing about
  it, so run `scripts/dev.ps1 all` by hand before pushing unless you know the gate
  is live.
- Routine edits to this file — keeping facts accurate, adding a mistake — go in
  directly; flag them in the turn summary. Ask first when an edit would change a
  stated decision.
