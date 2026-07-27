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

**Building the change-detection cache (2026-07-27, ~23:50). Pieces 1 and 2 done.**

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

**Piece 2 done:** `internal/sectioncache` - `.opal-sync.sections.json` beside
the manifest, storing only `sectionURL -> hash`, NOT the extractor output. The
previous build stored the output and produced a **52 MB** file for 276
sections; on a hit that section's files are already known from the previous
run, so none of it is needed and the interning problem disappears.

Two safety mechanisms in it worth not undoing: `sectionhash.PatternsVersion` is
stored in the file and entries from a different version are refused (widening a
pattern would otherwise make old hashes match HTML they should not), and a
section not recorded this run is dropped rather than carried over, so a section
removed from OPAL cannot answer "unchanged" forever.

**Piece 3, next and the only risky one — but the design in "piece 2 done"
above needs one correction first, found while starting the wiring:**

A hit has to *return that section's files*, and nothing currently stores them
per section. `SectionURL` exists on `DiscoveryProgress` (`crawl.go:175`) but
**not** on `FileRef` (`internal/scraper/types.go:15`) and not on `RemoteFile`
(`scraper.go:19`). The manifest records course + section *titles*, not URLs, so
it cannot supply them either without a schema change and migration.

**So the cache must store the file rows after all, and that is fine.** The
52 MB disaster of 2026-07-21 came from storing the extractor's raw candidates,
where the same section text was duplicated across 9,958 of them. Storing the
resulting FileRef fields instead - course, section, name, URL, size, modified -
is about 345 rows of ~200 bytes, i.e. **~70 KB**. It was the wrong *thing*
being stored, not the storing.

Concretely, before wiring:
1. Add `SectionURL` to `FileRef` (populated at the point crawl.go already has
   `currentURL`), so a file knows which section produced it.
2. Widen `sectioncache` from `sectionURL -> hash` to
   `sectionURL -> {hash, files[]}`. Bump `SchemaVersion`; the existing
   degrade-to-crawl paths already cover an old file.

**Then** wire it into `collectCourseFiles` - fetch + hash before crawling a
section, crawl on any miss or any doubt. Then measure a warm no-op sync against
the 210.3s baseline, with a byte-for-byte file-list diff as the acceptance test,
never a count.

**Open question that is genuinely the maintainer's, not mine:** realising the
~70s floor rather than ~93s means letting the effective request rate rise
toward `docs/server-load.md`'s 4/s ceiling. It asks for the same *number* of
things while dropping payload enormously (91 KiB of HTML vs a full render plus
~30 MB of previews per course), but it is a policy call. The 2.3x win does not
depend on it.
