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

### Where I am
Run A launched in the background. Next: read its per-course counts, then run B
twice and compare. If any run loses files, the honest outcome is to set
`DefaultSectionConcurrency = 1` and report the axis as tried-and-rejected with
numbers, which the campaign log explicitly treats as a valid result.
