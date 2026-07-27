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

**Testing route.Fulfill vs route.Abort (2026-07-27, ~19:05).**

The A/B repeat is DONE and committed (`3316eb3`). Result: the slowdown is real,
confirmed across two pairs — 248.3→324.3s (+30.6%) and 210.3→265.0s (+26.0%),
both with byte-identical 345-file lists. Blocking previews is safe and costs
~26–31%.

**In flight now:** `internal/scraper/previews.go` is patched *uncommitted* —
`route.Abort("blockedbyclient")` replaced with `route.Fulfill` serving an empty
200 `text/html`. This tests the one recorded guess for the slowdown: that the
abort's error state generates the DOM churn the 300ms settle-wait debounce is
watching for. The comment in that file argued the opposite, reasoned and never
measured.

Running: `OPAL_FILELIST=repeat1_fulfill OPAL_BLOCK_FILE_PREVIEWS=1 go test
./internal/scraper/ -run TestFileListSnapshot -v -timeout 30m`

Compare against **265.0s** (Abort, same session, same evening). Beat it clearly
→ keep Fulfill and re-argue the comment. Not faster → `git checkout
internal/scraper/previews.go` to restore Abort, and record that the guess is
dead so nobody tries it a third time. Either way the file list must diff empty
against `tmp/filelist-repeat1_before.txt`.

---

**Earlier this turn (all committed, nothing pending):**

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
