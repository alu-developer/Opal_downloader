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

**Sync speed: measured, written up, not yet attacked.** The numbers are in
docs/sync-speed-campaign.md (top entry) and summarised in docs/BACKLOG.md.

Short version: 63% of in-section time is the 300ms mutation-observer debounce,
33% the stability poll, 2% the actual extraction. ~84s of a 216.6s run is a
fixed per-section toll paid for silence.

Next, and NOT started: test whether a positive completion signal exists -
"AJAX_CALL_DONE fired AND the response body carried the file-table markup" -
instead of inferring completion from absence of change. AJAX_CALL_DONE alone is
already known insufficient (lost 52 files, see internal/scraper/wicket.go); the
stronger condition is untried. Any attempt must be validated by a byte-for-byte
file-list diff against a serial ground-truth run, not by wall clock.
