# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end - the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The scheduled Desktop task's prompt reads this file first, so stale content
here sends an unattended run after work that is already done. Clear it.

---

**In flight (2026-08-10, autopilot):** Question 32's prediction is written
and committed (`docs/sync-speed-model.md`). About to run two fresh
`TestFileListSnapshot` live crawls against the real account: a
`course_concurrency=1` baseline (`OPAL_FILELIST=q32-conc1`), then the
untested combination `OPAL_COURSE_CONCURRENCY_OVERRIDE=2
OPAL_DEBOUNCE_MS_OVERRIDE=150` (`OPAL_FILELIST=q32-conc2-deb150`), then a
byte-diff between them. Discovery-only, scratch `download_path`, no writes
to the real sync folder or `config.yaml`. If this turn dies mid-run, the two
`tmp/filelist-q32-*.txt` files (if either completed) are the only recoverable
state — rerun the missing side and diff.

Six questions landed 2026-08-09 (autopilot): Q27 (warm-session delta
confirmed, mostly `go test` noise), Q28 (pinned that noise to `go test`'s
own cache-staleness check), Q2 and Q6 (both closed as documentation debt,
no live run), Q30 (OpenOLAT's folder browser does offer a participant
bulk-ZIP download, but bounded to the 86s first-sync floor, not the 207s
crawl floor — no live run needed to find that out), and Q24 (closed live:
6 trials, 0 truncated, but the original prediction used the wrong reference
rate — see below). Fifth-cycle report appended per the reporting cadence.

**Maintainer decision, same day: the "one live-run batch per day"
self-caution this campaign had been applying is retired** — server load was
never actually bound by that (`docs/server-load.md`'s real mechanisms are
unchanged), it was just this campaign rationing its own cycles. Proceeded
straight into Question 24's live run the same session rather than waiting.

**Question 24's run also surfaced a real methodology hazard: `go test`
silently cached and replayed one trial instead of re-executing it** (identical
env vars, no `-count=1` — confirmed by a byte-identical log with the network
call never actually happening). Every run after that used `-count=1`. This
cannot be retroactively ruled out for Questions 20/21's older "N clean runs
in a row" batches (their raw logs are gone) — recorded as an open caveat on
those two closed questions, not a reopening. **`-count=1` is now required
for any repeated-trial live-run design in this campaign** going forward.

Question 31 (does the Question 25 fix also survive `course_concurrency>1`
contention) ran to completion the same session: 4-trial 2-course probe
clean (0/6 truncated across it and Q24 combined that day), then a full
6-course/349-file byte-diff at `course_concurrency=1` vs `=2` — also empty.
**Correctness is refuted at full scale; speed is not** — concurrency alone
ran 17% *slower* in the full-account pass, matching what this project
already knew (`docs/BACKLOG.md`'s "Concurrency REOPENED" entry). The 85%
wall-clock win the 2-course probe found came from pairing concurrency with
the 150ms debounce override, not concurrency by itself, and that pairing
has not been tested at full scale — opened as Question 32, now top of the
queue, needs its own prediction written and committed before it runs, per
Rule 1. `OPAL_COURSE_CONCURRENCY_OVERRIDE` was added to
`filelist_probe_test.go` to make this testable without ever touching the
maintainer's real `config.yaml` (which stayed at its shipped default of 1
throughout — verified via `git diff config.yaml` empty at every point in
this session).

Full write-up in `docs/sync-speed-model.md` (Questions 24, 30, 31, 32);
short versions in `docs/BACKLOG.md`'s "Done recently". Ten live crawls this
session (6 for Question 24, 4+2 for Question 31), all against the real
account, all discovery-only (no downloads, no writes to the real sync
folder). Six commits, `go build`/`go vet` clean throughout.
