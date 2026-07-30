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

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

## In flight: implementing the jsTree completion signal (sync speed)

`docs/sync-speed-campaign.md`'s 2026-07-30 entries found a real candidate and
answered both follow-up questions this session:

- The course-navigation **jsTree** widget sets `aria-busy="false"` (+
  `aria-activedescendant`) as its own completion signal.
- Confirmed on **6 sections across 4 courses, root and non-root alike** -
  generalizes, not a root-only artifact.
- Its ordering against **MathJax** (on courses that use it) is genuinely
  inconsistent - sometimes MathJax finishes first, sometimes jsTree does,
  even across different sections of the same course. **A jsTree-only wait is
  not safe** as a drop-in replacement for the debounce.

`internal/scraper/mutationmarker_probe_test.go`
(`OPAL_MUTATION_MARKER_TRACE=1`, `OPAL_MUTATION_MARKER_COURSE=<name>`) is the
reusable probe; re-run it any time for more samples.

**Not implemented yet. The next step is a real implementation task**, not
more probing:

1. Build a combined wait condition in `navigation.go`
   (`waitForInteractiveLinks`/`waitForContentSettled`): wait for jsTree's
   `aria-busy="false"`, AND, only when `typeof MathJax !== 'undefined'` on
   the page, also wait for MathJax's own completion (it exposes queueable
   "done" callbacks in the version OPAL loads - confirm the exact API before
   relying on it).
2. Gate it behind an env flag the same way every other experimental wait
   condition in this codebase is (`OPAL_SKIP_SETTLE_WAIT` etc.), so it can be
   A/B tested without risking a real sync.
3. **Byte-for-byte ground truth is non-negotiable here** -
   `scripts/compare-visit-runs.ps1` + the 345-file full-account baseline
   already exist for exactly this. A file *count* is not acceptable evidence
   (this project has been burned by that twice); the sorted file list must be
   identical.
4. This is the most correctness-sensitive code in the repo (documented
   history of *silent* file loss from changes here) - if in doubt about
   whether an ordering edge case is safe, that doubt is itself a reason to
   test more before shipping, not a reason to skip the test.

**Do not lose:** the two "needs your eyes" GUI-review backlog items are
still unanswered by the maintainer - unrelated to this, just still true.
