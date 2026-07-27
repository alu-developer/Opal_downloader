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

**Building the change-detection cache (2026-07-27, ~23:30). Piece 1 of 3 done.**

The sync-speed question moved a long way tonight and this is where it stands.
Everything below is committed.

**Settled and closed, do not redo:**

- The 300ms debounce cannot go. Skipping the settle wait is byte-identical and
  **51% slower** (317.1s vs 210.3s); skipping it while asserting its calm
  verdict is still 293.5s. It is not overhead - it is the *cheap* way to wait,
  and the stability poll is the expensive way. Three measured attempts, all
  negative (`dd68b0e`).
- Blocking file previews is safe but costs ~26-31%, and the cost is `ctx.Route`
  itself: a route matching **nothing** still costs ~62s (`1be1001`).

**The live thread: the change-detection cache, reopened by measurement.**
July rejected it because normalised section HTML matched across runs 0 of 276
times. That entry named a diagnostic it never ran. Run now, the entire
difference is Wicket bookkeeping - a page-version counter, generated component
ids, a table-widget instance counter. Normalise those and the match rate is
**11 of 12**, with a file-bearing section byte-identical (`350ecc1`).

Safety is tested and passes: a file renamed, a row added, one character changed
are all still visible after normalisation (`2e4ba1e`). That asymmetry is the
design - a miss costs one crawl, a false match stops downloads silently.

**The ceiling, measured BEFORE building** (the previous attempt built first and
then found nothing hit): HTTP fetch is 331ms mean, 91 KiB/section, so 280
sections is **~93s** against today's 210.3s. A 2.3x win, and the ~30s target is
out of reach for this design too - stop quoting it (`b471609`).

**Piece 1 done:** `internal/sectionhash` - normalise + SHA-256, pure, no
integration, fully tested including the case a file count cannot see (same row
count, different rows).

**Piece 2, next:** the cache file. **Design decision made tonight and not yet
written anywhere else:** store only `sectionURL -> hash`, NOT the extractor
output. The previous build stored extractor output and produced a **52 MB**
file for 276 sections. It does not need to: on a hit, that section's files can
be taken from the previous run's own results, and the manifest already records
course + section per file. So the cache is tiny and the interning problem
disappears entirely.

**Piece 3:** wire it into `collectCourseFiles` - fetch + hash before crawling a
section, crawl on any miss or any doubt. Then measure a warm no-op sync against
the 210.3s baseline, with a byte-for-byte file-list diff as the acceptance test,
never a count.

**Open question that is genuinely the maintainer's, not mine:** realising the
~70s floor rather than ~93s means letting the effective request rate rise
toward `docs/server-load.md`'s 4/s ceiling. It asks for the same *number* of
things while dropping payload enormously (91 KiB of HTML vs a full render plus
~30 MB of previews per course), but it is a policy call. The 2.3x win does not
depend on it.
