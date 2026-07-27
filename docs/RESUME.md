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

**Repeating the preview-blocker A/B (2026-07-27, ~18:45).**

Committed and safe already: `62d0515` fixes discovery reporting "0 courses"
instead of an error when every source fails. That work is done and green.

In flight: the sync-speed measurement that was blocked on a login. The session
is fresh (TU-Fast completed by hand at 18:31), so the pair can finally be
repeated. Running:

    OPAL_FILELIST=repeat1_before  go test ./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m
    OPAL_FILELIST=repeat1_after OPAL_BLOCK_FILE_PREVIEWS=1  (same)

**Half done.** `repeat1_before` (previews kept): **345 files, 210.3s** — the
fastest run this account has recorded, and well inside the 212–245s band. The
`repeat1_after` side is running now; if it lands near 210s too, the earlier
324.3s was noise and the blocker is not actually slower.

The question being answered: the single existing pair measured 248.3s with
previews kept and 324.3s with them blocked (31% slower), which is outside the
212–245s band this account has measured before. One pair is not proof. If the
slowdown is real, the next guess to test is that an aborted subframe leaves the
parent churning over an error state — the very thing the 300ms settle-wait
debounce watches for — in which case `route.Fulfill` with an empty body may
behave differently from `route.Abort`.

Results go in `docs/sync-speed-campaign.md` and the backlog entry. If this is
picked up cold: check `tmp/` for whichever snapshots completed, and just rerun
the missing side.
