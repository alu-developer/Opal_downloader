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

Sync-speed Frage 15, third attempt (autopilot, 2026-08-02): first two attempts
failed on environment issues, not the debounce hypothesis (see
`docs/sync-speed-model.md` "Erster/Zweiter Versuch", `docs/BACKLOG.md`
Noticed). Just landed a diagnostic fix (commit a48eec1, pushed) so a repeat of
the second failure (300s course-list timeout) would at least report the
stuck page's URL. Verified no chrome/opal-downloader process running before
this retry. Command:
`OPAL_DEBOUNCE_OVERRIDE_TRACE=1 OPAL_DEBOUNCE_OVERRIDE_COURSE="Softwaretechnologie (SoSe 26)" OPAL_DEBOUNCE_OVERRIDE_SKIP_BASELINE=1 OPAL_DEBOUNCE_OVERRIDE_HISTORICAL_COUNT=198 go test ./internal/scraper/ -run TestDebounceOverrideCorrectness -v -timeout 20m > tmp/frage15_run3.log 2>&1`.
If this also fails on the same timeout, stop retrying this cycle - three
failures in one day without a single successful measurement is itself a
signal (about the login path's reliability, not about Frage 15), worth its
own backlog entry rather than a fourth blind attempt.
