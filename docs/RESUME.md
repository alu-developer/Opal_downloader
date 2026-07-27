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

**Sync-speed measurement: instrumented, live run in progress.**
`internal/scraper/sectiontiming.go` records per-section settle-wait vs
stability-poll vs everything-else, and logs one summary line at Close (audience
= diagnostic, so it lands in ~/.opal-downloader/logs/, not on screen).

To read the result: `grep "section timing" ~/.opal-downloader/logs/opal-downloader.log | tail -1`

What the numbers would mean:
- If **settle + poll is most of the total**, the runtime is this tool waiting on
  itself and the lead is real. Do NOT then just lower the constants - 1 stable
  read instead of 4 was live A/B tested and lost files byte-for-byte like the
  unfixed code. The question becomes whether a time-based poll is the right
  mechanism at all, given the files arrive in a Wicket response the browser
  already has.
- If **"everything else" dominates**, the wait constants are a red herring and
  the cost is navigation/render, i.e. genuinely OPAL. That is a negative result
  and worth writing into docs/sync-speed-campaign.md as the first real
  measurement of where the second goes.
