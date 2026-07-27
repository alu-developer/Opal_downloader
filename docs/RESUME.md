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

**Nothing in flight. Waiting on the maintainer for one decision.**

The sync-speed work this session is finished and written up in
`docs/sync-speed-campaign.md` and `docs/BACKLOG.md`:

1. The AJAX positive-signal lead is **refuted** — no AJAX fires for an ordinary
   section's initial render (2 courses, 263 and 8139 responses, every xhr a
   known `showAllLink`).
2. On the way, a better lead: **discovery downloads ~29 MB of the course's own
   files per pass and never reads them** (72 subframe `FolderResource`
   documents, 0 in the main frame). That one needs sign-off before being built,
   because aborting requests changes what the page renders.

Next session: if the maintainer has approved the preview-blocking experiment,
build it (abort non-main-frame `document` requests under `/opal/FolderResource/`)
and verify with a **byte-for-byte file-list diff** against the 345-file ground
truth, more than once. If not approved, the next item is the DOM-level
completion marker.
