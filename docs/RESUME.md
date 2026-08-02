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

Sync-speed Frage 15 in flight (autopilot, 2026-08-02), first attempt already
failed and is documented (`docs/BACKLOG.md` Noticed, `docs/sync-speed-model.md`
"Erster Versuch"): collided with a second concurrently-running Routine on the
shared `login-profile`, hung 22 minutes, no result. Orphaned chrome.exe
processes reaped by hand. Verified no opal-downloader/go process running
before retrying. About to retry:
`OPAL_DEBOUNCE_OVERRIDE_TRACE=1 OPAL_DEBOUNCE_OVERRIDE_COURSE="Softwaretechnologie (SoSe 26)" OPAL_DEBOUNCE_OVERRIDE_SKIP_BASELINE=1 OPAL_DEBOUNCE_OVERRIDE_HISTORICAL_COUNT=198 go test ./internal/scraper/ -run TestDebounceOverrideCorrectness -v -timeout 20m`
against the real account (discovery only, no downloads) - use `> file 2>&1`
directly this time, not `| tee | tail`, since piping through tail swallowed
`go test`'s real exit code last time. Result lands in
`tmp/debounce-override-probe.txt`. If this also collides, stop retrying
in this cycle and leave Frage 15 open with both failures recorded rather than
burning more turns on the same collision. Next: write the result into
`docs/sync-speed-model.md` under Frage 15, commit, then either close it or
open Frage 16 (real multi-course concurrency contention).
