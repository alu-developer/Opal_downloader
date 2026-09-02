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

**Phase 3 sync-speed cycle in flight (2026-09-02 autopilot).** Question 43
direction (a): extend `internal/scraper/bulkzip_probe_test.go` (env
`OPAL_BULKZIP_PROBE=1`, new `OPAL_BULKZIP_NETTRACE=1` mode) to attach a
`page.On("request")` listener and a 500ms selection-column-presence poll
after navigation, log both on one timeline, and see whether a periodic
Wicket XHR correlates with the row-selection `<td>`/`<th>` column
appearing/disappearing. Prediction + kill criterion already committed to
`docs/sync-speed-model.md` "Next experiment". If killed mid-run: the probe
change is committed; re-run
`OPAL_BULKZIP_PROBE=1 OPAL_BULKZIP_NETTRACE=1 go test ./internal/scraper/ -run TestBulkZipProbe -count=1 -v -timeout 15m`
and read the timeline, then diagnose per the kill criterion.

Phase 1 (Walk 18 config bloat) and Phase 2 (Walk 19 CLI course-selector
friction) already committed + pushed this run.

**2026-09-02 08:58: a real scheduled sync (`main.exe sync --scheduled`, pid
31220) started and holds `~/.opal-downloader/sync.lock`.** The probe can't
run until it releases (one-crawl-at-a-time). Waiting on it in a background
poll; run the probe command above the moment the lock is gone. If this run
is killed before that: re-check the lock, then run the probe; the code and
prediction are already committed and pushed (dc11c63).
