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

The SessionStart hook reads this file and hands it to the next session. The
scheduled resume runner also treats a non-placeholder file here as "there is
work", so leaving stale content in it will wake an unattended run for nothing.

---

**2026-07-30: applying today's User-Agent finding to the measurements that
rejected HTTP-first discovery.** New evidence, not a re-litigation.

Today's bug: an HTTP client sending `User-Agent: "...opal-downloader"` (no
AppleWebKit/Chrome/Safari tokens) alongside the browser made OPAL return
stub pages for **5 of 6 courses** - "silently emptied whole courses". Fixed and
verified live (`cd1282c`), 345 files, diff empty.

That is the *same signature* the 2026-07-21 rejection of HTTP-first discovery
rests on: *"REJECTED - unsafe. Fast (22s) but silently emptied whole courses."*
And its parallelism table lost files at every level (1 -> 257 files, 2 -> 176,
3 -> 219) against a 345-file ground truth. Nobody knew about the UA effect
then.

Found so far: `htmlstability_probe_test.go:198` still sends
`"Mozilla/5.0 (Windows NT 10.0; Win64; x64) opal-downloader-probe"` - the same
non-browser fingerprint. That probe is what the 2026-07-27 reopening's
reproducibility numbers came from, so its measurements are suspect too.

Next: read what UA `httpdiscovery_probe_test.go` sends, then decide whether a
re-measurement with a browser-shaped UA is worth one live pass. Do NOT claim
the rejection is wrong - the honest position is that its measurement was taken
under a condition now known to corrupt results, which is a reason to re-measure
once, not a reason to believe the opposite.

Nothing committed yet this iteration.
