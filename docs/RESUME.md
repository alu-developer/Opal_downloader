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

**2026-07-26 maintainer feedback batch: nine of ten done**, all committed. See
the top of `docs/BACKLOG.md` for the list and "Done recently" for what each
one turned out to involve.

Left: **code size**, which is a standing rule rather than a task - keep
`crawl.go` (1248 lines), `gui.go` and `settings.go` (~1000 each) from growing
while touching them.

One measurement still outstanding: a live `list` run is checking how often the
new rate ceiling actually held a navigation back. The claim in
`docs/server-load.md` is that it does not bind on today's crawl, and the last
timed run came in at 244.6s against 223.4s and 211.9s earlier - top of the
observed spread, so it is being measured rather than assumed. If it turns out
to bind, raise `polite.DefaultMinInterval` and say so in that doc.
