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

**2026-08-11, autopilot, phase 2 (sync speed), Question 40.** Implementation
landed (`4c88b91`): `scrapeCoursesHTTPFirst` now uses
`collectCourseFilesConcurrently` (the browser path's own worker pool) via a
new `newHTTPCourseFileCollector`, gated by `httpFirstCourseConcurrency()` -
defaults to 1 (serial, unchanged production behavior), only
`OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE` changes it. Also wired through
`downloadCandidates` (was hardcoded `nil`). Full local suite green, gofmt/vet
clean. Prediction already committed in `docs/sync-speed-model.md` Question 40:
`OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2` full byte-diff, expect empty/349
files, ~35-45s, failed on any missing file.

**Not yet run: the live byte-diff verification.** Preparing to run it, found
an active `go`/`opal-downloader`/`node` process cluster on this machine
(started ~14:00 CEST, confirmed via `Get-Process`) - almost certainly the
maintainer checking on the scheduled-sync-failure report forwarded mid-session
(already resolved - predates the 2026-08-10 netcheck fix - but plausibly
prompted a manual check). Did not start a live probe against the shared login
profile while that was active, per this project's own repeated collision
history (`docs/BACKLOG.md` Noticed section).

**Next step for whoever picks this up:** confirm quiet (`tasklist` no
`chrome.exe`/`chrome-headless-shell.exe`, no recent `go`/`opal-downloader`/
`node` processes, `git log -3` nothing from another session in the last few
minutes), then run:

    OPAL_FILELIST=before                                    go test ./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m -count=1
    OPAL_FILELIST=after OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE=2 go test ./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m -count=1
    diff tmp/filelist-before.txt tmp/filelist-after.txt

Both use the existing `TestFileListSnapshot` probe unmodified - it calls
`sc.ScrapeWithSavedSession`, which reads `OPAL_HTTP_COURSE_CONCURRENCY_OVERRIDE`
directly (no wiring needed). Write the result into
`docs/sync-speed-model.md` Question 40 against the committed prediction, then
clear this note.
