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

**Piece 3, next and the only risky one — the design is now fully worked out.
Two traps were found by reading the crawl rather than by assuming, and both
would have caused silent loss.**

**Trap 1: skipping a section also skips discovering its children.**
`crawl.go:205` feeds `appendSectionFolderTargets` the *same* `candidates` the
file extraction uses, so a cache hit that returned only files would leave that
section's subfolders permanently unqueued. The course would come back short and
nothing would warn.

**The fix makes the cached path and the crawled path literally the same code:**
cache the `candidates` slice itself. On a hit, replay
`appendSectionFiles` and `appendSectionFolderTargets` against the cached
candidates exactly as a fresh crawl does. No parallel logic to drift, and
children fall out for free.

**Trap 2, and it explains the 52 MB.** Nine candidate keys are read across
`files.go` and `crawl.go`: `className`, `dataHref`, `dataUrl`, `href`,
`onclick`, `rootText`, `rowText`, `text`, `title`. Eight are per-candidate and
small. **`rootText` is the section root's entire textContent** - identical for
every candidate in that section, read once, by `deriveSectionTitle`
(`crawl.go:1142`). That is precisely the 31 MB of duplication the 2026-07-21
entry recorded. Store it **once per section** and restore it into each
candidate on load. The campaign entry already demanded interning; this is
exactly where it goes and why.

So the cache payload per section is: the candidates with `rootText` stripped,
plus one copy of `rootText`. The `sectioncache` API needs no change at all -
its payload is opaque JSON, and that decision now pays for itself.

**Remaining work, in order:**

1. An HTTP client in the scraper built from `s.stateFile`'s cookies (the probe
   in `htmlstability_probe_test.go` already does this - lift it).
2. Before visiting a section: fetch, `sectionhash.Of`, `cache.Unchanged`. On a
   hit, replay from cached candidates and skip the visit. On anything doubtful,
   crawl.
3. **The HTTP fetches must go through `gotoPolitely`'s rate ceiling** or an
   equivalent - `docs/server-load.md` is a standing constraint and this adds a
   request per section.
4. Off by default behind a flag until measured, per this campaign's own rule.

**Acceptance, unchanged and non-negotiable:** a byte-for-byte diff of the file
list against `tmp/filelist-repeat1_before.txt` (345 files). Never a count -
one section changed rows without changing their count in a single real run
tonight.

**Open question that is genuinely the maintainer's, not mine:** realising the
~70s floor rather than ~93s means letting the effective request rate rise
toward `docs/server-load.md`'s 4/s ceiling. It asks for the same *number* of
things while dropping payload enormously (91 KiB of HTML vs a full render plus
~30 MB of previews per course), but it is a policy call. The 2.3x win does not
depend on it.
