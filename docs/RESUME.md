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

**In flight (2026-08-10, autopilot): Question 36 Step B2 production restructure,
on branch `restructure-hybrid-http-first-discovery`.**
`scrapeCoursesHTTPFirst` (`internal/scraper/orchestrator.go`) and
`discoverSectionsHTTP` (`internal/scraper/httpdiscovery_seed.go`) are written,
unit-tested offline (5 tests), and wired behind `OPAL_HTTP_DISCOVERY=2` in
`ScrapeWithSavedSession` (`scraper.go`). Build/vet/full test suite pass.

Two live runs so far, both diagnosed and fixed in-branch (see
`docs/sync-speed-model.md` Question 36 Step B2 for the full mechanism of
each): run 1 hung 20+ minutes on an untimed `fetch.Get` (fixed:
`httpGetOptions()`, 20s budget on every HTTP GET); run 2 passed the
byte-diff (349 files, empty diff) but took 4m45s / 604 requests because
every section was fetched twice (fixed: `discoverSectionsHTTP` now extracts
files from the same body it already parses for child links, no second
fetch).

**Run 3 is in progress right now** (`OPAL_FILELIST=after
OPAL_HTTP_DISCOVERY=2`, background bash task `bfl8nydb0`, started ~2026-08-10,
10m timeout) - the first run expected to actually test the timing
prediction (<130s, ~320 requests) since run 1 never finished and run 2's
number was inflated by the bug just fixed. If this file is still here and
that run's result isn't yet in `docs/sync-speed-model.md`, the run's
completion is what to check for before doing anything else - do not start a
run 4 without reading run 3's outcome first.

Once run 3 passes both gates (empty diff AND under-130s), what's left: write
the result into `docs/sync-speed-model.md`, decide (with maintainer options,
not a unilateral call) whether `OPAL_HTTP_DISCOVERY=2` becomes the default
now or needs a second day's confirmation run, update `docs/BACKLOG.md`'s Now
item, push the branch, and open a PR per `CLAUDE.md` (this is one of the
three paths that has silently lost files before - no direct push to master).
