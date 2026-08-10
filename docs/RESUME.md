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

**In flight (2026-08-10, autopilot): Question 36 Step B2 production restructure.**
`scrapeCoursesHTTPFirst` (`internal/scraper/orchestrator.go`) and
`discoverSectionsHTTP` (`internal/scraper/httpdiscovery_seed.go`) are written,
unit-tested offline, and wired behind `OPAL_HTTP_DISCOVERY=2` in
`ScrapeWithSavedSession` (`scraper.go`). Build/vet/full test suite pass.
Not yet done: the live byte-diff against the ground truth
(`filelist_probe_test.go`, `OPAL_FILELIST=after OPAL_HTTP_DISCOVERY=2` vs a
fresh `OPAL_FILELIST=before` baseline) that `docs/BACKLOG.md`'s Now item
requires before this can ship, and the PR (per `CLAUDE.md` - this is one of
the three paths that has silently lost files before, so no direct push to
master). If this file is still here and stale, re-run the live diff before
trusting any partial result already on disk.
