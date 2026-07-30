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

## In flight: the jsTree completion-signal lead (sync speed)

`docs/BACKLOG.md`'s "Sync speed" entry / `docs/sync-speed-campaign.md`'s
2026-07-30 entry found a real candidate: the course-navigation jsTree widget
sets `aria-busy="false"` as its own completion signal, confirmed across 4
courses' root sections via `internal/scraper/mutationmarker_probe_test.go`
(`OPAL_MUTATION_MARKER_TRACE=1`, `OPAL_MUTATION_MARKER_COURSE=<name>`). First
signal this whole campaign has found after three rejected attempts at others.

**Not implemented. Two open questions before anything is built**, both
answerable with the same probe, cheaply (a few seconds per section, reuses
the saved session):

1. **Does the signal fire on non-root sections too?** All 4 samples so far
   are course *root* sections. The probe currently only visits `course.URL`
   (see `visitSection(page, course.URL, course.Title)` in the test) - it
   would need a real subfolder section URL to test this, which means either
   extending the probe to accept a URL directly, or discovering one section
   URL cheaply first (e.g. querying the root page's own section links rather
   than running a full course crawl).
2. **How does it order against MathJax on courses that use it?** MathJax
   finished after jsTree in 2 of 4 samples, before it in 1 - a jsTree-only
   wait could read a math-heavy section before its content has actually
   rendered. Needs a few more samples specifically on math-content sections,
   ideally ones where MathJax has real formulas to typeset (not just the
   library loaded but idle).

**After those:** building the actual replacement wait condition is real work
in `navigation.go`'s `waitForInteractiveLinks`/`waitForContentSettled`, the
most correctness-sensitive part of this codebase (documented history of
*silent* file loss from changes here). It needs the same byte-for-byte
ground-truth comparison every prior change in this campaign has been held
to (`scripts/compare-visit-runs.ps1` + the 345-file full-account baseline
already exist for this) - not a quick continuation.

**Do not lose:** the two "needs your eyes" GUI-review backlog items are
still unanswered by the maintainer - unrelated to this, just still true.
