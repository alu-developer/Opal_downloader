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
committed (`597bd6d`).** Off by default (`OPAL_SECTION_CACHE`). Live
verification is in progress (2026-07-28) — see the status block below for
exactly where it's at; do not restart from scratch.

**Live verification status (2026-07-28):**

- Ground truth was already captured by an earlier iteration today (11:17):
  `tmp/filelist-cache_ground_truth.txt`, 345 files, confirmed byte-identical
  to `tmp/filelist-repeat1_before.txt` via `diff` (exit 0). No need to
  re-run it unless the account has changed.
- The probe's missing piece — `sc.SetSectionCache(...)` wired into
  `TestFileListSnapshot`, gated on `SectionCacheEnv` exactly like
  `syncer.go` — is done and committed (`50fab5b`).
- Cold run in progress in the background: `OPAL_FILELIST=cache_cold
  OPAL_SECTION_CACHE=1 go test ./internal/scraper/ -run TestFileListSnapshot
  -v -timeout 30m` (stale `tmp/.opal-sync.sections.json` deleted first, so
  this is a genuine all-miss pass). Produces `tmp/filelist-cache_cold.txt`
  and the cache file `tmp/.opal-sync.sections.json`.
- **Next, once the cold run finishes:** diff `filelist-cache_cold.txt`
  against `filelist-cache_ground_truth.txt` (must be empty), then
  immediately run the warm pass with the same command but
  `OPAL_FILELIST=cache_warm` — leave the cache file from the cold run in
  place so hits can fire. Diff the warm output against ground truth too;
  that diff is the acceptance criterion. Also capture from the warm run:
  hit/miss counts if the `go test -v` output logs them, and the wall-clock
  delta vs the cold run and vs the ~93s/~210s figures already on record.
- Only after both diffs come back empty: update `docs/BACKLOG.md`'s
  sync-speed entry with the result and clear this note. A non-empty diff on
  either pass is a real correctness finding — write it up plainly, don't
  paper over it.

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
