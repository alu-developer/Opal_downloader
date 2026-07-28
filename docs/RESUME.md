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

**Section cache piece 3 (wiring into crawl.go) is built, unit-tested, and
committed (`597bd6d`).** Off by default (`OPAL_SECTION_CACHE`). Not yet
live-verified against the real account — that is the only thing left before
this can be marked done in `docs/BACKLOG.md`.

**What landed:** `internal/scraper/sectioncachewiring.go` (the HTTP probe:
fetch a section over plain HTTP through the same `polite.Limiter` every
browser `Goto` uses, hash it, compare against `internal/sectioncache`), the
`crawl.go` loop change (probes a whole BFS level before dispatching to the tab
pool; a hit replays through the real `appendSectionFiles`/
`appendSectionFolderTargets` against cached candidates so subfolder discovery
and the show-all retry fields both survive), and `syncer.go`'s load/save
around a sync — gated on the same flag, so an unconfigured caller never
touches the cache file at all.

Proven with a `collectCourseFiles`-level test using a page that panics if any
browser method is touched: a two-level, fully-cached course must complete
without ever calling the browser. Confirmed to actually panic on the intended
mutation (disabling the hit branch) before being trusted.

**Left, in order:**

1. **Live verification**, the acceptance criterion from the start of this
   effort: a byte-for-byte diff of the file list against
   `tmp/filelist-repeat1_before.txt` (345 files) — never a count. Reuse
   `internal/scraper/filelist_probe_test.go`'s `TestFileListSnapshot` harness
   (`OPAL_FILELIST=<label> go test ./internal/scraper/ -run
   TestFileListSnapshot -v -timeout 30m`), the same one used for the previews
   A/B — it already does discovery-only (`ScrapeWithSavedSession`, no
   downloads) and writes a sorted course/section/name/URL list to `tmp/`.
   **It needs one addition first**: wire `sc.SetSectionCache(...)` into that
   test, gated on `scraper.SectionCacheEnv` exactly like `syncer.go` now does,
   so the same probe can be reused for this A/B the way it was for previews.
   Three runs needed: a fresh ground truth (or reuse the existing
   `filelist-repeat1_before.txt` if the account hasn't changed since), a cold
   run with the flag on (populates the cache, all misses — this pass alone
   tells you nothing about correctness or speed since nothing can hit yet),
   and a warm second run right after (this is the one that matters — hits
   should fire, and the file list must still diff empty against ground
   truth).
2. **Also worth capturing from the warm run**: how many sections hit vs
   missed, and the wall-clock delta vs the ~93s ceiling projected 2026-07-27.
   If the hit rate is far below what "unchanged since last sync" should
   produce, that is worth understanding before flipping this on by default
   anywhere — a mid-run session/page-version rollover invalidating hashes
   would show up here.
3. Only after (1) comes back clean: update `docs/BACKLOG.md`'s sync-speed
   entry with the result (found dead, or found real — either is a legitimate
   outcome, see the "unsolved is acceptable" rule) and clear this note.

**Not this session's decision, and already flagged in `docs/BACKLOG.md`:**
whether to actually flip the default on, and whether to let the effective
request rate rise toward `docs/server-load.md`'s ceiling to reach the ~70s
floor rather than ~93s — both are the maintainer's call once the live number
exists.
