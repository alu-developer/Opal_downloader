# Resume note

Scratch state for work that is **in flight right now**. Kept in git so it
survives a killed turn, a dead session, and a fresh clone.

`docs/BACKLOG.md` says what should happen and stays tidy. This file is allowed
to be messy: it is the thought that would otherwise only exist in a context
window, and a context window does not survive the usage limit being hit
mid-turn.

**Keep it current while working**, not at the end — the end is exactly the part
that does not always arrive. Update it whenever the answer to "what am I doing
and what's next" changes materially. When the work lands, clear it back to the
placeholder line below.

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

## In flight: verifying section-level concurrency against the real account

Implementation is **built, tested and committed** (`ca299c5`) but **NOT
verified** — which is the only thing that decides whether it stays. Design and
rationale are in that commit message and in `internal/scraper/section_pool.go`.

### The rule this must satisfy
`docs/sync-speed-campaign.md`: byte-for-byte file-set parity with the serial
ground truth, multiple runs. **A faster run that finds fewer files is a
failure, not a tradeoff.** Course concurrency 4 once lost 9 files while looking
20% faster; that is the precedent.

### Experiment design
`--section-concurrency` was added to `list`/`sync` for this. Comparison unit is
per-course file counts plus the total, which is the campaign's own established
unit (past entries read "Analysis: -8 files", "337, 339, 331, 333 of 345").

- **Run A (baseline):** `--section-concurrency 1`. Also checks the refactor did
  not change the serial path, which it touched.
- **Run B, repeated:** `--section-concurrency 4` (the proposed default).
- `course_concurrency` is held constant at its config value (2) across both, so
  the only variable is section concurrency.
- Expected reference from earlier today's live GUI run: **6 courses, 345
  files**.

### Deliberately NOT done yet
`main.exe` has **not** been rebuilt. The maintainer's real 08:00 daily sync
task points at it, and the new default (`DefaultSectionConcurrency = 4`) is
exactly what is unverified. A/B runs use `tmp/opal-abtest.exe`. Rebuild
`main.exe` only once parity holds — or after setting the default back to 1.

### Where I am — first result is a RED FLAG, not a green light

**Run A** (`--section-concurrency 1`, course concurrency 2 from config):
**336 files in 228.2s**, against an expected 345. Per course:
`2026 LA20 39 | AlgoDat 38 | Analysis 21 | So26 Prog 14 | SWT 207 | NuMa 17`.
**Analysis came back 21, not 30 — nine files short**, and this was the run that
was supposed to be the *unchanged* serial path.

Two candidate explanations, and they need separating before any number from
this campaign means anything:

1. **The known course-concurrency race.** `config.yaml` has
   `course_concurrency: 2`, and the campaign's 2026-07-17 entry recorded
   exactly this shape: "Analysis: -8 files in 3 of 4 runs" at course
   concurrency 2. If so, run A is contaminated by a pre-existing bug and says
   nothing about section concurrency — but it also means
   `DefaultCourseConcurrency = 2` is still losing the maintainer's files on
   every real sync, which would be a serious finding in its own right and
   contradicts the "Concurrency SOLVED" note in the backlog.
2. **My refactor broke show-all expansion.** Analysis is the course whose
   "Übungsblätter" section holds 28 files against OPAL's ~20-item page size,
   i.e. an 8-file overflow that only appears if the "Alle anzeigen" control is
   clicked. Losing ~9 files from exactly that course is uncomfortably close to
   losing exactly that overflow. `visitSection` now owns the
   `expandShowAllInSection` call and its multi-return assignment; that is the
   first place to look.

**Deciding experiment, running now:** `--course-concurrency 1
--section-concurrency 1` with the new binary. That is the campaign's true
serial ground truth with every concurrency off.
- Comes back **345** → my serial path is intact, and explanation 1 holds.
- Comes back **336** → my refactor is losing files; fix that before anything
  else, and do not trust any earlier number.

Do not proceed to measuring section concurrency until this is resolved.
