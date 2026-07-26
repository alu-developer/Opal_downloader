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

**Working the 2026-07-26 maintainer feedback batch** (ten items, listed at the
top of `docs/BACKLOG.md` in the order they are being worked).

Done so far: mojibake + guard; unsaved-settings warning; the GUI sync log
rewritten for a user; `internal/logging` (two audiences, console + scrubbed
rotating file) with the scraper migrated onto it; automatic sync moved to its
own `/schedule` page; the course picker rebuilt as one list.

Next, in order: TU-Fast indicator-based wait → server-load policy (the
maintainer asked for this to be set up "langfristig", not spot-checked) → hang
watchdog. Code size is a standing rule, not a step.

Useful finding while working: the GUI never showed "skipping section" at all -
the scraper's `fmt.Printf` goes to a stdout nobody sees when the GUI runs as a
window. What the maintainer was actually reading was one `skipped:` row per
file, ~345 per run. Both readings point the same way, so both get fixed, but
the CLI half is the one still outstanding.

Each lands as its own commit. Cross items off the backlog list as they go.
